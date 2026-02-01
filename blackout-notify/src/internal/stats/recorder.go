package stats

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/yourusername/haaddon/telegram-bot/internal/logger"
)

// Recorder handles recording power state changes
type Recorder struct {
	db       *DB
	entityID string
}

// NewRecorder creates a new statistics recorder
func NewRecorder(db *DB, entityID string) *Recorder {
	return &Recorder{
		db:       db,
		entityID: entityID,
	}
}

// PowerEvent represents a power state change event
type PowerEvent struct {
	ID              int64
	EntityID        string
	State           string
	ChangedAt       time.Time
	DurationSeconds *int64
	CreatedAt       time.Time
}

// RecordStateChange records a power state change event
func (r *Recorder) RecordStateChange(ctx context.Context, state string, changedAt time.Time) error {
	// Get the last event to calculate duration
	lastEvent, err := r.getLastEvent(ctx)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get last event: %w", err)
	}

	var durationSeconds *int64
	if lastEvent != nil {
		// Calculate duration of the previous state
		duration := int64(changedAt.Sub(lastEvent.ChangedAt).Seconds())
		durationSeconds = &duration
		logger.Debug("State change detected: %s -> %s, previous state lasted %d seconds", lastEvent.State, state, duration)
	} else {
		logger.Debug("First state change recorded: %s", state)
	}

	// Insert new event
	query := `
		INSERT INTO power_events (entity_id, state, changed_at, duration_seconds)
		VALUES (?, ?, ?, ?)
	`

	_, err = r.db.conn.ExecContext(ctx, query, r.entityID, state, changedAt.UTC(), durationSeconds)
	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	logger.Debug("Recorded power state change: entity=%s, state=%s, time=%s", r.entityID, state, changedAt.Format(time.RFC3339))
	return nil
}

// getLastEvent retrieves the most recent power event
func (r *Recorder) getLastEvent(ctx context.Context) (*PowerEvent, error) {
	query := `
		SELECT id, entity_id, state, changed_at, duration_seconds, created_at
		FROM power_events
		WHERE entity_id = ?
		ORDER BY changed_at DESC
		LIMIT 1
	`

	row := r.db.conn.QueryRowContext(ctx, query, r.entityID)

	event := &PowerEvent{}
	var changedAtStr, createdAtStr string

	err := row.Scan(
		&event.ID,
		&event.EntityID,
		&event.State,
		&changedAtStr,
		&event.DurationSeconds,
		&createdAtStr,
	)

	if err != nil {
		return nil, err
	}

	// Parse timestamps - SQLite stores in RFC3339 format
	event.ChangedAt, err = time.Parse(time.RFC3339, changedAtStr)
	if err != nil {
		// Try alternative format
		event.ChangedAt, err = time.Parse("2006-01-02 15:04:05", changedAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse changed_at: %w", err)
		}
	}

	event.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		// Try alternative format
		event.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at: %w", err)
		}
	}

	return event, nil
}

// GetCurrentStateDuration returns how long the current state has been active
func (r *Recorder) GetCurrentStateDuration(ctx context.Context) (string, time.Duration, error) {
	lastEvent, err := r.getLastEvent(ctx)
	if err == sql.ErrNoRows {
		return "unknown", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("failed to get last event: %w", err)
	}

	duration := time.Since(lastEvent.ChangedAt)
	return lastEvent.State, duration, nil
}
