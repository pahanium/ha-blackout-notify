package watcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pahanium/ha-blackout-notify/internal/config"
	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
)

const testDebounce = 20 * time.Millisecond

// fakeNotifier records calls instead of sending Telegram messages
type fakeNotifier struct {
	mu            sync.Mutex
	powerOn       []int64
	powerOff      []int64
	scheduleCalls []string
	yasnoTomorrow int
	yasnoDates    []time.Time
	emergency     int
	scheduled     map[string]*time.Time
	loc           *time.Location
}

func (f *fakeNotifier) NotifyPowerOn(ctx context.Context, d int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.powerOn = append(f.powerOn, d)
	return nil
}

func (f *fakeNotifier) NotifyPowerOff(ctx context.Context, d int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.powerOff = append(f.powerOff, d)
	return nil
}

func (f *fakeNotifier) NotifyScheduleChanged(ctx context.Context, scheduleType string, oldTime, newTime *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scheduleCalls = append(f.scheduleCalls, scheduleType)
	return nil
}

func (f *fakeNotifier) NotifyYasnoScheduleTomorrow(ctx context.Context, events []homeassistant.CalendarEvent, groupName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.yasnoTomorrow++
	return nil
}

func (f *fakeNotifier) NotifyYasnoEmergencyShutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emergency++
	return nil
}

func (f *fakeNotifier) GetScheduledTime(ctx context.Context, sensorID string) (*time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scheduled[sensorID], nil
}

func (f *fakeNotifier) GetYasnoCalendarEvents(ctx context.Context, calendarID string, date time.Time) ([]homeassistant.CalendarEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.yasnoDates = append(f.yasnoDates, date)
	return nil, nil
}

func (f *fakeNotifier) GetLocation() *time.Location { return f.loc }

func (f *fakeNotifier) counts() (on, off int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.powerOn), len(f.powerOff)
}

// fakeRecorder records state changes and returns a fixed previous duration
type fakeRecorder struct {
	mu        sync.Mutex
	states    []string
	times     []time.Time
	lastState string
	duration  int64
}

func (r *fakeRecorder) RecordStateChange(ctx context.Context, state string, changedAt time.Time) (string, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := r.lastState
	r.states = append(r.states, state)
	r.times = append(r.times, changedAt)
	r.lastState = state
	return prev, r.duration, nil
}

func (r *fakeRecorder) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.states...)
}

func newTestWatcher(t *testing.T, initial PowerState) (*Watcher, *fakeNotifier, *fakeRecorder) {
	t.Helper()
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	n := &fakeNotifier{scheduled: map[string]*time.Time{}, loc: kyiv}
	r := &fakeRecorder{lastState: string(initial), duration: 3600}
	cfg := &config.Config{
		WatchedEntityID:       "binary_sensor.power",
		NextOnSensorID:        "sensor.next_on",
		NextOffSensorID:       "sensor.next_off",
		YasnoTodaySensorID:    "sensor.yasno_today",
		YasnoTomorrowSensorID: "sensor.yasno_tomorrow",
		YasnoCalendarID:       "calendar.yasno_kiiv_2_1_planned_outages",
	}
	w := NewWatcher(cfg, nil, nil, n, r)
	w.debounceTime = testDebounce
	w.lastState = initial
	t.Cleanup(w.Stop)
	return w, n, r
}

func send(w *Watcher, state string) {
	w.handleStateChange(context.Background(), nil, &homeassistant.Entity{EntityID: "binary_sensor.power", State: state})
}

// settle waits until pending debounce timers have fired
func settle() {
	time.Sleep(testDebounce * 4)
}

func TestPowerChange_NotifiesAfterDebounce(t *testing.T) {
	w, n, r := newTestWatcher(t, PowerStateOn)

	send(w, "off")
	if on, off := n.counts(); on+off != 0 {
		t.Fatalf("notification sent before debounce expired: on=%d off=%d", on, off)
	}

	settle()
	on, off := n.counts()
	if on != 0 || off != 1 {
		t.Fatalf("want 1 off notification, got on=%d off=%d", on, off)
	}
	if n.powerOff[0] != 3600 {
		t.Errorf("want previous duration 3600 from recorder, got %d", n.powerOff[0])
	}
	if got := r.recorded(); len(got) != 1 || got[0] != "off" {
		t.Errorf("want recorded [off], got %v", got)
	}
	if w.GetCurrentState() != PowerStateOff {
		t.Errorf("want state off, got %s", w.GetCurrentState())
	}
}

