package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
	"github.com/pahanium/ha-blackout-notify/internal/notifications"
)

func definite(start, end string) homeassistant.CalendarEvent {
	return homeassistant.CalendarEvent{Start: start, End: end, Description: "Definite"}
}

// scheduleDays returns today's and tomorrow's date keys in Kyiv
func scheduleDays(env *testEnv) (today, tomorrow time.Time, todayKey, tomorrowKey string) {
	today = time.Now().In(env.notifier.loc)
	tomorrow = today.AddDate(0, 0, 1)
	return today, tomorrow, today.Format("2006-01-02"), tomorrow.Format("2006-01-02")
}

func fingerprint(env *testEnv, day time.Time, events []homeassistant.CalendarEvent) string {
	return notifications.ScheduleFingerprint(events, day, env.notifier.loc)
}

func reconcile(w *Watcher) {
	w.reconcileSchedule(context.Background())
}

func (f *fakeNotifier) snapshot() (schedules, changed, ended []string, emergency int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.schedules...), append([]string(nil), f.scheduleChanged...),
		append([]string(nil), f.emergencyEnded...), f.emergency
}

func TestReconcile_TomorrowSentOnceAndNotRepeatedAtMidnight(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, tomorrow, todayKey, tomorrowKey := scheduleDays(env)
	tomorrowEvents := []homeassistant.CalendarEvent{definite(tomorrowKey+"T01:00:00+02:00", tomorrowKey+"T08:00:00+02:00")}
	env.notifier.events[tomorrowKey] = tomorrowEvents
	env.log.sent[todayKey] = fingerprint(env, time.Now(), nil)
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	env.states.set("sensor.yasno_tomorrow", notifications.YasnoStatusScheduleApplies)

	reconcile(w)
	reconcile(w)

	schedules, _, _, _ := env.notifier.snapshot()
	if len(schedules) != 1 || schedules[0] != tomorrowKey {
		t.Fatalf("want tomorrow's schedule sent exactly once, got %v", schedules)
	}

	// Next day the same schedule becomes "today": its fingerprint is already logged under that date
	if env.log.sent[tomorrowKey] != fingerprint(env, tomorrow, tomorrowEvents) {
		t.Errorf("tomorrow's schedule must be logged under its own date")
	}
}

func TestReconcile_TodaySentWhenNotSentYesterday(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, _ := scheduleDays(env)
	env.notifier.events[todayKey] = []homeassistant.CalendarEvent{definite(todayKey+"T19:00:00+02:00", todayKey+"T23:00:00+02:00")}
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	env.states.set("sensor.yasno_tomorrow", notifications.YasnoStatusWaitingForSchedule)

	reconcile(w)
	reconcile(w)

	schedules, _, _, _ := env.notifier.snapshot()
	if len(schedules) != 1 || schedules[0] != todayKey {
		t.Errorf("want today's schedule once, got %v", schedules)
	}
}

func TestReconcile_TodayResentWhenChanged(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, _ := scheduleDays(env)
	old := []homeassistant.CalendarEvent{definite(todayKey+"T19:00:00+02:00", todayKey+"T23:00:00+02:00")}
	env.log.sent[todayKey] = fingerprint(env, time.Now(), old)
	env.notifier.events[todayKey] = []homeassistant.CalendarEvent{definite(todayKey+"T18:00:00+02:00", todayKey+"T23:00:00+02:00")}
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)

	reconcile(w)

	schedules, changed, _, _ := env.notifier.snapshot()
	if len(schedules) != 0 || len(changed) != 1 || changed[0] != "off" {
		t.Errorf("changed today's schedule: want one \"schedule updated\" with next off, got schedules=%v changed=%v", schedules, changed)
	}
	if got := env.log.sent[todayKey]; got != fingerprint(env, time.Now(), env.notifier.events[todayKey]) {
		t.Errorf("new today's schedule must be marked as sent")
	}
}

func TestReconcile_EmergencyWhilePowerOff(t *testing.T) {
	for _, power := range []PowerState{PowerStateOn, PowerStateOff} {
		w, env := newScheduleWatcher(t, power)
		w.handleTodayStatus(context.Background(), &homeassistant.Entity{State: notifications.YasnoStatusEmergencyShutdowns})

		env.notifier.mu.Lock()
		alerts, powerOff := env.notifier.emergency, env.notifier.emergencyPowerOff
		env.notifier.mu.Unlock()
		if alerts != 1 || powerOff != (power == PowerStateOff) {
			t.Errorf("power %s: want 1 alert with powerOff=%v, got %d alerts powerOff=%v", power, power == PowerStateOff, alerts, powerOff)
		}
	}
}

