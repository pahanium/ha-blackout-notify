package watcher

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pahanium/ha-blackout-notify/internal/config"
	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
	"github.com/pahanium/ha-blackout-notify/internal/notifications"
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
	NotifyScheduleChanged(ctx context.Context, scheduleType string, oldTime, newTime *time.Time) error
	NotifyYasnoScheduleTomorrow(ctx context.Context, events []homeassistant.CalendarEvent, groupName string) error
	NotifyYasnoEmergencyShutdown(ctx context.Context) error
	GetScheduledTime(ctx context.Context, sensorID string) (*time.Time, error)
	GetYasnoCalendarEvents(ctx context.Context, calendarID string, date time.Time) ([]homeassistant.CalendarEvent, error)
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

	mu              sync.Mutex
	lastState       PowerState
	lastNextOnTime  *time.Time
	lastNextOffTime *time.Time

	// Power state debounce: a new state is applied only after it stays unchanged for debounceTime
	debounceTime  time.Duration
	pendingState  PowerState
	pendingSince  time.Time
	debounceTimer *time.Timer

	// Schedule sensor debounce, tracked per schedule type ("on"/"off")
	lastScheduleChange map[string]time.Time

	// Yasno schedule monitoring, debounced per sensor
	lastYasnoTodayState     string
	lastYasnoTomorrowState  string
	yasnoDebounceTime       time.Duration
	lastYasnoTodayChange    time.Time
	lastYasnoTomorrowChange time.Time
}

