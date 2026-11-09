package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
)

// Yasno status sensor states
const (
	YasnoStatusScheduleApplies     = "schedule_applies"
	YasnoStatusWaitingForSchedule  = "waiting_for_schedule"
	YasnoStatusEmergencyShutdowns  = "emergency_shutdowns"
	definiteOutageEventDescription = "Definite"
)

// anomalyMargin is how far a power change may deviate from the schedule and still be treated as scheduled
const anomalyMargin = 30 * time.Minute

// outageInterval is a planned outage from the Yasno calendar
type outageInterval struct {
	Start time.Time
	End   time.Time
}

// definiteOutages returns confirmed ("Definite") outages sorted by start, in loc
func definiteOutages(events []homeassistant.CalendarEvent, loc *time.Location) []outageInterval {
	var outages []outageInterval
	for _, event := range events {
		if event.Description != definiteOutageEventDescription {
			continue
		}
		start, err := parseEventTime(event.Start, loc)
		if err != nil {
			slog.Warn("Failed to parse outage start time", "value", event.Start, "error", err)
			continue
		}
		end, err := parseEventTime(event.End, loc)
		if err != nil {
			slog.Warn("Failed to parse outage end time", "value", event.End, "error", err)
			continue
		}
		outages = append(outages, outageInterval{Start: start.In(loc), End: end.In(loc)})
	}
	sort.Slice(outages, func(i, j int) bool { return outages[i].Start.Before(outages[j].Start) })
	return outages
}

// mergeOutages joins overlapping and back-to-back intervals (e.g. 22:00-00:00 and 00:00-03:30)
func mergeOutages(outages []outageInterval) []outageInterval {
	var merged []outageInterval
	for _, o := range outages {
		if n := len(merged); n > 0 && !o.Start.After(merged[n-1].End) {
			if o.End.After(merged[n-1].End) {
				merged[n-1].End = o.End
			}
			continue
		}
		merged = append(merged, o)
	}
	return merged
}

// fingerprintVersion prefixes schedule fingerprints. Fingerprints stored by older versions
// (built from raw calendar events) lack it and are replaced silently instead of announced.
const fingerprintVersion = "v2:"

// dayOutages returns confirmed outages of day sorted by start, cut to the day: an outage crossing
// midnight, e.g. 20:30-02:00 from the previous evening, becomes 00:00-02:00
func dayOutages(events []homeassistant.CalendarEvent, day time.Time, loc *time.Location) []outageInterval {
	day = day.In(loc)
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)

	var outages []outageInterval
	for _, o := range definiteOutages(events, loc) {
		if o.Start.Before(dayStart) {
			o.Start = dayStart
		}
		if o.End.After(dayEnd) {
			o.End = dayEnd
		}
		if o.Start.Before(o.End) {
			outages = append(outages, o)
		}
	}
	return outages
}

// ScheduleFingerprint identifies the outages of day as they are shown to users, to detect schedule
// changes. Changes outside the day (e.g. an evening outage extended past midnight once the next
// day's schedule is published) or in how Yasno splits the same outages don't change it.
func ScheduleFingerprint(events []homeassistant.CalendarEvent, day time.Time, loc *time.Location) string {
	var parts []string
	for _, o := range mergeOutages(dayOutages(events, day, loc)) {
		parts = append(parts, o.Start.Format(time.RFC3339)+"/"+o.End.Format(time.RFC3339))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, ";")))
	return fingerprintVersion + hex.EncodeToString(sum[:8])
}

// IsCurrentFingerprint reports whether fingerprint was made by this version of ScheduleFingerprint
func IsCurrentFingerprint(fingerprint string) bool {
	return strings.HasPrefix(fingerprint, fingerprintVersion)
}

// formatOutageLines renders confirmed outages of day as "🪫HH:MM - HH:MM" lines, cut to the day
func (s *Service) formatOutageLines(events []homeassistant.CalendarEvent, day time.Time) string {
	var lines []string
	for _, o := range dayOutages(events, day, s.location) {
		lines = append(lines, fmt.Sprintf(MsgYasnoOutageInterval, o.Start.Format("15:04"), o.End.Format("15:04")))
	}
	return strings.Join(lines, "\n")
}

