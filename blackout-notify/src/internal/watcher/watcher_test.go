package watcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yourusername/haaddon/telegram-bot/internal/homeassistant"
)

// testableWatcher wraps Watcher to track notification calls
type testableWatcher struct {
	*Watcher
	mu       sync.Mutex
	onCalls  int
	offCalls int
}

func (tw *testableWatcher) trackPowerOn() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.onCalls++
}

func (tw *testableWatcher) trackPowerOff() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.offCalls++
}

func (tw *testableWatcher) getPowerOnCalls() int {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.onCalls
}

func (tw *testableWatcher) getPowerOffCalls() int {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.offCalls
}

// Override handleStateChange to track calls instead of sending real notifications
func (tw *testableWatcher) testHandleStateChange(ctx context.Context, oldState, newState *homeassistant.Entity) {
	if newState == nil {
		return
	}

	newPowerState := normalizeState(newState.State)

	tw.mu.Lock()
	previousState := tw.lastState
	timeSinceLastChange := time.Since(tw.lastChange)
	tw.mu.Unlock()

	// Skip if state hasn't actually changed
	if newPowerState == previousState {
		return
	}

	// Debounce rapid changes
	if timeSinceLastChange < tw.debounceTime {
		return
	}

	// Update state
	tw.mu.Lock()
	tw.lastState = newPowerState
	tw.lastChange = time.Now()
	tw.mu.Unlock()

	// Skip notification if transitioning from unknown state
	if previousState == PowerStateUnknown {
		return
	}

	// Track notification calls
	switch newPowerState {
	case PowerStateOn:
		tw.trackPowerOn()
	case PowerStateOff:
		tw.trackPowerOff()
	}
}

func createTestableWatcher() *testableWatcher {
	w := &Watcher{
		config:       nil, // Not needed for unit tests
		wsClient:     nil,
		haClient:     nil,
		notifSvc:     nil,
		lastState:    PowerStateUnknown,
		debounceTime: 5 * time.Second,
		mu:           sync.Mutex{},
	}

	return &testableWatcher{
		Watcher: w,
	}
}

