package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pahanium/ha-blackout-notify/internal/config"
	"github.com/pahanium/ha-blackout-notify/internal/notifications"
	"github.com/pahanium/ha-blackout-notify/internal/stats"
)

func TestHandleStatsWithoutStatsProvider(t *testing.T) {
	b := &Bot{config: &config.Config{Timezone: "Europe/Kyiv", Location: time.UTC}}

	response, err := b.handleStats(context.Background(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if response != MsgStatsUnavailable {
		t.Errorf("expected %q, got %q", MsgStatsUnavailable, response)
	}
}

func TestHandleStatsUnknownPeriodEscaped(t *testing.T) {
	b := &Bot{config: &config.Config{Location: time.UTC}, statsDB: &stats.DB{}, statsRecorder: &stats.Recorder{}}

	response, err := b.handleStats(context.Background(), "last_year")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(response, `last\_year`) {
		t.Errorf("user input must be Markdown-escaped, got %q", response)
	}
}

func TestHandleCalendarNotConfigured(t *testing.T) {
	b := &Bot{config: &config.Config{Location: time.UTC}, notifService: &notifications.Service{}}

	response, err := b.handleCalendar(context.Background(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if response != MsgCalendarNotConfigured {
		t.Errorf("expected %q, got %q", MsgCalendarNotConfigured, response)
	}
}
