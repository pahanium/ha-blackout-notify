package watcher

import (
	"context"
	"log/slog"
	"time"

	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
	"github.com/pahanium/ha-blackout-notify/internal/notifications"
)

// registerScheduleHandlers subscribes to schedule sensors. Every schedule-related event
// triggers one delayed reconciliation, so a schedule update that touches several sensors
// at once produces a single message.
func (w *Watcher) registerScheduleHandlers(ctx context.Context) {
	trigger := func(string, *homeassistant.Entity, *homeassistant.Entity) { w.requestReconcile(ctx) }

	for _, id := range []string{w.config.NextOnSensorID, w.config.NextOffSensorID, w.config.YasnoTomorrowSensorID} {
		if id != "" {
			w.wsClient.OnStateChange(id, trigger)
		}
	}
	if w.config.YasnoTodaySensorID != "" {
		w.wsClient.OnStateChange(w.config.YasnoTodaySensorID, func(_ string, _, newState *homeassistant.Entity) {
			w.handleTodayStatus(ctx, newState)
			w.requestReconcile(ctx)
		})
	}

	if w.config.NextOnSensorID != "" || w.config.NextOffSensorID != "" || w.config.YasnoCalendarID != "" {
		go w.runPeriodicReconcile(ctx)
	}
}

// initSchedule remembers the current schedule state at startup, so nothing is announced for it.
// On the very first run (empty schedule log) the current schedules are marked as sent.
func (w *Watcher) initSchedule(ctx context.Context) {
	for _, typ := range []string{"on", "off"} {
		sensorID := w.nextTimeSensor(typ)
		if sensorID == "" {
			continue
		}
		t, err := w.notifSvc.GetScheduledTime(ctx, sensorID)
		if err != nil {
			slog.Warn("Failed to get initial schedule time", "type", typ, "error", err)
			continue
		}
		w.mu.Lock()
		w.setNextTime(typ, t)
		w.mu.Unlock()
		slog.Info("Initial schedule time", "type", typ, "time", formatTimePtr(t))
	}

	status := w.readStatus(ctx, w.config.YasnoTodaySensorID)
	w.mu.Lock()
	w.todayStatus = status
	w.emergencyActive = status == notifications.YasnoStatusEmergencyShutdowns
	w.mu.Unlock()

	if w.scheduleLog == nil || w.config.YasnoCalendarID == "" {
		return
	}
	empty, err := w.scheduleLog.IsEmpty(ctx)
	if err != nil {
		slog.Warn("Failed to read schedule log", "error", err)
		return
	}
	if empty {
		now := time.Now().In(w.config.Location)
		for _, day := range []time.Time{now, now.AddDate(0, 0, 1)} {
			if events, err := w.notifSvc.GetYasnoCalendarEvents(ctx, w.config.YasnoCalendarID, day); err == nil {
				w.markSent(ctx, day, notifications.ScheduleFingerprint(events, day, w.config.Location))
			}
		}
		slog.Info("First run: current schedules marked as already sent")
	}

	// Announce schedules that changed while the add-on was not running
	w.requestReconcile(ctx)
}

// handleTodayStatus announces the start of emergency shutdowns immediately (without schedule)
func (w *Watcher) handleTodayStatus(ctx context.Context, newState *homeassistant.Entity) {
	if newState == nil {
		return
	}

	w.mu.Lock()
	switch newState.State {
	case notifications.YasnoStatusEmergencyShutdowns:
		if w.emergencyActive {
			w.mu.Unlock()
			return
		}
		w.emergencyActive = true
		notify := time.Since(w.lastEmergencyNotice) >= w.emergencyNoticeBackoff
		if notify {
			w.lastEmergencyNotice = time.Now()
		}
		powerOff := w.lastState == PowerStateOff
		w.mu.Unlock()

		slog.Info("Yasno: emergency shutdowns started")
		if notify {
			if err := w.notifSvc.NotifyYasnoEmergencyShutdown(ctx, powerOff); err != nil {
				slog.Error("Failed to send Yasno emergency notification", "error", err)
			}
		}
		return
	case notifications.YasnoStatusScheduleApplies, notifications.YasnoStatusWaitingForSchedule:
		w.emergencyActive = false
	}
	w.mu.Unlock()
}

