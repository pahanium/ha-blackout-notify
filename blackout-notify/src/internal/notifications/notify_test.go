package notifications

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/pahanium/ha-blackout-notify/internal/config"
	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
)

// fakeSender captures messages instead of calling Telegram
type fakeSender struct {
	sent []tgbotapi.MessageConfig
}

func (f *fakeSender) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	msg, ok := c.(tgbotapi.MessageConfig)
	if !ok {
		return tgbotapi.Message{}, fmt.Errorf("unexpected chattable %T", c)
	}
	f.sent = append(f.sent, msg)
	return tgbotapi.Message{}, nil
}

// fakeHA serves entity states from a map
type fakeHA struct {
	states map[string]string
}

func (f *fakeHA) GetState(ctx context.Context, entityID string) (*homeassistant.Entity, error) {
	state, ok := f.states[entityID]
	if !ok {
		return nil, fmt.Errorf("entity %s not found", entityID)
	}
	return &homeassistant.Entity{EntityID: entityID, State: state}, nil
}

func (f *fakeHA) GetCalendarEvents(ctx context.Context, calendarID, start, end string) ([]homeassistant.CalendarEvent, error) {
	return nil, nil
}

func newTestService(t *testing.T, states map[string]string) (*Service, *fakeSender) {
	t.Helper()
	cfg := &config.Config{
		NotificationChatIDs: []int64{-100111, -100222},
		NextOnSensorID:      "sensor.next_on",
		NextOffSensorID:     "sensor.next_off",
		PauseEntityID:       "input_boolean.pause",
		Timezone:            "Europe/Kyiv",
	}
	if states == nil {
		states = map[string]string{}
	}
	if _, ok := states["input_boolean.pause"]; !ok {
		states["input_boolean.pause"] = "off"
	}
	sender := &fakeSender{}
	svc, err := NewService(sender, cfg, &fakeHA{states: states})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, sender
}

func TestNotifyPowerOff_MessageContent(t *testing.T) {
	nextOn := time.Now().Add(2*time.Hour + 30*time.Minute).UTC().Format(time.RFC3339)
	svc, sender := newTestService(t, map[string]string{"sensor.next_on": nextOn})

	if err := svc.NotifyPowerOff(context.Background(), 3*3600+25*60); err != nil {
		t.Fatalf("NotifyPowerOff: %v", err)
	}

	if len(sender.sent) != 2 {
		t.Fatalf("want message to each of 2 chats, got %d", len(sender.sent))
	}
	text := sender.sent[0].Text
	for _, want := range []string{MsgPowerOff, fmt.Sprintf(MsgWasOn, "3год 25хв"), "📅 Заживлення через", MsgScheduleSource} {
		if !strings.Contains(text, want) {
			t.Errorf("message missing %q:\n%s", want, text)
		}
	}
	if sender.sent[0].ParseMode != tgbotapi.ModeMarkdown {
		t.Errorf("want Markdown parse mode, got %q", sender.sent[0].ParseMode)
	}
}

func TestNotifyPowerOn_ShortOrUnknownDurationOmitted(t *testing.T) {
	for _, d := range []int64{0, 60, 86400} {
		svc, sender := newTestService(t, nil)
		if err := svc.NotifyPowerOn(context.Background(), d); err != nil {
			t.Fatalf("NotifyPowerOn: %v", err)
		}
		if strings.Contains(sender.sent[0].Text, "🕐") {
			t.Errorf("duration %ds should not be shown:\n%s", d, sender.sent[0].Text)
		}
	}
}

func TestNotify_PausedSendsNothing(t *testing.T) {
	svc, sender := newTestService(t, map[string]string{"input_boolean.pause": "on"})
	ctx := context.Background()

	_ = svc.NotifyPowerOn(ctx, 3600)
	_ = svc.NotifyPowerOff(ctx, 3600)
	_ = svc.NotifyYasnoEmergencyShutdown(ctx)
	_ = svc.NotifyYasnoScheduleTomorrow(ctx, nil, "група 2.1")

	if len(sender.sent) != 0 {
		t.Errorf("paused: want no messages, got %d", len(sender.sent))
	}
}

func TestNotifyYasnoScheduleTomorrow_QuietHours(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	tests := []struct {
		hour   int
		silent bool
	}{
		{22, false},
		{23, true},
		{3, true},
		{6, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%02d:00", tt.hour), func(t *testing.T) {
			svc, sender := newTestService(t, nil)
			svc.now = func() time.Time { return time.Date(2026, 10, 31, tt.hour, 0, 0, 0, kyiv) }

			if err := svc.NotifyYasnoScheduleTomorrow(context.Background(), nil, "група 2.1"); err != nil {
				t.Fatalf("NotifyYasnoScheduleTomorrow: %v", err)
			}
			if got := sender.sent[0].DisableNotification; got != tt.silent {
				t.Errorf("DisableNotification = %v, want %v", got, tt.silent)
			}
		})
	}
}

func TestFormatYasnoSchedule_OnlyDefiniteOutages(t *testing.T) {
	svc, _ := newTestService(t, nil)
	events := []homeassistant.CalendarEvent{
		{Start: "2026-11-01T07:30:00+02:00", End: "2026-11-01T11:00:00+02:00", Description: "Definite"},
		{Start: "2026-11-01T14:00:00+02:00", End: "2026-11-01T17:30:00+02:00", Description: "Possible"},
	}

	text := svc.FormatYasnoSchedule(events, time.Date(2026, 11, 1, 0, 0, 0, 0, svc.location), "група 2.1")

	if !strings.Contains(text, fmt.Sprintf(MsgYasnoOutageInterval, "07:30", "11:00")) {
		t.Errorf("definite outage missing:\n%s", text)
	}
	if strings.Contains(text, "14:00") {
		t.Errorf("possible outage must be filtered out:\n%s", text)
	}
	if !strings.Contains(text, "група 2.1") {
		t.Errorf("group name missing:\n%s", text)
	}
}