func TestReconcile_TomorrowWaitingNotSent(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, _ := scheduleDays(env)
	env.log.sent[todayKey] = fingerprint(env, time.Now(), nil)
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	env.states.set("sensor.yasno_tomorrow", notifications.YasnoStatusWaitingForSchedule)

	reconcile(w)

	if schedules, _, _, _ := env.notifier.snapshot(); len(schedules) != 0 {
		t.Errorf("no schedule for tomorrow yet: want nothing, got %v", schedules)
	}
}

func TestReconcile_TomorrowStatusFromYesterdayNotSent(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	today, _, todayKey, tomorrowKey := scheduleDays(env)
	env.log.sent[todayKey] = fingerprint(env, time.Now(), nil)
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	// Just after midnight: still yesterday's "tomorrow applies", tomorrow's calendar is empty
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, env.notifier.loc)
	env.states.setChanged("sensor.yasno_tomorrow", notifications.YasnoStatusScheduleApplies, midnight.Add(-2*time.Hour))

	reconcile(w)

	if schedules, _, _, _ := env.notifier.snapshot(); len(schedules) != 0 {
		t.Errorf("stale tomorrow status: want nothing, got %v", schedules)
	}

	// Yasno publishes tomorrow's schedule later today
	env.states.setChanged("sensor.yasno_tomorrow", notifications.YasnoStatusScheduleApplies, midnight.Add(time.Minute))
	reconcile(w)

	if schedules, _, _, _ := env.notifier.snapshot(); len(schedules) != 1 || schedules[0] != tomorrowKey {
		t.Errorf("fresh tomorrow status: want [%s], got %v", tomorrowKey, schedules)
	}
}

func TestReconcile_EmergencyStartOnceThenEndedWithSchedule(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, _ := scheduleDays(env)
	env.notifier.events[todayKey] = []homeassistant.CalendarEvent{definite(todayKey+"T19:00:00+02:00", todayKey+"T23:00:00+02:00")}
	env.states.set("sensor.yasno_today", notifications.YasnoStatusEmergencyShutdowns)
	emergency := &homeassistant.Entity{State: notifications.YasnoStatusEmergencyShutdowns}

	w.handleTodayStatus(context.Background(), emergency)
	w.handleTodayStatus(context.Background(), emergency)
	reconcile(w)

	schedules, changed, ended, alerts := env.notifier.snapshot()
	if alerts != 1 {
		t.Errorf("want 1 emergency alert, got %d", alerts)
	}
	if len(schedules)+len(changed)+len(ended) != 0 {
		t.Errorf("emergency start must not send schedule: schedules=%v changed=%v ended=%v", schedules, changed, ended)
	}

	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	w.handleTodayStatus(context.Background(), &homeassistant.Entity{State: notifications.YasnoStatusScheduleApplies})
	reconcile(w)
	reconcile(w)

	schedules, _, ended, _ = env.notifier.snapshot()
	if len(ended) != 1 || ended[0] != "off" {
		t.Errorf("want one emergency-ended message with next off time (power on), got %v", ended)
	}
	if len(schedules) != 0 {
		t.Errorf("today's schedule is part of the emergency-ended message, got extra %v", schedules)
	}
}

func TestReconcile_EmergencyAlertNotRepeatedOnFlap(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	ctx := context.Background()

	w.handleTodayStatus(ctx, &homeassistant.Entity{State: notifications.YasnoStatusEmergencyShutdowns})
	w.handleTodayStatus(ctx, &homeassistant.Entity{State: notifications.YasnoStatusScheduleApplies})
	w.handleTodayStatus(ctx, &homeassistant.Entity{State: notifications.YasnoStatusEmergencyShutdowns})

	if _, _, _, alerts := env.notifier.snapshot(); alerts != 1 {
		t.Errorf("flapping status must not repeat the alert within backoff, got %d", alerts)
	}
}