func TestPowerChange_RecordsTimeOfFirstEvent(t *testing.T) {
	w, _, r := newTestWatcher(t, PowerStateOn)

	before := time.Now()
	send(w, "off")
	settle()

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.times) != 1 {
		t.Fatalf("want 1 recorded change, got %d", len(r.times))
	}
	if r.times[0].Before(before) || r.times[0].After(before.Add(testDebounce)) {
		t.Errorf("change time %v should be when the event arrived (%v), not after debounce", r.times[0], before)
	}
}

func TestPowerChange_FlickerIgnored(t *testing.T) {
	w, n, r := newTestWatcher(t, PowerStateOn)

	send(w, "off")
	send(w, "on")
	settle()

	if on, off := n.counts(); on+off != 0 {
		t.Errorf("flicker on->off->on should not notify, got on=%d off=%d", on, off)
	}
	if got := r.recorded(); len(got) != 0 {
		t.Errorf("flicker should not be recorded, got %v", got)
	}
	if w.GetCurrentState() != PowerStateOn {
		t.Errorf("want state on, got %s", w.GetCurrentState())
	}
}

func TestPowerChange_LastStateOfBurstApplied(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)

	// Old implementation dropped every change within 5s of the previous one,
	// so the final "off" could be lost and the bot stayed "on"
	send(w, "off")
	send(w, "on")
	send(w, "off")
	settle()

	if on, off := n.counts(); on != 0 || off != 1 {
		t.Errorf("want exactly 1 off notification, got on=%d off=%d", on, off)
	}
	if w.GetCurrentState() != PowerStateOff {
		t.Errorf("want state off, got %s", w.GetCurrentState())
	}
}

func TestPowerChange_DuplicateEventsDoNotRestartDebounce(t *testing.T) {
	w, n, r := newTestWatcher(t, PowerStateOn)

	first := time.Now()
	send(w, "off")
	time.Sleep(testDebounce / 2)
	send(w, "off") // attribute update with the same state
	settle()

	if _, off := n.counts(); off != 1 {
		t.Fatalf("want 1 off notification, got %d", off)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.times[0].After(first.Add(testDebounce / 4)) {
		t.Errorf("repeated event restarted debounce: recorded %v, first event at %v", r.times[0], first)
	}
}

func TestPowerChange_NoChange(t *testing.T) {
	w, n, r := newTestWatcher(t, PowerStateOn)

	send(w, "on")
	settle()

	if on, off := n.counts(); on+off != 0 {
		t.Errorf("same state should not notify, got on=%d off=%d", on, off)
	}
	if got := r.recorded(); len(got) != 0 {
		t.Errorf("same state should not be recorded, got %v", got)
	}
}

func TestPowerChange_InitialStateIsSilent(t *testing.T) {
	w, n, r := newTestWatcher(t, PowerStateUnknown)

	send(w, "off")
	settle()

	if on, off := n.counts(); on+off != 0 {
		t.Errorf("initial state detection should not notify, got on=%d off=%d", on, off)
	}
	if got := r.recorded(); len(got) != 0 {
		t.Errorf("initial state detection should not be recorded, got %v", got)
	}
	if w.GetCurrentState() != PowerStateOff {
		t.Errorf("want state off, got %s", w.GetCurrentState())
	}
}

func TestPowerChange_ReconnectKeepsDuration(t *testing.T) {
	w, n, r := newTestWatcher(t, PowerStateOff)

	// off -> unavailable -> off during WebSocket reconnect: nothing happened on the grid
	send(w, "unavailable")
	settle()
	send(w, "off")
	settle()

	if on, off := n.counts(); on+off != 0 {
		t.Errorf("reconnect should not notify, got on=%d off=%d", on, off)
	}
	if got := r.recorded(); len(got) != 0 {
		t.Errorf("reconnect should not be recorded, got %v", got)
	}

	send(w, "on")
	settle()
	if on, _ := n.counts(); on != 1 {
		t.Errorf("want 1 on notification after reconnect, got %d", on)
	}
}

func TestPowerChange_OutageDuringReconnectReported(t *testing.T) {
	w, n, r := newTestWatcher(t, PowerStateOn)

	// on -> unknown -> off: power went out while HA was unreachable
	send(w, "unknown")
	settle()
	send(w, "off")
	settle()

	if _, off := n.counts(); off != 1 {
		t.Errorf("outage across reconnect must be reported, got off=%d", off)
	}
	if got := r.recorded(); len(got) != 1 || got[0] != "off" {
		t.Errorf("want recorded [off], got %v", got)
	}
}

func TestPowerChange_MultipleTransitions(t *testing.T) {
	w, n, r := newTestWatcher(t, PowerStateOn)

	for _, s := range []string{"off", "on", "off", "on"} {
		send(w, s)
		settle()
	}

	if on, off := n.counts(); on != 2 || off != 2 {
		t.Errorf("want 2 on + 2 off notifications, got on=%d off=%d", on, off)
	}
	if got := r.recorded(); len(got) != 4 {
		t.Errorf("want 4 recorded changes, got %v", got)
	}
}

func TestPowerChange_StopCancelsPending(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)

	send(w, "off")
	w.Stop()
	settle()

	if on, off := n.counts(); on+off != 0 {
		t.Errorf("pending change must be dropped on Stop, got on=%d off=%d", on, off)
	}
}

