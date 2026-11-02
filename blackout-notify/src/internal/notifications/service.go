package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/pahanium/ha-blackout-notify/internal/config"
	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
)

// Sender sends Telegram messages (implemented by *tgbotapi.BotAPI)
type Sender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

// HAReader reads Home Assistant data (implemented by *homeassistant.Client)
type HAReader interface {
	GetState(ctx context.Context, entityID string) (*homeassistant.Entity, error)
	GetCalendarEvents(ctx context.Context, calendarID, startDateTime, endDateTime string) ([]homeassistant.CalendarEvent, error)
}

// Service handles power notifications
type Service struct {
	bot      Sender
	config   *config.Config
	haClient HAReader
	location *time.Location
	now      func() time.Time
}

// NewService creates a new notification service
func NewService(bot Sender, cfg *config.Config, haClient HAReader) (*Service, error) {
	return &Service{
		bot:      bot,
		config:   cfg,
		haClient: haClient,
		location: cfg.Location,
		now:      time.Now,
	}, nil
}

// NotifyPowerOn sends notification when power is restored
// previousOffDurationSec - how long power was off in seconds (0 if unknown)
func (s *Service) NotifyPowerOn(ctx context.Context, previousOffDurationSec int64) error {
	if s.isPaused(ctx) {
		slog.Debug("Notifications paused, skipping power on notification")
		return nil
	}

	now := time.Now().In(s.location)

	var sb strings.Builder
	sb.WriteString(MsgPowerOn)

	// Add duration if > 60 seconds and < 24 hours
	// (longer durations likely mean the bot was not running and may have missed events)
	if previousOffDurationSec > 60 && previousOffDurationSec < 86400 {
		durationStr := formatDuration(time.Duration(previousOffDurationSec) * time.Second)
		fmt.Fprintf(&sb, "\n%s", fmt.Sprintf(MsgWasOff, durationStr))
	}

	// Next scheduled off time, unless power came back outside the schedule
	offDuration := time.Duration(previousOffDurationSec) * time.Second
	if s.config.NextOffSensorID != "" && s.isScheduledPowerChange(ctx, true, now, offDuration) {
		nextOff, err := s.getScheduledTime(ctx, s.config.NextOffSensorID)
		if err != nil {
			slog.Warn("Failed to get next off time", "error", err)
		} else if next := s.formatNextTime("off", nextOff, now); next != "" {
			sb.WriteString("\n\n" + next)
		}
	}

	return s.sendToAllChats(sb.String())
}

// NotifyPowerOff sends notification when power is lost
// previousOnDurationSec - how long power was on in seconds (0 if unknown)
func (s *Service) NotifyPowerOff(ctx context.Context, previousOnDurationSec int64) error {
	if s.isPaused(ctx) {
		slog.Debug("Notifications paused, skipping power off notification")
		return nil
	}

	now := time.Now().In(s.location)

	var sb strings.Builder
	sb.WriteString(MsgPowerOff)

	// Add duration if > 60 seconds and < 24 hours
	// (longer durations likely mean the bot was not running and may have missed events)
	if previousOnDurationSec > 60 && previousOnDurationSec < 86400 {
		durationStr := formatDuration(time.Duration(previousOnDurationSec) * time.Second)
		fmt.Fprintf(&sb, "\n%s", fmt.Sprintf(MsgWasOn, durationStr))
	}

	// Next scheduled on time, unless power went out outside the schedule
	if s.config.NextOnSensorID != "" && s.isScheduledPowerChange(ctx, false, now, 0) {
		nextOn, err := s.getScheduledTime(ctx, s.config.NextOnSensorID)
		if err != nil {
			slog.Warn("Failed to get next on time", "error", err)
		} else if next := s.formatNextTime("on", nextOn, now); next != "" {
			sb.WriteString("\n\n" + next)
		}
	}

	return s.sendToAllChats(sb.String())
}

