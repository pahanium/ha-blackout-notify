package watcher

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pahanium/ha-blackout-notify/internal/config"
	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
)

// PowerState represents power status
type PowerState string

const (
	PowerStateOn      PowerState = "on"
	PowerStateOff     PowerState = "off"
	PowerStateUnknown PowerState = "unknown"
)

// EventSource delivers Home Assistant state change events (implemented by homeassistant.WSClient)
type EventSource interface {
	OnStateChange(entityID string, handler homeassistant.StateChangeHandler)
	RunWithReconnect(ctx context.Context) error
	Stop()
}

// StateGetter reads current entity state (implemented by homeassistant.Client)
type StateGetter interface {
	GetState(ctx context.Context, entityID string) (*homeassistant.Entity, error)
}

// Notifier sends notifications and reads schedule data (implemented by notifications.Service)
type Notifier interface {
	NotifyPowerOn(ctx context.Context, previousOffDurationSec int64) error
	NotifyPowerOff(ctx context.Context, previousOnDurationSec int64) error
	NotifyScheduleChanged(ctx context.Context, scheduleType string, newTime *time.Time, todayEvents []homeassistant.CalendarEvent) error
	NotifyEmergencyEnded(ctx context.Context, scheduleType string, nextTime *time.Time, todayEvents []homeassistant.CalendarEvent) error
	NotifySchedule(ctx context.Context, events []homeassistant.CalendarEvent, date time.Time) error
	NotifyYasnoEmergencyShutdown(ctx context.Context) error
	GetScheduledTime(ctx context.Context, sensorID string) (*time.Time, error)
	GetYasnoCalendarEvents(ctx context.Context, calendarID string, date time.Time) ([]homeassistant.CalendarEvent, error)
}

// ScheduleLog remembers which schedule was sent for each date (implemented by stats.ScheduleLog)
type ScheduleLog interface {
	LastSent(ctx context.Context, date string) (string, error)
	MarkSent(ctx context.Context, date, fingerprint string) error
	IsEmpty(ctx context.Context) (bool, error)
}

// StateRecorder persists power state changes (implemented by stats.Recorder)
type StateRecorder interface {
	RecordStateChange(ctx context.Context, state string, changedAt time.Time) (previousState string, previousDuration int64, err error)
}

// Watcher monitors entity state changes and triggers notifications
type Watcher struct {
	config        *config.Config
	wsClient      EventSource
	haClient      StateGetter
	notifSvc      Notifier
	statsRecorder StateRecorder
	scheduleLog   ScheduleLog

	mu              sync.Mutex
	lastState       PowerState
	lastNextOnTime  *time.Time
	lastNextOffTime *time.Time

	// Power state debounce: a new state is applied only after it stays unchanged for debounceTime
	debounceTime  time.Duration
	pendingState  PowerState
	pendingSince  time.Time
	debounceTimer *time.Timer

	// Schedule reconciliation: schedule-related events are collected for reconcileDelay,
	// then reconcileSchedule decides which single message (if any) to send
	reconcileMu       sync.Mutex // serializes reconcileSchedule runs
	reconcileDelay    time.Duration
	reconcileInterval time.Duration
	reconcileTimer    *time.Timer
	todayStatus       string // Yasno today status as of the last reconciliation

	// Emergency shutdown start is announced immediately, not via reconciliation
	emergencyActive        bool
	lastEmergencyNotice    time.Time
	emergencyNoticeBackoff time.Duration
}

// NewWatcher creates a new state watcher
func NewWatcher(
	cfg *config.Config,
	wsClient EventSource,
	haClient StateGetter,
	notifSvc Notifier,
	statsRecorder StateRecorder,
	scheduleLog ScheduleLog,
) *Watcher {
	return &Watcher{
		config:                 cfg,
		wsClient:               wsClient,
		haClient:               haClient,
		notifSvc:               notifSvc,
		statsRecorder:          statsRecorder,
		scheduleLog:            scheduleLog,
		lastState:              PowerStateUnknown,
		debounceTime:           5 * time.Second,  // State must be stable this long before it is applied
		reconcileDelay:         10 * time.Second, // Sensors of one schedule update arrive together
		reconcileInterval:      10 * time.Minute, // Catch schedule changes that don't move next on/off
		emergencyNoticeBackoff: 30 * time.Minute, // Don't repeat the emergency alert if the status flaps
	}
}

// Start initializes the watcher and begins monitoring
func (w *Watcher) Start(ctx context.Context) error {
	if w.config.WatchedEntityID == "" {
		slog.Info("No watched entity configured, power monitoring disabled")
		return nil
	}

	slog.Info("Starting power watcher", "entity", w.config.WatchedEntityID)

	// Get initial state
	if err := w.fetchInitialState(ctx); err != nil {
		slog.Warn("Failed to get initial state", "error", err)
	}

	// Remember current schedule so startup doesn't announce it again
	w.initSchedule(ctx)

	// Register handler for power state changes
	w.wsClient.OnStateChange(w.config.WatchedEntityID, func(entityID string, oldState, newState *homeassistant.Entity) {
		w.handleStateChange(ctx, oldState, newState)
	})

	// Register handlers for schedule sensors
	w.registerScheduleHandlers(ctx)

	// Start WebSocket client with reconnect
	return w.wsClient.RunWithReconnect(ctx)
}