func TestReconcile_ScheduleChangedSingleMessage(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, _ := scheduleDays(env)
	now := time.Now()
	newOff := now.Add(10 * time.Minute)
	if !sameDay(newOff, now, env.notifier.loc) {
		t.Skip("too close to midnight")
	}
	oldOff := now.Add(5 * time.Hour)
	w.lastNextOffTime = &oldOff
	env.notifier.scheduled["sensor.next_off"] = &newOff
	env.notifier.events[todayKey] = []homeassistant.CalendarEvent{definite(todayKey+"T19:00:00+02:00", todayKey+"T23:00:00+02:00")}
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)

	reconcile(w)
	reconcile(w)

	schedules, changed, _, _ := env.notifier.snapshot()
	if len(changed) != 1 || changed[0] != "off" {
		t.Errorf("power on + next off moved: want one 'off' schedule update, got %v", changed)
	}
	if len(schedules) != 0 {
		t.Errorf("schedule update already contains today's intervals, got extra %v", schedules)
	}
}

func TestReconcile_IrrelevantNextTimeNotAnnounced(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, _ := scheduleDays(env)
	env.log.sent[todayKey] = fingerprint(env, time.Now(), nil)
	nextOn := time.Now().Add(time.Hour)
	env.notifier.scheduled["sensor.next_on"] = &nextOn

	reconcile(w)

	if _, changed, _, _ := env.notifier.snapshot(); len(changed) != 0 {
		t.Errorf("power on + next on moved: want no message, got %v", changed)
	}
	if w.lastNextOnTime == nil || !w.lastNextOnTime.Equal(nextOn) {
		t.Errorf("next on time must still be stored, got %v", w.lastNextOnTime)
	}
}

func TestReconcile_RecentlyPassedOldTimeSuppressed(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, _ := scheduleDays(env)
	env.log.sent[todayKey] = fingerprint(env, time.Now(), nil)
	old := time.Now().Add(-30 * time.Minute)
	next := time.Now().Add(time.Hour)
	w.lastNextOffTime = &old
	env.notifier.scheduled["sensor.next_off"] = &next

	reconcile(w)

	if _, changed, _, _ := env.notifier.snapshot(); len(changed) != 0 {
		t.Errorf("old time passed 30 min ago: want suppressed, got %v", changed)
	}
	if w.lastNextOffTime == nil || !w.lastNextOffTime.Equal(next) {
		t.Errorf("new time must be stored even when suppressed, got %v", w.lastNextOffTime)
	}
}

func TestReconcile_CancellationKeepsOldTime(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	old := time.Now().Add(-30 * time.Minute)
	w.lastNextOffTime = &old

	reconcile(w)

	if w.lastNextOffTime == nil || !w.lastNextOffTime.Equal(old) {
		t.Errorf("cancelled schedule must keep previous time, got %v", w.lastNextOffTime)
	}
	if _, changed, _, _ := env.notifier.snapshot(); len(changed) != 0 {
		t.Errorf("cancellation must not notify, got %v", changed)
	}
}

func TestReconcile_NoScheduleUpdateDuringEmergency(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	w.todayStatus = notifications.YasnoStatusEmergencyShutdowns
	env.states.set("sensor.yasno_today", notifications.YasnoStatusEmergencyShutdowns)
	oldOff := time.Now().Add(5 * time.Hour)
	newOff := time.Now().Add(10 * time.Minute)
	w.lastNextOffTime = &oldOff
	env.notifier.scheduled["sensor.next_off"] = &newOff

	reconcile(w)

	if schedules, changed, ended, _ := env.notifier.snapshot(); len(schedules)+len(changed)+len(ended) != 0 {
		t.Errorf("during emergency nothing schedule-related is sent: schedules=%v changed=%v ended=%v", schedules, changed, ended)
	}
}

func TestReconcile_BurstOfEventsReconcilesOnce(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, _ := scheduleDays(env)
	env.notifier.events[todayKey] = []homeassistant.CalendarEvent{definite(todayKey+"T19:00:00+02:00", todayKey+"T23:00:00+02:00")}
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		w.requestReconcile(ctx)
	}
	settle()

	if schedules, _, _, _ := env.notifier.snapshot(); len(schedules) != 1 {
		t.Errorf("want one message for a burst of events, got %v", schedules)
	}
}

func TestInitSchedule_FirstRunMarksCurrentAsSent(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, tomorrowKey := scheduleDays(env)
	env.notifier.events[todayKey] = []homeassistant.CalendarEvent{definite(todayKey+"T19:00:00+02:00", todayKey+"T23:00:00+02:00")}
	env.notifier.events[tomorrowKey] = []homeassistant.CalendarEvent{definite(tomorrowKey+"T01:00:00+02:00", tomorrowKey+"T08:00:00+02:00")}
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	env.states.set("sensor.yasno_tomorrow", notifications.YasnoStatusScheduleApplies)

	w.initSchedule(context.Background())
	settle()

	if schedules, _, _, _ := env.notifier.snapshot(); len(schedules) != 0 {
		t.Errorf("first run must not announce current schedules, got %v", schedules)
	}
}

