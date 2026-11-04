package stats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ScheduleLog remembers which outage schedule was last sent for each date,
// so the same schedule is not announced twice (e.g. tomorrow's schedule becoming today's at midnight)
type ScheduleLog struct {
	db *DB
}

// NewScheduleLog creates a schedule log backed by the statistics database
func NewScheduleLog(db *DB) *ScheduleLog {
	return &ScheduleLog{db: db}
}

// LastSent returns the fingerprint of the schedule last sent for date (YYYY-MM-DD), or "" if none
func (l *ScheduleLog) LastSent(ctx context.Context, date string) (string, error) {
	var fingerprint string
	err := l.db.conn.QueryRowContext(ctx,
		`SELECT fingerprint FROM sent_schedules WHERE date = ?`, date).Scan(&fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read sent schedule for %s: %w", date, err)
	}
	return fingerprint, nil
}

// MarkSent stores the fingerprint of the schedule sent for date (YYYY-MM-DD)
func (l *ScheduleLog) MarkSent(ctx context.Context, date, fingerprint string) error {
	_, err := l.db.conn.ExecContext(ctx, `
		INSERT INTO sent_schedules (date, fingerprint, sent_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(date) DO UPDATE SET fingerprint = excluded.fingerprint, sent_at = excluded.sent_at`,
		date, fingerprint)
	if err != nil {
		return fmt.Errorf("failed to store sent schedule for %s: %w", date, err)
	}
	return nil
}

// IsEmpty reports whether no schedule was ever recorded (first run after upgrade)
func (l *ScheduleLog) IsEmpty(ctx context.Context) (bool, error) {
	var n int
	if err := l.db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM sent_schedules`).Scan(&n); err != nil {
		return false, fmt.Errorf("failed to count sent schedules: %w", err)
	}
	return n == 0, nil
}