// isScheduledPowerChange reports whether a power change at now matches the outage schedule.
// For unscheduled (anomalous) changes the next on/off time from the schedule would be misleading.
//
//   - off is scheduled if it happens no earlier than 30 min before a planned outage and before its end
//   - on is anomalous if it happens within 30 min after a planned outage started,
//     or if the whole outage (now - offDuration .. now) did not overlap any planned outage
//
// Without schedule data (no calendar, request failed) every change counts as scheduled,
// so the next time is shown as before. During emergency shutdowns nothing is scheduled.
func (s *Service) isScheduledPowerChange(ctx context.Context, powerOn bool, now time.Time, offDuration time.Duration) bool {
	if s.config.YasnoCalendarID == "" {
		return true
	}
	if s.config.YasnoTodaySensorID != "" {
		if entity, err := s.haClient.GetState(ctx, s.config.YasnoTodaySensorID); err == nil && entity.State == YasnoStatusEmergencyShutdowns {
			return false
		}
	}

	day := now.In(s.location)
	events, err := s.haClient.GetCalendarEvents(ctx, s.config.YasnoCalendarID,
		day.AddDate(0, 0, -1).Format("2006-01-02")+" 00:00:00",
		day.AddDate(0, 0, 1).Format("2006-01-02")+" 23:59:59")
	if err != nil {
		slog.Warn("Failed to get outage schedule, assuming scheduled power change", "error", err)
		return true
	}
	outages := mergeOutages(definiteOutages(events, s.location))

	if !powerOn {
		for _, o := range outages {
			if !now.Before(o.Start.Add(-anomalyMargin)) && now.Before(o.End) {
				return true
			}
		}
		return false
	}

	for _, o := range outages {
		if !now.Before(o.Start) && now.Before(o.Start.Add(anomalyMargin)) {
			return false
		}
	}
	if offDuration > 0 {
		offAt := now.Add(-offDuration)
		for _, o := range outages {
			if o.Start.Before(now) && o.End.After(offAt) {
				return true
			}
		}
		return false
	}
	return true
}

// formatNextTime renders the "next on/off in ..." line with source, or "" if t is nil or already
// passed (power didn't follow the schedule, so the time is meaningless)
func (s *Service) formatNextTime(scheduleType string, t *time.Time, now time.Time) string {
	if t == nil || !t.After(now) {
		return ""
	}
	format := MsgScheduleOffIn
	if scheduleType == "on" {
		format = MsgScheduleOnIn
	}
	return fmt.Sprintf(format, formatDuration(t.Sub(now)), t.In(s.location).Format("15:04")) + "\n" + MsgScheduleSource
}

// buildScheduleUpdate renders a header, the next on/off time and today's outage intervals.
// todayEvents nil means today's schedule is unknown; empty means it is known and has no outages.
func (s *Service) buildScheduleUpdate(header, scheduleType string, nextTime *time.Time, todayEvents []homeassistant.CalendarEvent) string {
	var sb strings.Builder
	sb.WriteString(header)
	now := s.now()
	if next := s.formatNextTime(scheduleType, nextTime, now); next != "" {
		sb.WriteString("\n\n" + next)
	}
	if lines := s.formatOutageLines(todayEvents, now); lines != "" {
		sb.WriteString("\n\n" + lines)
	} else if todayEvents != nil {
		sb.WriteString("\n\n" + MsgYasnoNoOutagesLine)
	}
	return sb.String()
}

// NotifyScheduleChanged sends "schedule updated" with the new next on/off time and today's outages
// scheduleType: "on" for next power on, "off" for next power off
func (s *Service) NotifyScheduleChanged(ctx context.Context, scheduleType string, newTime *time.Time, todayEvents []homeassistant.CalendarEvent) error {
	if s.isPaused(ctx) {
		slog.Debug("Notifications paused, skipping schedule change notification")
		return nil
	}
	return s.sendToAllChats(s.buildScheduleUpdate(MsgScheduleUpdate, scheduleType, newTime, todayEvents))
}

// NotifyEmergencyEnded sends "emergency shutdowns cancelled" with the next on/off time and today's outages
func (s *Service) NotifyEmergencyEnded(ctx context.Context, scheduleType string, nextTime *time.Time, todayEvents []homeassistant.CalendarEvent) error {
	if s.isPaused(ctx) {
		slog.Debug("Notifications paused, skipping emergency ended notification")
		return nil
	}
	return s.sendToAllChats(s.buildScheduleUpdate(MsgYasnoEmergencyEnded, scheduleType, nextTime, todayEvents))
}

// NotifySchedule sends the outage schedule for date in the same format as the /calendar command
func (s *Service) NotifySchedule(ctx context.Context, events []homeassistant.CalendarEvent, date time.Time) error {
	if s.isPaused(ctx) {
		slog.Debug("Notifications paused, skipping schedule notification")
		return nil
	}
	group := ExtractYasnoGroupFromCalendarID(s.config.YasnoCalendarID)
	return s.sendToAllChatsWithQuietHours(ctx, s.FormatYasnoSchedule(events, date, group))
}
