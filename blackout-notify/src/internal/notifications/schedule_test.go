package notifications

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
)

func outage(start, end string) homeassistant.CalendarEvent {
	return homeassistant.CalendarEvent{Start: start, End: end, Description: definiteOutageEventDescription}
}

func newScheduleService(t *testing.T, todayStatus string, events ...homeassistant.CalendarEvent) (*Service, *fakeSender) {
	t.Helper()
	svc, sender := newTestService(t, map[string]string{"sensor.yasno_today": todayStatus})
	svc.config.YasnoCalendarID = "calendar.yasno_kiiv_2_1_planned_outages"
	svc.config.YasnoTodaySensorID = "sensor.yasno_today"
	svc.haClient.(*fakeHA).events = events
	return svc, sender
}

func kyivTime(t *testing.T, value string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %s: %v", value, err)
	}
	return ts
}

func TestIsScheduledPowerChange(t *testing.T) {
	// Planned outage 14:30-23:00 Kyiv (UTC+2 in November)
	day := []homeassistant.CalendarEvent{
		outage("2026-11-02T14:30:00+02:00", "2026-11-02T23:00:00+02:00"),
		{Start: "2026-11-02T08:00:00+02:00", End: "2026-11-02T10:00:00+02:00", Description: "Possible"},
	}
	// Back-to-back outages over midnight
	midnight := []homeassistant.CalendarEvent{
		outage("2026-11-02T22:00:00+02:00", "2026-11-03T00:00:00+02:00"),
		outage("2026-11-03T00:00:00+02:00", "2026-11-03T03:30:00+02:00"),
	}

	tests := []struct {
		name      string
		events    []homeassistant.CalendarEvent
		status    string
		powerOn   bool
		at        string
		offFor    time.Duration
		scheduled bool
	}{
		{"off 1h35m before planned outage", day, YasnoStatusScheduleApplies, false, "2026-11-02T12:55:00+02:00", 0, false},
		{"off 25 min before planned outage", day, YasnoStatusScheduleApplies, false, "2026-11-02T14:05:00+02:00", 0, true},
		{"off during planned outage", day, YasnoStatusScheduleApplies, false, "2026-11-02T16:00:00+02:00", 0, true},
		{"off after planned outage ended", day, YasnoStatusScheduleApplies, false, "2026-11-02T23:10:00+02:00", 0, false},
		{"off during possible-only outage", day, YasnoStatusScheduleApplies, false, "2026-11-02T08:30:00+02:00", 0, false},
		{"on 20 min after planned outage started", day, YasnoStatusScheduleApplies, true, "2026-11-02T14:50:00+02:00", 20 * time.Minute, false},
		{"on after planned outage ended", day, YasnoStatusScheduleApplies, true, "2026-11-02T23:05:00+02:00", 8*time.Hour + 35*time.Minute, true},
		{"on after unscheduled outage in light period", day, YasnoStatusScheduleApplies, true, "2026-11-02T13:30:00+02:00", 35 * time.Minute, false},
		{"on at planned end after early outage", day, YasnoStatusScheduleApplies, true, "2026-11-02T23:00:00+02:00", 10*time.Hour + 5*time.Minute, true},
		{"on mid planned outage", day, YasnoStatusScheduleApplies, true, "2026-11-02T18:00:00+02:00", 3*time.Hour + 30*time.Minute, true},
		{"on after midnight inside merged outage", midnight, YasnoStatusScheduleApplies, true, "2026-11-03T00:10:00+02:00", 2*time.Hour + 10*time.Minute, true},
		{"off during emergency shutdowns", day, YasnoStatusEmergencyShutdowns, false, "2026-11-02T16:00:00+02:00", 0, false},
		{"on during emergency shutdowns", day, YasnoStatusEmergencyShutdowns, true, "2026-11-02T23:05:00+02:00", 8 * time.Hour, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newScheduleService(t, tt.status, tt.events...)
			got := svc.isScheduledPowerChange(context.Background(), tt.powerOn, kyivTime(t, tt.at), tt.offFor)
			if got != tt.scheduled {
				t.Errorf("isScheduledPowerChange = %v, want %v", got, tt.scheduled)
			}
		})
	}
}

func TestIsScheduledPowerChange_NoCalendarKeepsOldBehaviour(t *testing.T) {
	svc, _ := newTestService(t, nil)
	if !svc.isScheduledPowerChange(context.Background(), false, time.Now(), 0) {
		t.Error("without calendar every change must count as scheduled")
	}
}

func TestNotifyPowerOff_AnomalyOmitsNextTime(t *testing.T) {
	nextOn := time.Now().Add(10 * time.Hour).UTC().Format(time.RFC3339)
	svc, sender := newScheduleService(t, YasnoStatusScheduleApplies) // no planned outages at all
	svc.haClient.(*fakeHA).states["sensor.next_on"] = nextOn

	if err := svc.NotifyPowerOff(context.Background(), 21*60); err != nil {
		t.Fatalf("NotifyPowerOff: %v", err)
	}
	text := sender.sent[0].Text
	if !strings.Contains(text, MsgPowerOff) || !strings.Contains(text, "21хв") {
		t.Errorf("power off fact and duration must be kept:\n%s", text)
	}
	if strings.Contains(text, "Заживлення") || strings.Contains(text, "Yasno") {
		t.Errorf("unscheduled outage must not show next on time:\n%s", text)
	}
}

