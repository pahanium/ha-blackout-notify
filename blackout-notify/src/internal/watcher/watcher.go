package watcher

import (
	"context"
	"sync"
	"time"

	"github.com/yourusername/haaddon/telegram-bot/internal/config"
	"github.com/yourusername/haaddon/telegram-bot/internal/homeassistant"
	"github.com/yourusername/haaddon/telegram-bot/internal/logger"
	"github.com/yourusername/haaddon/telegram-bot/internal/notifications"
)

// PowerState represents power status
type PowerState string

const (
	PowerStateOn      PowerState = "on"
	PowerStateOff     PowerState = "off"
	PowerStateUnknown PowerState = "unknown"
)

// Watcher monitors entity state changes and triggers notifications
type Watcher struct {
	config             *config.Config
	wsClient           *homeassistant.WSClient
	haClient           *homeassistant.Client
	notifSvc           *notifications.Service
	lastState          PowerState
	lastNextOnTime     *time.Time
	lastNextOffTime    *time.Time
	mu                 sync.Mutex
	debounceTime       time.Duration
	lastChange         time.Time
	lastScheduleChange time.Time
}

// NewWatcher creates a new state watcher
func NewWatcher(
	cfg *config.Config,
	wsClient *homeassistant.WSClient,
	haClient *homeassistant.Client,
	notifSvc *notifications.Service,
) *Watcher {
	return &Watcher{
		config:       cfg,
		wsClient:     wsClient,
		haClient:     haClient,
		notifSvc:     notifSvc,
		lastState:    PowerStateUnknown,
		debounceTime: 5 * time.Second, // Debounce to avoid rapid state changes
	}
}

// Start initializes the watcher and begins monitoring
func (w *Watcher) Start(ctx context.Context) error {
	if w.config.WatchedEntityID == "" {
		logger.Info("No watched entity configured, power monitoring disabled")
		return nil
	}

	logger.Info("Starting power watcher for entity: %s", w.config.WatchedEntityID)

	// Get initial state
	if err := w.fetchInitialState(ctx); err != nil {
		logger.Warn("Failed to get initial state: %v", err)
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

	logger.Info("Initial power state: %s", w.lastState)
	return nil
}

// handleStateChange processes state change events
func (w *Watcher) handleStateChange(ctx context.Context, oldState, newState *homeassistant.Entity) {
	if newState == nil {
		return
	}

	newPowerState := normalizeState(newState.State)

	w.mu.Lock()
	previousState := w.lastState
	timeSinceLastChange := time.Since(w.lastChange)
	w.mu.Unlock()

	// Skip if state hasn't actually changed
	if newPowerState == previousState {
		return
	}

	// Debounce rapid changes
	if timeSinceLastChange < w.debounceTime {
		logger.Debug("Debouncing state change, ignoring %s -> %s", previousState, newPowerState)
		return
	}

	logger.Info("Power state changed: %s -> %s", previousState, newPowerState)

	// Update state
	w.mu.Lock()
	w.lastState = newPowerState
	w.lastChange = time.Now()
	w.mu.Unlock()

	// Skip notification if transitioning from unknown state
	if previousState == PowerStateUnknown {
		logger.Info("State transition from unknown to %s, skipping notification (initial state detection)", newPowerState)
		return
	}

	// Send notification based on new state
	switch newPowerState {
	case PowerStateOn:
		if err := w.notifSvc.NotifyPowerOn(ctx); err != nil {
			logger.Error("Failed to send power on notification: %v", err)
		}
	case PowerStateOff:
		if err := w.notifSvc.NotifyPowerOff(ctx); err != nil {
			logger.Error("Failed to send power off notification: %v", err)
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
	if w.wsClient != nil {
		w.wsClient.Stop()
	}
}

// fetchInitialScheduleTimes gets current schedule times
func (w *Watcher) fetchInitialScheduleTimes(ctx context.Context) {
	if w.config.NextOnSensorID != "" {
		nextOn, err := w.notifSvc.GetScheduledTime(ctx, w.config.NextOnSensorID)
		if err != nil {
			logger.Warn("Failed to get initial next on time: %v", err)
		} else {
			w.mu.Lock()
			w.lastNextOnTime = nextOn
			w.mu.Unlock()
			if nextOn != nil {
				logger.Info("Initial next power on time: %s", nextOn.Format("15:04"))
			}
		}
	}

	if w.config.NextOffSensorID != "" {
		nextOff, err := w.notifSvc.GetScheduledTime(ctx, w.config.NextOffSensorID)
		if err != nil {
			logger.Warn("Failed to get initial next off time: %v", err)
		} else {
			w.mu.Lock()
			w.lastNextOffTime = nextOff
			w.mu.Unlock()
			if nextOff != nil {
				logger.Info("Initial next power off time: %s", nextOff.Format("15:04"))
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
	timeSinceLastScheduleChange := time.Since(w.lastScheduleChange)
	w.mu.Unlock()

	// Debounce schedule changes (avoid spam on reconnect)
	if timeSinceLastScheduleChange < w.debounceTime {
		logger.Debug("Debouncing schedule change")
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
		logger.Warn("Failed to parse schedule time: %v", err)
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

	logger.Info("Schedule changed (%s): %v -> %v", scheduleType, formatTimePtr(oldTime), formatTimePtr(newTime))

	// Update stored time
	w.mu.Lock()
	if scheduleType == "on" {
		w.lastNextOnTime = newTime
	} else {
		w.lastNextOffTime = newTime
	}
	w.lastScheduleChange = time.Now()
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
			logger.Debug("Suppressing schedule notification: old time %s already passed (%.0f min ago)",
				formatTimePtr(oldTime), timeSinceOldSchedule.Minutes())
			shouldNotify = false
		}
	}

	if shouldNotify {
		if err := w.notifSvc.NotifyScheduleChanged(ctx, scheduleType, oldTime, newTime); err != nil {
			logger.Error("Failed to send schedule change notification: %v", err)
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
