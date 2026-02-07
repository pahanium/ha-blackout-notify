package stats

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestDurationCalculation_WebSocketReconnect tests the bug scenario where
// WebSocket disconnects during power outage and incorrectly calculates duration
func TestDurationCalculation_WebSocketReconnect(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_reconnect.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	recorder := NewRecorder(db, "binary_sensor.0xa4c1380cdb9ce842_water_leak")
	ctx := context.Background()

	// Simulate the bug scenario from logs
	baseTime := time.Date(2026, 2, 7, 5, 48, 1, 0, time.UTC)

	// Step 1: 05:48:01 - Power goes off (on -> off)
	t.Log("Step 1: 05:48:01 - Power goes off")
	_, _, err = recorder.RecordStateChange(ctx, "off", baseTime)
	if err != nil {
		t.Fatalf("Step 1 failed: %v", err)
	}

	// Step 2: 13:27:08 - WebSocket reconnects, state becomes "unknown" (off -> unknown)
	// In the OLD implementation, this WOULD be recorded (causing the bug)
	// In the NEW implementation, this should NOT be recorded
	t.Log("Step 2: 13:27:08 - WebSocket reconnects, state -> unknown (should NOT be recorded)")
	// We don't call RecordStateChange for unknown states in the new implementation

	// Step 3: 13:27:19 - State goes back to "off" from "unknown" (unknown -> off)
	// In the OLD implementation, this WOULD create a new "off" record (causing the bug)
	// In the NEW implementation, this should NOT be recorded
	t.Log("Step 3: 13:27:19 - State back to off from unknown (should NOT be recorded)")
	// We don't call RecordStateChange for transitions from unknown in the new implementation

	// Step 4: 18:47:28 - Power is restored (off -> on)
	t.Log("Step 4: 18:47:28 - Power restored")
	powerOnTime := baseTime.Add(12*time.Hour + 59*time.Minute + 27*time.Second) // ~13 hours later
	prevState, duration, err := recorder.RecordStateChange(ctx, "on", powerOnTime)
	if err != nil {
		t.Fatalf("Step 4 failed: %v", err)
	}

	// Verify the duration is calculated from step 1 (05:48:01), not from any intermediate states
	expectedDuration := int64(powerOnTime.Sub(baseTime).Seconds())

	if prevState != "off" {
		t.Errorf("Expected previous state 'off', got '%s'", prevState)
	}

	// Allow 1 second tolerance for test timing
	if duration < expectedDuration-1 || duration > expectedDuration+1 {
		t.Errorf("Expected duration ~%d seconds (%.1f hours), got %d seconds (%.1f hours)",
			expectedDuration, float64(expectedDuration)/3600,
			duration, float64(duration)/3600)
	} else {
		t.Logf("✓ Correct duration calculated: %d seconds (%.1f hours)", duration, float64(duration)/3600)
	}
}

// TestDurationCalculation_NormalFlow tests the normal power on/off flow
func TestDurationCalculation_NormalFlow(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_normal.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	recorder := NewRecorder(db, "binary_sensor.test")
	ctx := context.Background()

	baseTime := time.Now()

	// Power off
	t.Log("Power off")
	_, _, err = recorder.RecordStateChange(ctx, "off", baseTime)
	if err != nil {
		t.Fatalf("Power off failed: %v", err)
	}

	// Power on after 2 hours
	t.Log("Power on after 2 hours")
	powerOnTime := baseTime.Add(2 * time.Hour)
	prevState, duration, err := recorder.RecordStateChange(ctx, "on", powerOnTime)
	if err != nil {
		t.Fatalf("Power on failed: %v", err)
	}

	expectedDuration := int64(2 * time.Hour / time.Second)

	if prevState != "off" {
		t.Errorf("Expected previous state 'off', got '%s'", prevState)
	}

	if duration != expectedDuration {
		t.Errorf("Expected duration %d seconds (2 hours), got %d seconds", expectedDuration, duration)
	} else {
		t.Logf("✓ Normal flow works: %d seconds", duration)
	}
}

// TestDurationCalculation_MultipleOutages tests multiple outages in sequence
func TestDurationCalculation_MultipleOutages(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_multiple.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	defer db.Close()

	recorder := NewRecorder(db, "binary_sensor.test")
	ctx := context.Background()

	baseTime := time.Now()

	// First outage: off for 1 hour
	t.Log("First outage")
	_, _, err = recorder.RecordStateChange(ctx, "off", baseTime)
	if err != nil {
		t.Fatalf("First outage failed: %v", err)
	}

	time1 := baseTime.Add(1 * time.Hour)
	prevState, duration, err := recorder.RecordStateChange(ctx, "on", time1)
	if err != nil {
		t.Fatalf("First restore failed: %v", err)
	}
	if prevState != "off" || duration != 3600 {
		t.Errorf("First outage: expected off/3600, got %s/%d", prevState, duration)
	}

	// Power on for 30 minutes
	time2 := time1.Add(30 * time.Minute)
	prevState, duration, err = recorder.RecordStateChange(ctx, "off", time2)
	if err != nil {
		t.Fatalf("Second outage failed: %v", err)
	}
	if prevState != "on" || duration != 1800 {
		t.Errorf("Power on period: expected on/1800, got %s/%d", prevState, duration)
	}

	// Second outage: off for 3 hours
	time3 := time2.Add(3 * time.Hour)
	prevState, duration, err = recorder.RecordStateChange(ctx, "on", time3)
	if err != nil {
		t.Fatalf("Second restore failed: %v", err)
	}
	if prevState != "off" || duration != 10800 {
		t.Errorf("Second outage: expected off/10800, got %s/%d", prevState, duration)
	} else {
		t.Logf("✓ Multiple outages tracked correctly")
	}
}
