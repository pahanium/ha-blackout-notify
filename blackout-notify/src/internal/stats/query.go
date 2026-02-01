package stats

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PeriodStats represents statistics for a time period
type PeriodStats struct {
	TotalOnSeconds  int64
	TotalOffSeconds int64
	OnCount         int
	OffCount        int
	StartTime       time.Time
	EndTime         time.Time
}

// Query handles statistics queries
type Query struct {
	db       *DB
	entityID string
}

// NewQuery creates a new statistics query handler
func NewQuery(db *DB, entityID string) *Query {
	return &Query{
		db:       db,
		entityID: entityID,
	}
}

// GetStatsForPeriod returns statistics for a specific time period
func (q *Query) GetStatsForPeriod(ctx context.Context, startTime, endTime time.Time) (*PeriodStats, error) {
	query := `
		SELECT
			state,
			COUNT(*) as count,
			COALESCE(SUM(duration_seconds), 0) as total_seconds
		FROM power_events
		WHERE entity_id = ?
			AND changed_at >= ?
			AND changed_at < ?
			AND state IN ('on', 'off')
		GROUP BY state
	`

	rows, err := q.db.conn.QueryContext(ctx, query, q.entityID, startTime.UTC(), endTime.UTC())
	if err != nil {
		return nil, fmt.Errorf("failed to query stats: %w", err)
	}
	defer rows.Close()

	stats := &PeriodStats{
		StartTime: startTime,
		EndTime:   endTime,
	}

	for rows.Next() {
		var state string
		var count int
		var totalSeconds int64

		if err := rows.Scan(&state, &count, &totalSeconds); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if state == "on" {
			stats.OnCount = count
			stats.TotalOnSeconds = totalSeconds
		} else if state == "off" {
			stats.OffCount = count
			stats.TotalOffSeconds = totalSeconds
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Handle current ongoing state
	if err := q.addCurrentStateDuration(ctx, stats); err != nil {
		return nil, fmt.Errorf("failed to add current state duration: %w", err)
	}

	return stats, nil
}

// addCurrentStateDuration adds the duration of the current ongoing state to stats
func (q *Query) addCurrentStateDuration(ctx context.Context, stats *PeriodStats) error {
	// Get the last event before endTime
	query := `
		SELECT state, changed_at
		FROM power_events
		WHERE entity_id = ?
			AND changed_at < ?
			AND state IN ('on', 'off')
		ORDER BY changed_at DESC
		LIMIT 1
	`

	var state string
	var changedAtStr string
	err := q.db.conn.QueryRowContext(ctx, query, q.entityID, stats.EndTime.UTC()).Scan(&state, &changedAtStr)

	if err == sql.ErrNoRows {
		return nil // No events in this period
	}
	if err != nil {
		return fmt.Errorf("failed to get last event: %w", err)
	}

	changedAt, err := time.Parse(time.RFC3339, changedAtStr)
	if err != nil {
		// Try alternative format
		changedAt, err = time.Parse("2006-01-02 15:04:05", changedAtStr)
		if err != nil {
			return fmt.Errorf("failed to parse timestamp: %w", err)
		}
	}

	// If the last event is within our period, calculate duration until endTime
	if changedAt.After(stats.StartTime) {
		duration := int64(stats.EndTime.Sub(changedAt).Seconds())
		if state == "on" {
			stats.TotalOnSeconds += duration
		} else if state == "off" {
			stats.TotalOffSeconds += duration
		}
	}

	return nil
}

// GetStatsToday returns statistics for today (from midnight to now)
func (q *Query) GetStatsToday(ctx context.Context, location *time.Location) (*PeriodStats, error) {
	now := time.Now().In(location)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	return q.GetStatsForPeriod(ctx, startOfDay, now)
}

// GetStatsWeek returns statistics for the last 7 days
func (q *Query) GetStatsWeek(ctx context.Context, location *time.Location) (*PeriodStats, error) {
	now := time.Now().In(location)
	startOfWeek := now.AddDate(0, 0, -7)
	return q.GetStatsForPeriod(ctx, startOfWeek, now)
}

// GetStatsMonth returns statistics for the last 30 days
func (q *Query) GetStatsMonth(ctx context.Context, location *time.Location) (*PeriodStats, error) {
	now := time.Now().In(location)
	startOfMonth := now.AddDate(0, 0, -30)
	return q.GetStatsForPeriod(ctx, startOfMonth, now)
}

// FormatDuration formats duration in seconds to human-readable string
func FormatDuration(seconds int64) string {
	duration := time.Duration(seconds) * time.Second
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60

	if hours > 0 {
		return fmt.Sprintf("%d год %d хв", hours, minutes)
	}
	return fmt.Sprintf("%d хв", minutes)
}