func scheduleEvent(w *Watcher, scheduleType string) {
	w.handleScheduleChange(context.Background(), scheduleType, nil, &homeassistant.Entity{State: "changed"})
}

func TestScheduleChange_NotifiesRelevantType(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)
	inOneHour := time.Now().Add(time.Hour)
	if !sameDay(inOneHour, time.Now(), n.loc) {
		t.Skip("next hour is tomorrow in Kyiv; same-day suppression would apply")
	}
	n.scheduled["sensor.next_off"] = &inOneHour

	scheduleEvent(w, "off")

	if len(n.scheduleCalls) != 1 || n.scheduleCalls[0] != "off" {
		t.Errorf("power on + next_off changed: want [off] notification, got %v", n.scheduleCalls)
	}
}

func TestScheduleChange_IrrelevantTypeNotNotified(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)
	inOneHour := time.Now().Add(time.Hour)
	n.scheduled["sensor.next_on"] = &inOneHour

	scheduleEvent(w, "on")

	if len(n.scheduleCalls) != 0 {
		t.Errorf("power on + next_on changed: want no notification, got %v", n.scheduleCalls)
	}
}

func TestScheduleChange_OnAndOffTrackedIndependently(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)
	on := time.Now().Add(2 * time.Hour)
	off := time.Now().Add(time.Hour)
	n.scheduled["sensor.next_on"] = &on
	n.scheduled["sensor.next_off"] = &off

	// Both sensors update at once when a new schedule is published
	scheduleEvent(w, "on")
	scheduleEvent(w, "off")

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lastNextOnTime == nil || w.lastNextOffTime == nil {
		t.Errorf("both schedule times must be stored, got on=%v off=%v", w.lastNextOnTime, w.lastNextOffTime)
	}
}

func TestScheduleChange_CancellationKeepsOldTime(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)
	old := time.Now().Add(-30 * time.Minute)
	w.lastNextOffTime = &old
	n.scheduled["sensor.next_off"] = nil

	scheduleEvent(w, "off")

	if w.lastNextOffTime == nil || !w.lastNextOffTime.Equal(old) {
		t.Errorf("cancelled schedule must keep previous time, got %v", w.lastNextOffTime)
	}
	if len(n.scheduleCalls) != 0 {
		t.Errorf("cancellation must not notify, got %v", n.scheduleCalls)
	}
}

func TestScheduleChange_RecentlyPassedOldTimeSuppressed(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)
	old := time.Now().Add(-30 * time.Minute)
	next := time.Now().Add(time.Hour)
	w.lastNextOffTime = &old
	n.scheduled["sensor.next_off"] = &next

	scheduleEvent(w, "off")

	if len(n.scheduleCalls) != 0 {
		t.Errorf("old time passed 30 min ago: want suppressed, got %v", n.scheduleCalls)
	}
	if w.lastNextOffTime == nil || !w.lastNextOffTime.Equal(next) {
		t.Errorf("new time must be stored even when suppressed, got %v", w.lastNextOffTime)
	}
}

func TestSameDay_UsesLocalMidnight(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	// 00:30 and 23:00 Kyiv on the same date; UTC dates differ (previous day 21:30/22:30 vs 20:00/21:00)
	early := time.Date(2026, 10, 31, 0, 30, 0, 0, kyiv)
	late := time.Date(2026, 10, 31, 23, 0, 0, 0, kyiv)
	nextDay := time.Date(2026, 11, 1, 0, 30, 0, 0, kyiv)

	if !sameDay(early, late, kyiv) {
		t.Error("00:30 and 23:00 Kyiv on the same date must be the same day")
	}
	if sameDay(late, nextDay, kyiv) {
		t.Error("23:00 and next 00:30 Kyiv must be different days")
	}
}