// requestReconcile schedules reconcileSchedule after reconcileDelay, restarting the delay on every event
func (w *Watcher) requestReconcile(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.reconcileTimer != nil {
		w.reconcileTimer.Stop()
	}
	w.reconcileTimer = time.AfterFunc(w.reconcileDelay, func() {
		if ctx.Err() == nil {
			w.reconcileSchedule(ctx)
		}
	})
}

// runPeriodicReconcile catches schedule changes that don't move the next on/off time
func (w *Watcher) runPeriodicReconcile(ctx context.Context) {
	ticker := time.NewTicker(w.reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.reconcileSchedule(ctx)
		}
	}
}

// reconcileSchedule compares the current schedule with what was already announced and sends
// at most one message about today (emergency ended, schedule updated, or today's schedule)
// plus tomorrow's schedule if it is new
func (w *Watcher) reconcileSchedule(ctx context.Context) {
	w.reconcileMu.Lock()
	defer w.reconcileMu.Unlock()

	now := time.Now().In(w.config.Location)
	status := w.readStatus(ctx, w.config.YasnoTodaySensorID)

	w.mu.Lock()
	prevStatus := w.todayStatus
	if status != "" {
		w.todayStatus = status
	}
	power := w.lastState
	w.mu.Unlock()

	emergencyEnded := prevStatus == notifications.YasnoStatusEmergencyShutdowns && status == notifications.YasnoStatusScheduleApplies
	emergencyNow := status == notifications.YasnoStatusEmergencyShutdowns
	changedType, changedTime := w.updateNextTimes(ctx, power, now)

	// Today's outages, only when a regular schedule applies
	var todayEvents []homeassistant.CalendarEvent
	todayApplies := w.config.YasnoCalendarID != "" &&
		(w.config.YasnoTodaySensorID == "" || status == notifications.YasnoStatusScheduleApplies)
	if todayApplies {
		events, err := w.notifSvc.GetYasnoCalendarEvents(ctx, w.config.YasnoCalendarID, now)
		if err != nil {
			slog.Error("Failed to get today's Yasno calendar events", "error", err)
			todayApplies = false
		} else {
			// Non-nil even without events: the schedule is known and has no outages
			todayEvents = append([]homeassistant.CalendarEvent{}, events...)
		}
	}
	todayFingerprint := notifications.ScheduleFingerprint(todayEvents, now, w.config.Location)

	switch {
	case emergencyEnded:
		slog.Info("Yasno: emergency shutdowns ended")
		typ, next := w.relevantNextTime(power)
		if err := w.notifSvc.NotifyEmergencyEnded(ctx, typ, next, todayEvents); err != nil {
			slog.Error("Failed to send emergency ended notification", "error", err)
		} else if todayApplies {
			w.markSent(ctx, now, todayFingerprint)
		}
	case changedType != "" && !emergencyNow:
		if err := w.notifSvc.NotifyScheduleChanged(ctx, changedType, changedTime, todayEvents); err != nil {
			slog.Error("Failed to send schedule change notification", "error", err)
		} else if todayApplies {
			w.markSent(ctx, now, todayFingerprint)
		}
	case todayApplies && w.isNewSchedule(ctx, now, todayFingerprint):
		var err error
		if w.wasSent(ctx, now) {
			// Today's schedule was already announced and changed: same message as a next time change
			slog.Info("Yasno: today's schedule changed")
			typ, next := w.relevantNextTime(power)
			err = w.notifSvc.NotifyScheduleChanged(ctx, typ, next, todayEvents)
		} else {
			slog.Info("Yasno: sending today's schedule")
			err = w.notifSvc.NotifySchedule(ctx, todayEvents, now)
		}
		if err != nil {
			slog.Error("Failed to send today's schedule", "error", err)
		} else {
			w.markSent(ctx, now, todayFingerprint)
		}
	}

	w.reconcileTomorrow(ctx, now)
}

