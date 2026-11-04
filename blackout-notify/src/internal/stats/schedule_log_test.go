package stats

import (
	"context"
	"testing"
)

func TestScheduleLog(t *testing.T) {
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	log := NewScheduleLog(db)
	ctx := context.Background()

	if empty, err := log.IsEmpty(ctx); err != nil || !empty {
		t.Fatalf("new log: empty=%v err=%v, want empty", empty, err)
	}
	if got, err := log.LastSent(ctx, "2026-11-02"); err != nil || got != "" {
		t.Fatalf("LastSent on empty log = %q, %v", got, err)
	}

	if err := log.MarkSent(ctx, "2026-11-02", "a"); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	if err := log.MarkSent(ctx, "2026-11-02", "b"); err != nil {
		t.Fatalf("MarkSent overwrite: %v", err)
	}
	if got, _ := log.LastSent(ctx, "2026-11-02"); got != "b" {
		t.Errorf("LastSent = %q, want b", got)
	}
	if got, _ := log.LastSent(ctx, "2026-11-03"); got != "" {
		t.Errorf("other date LastSent = %q, want empty", got)
	}
	if empty, _ := log.IsEmpty(ctx); empty {
		t.Error("log should not be empty after MarkSent")
	}
}