func yasnoEvent(w *Watcher, sensor, state string) {
	e := &homeassistant.Entity{State: state}
	if sensor == "today" {
		w.handleYasnoTodayChange(context.Background(), nil, e)
	} else {
		w.handleYasnoTomorrowChange(context.Background(), nil, e)
	}
}

func TestYasno_TodayChangeDoesNotBlockTomorrow(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)

	yasnoEvent(w, "today", "emergency_shutdowns")
	yasnoEvent(w, "tomorrow", "schedule_applies")

	if n.emergency != 1 {
		t.Errorf("want 1 emergency notification, got %d", n.emergency)
	}
	if n.yasnoTomorrow != 1 {
		t.Errorf("tomorrow schedule must be sent even right after a today change, got %d", n.yasnoTomorrow)
	}
}

func TestYasno_TomorrowUsesConfiguredTimezone(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)

	yasnoEvent(w, "tomorrow", "schedule_applies")

	if len(n.yasnoDates) != 1 {
		t.Fatalf("want 1 calendar request, got %d", len(n.yasnoDates))
	}
	want := time.Now().In(n.loc).AddDate(0, 0, 1).Format("2006-01-02")
	if got := n.yasnoDates[0].In(n.loc).Format("2006-01-02"); got != want {
		t.Errorf("want tomorrow %s in Kyiv, got %s", want, got)
	}
}

func TestYasno_DebouncePerSensor(t *testing.T) {
	w, n, _ := newTestWatcher(t, PowerStateOn)

	yasnoEvent(w, "today", "emergency_shutdowns")
	yasnoEvent(w, "today", "schedule_applies")
	yasnoEvent(w, "today", "emergency_shutdowns")

	if n.emergency != 1 {
		t.Errorf("repeated today changes within debounce should notify once, got %d", n.emergency)
	}
}

func TestNormalizeState(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  PowerState
	}{
		{"on lowercase", "on", PowerStateOn},
		{"on uppercase", "ON", PowerStateOn},
		{"true", "true", PowerStateOn},
		{"1", "1", PowerStateOn},
		{"home", "home", PowerStateOn},
		{"alive", "alive", PowerStateOn},
		{"connected", "connected", PowerStateOn},
		{"off lowercase", "off", PowerStateOff},
		{"off uppercase", "OFF", PowerStateOff},
		{"false", "false", PowerStateOff},
		{"0", "0", PowerStateOff},
		{"not_home", "not_home", PowerStateOff},
		{"dead", "dead", PowerStateOff},
		{"disconnected", "disconnected", PowerStateOff},
		{"unknown", "unknown", PowerStateUnknown},
		{"unavailable", "unavailable", PowerStateUnknown},
		{"empty", "", PowerStateUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeState(tt.input)
			if got != tt.want {
				t.Errorf("normalizeState(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTimesEqual(t *testing.T) {
	// Use a fixed time for predictable test results
	baseTime := time.Date(2026, 1, 4, 14, 30, 0, 0, time.UTC)
	baseTimePlus30Sec := baseTime.Add(30 * time.Second)
	baseTimePlus2Min := baseTime.Add(2 * time.Minute)

	tests := []struct {
		name string
		a    *time.Time
		b    *time.Time
		want bool
	}{
		{"both nil", nil, nil, true},
		{"first nil", nil, &baseTime, false},
		{"second nil", &baseTime, nil, false},
		{"same time", &baseTime, &baseTime, true},
		{"within same minute", &baseTime, &baseTimePlus30Sec, true}, // 14:30:00 and 14:30:30 truncate to same minute
		{"different minutes", &baseTime, &baseTimePlus2Min, false},  // 14:30:00 and 14:32:00 are different minutes
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := timesEqual(tt.a, tt.b)
			if got != tt.want {
				aStr := "nil"
				bStr := "nil"
				if tt.a != nil {
					aStr = tt.a.Format("15:04:05")
				}
				if tt.b != nil {
					bStr = tt.b.Format("15:04:05")
				}
				t.Errorf("timesEqual(%s, %s) = %v, want %v", aStr, bStr, got, tt.want)
			}
		})
	}
}

func TestFormatTimePtr(t *testing.T) {
	tests := []struct {
		name string
		time *time.Time
		want string
	}{
		{
			name: "nil time",
			time: nil,
			want: "nil",
		},
		{
			name: "valid time",
			time: func() *time.Time {
				t := time.Date(2026, 1, 4, 14, 30, 0, 0, time.UTC)
				return &t
			}(),
			want: "14:30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTimePtr(tt.time)
			if got != tt.want {
				t.Errorf("formatTimePtr() = %v, want %v", got, tt.want)
			}
		})
	}
}