// reconcileTomorrow sends tomorrow's schedule once it is published or when it changes
func (w *Watcher) reconcileTomorrow(ctx context.Context, now time.Time) {
	if w.config.YasnoCalendarID == "" || w.config.YasnoTomorrowSensorID == "" {
		return
	}
	if w.readTomorrowStatus(ctx, now) != notifications.YasnoStatusScheduleApplies {
		return
	}

	tomorrow := now.AddDate(0, 0, 1)
	events, err := w.notifSvc.GetYasnoCalendarEvents(ctx, w.config.YasnoCalendarID, tomorrow)
	if err != nil {
		slog.Error("Failed to get tomorrow's Yasno calendar events", "error", err)
		return
	}
	fingerprint := notifications.ScheduleFingerprint(events, tomorrow, w.config.Location)
	if !w.isNewSchedule(ctx, tomorrow, fingerprint) {
		return
	}

	slog.Info("Yasno: sending tomorrow's schedule", "events", len(events))
	if err := w.notifSvc.NotifySchedule(ctx, events, tomorrow); err != nil {
		slog.Error("Failed to send tomorrow's schedule", "error", err)
		return
	}
	w.markSent(ctx, tomorrow, fingerprint)
}

// updateNextTimes reads next on/off sensors, stores changed times and returns the change worth
// announcing: the type relevant to the current power state, not just a natural move after a
// scheduled change, and on the current day
func (w *Watcher) updateNextTimes(ctx context.Context, power PowerState, now time.Time) (string, *time.Time) {
	var changedType string
	var changedTime *time.Time

	for _, typ := range []string{"on", "off"} {
		sensorID := w.nextTimeSensor(typ)
		if sensorID == "" {
			continue
		}
		newTime, err := w.notifSvc.GetScheduledTime(ctx, sensorID)
		if err != nil {
			slog.Warn("Failed to parse schedule time", "type", typ, "error", err)
			continue
		}

		w.mu.Lock()
		oldTime := w.nextTime(typ)
		if timesEqual(oldTime, newTime) {
			w.mu.Unlock()
			continue
		}
		// A cancelled time (nil) keeps the previous one: it is needed for the suppression below
		if newTime == nil {
			w.mu.Unlock()
			slog.Debug("Schedule cancelled, keeping previous time", "type", typ, "previous", formatTimePtr(oldTime))
			continue
		}
		w.setNextTime(typ, newTime)
		w.mu.Unlock()

		slog.Info("Schedule changed", "type", typ, "from", formatTimePtr(oldTime), "to", formatTimePtr(newTime))

		relevant := (typ == "on" && power == PowerStateOff) || (typ == "off" && power == PowerStateOn)
		if !relevant {
			continue
		}
		if oldTime != nil {
			// The old time just passed: the schedule moved on naturally (power change may be delayed)
			if since := now.Sub(*oldTime); since > -time.Minute && since < 60*time.Minute {
				slog.Debug("Suppressing schedule notification: old time recently passed", "type", typ, "old_time", formatTimePtr(oldTime))
				continue
			}
		}
		if !sameDay(*newTime, now, w.config.Location) {
			slog.Debug("Suppressing schedule notification: new time is not today", "type", typ, "new_time", newTime.Format("2006-01-02 15:04"))
			continue
		}
		changedType, changedTime = typ, newTime
	}

	return changedType, changedTime
}

// relevantNextTime returns the next time worth showing for the current power state
func (w *Watcher) relevantNextTime(power PowerState) (string, *time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch power {
	case PowerStateOn:
		return "off", w.lastNextOffTime
	case PowerStateOff:
		return "on", w.lastNextOnTime
	}
	return "", nil
}

