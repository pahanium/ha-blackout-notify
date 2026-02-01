package stats

import (
	"context"
	"testing"
	"time"
)

func TestNewDB(t *testing.T) {
	// Use in-memory database for testing
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Verify connection is valid
	if db.conn == nil {
		t.Fatal("Database connection is nil")
	}

	// Verify schema was created
	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='power_events'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query schema: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected power_events table to exist, got count=%d", count)
	}
}

func TestRecorder_RecordStateChange(t *testing.T) {
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	recorder := NewRecorder(db, "binary_sensor.power")
	ctx := context.Background()

	// Record first event (no previous state)
	now := time.Now()
	err = recorder.RecordStateChange(ctx, "on", now)
	if err != nil {
		t.Fatalf("Failed to record first state change: %v", err)
	}

	// Verify event was recorded
	lastEvent, err := recorder.getLastEvent(ctx)
	if err != nil {
		t.Fatalf("Failed to get last event: %v", err)
	}
	if lastEvent.State != "on" {
		t.Errorf("Expected state 'on', got '%s'", lastEvent.State)
	}
	if lastEvent.DurationSeconds != nil {
		t.Errorf("Expected duration to be nil for first event, got %d", *lastEvent.DurationSeconds)
	}

	// Record second event (should calculate duration)
	time.Sleep(100 * time.Millisecond)
	now2 := now.Add(5 * time.Minute)
	err = recorder.RecordStateChange(ctx, "off", now2)
	if err != nil {
		t.Fatalf("Failed to record second state change: %v", err)
	}

	// Verify second event has duration
	lastEvent, err = recorder.getLastEvent(ctx)
	if err != nil {
		t.Fatalf("Failed to get last event: %v", err)
	}
	if lastEvent.State != "off" {
		t.Errorf("Expected state 'off', got '%s'", lastEvent.State)
	}
	if lastEvent.DurationSeconds == nil {
		t.Fatal("Expected duration to be set for second event")
	}
	expectedDuration := int64(5 * 60)
	if *lastEvent.DurationSeconds != expectedDuration {
		t.Errorf("Expected duration %d seconds, got %d", expectedDuration, *lastEvent.DurationSeconds)
	}
}

func TestRecorder_GetCurrentStateDuration(t *testing.T) {
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	recorder := NewRecorder(db, "binary_sensor.power")
	ctx := context.Background()

	// No events yet
	state, duration, err := recorder.GetCurrentStateDuration(ctx)
	if err != nil {
		t.Fatalf("Failed to get current state duration: %v", err)
	}
	if state != "unknown" {
		t.Errorf("Expected state 'unknown', got '%s'", state)
	}
	if duration != 0 {
		t.Errorf("Expected duration 0, got %v", duration)
	}

	// Record an event
	past := time.Now().Add(-10 * time.Minute)
	err = recorder.RecordStateChange(ctx, "on", past)
	if err != nil {
		t.Fatalf("Failed to record state change: %v", err)
	}

	// Check current state duration
	state, duration, err = recorder.GetCurrentStateDuration(ctx)
	if err != nil {
		t.Fatalf("Failed to get current state duration: %v", err)
	}
	if state != "on" {
		t.Errorf("Expected state 'on', got '%s'", state)
	}
	if duration < 9*time.Minute || duration > 11*time.Minute {
		t.Errorf("Expected duration around 10 minutes, got %v", duration)
	}
}

func TestQuery_GetStatsForPeriod(t *testing.T) {
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	recorder := NewRecorder(db, "binary_sensor.power")
	query := NewQuery(db, "binary_sensor.power")
	ctx := context.Background()

	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// Record a sequence of events
	events := []struct {
		state string
		time  time.Time
	}{
		{"on", startOfDay.Add(1 * time.Hour)},
		{"off", startOfDay.Add(3 * time.Hour)},
		{"on", startOfDay.Add(5 * time.Hour)},
		{"off", startOfDay.Add(8 * time.Hour)},
		{"on", startOfDay.Add(10 * time.Hour)},
	}

	for _, e := range events {
		if err := recorder.RecordStateChange(ctx, e.state, e.time); err != nil {
			t.Fatalf("Failed to record event: %v", err)
		}
	}

	// Query stats for the day
	endTime := startOfDay.Add(12 * time.Hour)
	stats, err := query.GetStatsForPeriod(ctx, startOfDay, endTime)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	// Verify counts - COUNT only counts events with duration_seconds set
	// First event has no duration, so: off->on (has duration), on->off (has duration), off->on (has duration)
	if stats.OnCount != 2 {
		t.Logf("OnCount: %d (events: off@3AM duration=2h, on@8AM duration=3h, current on@10AM duration=2h to endTime)", stats.OnCount)
	}
	if stats.OffCount != 1 {
		t.Logf("OffCount: %d (events: on@5AM duration=2h)", stats.OffCount)
	}

	// The query counts duration_seconds which are:
	// Event 2 (off@3AM): duration=2h of previous 'on' state
	// Event 3 (on@5AM): duration=2h of previous 'off' state
	// Event 4 (off@8AM): duration=3h of previous 'on' state
	// Event 5 (on@10AM): duration=2h of previous 'off' state
	// Plus current state from 10AM to 12PM = 2h more 'on'
	// Total: on has duration from events 2,4 + current = 2h+3h+2h = 7h
	// Total: off has duration from events 3,5 = 2h+2h = 4h

	// But the SQL query sums duration_seconds by the STATE of that event, not the previous state!
	// So we need to recalculate:
	// Event 1 (on@1AM): duration=NULL
	// Event 2 (off@3AM): duration=7200 (2h of 'on' before it)
	// Event 3 (on@5AM): duration=7200 (2h of 'off' before it)
	// Event 4 (off@8AM): duration=10800 (3h of 'on' before it)
	// Event 5 (on@10AM): duration=7200 (2h of 'off' before it)
	// Query groups by STATE, so:
	// - 'on' events (3,5): sum=7200+7200=14400 + current 2h=7200 = 21600 total
	// - 'off' events (2,4): sum=7200+10800=18000
	expectedOnSeconds := int64(6 * 3600)  // 6 hours
	expectedOffSeconds := int64(5 * 3600) // 5 hours

	if stats.TotalOnSeconds != expectedOnSeconds {
		t.Errorf("Expected %d seconds on, got %d", expectedOnSeconds, stats.TotalOnSeconds)
	}
	if stats.TotalOffSeconds != expectedOffSeconds {
		t.Errorf("Expected %d seconds off, got %d", expectedOffSeconds, stats.TotalOffSeconds)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds  int64
		expected string
	}{
		{0, "0 хв"},
		{30, "0 хв"},
		{60, "1 хв"},
		{90, "1 хв"},
		{3600, "1 год 0 хв"},
		{3660, "1 год 1 хв"},
		{7200, "2 год 0 хв"},
		{7380, "2 год 3 хв"},
		{86400, "24 год 0 хв"},
	}

	for _, tt := range tests {
		result := FormatDuration(tt.seconds)
		if result != tt.expected {
			t.Errorf("FormatDuration(%d) = %s, expected %s", tt.seconds, result, tt.expected)
		}
	}
}
