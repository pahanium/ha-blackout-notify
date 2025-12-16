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
	config       *config.Config
	wsClient     *homeassistant.WSClient
	haClient     *homeassistant.Client
	notifSvc     *notifications.Service
	lastState    PowerState
	mu           sync.Mutex
	debounceTime time.Duration
	lastChange   time.Time
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

	// Register handler for state changes
	w.wsClient.OnStateChange(w.config.WatchedEntityID, func(entityID string, oldState, newState *homeassistant.Entity) {
		w.handleStateChange(ctx, oldState, newState)
	})

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