// fetchInitialState gets the current state of the watched entity
func (w *Watcher) fetchInitialState(ctx context.Context) error {
	entity, err := w.haClient.GetState(ctx, w.config.WatchedEntityID)
	if err != nil {
		return err
	}

	w.mu.Lock()
	w.lastState = normalizeState(entity.State)
	w.mu.Unlock()

	slog.Info("Initial power state", "state", w.lastState)
	return nil
}

// handleStateChange debounces power state events: the latest known state is applied
// only after it stays unchanged for debounceTime, so short flickers are ignored
// and the final state of a burst is never lost
func (w *Watcher) handleStateChange(ctx context.Context, oldState, newState *homeassistant.Entity) {
	if newState == nil {
		return
	}

	newPowerState := normalizeState(newState.State)

	// unknown/unavailable comes from HA restarts and WebSocket reconnects, not from the grid.
	// Ignoring it keeps the last real state, so off -> unknown -> off doesn't reset durations
	// and on -> unknown -> off is still reported as an outage.
	if newPowerState == PowerStateUnknown {
		slog.Debug("Ignoring non-power state of watched entity", "state", newState.State)
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if newPowerState == w.pendingState && w.debounceTimer != nil {
		return
	}

	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
		w.debounceTimer = nil
	}

	if newPowerState == w.lastState {
		// Flicker returned to the applied state before debounce expired
		w.pendingState = ""
		return
	}

	w.pendingState = newPowerState
	w.pendingSince = time.Now()
	slog.Debug("Power state pending", "state", newPowerState, "debounce", w.debounceTime)

	state, since := w.pendingState, w.pendingSince
	w.debounceTimer = time.AfterFunc(w.debounceTime, func() {
		w.mu.Lock()
		if w.pendingState != state || !w.pendingSince.Equal(since) {
			w.mu.Unlock()
			return
		}
		w.pendingState = ""
		w.debounceTimer = nil
		w.mu.Unlock()

		if ctx.Err() != nil {
			return
		}
		w.applyStateChange(ctx, state, since)
	})
}

// applyStateChange records a stable power state change and sends a notification
func (w *Watcher) applyStateChange(ctx context.Context, newPowerState PowerState, changedAt time.Time) {
	w.mu.Lock()
	previousState := w.lastState
	if newPowerState == previousState {
		w.mu.Unlock()
		return
	}
	w.lastState = newPowerState
	w.mu.Unlock()

	// First real state after startup: just remember it
	if previousState == PowerStateUnknown {
		slog.Info("Initial power state detected, skipping recording and notification", "state", newPowerState)
		return
	}

	slog.Info("Power state changed", "from", previousState, "to", newPowerState)

	// Record state change in statistics and get previous state duration
	var previousStateDuration int64 = 0
	if w.statsRecorder != nil {
		prevState, duration, err := w.statsRecorder.RecordStateChange(ctx, string(newPowerState), changedAt)
		if err != nil {
			slog.Error("Failed to record state change in statistics", "error", err)
		} else if prevState == string(previousState) && duration > 0 {
			previousStateDuration = duration
			slog.Debug("Previous state duration", "state", prevState, "duration_sec", duration)
		}
	}

	switch newPowerState {
	case PowerStateOn:
		if err := w.notifSvc.NotifyPowerOn(ctx, previousStateDuration); err != nil {
			slog.Error("Failed to send power on notification", "error", err)
		}
	case PowerStateOff:
		if err := w.notifSvc.NotifyPowerOff(ctx, previousStateDuration); err != nil {
			slog.Error("Failed to send power off notification", "error", err)
		}
	}
}

// GetCurrentState returns the current known power state
func (w *Watcher) GetCurrentState() PowerState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastState
}

// Stop stops the watcher
func (w *Watcher) Stop() {
	w.mu.Lock()
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
		w.debounceTimer = nil
	}
	if w.reconcileTimer != nil {
		w.reconcileTimer.Stop()
		w.reconcileTimer = nil
	}
	w.mu.Unlock()

	if w.wsClient != nil {
		w.wsClient.Stop()
	}
}

// timesEqual compares two time pointers
func timesEqual(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Compare with 1 minute tolerance to avoid noise
	return a.Truncate(time.Minute).Equal(b.Truncate(time.Minute))
}

// sameDay reports whether a and b fall on the same calendar day in loc
func sameDay(a, b time.Time, loc *time.Location) bool {
	ay, am, ad := a.In(loc).Date()
	by, bm, bd := b.In(loc).Date()
	return ay == by && am == bm && ad == bd
}

// formatTimePtr formats time pointer for logging
func formatTimePtr(t *time.Time) string {
	if t == nil {
		return "nil"
	}
	return t.Format("15:04")
}

// normalizeState converts entity state string to PowerState
func normalizeState(state string) PowerState {
	switch state {
	case "on", "ON", "true", "1", "home", "alive", "connected":
		return PowerStateOn
	case "off", "OFF", "false", "0", "not_home", "dead", "disconnected":
		return PowerStateOff
	default:
		return PowerStateUnknown
	}
}