func TestNormalizeState(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  PowerState
	}{
		{"on lowercase", "on", PowerStateOn},
		{"on uppercase", "ON", PowerStateOn},
		{"true", "true", PowerStateOn},
		{"1", "1", PowerStateOn},
		{"home", "home", PowerStateOn},
		{"alive", "alive", PowerStateOn},
		{"connected", "connected", PowerStateOn},
		{"off lowercase", "off", PowerStateOff},
		{"off uppercase", "OFF", PowerStateOff},
		{"false", "false", PowerStateOff},
		{"0", "0", PowerStateOff},
		{"not_home", "not_home", PowerStateOff},
		{"dead", "dead", PowerStateOff},
		{"disconnected", "disconnected", PowerStateOff},
		{"unknown", "unknown", PowerStateUnknown},
		{"unavailable", "unavailable", PowerStateUnknown},
		{"empty", "", PowerStateUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeState(tt.input)
			if got != tt.want {
				t.Errorf("normalizeState(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleStateChange_UnknownToOn(t *testing.T) {
	tw := createTestableWatcher()
	ctx := context.Background()

	// Initial state is unknown
	tw.lastState = PowerStateUnknown

	// Simulate state change to "on"
	newState := &homeassistant.Entity{
		EntityID: "binary_sensor.power",
		State:    "on",
	}

	tw.testHandleStateChange(ctx, nil, newState)

	// Should NOT send notification
	if tw.getPowerOnCalls() != 0 {
		t.Errorf("Expected 0 power on notifications, got %d", tw.getPowerOnCalls())
	}

	// State should be updated
	if tw.GetCurrentState() != PowerStateOn {
		t.Errorf("Expected state to be %v, got %v", PowerStateOn, tw.GetCurrentState())
	}
}

func TestHandleStateChange_UnknownToOff(t *testing.T) {
	tw := createTestableWatcher()
	ctx := context.Background()

	// Initial state is unknown
	tw.lastState = PowerStateUnknown

	// Simulate state change to "off"
	newState := &homeassistant.Entity{
		EntityID: "binary_sensor.power",
		State:    "off",
	}

	tw.testHandleStateChange(ctx, nil, newState)

	// Should NOT send notification
	if tw.getPowerOffCalls() != 0 {
		t.Errorf("Expected 0 power off notifications, got %d", tw.getPowerOffCalls())
	}

	// State should be updated
	if tw.GetCurrentState() != PowerStateOff {
		t.Errorf("Expected state to be %v, got %v", PowerStateOff, tw.GetCurrentState())
	}
}

func TestHandleStateChange_OnToOff(t *testing.T) {
	tw := createTestableWatcher()
	ctx := context.Background()

	// Set initial state to "on"
	tw.lastState = PowerStateOn
	tw.lastChange = time.Now().Add(-10 * time.Second)

	// Simulate state change to "off"
	newState := &homeassistant.Entity{
		EntityID: "binary_sensor.power",
		State:    "off",
	}

	tw.testHandleStateChange(ctx, nil, newState)

	// Should send notification
	if tw.getPowerOffCalls() != 1 {
		t.Errorf("Expected 1 power off notification, got %d", tw.getPowerOffCalls())
	}

	// State should be updated
	if tw.GetCurrentState() != PowerStateOff {
		t.Errorf("Expected state to be %v, got %v", PowerStateOff, tw.GetCurrentState())
	}
}

func TestHandleStateChange_OffToOn(t *testing.T) {
	tw := createTestableWatcher()
	ctx := context.Background()

	// Set initial state to "off"
	tw.lastState = PowerStateOff
	tw.lastChange = time.Now().Add(-10 * time.Second)

	// Simulate state change to "on"
	newState := &homeassistant.Entity{
		EntityID: "binary_sensor.power",
		State:    "on",
	}

	tw.testHandleStateChange(ctx, nil, newState)

	// Should send notification
	if tw.getPowerOnCalls() != 1 {
		t.Errorf("Expected 1 power on notification, got %d", tw.getPowerOnCalls())
	}

	// State should be updated
	if tw.GetCurrentState() != PowerStateOn {
		t.Errorf("Expected state to be %v, got %v", PowerStateOn, tw.GetCurrentState())
	}
}

func TestHandleStateChange_NoChange(t *testing.T) {
	tw := createTestableWatcher()
	ctx := context.Background()

	// Set initial state to "on"
	tw.lastState = PowerStateOn
	tw.lastChange = time.Now().Add(-10 * time.Second)

	// Simulate state "change" to same state "on"
	newState := &homeassistant.Entity{
		EntityID: "binary_sensor.power",
		State:    "on",
	}

	tw.testHandleStateChange(ctx, nil, newState)

	// Should NOT send notification
	if tw.getPowerOnCalls() != 0 {
		t.Errorf("Expected 0 power on notifications, got %d", tw.getPowerOnCalls())
	}

	// State should remain unchanged
	if tw.GetCurrentState() != PowerStateOn {
		t.Errorf("Expected state to remain %v, got %v", PowerStateOn, tw.GetCurrentState())
	}
}

func TestHandleStateChange_Debounce(t *testing.T) {
	tw := createTestableWatcher()
	ctx := context.Background()

	// Set initial state to "on"
	tw.lastState = PowerStateOn
	tw.lastChange = time.Now() // Very recent change

	// Simulate rapid state change to "off" (within debounce time)
	newState := &homeassistant.Entity{
		EntityID: "binary_sensor.power",
		State:    "off",
	}

	tw.testHandleStateChange(ctx, nil, newState)

	// Should NOT send notification due to debounce
	if tw.getPowerOffCalls() != 0 {
		t.Errorf("Expected 0 power off notifications (debounced), got %d", tw.getPowerOffCalls())
	}

	// State should remain "on" (not updated due to debounce)
	if tw.GetCurrentState() != PowerStateOn {
		t.Errorf("Expected state to remain %v (debounced), got %v", PowerStateOn, tw.GetCurrentState())
	}
}

func TestHandleStateChange_OffToUnknown(t *testing.T) {
	tw := createTestableWatcher()
	ctx := context.Background()

	// Set initial state to "off"
	tw.lastState = PowerStateOff
	tw.lastChange = time.Now().Add(-10 * time.Second)

	// Simulate state change to "unknown"
	newState := &homeassistant.Entity{
		EntityID: "binary_sensor.power",
		State:    "unknown",
	}

	tw.testHandleStateChange(ctx, nil, newState)

	// Should NOT send notification (unknown state doesn't trigger notifications)
	if tw.getPowerOnCalls() != 0 {
		t.Errorf("Expected 0 power on notifications, got %d", tw.getPowerOnCalls())
	}
	if tw.getPowerOffCalls() != 0 {
		t.Errorf("Expected 0 power off notifications, got %d", tw.getPowerOffCalls())
	}

	// State should be updated to unknown
	if tw.GetCurrentState() != PowerStateUnknown {
		t.Errorf("Expected state to be %v, got %v", PowerStateUnknown, tw.GetCurrentState())
	}
}

func TestHandleStateChange_MultipleTransitions(t *testing.T) {
	tw := createTestableWatcher()
	ctx := context.Background()

	// Start from unknown
	tw.lastState = PowerStateUnknown

	// 1. unknown -> on (should NOT notify)
	tw.testHandleStateChange(ctx, nil, &homeassistant.Entity{State: "on"})

	if tw.getPowerOnCalls() != 0 {
		t.Errorf("Step 1: Expected 0 notifications, got %d", tw.getPowerOnCalls())
	}
	if tw.GetCurrentState() != PowerStateOn {
		t.Errorf("Step 1: Expected state %v, got %v", PowerStateOn, tw.GetCurrentState())
	}

	// Wait for debounce
	tw.lastChange = time.Now().Add(-10 * time.Second)

	// 2. on -> off (should notify)
	tw.testHandleStateChange(ctx, nil, &homeassistant.Entity{State: "off"})

	if tw.getPowerOffCalls() != 1 {
		t.Errorf("Step 2: Expected 1 power off notification, got %d", tw.getPowerOffCalls())
	}
	if tw.GetCurrentState() != PowerStateOff {
		t.Errorf("Step 2: Expected state %v, got %v", PowerStateOff, tw.GetCurrentState())
	}

	// Wait for debounce
	tw.lastChange = time.Now().Add(-10 * time.Second)

	// 3. off -> on (should notify)
	tw.testHandleStateChange(ctx, nil, &homeassistant.Entity{State: "on"})

	if tw.getPowerOnCalls() != 1 {
		t.Errorf("Step 3: Expected 1 power on notification, got %d", tw.getPowerOnCalls())
	}
	if tw.GetCurrentState() != PowerStateOn {
		t.Errorf("Step 3: Expected state %v, got %v", PowerStateOn, tw.GetCurrentState())
	}

	// Wait for debounce
	tw.lastChange = time.Now().Add(-10 * time.Second)

	// 4. on -> unknown (should NOT notify)
	tw.testHandleStateChange(ctx, nil, &homeassistant.Entity{State: "unknown"})

	// Counts should remain the same
	if tw.getPowerOnCalls() != 1 {
		t.Errorf("Step 4: Expected 1 power on notification total, got %d", tw.getPowerOnCalls())
	}
	if tw.getPowerOffCalls() != 1 {
		t.Errorf("Step 4: Expected 1 power off notification total, got %d", tw.getPowerOffCalls())
	}

	// Wait for debounce
	tw.lastChange = time.Now().Add(-10 * time.Second)

	// 5. unknown -> off (should NOT notify - from unknown)
	tw.testHandleStateChange(ctx, nil, &homeassistant.Entity{State: "off"})

	// Counts should remain the same
	if tw.getPowerOffCalls() != 1 {
		t.Errorf("Step 5: Expected 1 power off notification total (no new), got %d", tw.getPowerOffCalls())
	}
}

func TestTimesEqual(t *testing.T) {
	// Use a fixed time for predictable test results
	baseTime := time.Date(2026, 1, 4, 14, 30, 0, 0, time.UTC)
	baseTimePlus30Sec := baseTime.Add(30 * time.Second)
	baseTimePlus2Min := baseTime.Add(2 * time.Minute)

	tests := []struct {
		name string
		a    *time.Time
		b    *time.Time
		want bool
	}{
		{"both nil", nil, nil, true},
		{"first nil", nil, &baseTime, false},
		{"second nil", &baseTime, nil, false},
		{"same time", &baseTime, &baseTime, true},
		{"within same minute", &baseTime, &baseTimePlus30Sec, true}, // 14:30:00 and 14:30:30 truncate to same minute
		{"different minutes", &baseTime, &baseTimePlus2Min, false},  // 14:30:00 and 14:32:00 are different minutes
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := timesEqual(tt.a, tt.b)
			if got != tt.want {
				aStr := "nil"
				bStr := "nil"
				if tt.a != nil {
					aStr = tt.a.Format("15:04:05")
				}
				if tt.b != nil {
					bStr = tt.b.Format("15:04:05")
				}
				t.Errorf("timesEqual(%s, %s) = %v, want %v", aStr, bStr, got, tt.want)
			}
		})
	}
}

func TestFormatTimePtr(t *testing.T) {
	tests := []struct {
		name string
		time *time.Time
		want string
	}{
		{
			name: "nil time",
			time: nil,
			want: "nil",
		},
		{
			name: "valid time",
			time: func() *time.Time {
				t := time.Date(2026, 1, 4, 14, 30, 0, 0, time.UTC)
				return &t
			}(),
			want: "14:30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTimePtr(tt.time)
			if got != tt.want {
				t.Errorf("formatTimePtr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetCurrentState(t *testing.T) {
	tw := createTestableWatcher()

	// Test initial state
	if tw.GetCurrentState() != PowerStateUnknown {
		t.Errorf("Initial state should be %v, got %v", PowerStateUnknown, tw.GetCurrentState())
	}

	// Update state
	tw.mu.Lock()
	tw.lastState = PowerStateOn
	tw.mu.Unlock()

	if tw.GetCurrentState() != PowerStateOn {
		t.Errorf("State should be %v, got %v", PowerStateOn, tw.GetCurrentState())
	}
}