func TestNotifyScheduleChanged_Format(t *testing.T) {
	svc, sender := newScheduleService(t, YasnoStatusScheduleApplies)
	now := time.Date(2026, 11, 2, 6, 0, 0, 0, svc.location)
	svc.now = func() time.Time { return now }
	next := now.Add(2*time.Hour + 30*time.Minute)
	events := []homeassistant.CalendarEvent{
		outage("2026-11-02T08:30:00+02:00", "2026-11-02T15:30:00+02:00"),
		outage("2026-11-02T00:00:00+02:00", "2026-11-02T03:30:00+02:00"),
	}

	if err := svc.NotifyScheduleChanged(context.Background(), "off", &next, events); err != nil {
		t.Fatalf("NotifyScheduleChanged: %v", err)
	}

	want := MsgScheduleUpdate + "\n\n" +
		"📅 Відключення через *2год 30хв* (" + next.In(svc.location).Format("15:04") + ")\n" + MsgScheduleSource + "\n\n" +
		"🪫00:00 - 03:30\n🪫08:30 - 15:30"
	if got := sender.sent[0].Text; got != want {
		t.Errorf("message mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestNotifyEmergencyEnded_Format(t *testing.T) {
	svc, sender := newScheduleService(t, YasnoStatusScheduleApplies)
	svc.now = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, svc.location) }
	events := []homeassistant.CalendarEvent{outage("2026-11-02T19:00:00+02:00", "2026-11-03T00:00:00+02:00")}

	if err := svc.NotifyEmergencyEnded(context.Background(), "on", nil, events); err != nil {
		t.Fatalf("NotifyEmergencyEnded: %v", err)
	}

	want := MsgYasnoEmergencyEnded + "\n\n🪫19:00 - 00:00"
	if got := sender.sent[0].Text; got != want {
		t.Errorf("message mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestNotifyScheduleChanged_NoOutagesToday(t *testing.T) {
	tests := []struct {
		name   string
		events []homeassistant.CalendarEvent
		want   string
	}{
		{"schedule unknown", nil, MsgScheduleUpdate},
		{"no outages", []homeassistant.CalendarEvent{}, MsgScheduleUpdate + "\n\n" + MsgYasnoNoOutagesLine},
		{"only possible outages", []homeassistant.CalendarEvent{
			{Start: "2026-11-02T18:00:00+02:00", End: "2026-11-02T19:00:00+02:00", Description: "Possible"},
		}, MsgScheduleUpdate + "\n\n" + MsgYasnoNoOutagesLine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, sender := newScheduleService(t, YasnoStatusScheduleApplies)
			if err := svc.NotifyScheduleChanged(context.Background(), "off", nil, tt.events); err != nil {
				t.Fatalf("NotifyScheduleChanged: %v", err)
			}
			if got := sender.sent[0].Text; got != tt.want {
				t.Errorf("message mismatch\ngot:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestFormatYasnoSchedule_OutagesCutToDay(t *testing.T) {
	svc, _ := newScheduleService(t, YasnoStatusScheduleApplies)
	events := []homeassistant.CalendarEvent{
		outage("2026-11-04T20:30:00+02:00", "2026-11-05T02:00:00+02:00"),
		outage("2026-11-05T06:00:00+02:00", "2026-11-05T13:00:00+02:00"),
		outage("2026-11-05T16:00:00+02:00", "2026-11-06T01:00:00+02:00"),
	}

	text := svc.FormatYasnoSchedule(events, time.Date(2026, 11, 5, 0, 0, 0, 0, svc.location), "")

	want := "🪫00:00 - 02:00\n🪫06:00 - 13:00\n🪫16:00 - 00:00"
	if !strings.HasSuffix(text, "\n\n"+want) {
		t.Errorf("outages must be cut to the day\ngot:\n%s\nwant lines:\n%s", text, want)
	}
}

func TestScheduleFingerprint(t *testing.T) {
	a := []homeassistant.CalendarEvent{
		outage("2026-11-02T08:30:00+02:00", "2026-11-02T15:30:00+02:00"),
		outage("2026-11-02T00:00:00+02:00", "2026-11-02T03:30:00+02:00"),
		{Start: "2026-11-02T18:00:00+02:00", End: "2026-11-02T19:00:00+02:00", Description: "Possible"},
	}
	reordered := []homeassistant.CalendarEvent{a[1], a[0]}
	changed := []homeassistant.CalendarEvent{a[1], outage("2026-11-02T09:00:00+02:00", "2026-11-02T15:30:00+02:00")}

	if ScheduleFingerprint(a) != ScheduleFingerprint(reordered) {
		t.Error("fingerprint must ignore order and non-definite events")
	}
	if ScheduleFingerprint(a) == ScheduleFingerprint(changed) {
		t.Error("fingerprint must change when an outage changes")
	}
	if ScheduleFingerprint(nil) == ScheduleFingerprint(a) {
		t.Error("empty schedule must differ from non-empty")
	}
}