func (w *Watcher) nextTimeSensor(typ string) string {
	if typ == "on" {
		return w.config.NextOnSensorID
	}
	return w.config.NextOffSensorID
}

// nextTime and setNextTime must be called with w.mu held
func (w *Watcher) nextTime(typ string) *time.Time {
	if typ == "on" {
		return w.lastNextOnTime
	}
	return w.lastNextOffTime
}

func (w *Watcher) setNextTime(typ string, t *time.Time) {
	if typ == "on" {
		w.lastNextOnTime = t
	} else {
		w.lastNextOffTime = t
	}
}

// readStatus returns a Yasno status sensor state, or "" if not configured or not available
func (w *Watcher) readStatus(ctx context.Context, sensorID string) string {
	if sensorID == "" || w.haClient == nil {
		return ""
	}
	entity, err := w.haClient.GetState(ctx, sensorID)
	if err != nil {
		slog.Warn("Failed to read Yasno status", "sensor", sensorID, "error", err)
		return ""
	}
	switch entity.State {
	case "", "unknown", "unavailable":
		return ""
	}
	return entity.State
}

// readTomorrowStatus returns the Yasno tomorrow status, or "" if it was last changed before
// today's midnight: until Yasno refreshes after midnight the sensor still describes the day that
// has just become today, and tomorrow's calendar is empty
func (w *Watcher) readTomorrowStatus(ctx context.Context, now time.Time) string {
	sensorID := w.config.YasnoTomorrowSensorID
	if sensorID == "" || w.haClient == nil {
		return ""
	}
	entity, err := w.haClient.GetState(ctx, sensorID)
	if err != nil {
		slog.Warn("Failed to read Yasno status", "sensor", sensorID, "error", err)
		return ""
	}
	switch entity.State {
	case "", "unknown", "unavailable":
		return ""
	}
	if changed, err := time.Parse(time.RFC3339Nano, entity.LastChanged); err == nil {
		day := now.In(w.config.Location)
		midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, w.config.Location)
		if changed.Before(midnight) {
			slog.Debug("Yasno tomorrow status is from yesterday, waiting for refresh",
				"state", entity.State, "last_changed", entity.LastChanged)
			return ""
		}
	}
	return entity.State
}

func dateKey(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

// isNewSchedule reports whether fingerprint differs from the schedule already sent for day.
// A fingerprint stored by an older version can't be compared: it is replaced silently.
func (w *Watcher) isNewSchedule(ctx context.Context, day time.Time, fingerprint string) bool {
	if w.scheduleLog == nil {
		return false
	}
	last, err := w.scheduleLog.LastSent(ctx, dateKey(day, w.config.Location))
	if err != nil {
		slog.Warn("Failed to read schedule log", "error", err)
		return false
	}
	if last != "" && !notifications.IsCurrentFingerprint(last) {
		slog.Info("Upgrading stored schedule fingerprint", "date", dateKey(day, w.config.Location))
		w.markSent(ctx, day, fingerprint)
		return false
	}
	return last != fingerprint
}

// wasSent reports whether a schedule for day was already announced
func (w *Watcher) wasSent(ctx context.Context, day time.Time) bool {
	if w.scheduleLog == nil {
		return false
	}
	last, err := w.scheduleLog.LastSent(ctx, dateKey(day, w.config.Location))
	if err != nil {
		slog.Warn("Failed to read schedule log", "error", err)
		return false
	}
	return last != ""
}

func (w *Watcher) markSent(ctx context.Context, day time.Time, fingerprint string) {
	if w.scheduleLog == nil {
		return
	}
	if err := w.scheduleLog.MarkSent(ctx, dateKey(day, w.config.Location), fingerprint); err != nil {
		slog.Warn("Failed to store sent schedule", "error", err)
	}
}