func TestReconcile_TomorrowUsesConfiguredTimezone(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, tomorrowKey := scheduleDays(env)
	env.log.sent[todayKey] = fingerprint(env, time.Now(), nil)
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	env.states.set("sensor.yasno_tomorrow", notifications.YasnoStatusScheduleApplies)

	reconcile(w)

	env.notifier.mu.Lock()
	defer env.notifier.mu.Unlock()
	last := env.notifier.yasnoDates[len(env.notifier.yasnoDates)-1]
	if got := last.In(env.notifier.loc).Format("2006-01-02"); got != tomorrowKey {
		t.Errorf("want tomorrow %s in Kyiv, got %s", tomorrowKey, got)
	}
}

func TestReconcile_YesterdaysTomorrowNotResentAsToday(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	_, _, todayKey, _ := scheduleDays(env)
	events := []homeassistant.CalendarEvent{definite(todayKey+"T01:00:00+02:00", todayKey+"T08:00:00+02:00")}
	env.notifier.events[todayKey] = events
	// Sent yesterday as "tomorrow" under today's date
	env.log.sent[todayKey] = fingerprint(env, time.Now(), events)
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	env.states.set("sensor.yasno_tomorrow", notifications.YasnoStatusWaitingForSchedule)

	reconcile(w)

	if schedules, _, _, _ := env.notifier.snapshot(); len(schedules) != 0 {
		t.Errorf("schedule already sent as tomorrow must not be repeated, got %v", schedules)
	}
}

func TestReconcile_MidnightCrossingOutageReshapedNotAnnounced(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOff)
	today, _, todayKey, _ := scheduleDays(env)
	loc := env.notifier.loc
	at := func(dayOffset, hour, minute int) string {
		return time.Date(today.Year(), today.Month(), today.Day()+dayOffset, hour, minute, 0, 0, loc).Format(time.RFC3339)
	}
	// Sent yesterday as "tomorrow": the evening outage ended at midnight
	sent := []homeassistant.CalendarEvent{
		definite(at(0, 0, 0), at(0, 4, 0)),
		definite(at(0, 7, 30), at(0, 15, 0)),
		definite(at(0, 17, 30), at(1, 0, 0)),
	}
	env.log.sent[todayKey] = fingerprint(env, today, sent)
	// After Yasno's refresh the same day comes as yesterday's outage extended past midnight and
	// tonight's one extended into tomorrow: the outages shown for today are unchanged
	env.notifier.events[todayKey] = []homeassistant.CalendarEvent{
		definite(at(-1, 17, 30), at(0, 4, 0)),
		definite(at(0, 7, 30), at(0, 11, 0)),
		definite(at(0, 11, 0), at(0, 15, 0)),
		definite(at(0, 17, 30), at(1, 4, 0)),
	}
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	env.states.set("sensor.yasno_tomorrow", notifications.YasnoStatusWaitingForSchedule)

	reconcile(w)

	if schedules, changed, _, _ := env.notifier.snapshot(); len(schedules)+len(changed) != 0 {
		t.Errorf("same outages for today: want no message, got schedules=%v changed=%v", schedules, changed)
	}
}

func TestReconcile_OldFingerprintUpgradedSilently(t *testing.T) {
	w, env := newScheduleWatcher(t, PowerStateOn)
	today, _, todayKey, _ := scheduleDays(env)
	events := []homeassistant.CalendarEvent{definite(todayKey+"T19:00:00+02:00", todayKey+"T23:00:00+02:00")}
	env.notifier.events[todayKey] = events
	env.log.sent[todayKey] = "0123456789abcdef" // made by an older version
	env.states.set("sensor.yasno_today", notifications.YasnoStatusScheduleApplies)
	env.states.set("sensor.yasno_tomorrow", notifications.YasnoStatusWaitingForSchedule)

	reconcile(w)

	if schedules, changed, _, _ := env.notifier.snapshot(); len(schedules)+len(changed) != 0 {
		t.Errorf("old fingerprint: want no message, got schedules=%v changed=%v", schedules, changed)
	}
	if got := env.log.sent[todayKey]; got != fingerprint(env, today, events) {
		t.Errorf("old fingerprint must be replaced, got %q", got)
	}
}