// getScheduledTime retrieves and parses time from a sensor
func (s *Service) getScheduledTime(ctx context.Context, sensorID string) (*time.Time, error) {
	entity, err := s.haClient.GetState(ctx, sensorID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sensor state: %w", err)
	}

	if entity.State == "" || entity.State == "unknown" || entity.State == "unavailable" {
		return nil, nil
	}

	// Try different time formats used by Home Assistant
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000000+00:00",
		"2006-01-02T15:04:05+00:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"15:04:05",
		"15:04",
	}

	var parsedTime time.Time
	var parseErr error
	var parsedFormat string

	for _, format := range formats {
		parsedTime, parseErr = time.Parse(format, entity.State)
		if parseErr == nil {
			parsedFormat = format
			break
		}
	}

	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse time '%s': %w", entity.State, parseErr)
	}

	// If only time was parsed (no date), use today's date in local timezone
	// Check if format was time-only (15:04:05 or 15:04)
	isTimeOnly := parsedFormat == "15:04:05" || parsedFormat == "15:04"
	if isTimeOnly || parsedTime.Year() == 0 {
		now := time.Now().In(s.location)
		parsedTime = time.Date(
			now.Year(), now.Month(), now.Day(),
			parsedTime.Hour(), parsedTime.Minute(), parsedTime.Second(),
			0, s.location,
		)
		// If the time has already passed today, assume it's tomorrow
		if parsedTime.Before(now) {
			parsedTime = parsedTime.Add(24 * time.Hour)
		}
	} else {
		// Convert to local timezone if it has a date
		parsedTime = parsedTime.In(s.location)
	}

	return &parsedTime, nil
}

// isPaused checks if notifications are paused via HA input_boolean
func (s *Service) isPaused(ctx context.Context) bool {
	if s.config.PauseEntityID == "" {
		return false
	}

	entity, err := s.haClient.GetState(ctx, s.config.PauseEntityID)
	if err != nil {
		slog.Debug("Failed to check pause state", "error", err)
		return false
	}

	return entity.State == "on"
}

// sendToAllChats sends message to all notification chat IDs
func (s *Service) sendToAllChats(text string) error {
	var lastErr error

	for _, chatID := range s.config.NotificationChatIDs {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeMarkdown

		if _, err := s.bot.Send(msg); err != nil {
			slog.Error("Failed to send notification", "chat_id", chatID, "error", err)
			lastErr = err
		} else {
			slog.Debug("Sent notification", "chat_id", chatID)
		}
	}

	return lastErr
}

// SendCustomMessage sends a custom message to notification chats
func (s *Service) SendCustomMessage(text string) error {
	return s.sendToAllChats(text)
}

// GetScheduledTime is a public wrapper for getScheduledTime
func (s *Service) GetScheduledTime(ctx context.Context, sensorID string) (*time.Time, error) {
	return s.getScheduledTime(ctx, sensorID)
}

// formatDuration formats duration in human-readable Ukrainian, rounded to minutes
// Format: "6год 25хв" (no spaces to save message width)
func formatDuration(d time.Duration) string {
	// If duration is in the past (negative), it's an error - don't show anything
	if d < 0 {
		return "невідомо"
	}

	// Round to nearest minute
	totalMinutes := int(d.Round(time.Minute).Minutes())

	// Handle edge case where rounding results in 0
	if totalMinutes == 0 {
		return "менше хвилини"
	}

	hours := totalMinutes / 60
	minutes := totalMinutes % 60

	if hours == 0 {
		return fmt.Sprintf("%dхв", minutes)
	}

	if minutes == 0 {
		return fmt.Sprintf("%dгод", hours)
	}

	return fmt.Sprintf("%dгод %dхв", hours, minutes)
}

// NotifyYasnoEmergencyShutdown sends notification about emergency shutdowns
func (s *Service) NotifyYasnoEmergencyShutdown(ctx context.Context) error {
	if s.isPaused(ctx) {
		slog.Debug("Notifications paused, skipping Yasno emergency notification")
		return nil
	}

	return s.sendToAllChats(MsgYasnoEmergencyShutdown)
}

// GetYasnoCalendarEvents retrieves calendar events for a specific date
func (s *Service) GetYasnoCalendarEvents(ctx context.Context, calendarID string, date time.Time) ([]homeassistant.CalendarEvent, error) {
	// Format date range for the entire day
	startDateTime := date.Format("2006-01-02") + " 00:00:00"
	endDateTime := date.Format("2006-01-02") + " 23:59:59"

	events, err := s.haClient.GetCalendarEvents(ctx, calendarID, startDateTime, endDateTime)
	if err != nil {
		return nil, fmt.Errorf("failed to get calendar events: %w", err)
	}

	return events, nil
}