// NewWatcher creates a new state watcher
func NewWatcher(
	cfg *config.Config,
	wsClient EventSource,
	haClient StateGetter,
	notifSvc Notifier,
	statsRecorder StateRecorder,
) *Watcher {
	return &Watcher{
		config:             cfg,
		wsClient:           wsClient,
		haClient:           haClient,
		notifSvc:           notifSvc,
		statsRecorder:      statsRecorder,
		lastState:          PowerStateUnknown,
		debounceTime:       5 * time.Second, // State must be stable this long before it is applied
		lastScheduleChange: make(map[string]time.Time),
		yasnoDebounceTime:  30 * time.Second, // Longer debounce for Yasno to avoid spam
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

	// Get initial schedule times
	w.fetchInitialScheduleTimes(ctx)

	// Register handler for power state changes
	w.wsClient.OnStateChange(w.config.WatchedEntityID, func(entityID string, oldState, newState *homeassistant.Entity) {
		w.handleStateChange(ctx, oldState, newState)
	})

	// Register handlers for schedule changes
	if w.config.NextOnSensorID != "" {
		w.wsClient.OnStateChange(w.config.NextOnSensorID, func(entityID string, oldState, newState *homeassistant.Entity) {
			w.handleScheduleChange(ctx, "on", oldState, newState)
		})
	}
	if w.config.NextOffSensorID != "" {
		w.wsClient.OnStateChange(w.config.NextOffSensorID, func(entityID string, oldState, newState *homeassistant.Entity) {
			w.handleScheduleChange(ctx, "off", oldState, newState)
		})
	}

	// Register handlers for Yasno sensors
	w.registerYasnoHandlers(ctx)

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
	w.mu.Unlock()

	if w.wsClient != nil {
		w.wsClient.Stop()
	}
}

// fetchInitialScheduleTimes gets current schedule times
func (w *Watcher) fetchInitialScheduleTimes(ctx context.Context) {
	if w.config.NextOnSensorID != "" {
		nextOn, err := w.notifSvc.GetScheduledTime(ctx, w.config.NextOnSensorID)
		if err != nil {
			slog.Warn("Failed to get initial next on time", "error", err)
		} else {
			w.mu.Lock()
			w.lastNextOnTime = nextOn
			w.mu.Unlock()
			if nextOn != nil {
				slog.Info("Initial next power on time", "time", nextOn.Format("15:04"))
			}
		}
	}

	if w.config.NextOffSensorID != "" {
		nextOff, err := w.notifSvc.GetScheduledTime(ctx, w.config.NextOffSensorID)
		if err != nil {
			slog.Warn("Failed to get initial next off time", "error", err)
		} else {
			w.mu.Lock()
			w.lastNextOffTime = nextOff
			w.mu.Unlock()
			if nextOff != nil {
				slog.Info("Initial next power off time", "time", nextOff.Format("15:04"))
			}
		}
	}
}

// handleScheduleChange processes schedule sensor changes
func (w *Watcher) handleScheduleChange(ctx context.Context, scheduleType string, oldState, newState *homeassistant.Entity) {
	if newState == nil {
		return
	}

	// Get current power state to decide if we should notify
	w.mu.Lock()
	currentPowerState := w.lastState
	timeSinceLastScheduleChange := time.Since(w.lastScheduleChange[scheduleType])
	w.mu.Unlock()

	// Debounce schedule changes per type (avoid spam on reconnect)
	if timeSinceLastScheduleChange < w.debounceTime {
		slog.Debug("Debouncing schedule change", "type", scheduleType)
		return
	}

	// Parse new time
	var sensorID string
	if scheduleType == "on" {
		sensorID = w.config.NextOnSensorID
	} else {
		sensorID = w.config.NextOffSensorID
	}

	newTime, err := w.notifSvc.GetScheduledTime(ctx, sensorID)
	if err != nil {
		slog.Warn("Failed to parse schedule time", "error", err)
		return
	}

	// Get previous time
	w.mu.Lock()
	var oldTime *time.Time
	if scheduleType == "on" {
		oldTime = w.lastNextOnTime
	} else {
		oldTime = w.lastNextOffTime
	}
	w.mu.Unlock()

	// Check if time actually changed
	if timesEqual(oldTime, newTime) {
		return
	}

	// Ignore schedule cancellations (when newTime becomes nil)
	// This prevents losing the original scheduled time which is needed for suppression logic
	if newTime == nil {
		slog.Debug("Schedule cancelled, keeping previous time for suppression logic", "type", scheduleType, "previous", formatTimePtr(oldTime))
		return
	}

	slog.Info("Schedule changed", "type", scheduleType, "from", formatTimePtr(oldTime), "to", formatTimePtr(newTime))

	// Update stored time
	w.mu.Lock()
	if scheduleType == "on" {
		w.lastNextOnTime = newTime
	} else {
		w.lastNextOffTime = newTime
	}
	w.lastScheduleChange[scheduleType] = time.Now()
	w.mu.Unlock()

	// Only notify about relevant schedule changes based on current power state
	// When power is OFF - notify about next ON time changes
	// When power is ON - notify about next OFF time changes
	shouldNotify := false
	if scheduleType == "on" && currentPowerState == PowerStateOff {
		shouldNotify = true
	} else if scheduleType == "off" && currentPowerState == PowerStateOn {
		shouldNotify = true
	}

	// Suppress notification if old scheduled time has already passed or passed recently (within 60 min)
	// This happens when power state change is delayed but schedule already updated
	if shouldNotify && oldTime != nil {
		timeSinceOldSchedule := time.Since(*oldTime)
		if timeSinceOldSchedule > -time.Minute && timeSinceOldSchedule < 60*time.Minute {
			slog.Debug("Suppressing schedule notification: old time recently passed", "old_time", formatTimePtr(oldTime), "minutes_ago", int(timeSinceOldSchedule.Minutes()))
			shouldNotify = false
		}
	}

	// Suppress notification if new scheduled time is for a different day (not today)
	// This prevents spam when evening schedule updates reflect tomorrow's schedule
	if shouldNotify && newTime != nil {
		loc := w.config.Location
		now := time.Now()
		if !sameDay(*newTime, now, loc) {
			slog.Debug("Suppressing schedule notification: new time is not today", "new_time", formatTimePtr(newTime), "new_date", newTime.In(loc).Format("2006-01-02"), "today", now.In(loc).Format("2006-01-02"))
			shouldNotify = false
		}
	}

	if shouldNotify {
		if err := w.notifSvc.NotifyScheduleChanged(ctx, scheduleType, oldTime, newTime); err != nil {
			slog.Error("Failed to send schedule change notification", "error", err)
		}
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

// registerYasnoHandlers registers WebSocket handlers for Yasno sensors
func (w *Watcher) registerYasnoHandlers(ctx context.Context) {
	if !w.config.IsYasnoMonitoringEnabled() {
		slog.Debug("Yasno monitoring not configured, skipping")
		return
	}

	slog.Info("Registering Yasno schedule monitors", "today", w.config.YasnoTodaySensorID, "tomorrow", w.config.YasnoTomorrowSensorID, "calendar", w.config.YasnoCalendarID)

	// Monitor today status sensor for emergency shutdowns
	if w.config.YasnoTodaySensorID != "" {
		w.wsClient.OnStateChange(w.config.YasnoTodaySensorID, func(entityID string, oldState, newState *homeassistant.Entity) {
			w.handleYasnoTodayChange(ctx, oldState, newState)
		})
	}

	// Monitor tomorrow status sensor for schedule announcements
	if w.config.YasnoTomorrowSensorID != "" {
		w.wsClient.OnStateChange(w.config.YasnoTomorrowSensorID, func(entityID string, oldState, newState *homeassistant.Entity) {
			w.handleYasnoTomorrowChange(ctx, oldState, newState)
		})
	}
}

// handleYasnoTodayChange processes changes to today's Yasno status
func (w *Watcher) handleYasnoTodayChange(ctx context.Context, oldState, newState *homeassistant.Entity) {
	if newState == nil {
		return
	}

	// Get schedule state from sensor state (not attributes)
	scheduleState := newState.State
	if scheduleState == "" {
		slog.Warn("Yasno today sensor has empty state")
		return
	}

	w.mu.Lock()
	previousState := w.lastYasnoTodayState
	timeSinceLastNotif := time.Since(w.lastYasnoTodayChange)
	w.mu.Unlock()

	// Skip if state hasn't changed
	if scheduleState == previousState {
		return
	}

	// Debounce to avoid spam
	if timeSinceLastNotif < w.yasnoDebounceTime {
		slog.Debug("Debouncing Yasno today change", "from", previousState, "to", scheduleState)
		return
	}

	slog.Info("Yasno today status changed", "from", previousState, "to", scheduleState)

	// Update stored state
	w.mu.Lock()
	w.lastYasnoTodayState = scheduleState
	w.lastYasnoTodayChange = time.Now()
	w.mu.Unlock()

	// Handle different state transitions
	switch scheduleState {
	case "emergency_shutdowns":
		slog.Info("Yasno: emergency shutdowns detected")
		if err := w.notifSvc.NotifyYasnoEmergencyShutdown(ctx); err != nil {
			slog.Error("Failed to send Yasno emergency notification", "error", err)
		}

	case "schedule_applies":
		// Schedule restored after emergency
		if previousState == "emergency_shutdowns" {
			slog.Info("Yasno: schedule restored after emergency shutdowns")
			// Currently just logging, can enable notification if needed
			// if err := w.notifSvc.NotifyYasnoScheduleRestored(ctx); err != nil {
			// 	slog.Error("Failed to send Yasno schedule restored notification", "error", err)
			// }
		} else {
			slog.Info("Yasno: schedule applies today")
		}

	case "waiting_for_schedule":
		slog.Info("Yasno: waiting for schedule")

	default:
		slog.Warn("Yasno: unknown today status", "state", scheduleState)
	}
}

// handleYasnoTomorrowChange processes changes to tomorrow's Yasno status
func (w *Watcher) handleYasnoTomorrowChange(ctx context.Context, oldState, newState *homeassistant.Entity) {
	if newState == nil {
		return
	}

	// Get schedule state from sensor state (not attributes)
	scheduleState := newState.State
	if scheduleState == "" {
		slog.Warn("Yasno tomorrow sensor has empty state")
		return
	}

	w.mu.Lock()
	previousState := w.lastYasnoTomorrowState
	timeSinceLastNotif := time.Since(w.lastYasnoTomorrowChange)
	w.mu.Unlock()

	// Skip if state hasn't changed
	if scheduleState == previousState {
		return
	}

	// Debounce to avoid spam
	if timeSinceLastNotif < w.yasnoDebounceTime {
		slog.Debug("Debouncing Yasno tomorrow change", "from", previousState, "to", scheduleState)
		return
	}

	slog.Info("Yasno tomorrow status changed", "from", previousState, "to", scheduleState)

	// Update stored state
	w.mu.Lock()
	w.lastYasnoTomorrowState = scheduleState
	w.lastYasnoTomorrowChange = time.Now()
	w.mu.Unlock()

	// Handle different state transitions
	switch scheduleState {
	case "schedule_applies":
		slog.Info("Yasno: tomorrow schedule available")

		// Get calendar events for tomorrow in the configured timezone
		tomorrow := time.Now().In(w.config.Location).AddDate(0, 0, 1)
		events, err := w.notifSvc.GetYasnoCalendarEvents(ctx, w.config.YasnoCalendarID, tomorrow)
		if err != nil {
			slog.Error("Failed to get Yasno calendar events", "error", err)
			return
		}

		slog.Debug("Retrieved calendar events for tomorrow", "count", len(events))

		// Extract group name from calendar entity ID
		// Example: calendar.yasno_kiiv_2_1_planned_outages -> "група 2.1"
		groupName := notifications.ExtractYasnoGroupFromCalendarID(w.config.YasnoCalendarID)
		slog.Debug("Yasno group extracted from calendar ID", "calendar", w.config.YasnoCalendarID, "group", groupName)

		// Send notification with schedule
		if err := w.notifSvc.NotifyYasnoScheduleTomorrow(ctx, events, groupName); err != nil {
			slog.Error("Failed to send Yasno tomorrow schedule notification", "error", err)
		}

	case "waiting_for_schedule":
		slog.Info("Yasno: waiting for tomorrow schedule")

	case "emergency_shutdowns":
		slog.Info("Yasno: emergency shutdowns expected tomorrow")

	default:
		slog.Warn("Yasno: unknown tomorrow status", "state", scheduleState)
	}
}