// sendToAllChatsWithQuietHours sends message to all chats, disabling notification during quiet hours (23:00-06:00)
func (s *Service) sendToAllChatsWithQuietHours(ctx context.Context, text string) error {
	now := s.now().In(s.location)
	hour := now.Hour()

	// Quiet hours: 23:00 (23) to 06:00 (6)
	disableNotification := hour >= 23 || hour < 6

	var lastErr error

	for _, chatID := range s.config.NotificationChatIDs {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.DisableNotification = disableNotification

		if _, err := s.bot.Send(msg); err != nil {
			slog.Error("Failed to send notification", "chat_id", chatID, "error", err)
			lastErr = err
		} else {
			if disableNotification {
				slog.Debug("Sent silent notification (quiet hours)", "chat_id", chatID, "hour", hour)
			} else {
				slog.Debug("Sent notification", "chat_id", chatID)
			}
		}
	}

	return lastErr
}

// formatYasnoDate formats date in Ukrainian for Yasno notifications
// Examples: "завтра сб, 07.02", "сьогодні пт, 06.02"
func formatYasnoDate(date time.Time, loc *time.Location) string {
	date = date.In(loc)
	now := time.Now().In(loc)

	// Ukrainian day names (short)
	dayNames := []string{"нд", "пн", "вт", "ср", "чт", "пт", "сб"}
	dayName := dayNames[date.Weekday()]

	// Check if today or tomorrow
	var prefix string
	if isSameDay(date, now) {
		prefix = "сьогодні"
	} else if isSameDay(date, now.AddDate(0, 0, 1)) {
		prefix = "завтра"
	} else {
		// Use date as prefix for other days
		prefix = date.Format("02.01")
	}

	return fmt.Sprintf("%s %s, %s", prefix, dayName, date.Format("02.01"))
}

// isSameDay checks if two times are on the same day
func isSameDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// FormatYasnoSchedule formats Yasno calendar events in the same style as automatic notifications
// date - target date to format schedule for (today or tomorrow)
func (s *Service) FormatYasnoSchedule(events []homeassistant.CalendarEvent, date time.Time, groupName string) string {
	date = date.In(s.location)
	dateStr := formatYasnoDate(date, s.location)

	// Add group name if available
	groupStr := ""
	if groupName != "" {
		groupStr = fmt.Sprintf(", %s", groupName)
	}

	lines := s.formatOutageLines(events)
	if lines == "" {
		return fmt.Sprintf(MsgYasnoNoOutages, dateStr, groupStr)
	}
	return fmt.Sprintf(MsgYasnoSchedule, dateStr, groupStr) + "\n\n" + lines
}

// ExtractYasnoGroupFromCalendarID extracts group number from calendar entity ID
// Examples:
//
//	calendar.yasno_kiiv_2_1_planned_outages -> "група 2.1"
//	calendar.yasno_kyiv_3_2_planned_outages -> "група 3.2"
//	calendar.other_calendar -> ""
func ExtractYasnoGroupFromCalendarID(calendarID string) string {
	if calendarID == "" {
		return ""
	}

	// Remove "calendar." prefix if present
	name := calendarID
	if idx := strings.Index(name, "."); idx >= 0 {
		name = name[idx+1:]
	}

	// Look for pattern like "2_1" or "3_2" in the name
	// Expected format: yasno_kiiv_X_Y_planned_outages where X and Y are digits
	parts := strings.Split(name, "_")

	// Find consecutive digit parts
	for i := 0; i < len(parts)-1; i++ {
		// Check if current and next parts are single digits
		if len(parts[i]) == 1 && len(parts[i+1]) == 1 {
			// Check if both are digits
			if parts[i][0] >= '0' && parts[i][0] <= '9' &&
				parts[i+1][0] >= '0' && parts[i+1][0] <= '9' {
				return fmt.Sprintf("група %s.%s", parts[i], parts[i+1])
			}
		}
	}

	return ""
}
