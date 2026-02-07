package notifications

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/yourusername/haaddon/telegram-bot/internal/config"
	"github.com/yourusername/haaddon/telegram-bot/internal/homeassistant"
	"github.com/yourusername/haaddon/telegram-bot/internal/logger"
)

// Service handles power notifications
type Service struct {
	bot      *tgbotapi.BotAPI
	config   *config.Config
	haClient *homeassistant.Client
	location *time.Location
}

// NewService creates a new notification service
func NewService(bot *tgbotapi.BotAPI, cfg *config.Config, haClient *homeassistant.Client) (*Service, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		logger.Warn("Failed to load timezone %s, using UTC: %v", cfg.Timezone, err)
		loc = time.UTC
	}

	return &Service{
		bot:      bot,
		config:   cfg,
		haClient: haClient,
		location: loc,
	}, nil
}

// NotifyPowerOn sends notification when power is restored
// previousOffDurationSec - how long power was off in seconds (0 if unknown)
func (s *Service) NotifyPowerOn(ctx context.Context, previousOffDurationSec int64) error {
	if s.isPaused(ctx) {
		logger.Debug("Notifications paused, skipping power on notification")
		return nil
	}

	now := time.Now().In(s.location)

	var sb strings.Builder
	sb.WriteString(MsgPowerOn)

	// Add duration if > 60 seconds and < 24 hours
	// (longer durations likely mean the bot was not running and may have missed events)
	if previousOffDurationSec > 60 && previousOffDurationSec < 86400 {
		durationStr := formatDuration(time.Duration(previousOffDurationSec) * time.Second)
		sb.WriteString(fmt.Sprintf("\n%s", fmt.Sprintf(MsgWasOff, durationStr)))
	}

	// Get next scheduled off time
	if s.config.NextOffSensorID != "" {
		nextOff, err := s.getScheduledTime(ctx, s.config.NextOffSensorID)
		if err != nil {
			logger.Warn("Failed to get next off time: %v", err)
		} else if nextOff != nil {
			duration := nextOff.Sub(now)
			sb.WriteString(fmt.Sprintf("\n\n%s\n%s",
				fmt.Sprintf(MsgScheduleOffIn, formatDuration(duration), nextOff.In(s.location).Format("15:04")),
				MsgScheduleSource))
		}
	}

	return s.sendToAllChats(sb.String())
}

// NotifyPowerOff sends notification when power is lost
// previousOnDurationSec - how long power was on in seconds (0 if unknown)
func (s *Service) NotifyPowerOff(ctx context.Context, previousOnDurationSec int64) error {
	if s.isPaused(ctx) {
		logger.Debug("Notifications paused, skipping power off notification")
		return nil
	}

	now := time.Now().In(s.location)

	var sb strings.Builder
	sb.WriteString(MsgPowerOff)

	// Add duration if > 60 seconds and < 24 hours
	// (longer durations likely mean the bot was not running and may have missed events)
	if previousOnDurationSec > 60 && previousOnDurationSec < 86400 {
		durationStr := formatDuration(time.Duration(previousOnDurationSec) * time.Second)
		sb.WriteString(fmt.Sprintf("\n%s", fmt.Sprintf(MsgWasOn, durationStr)))
	}

	// Get next scheduled on time
	if s.config.NextOnSensorID != "" {
		nextOn, err := s.getScheduledTime(ctx, s.config.NextOnSensorID)
		if err != nil {
			logger.Warn("Failed to get next on time: %v", err)
		} else if nextOn != nil {
			duration := nextOn.Sub(now)
			sb.WriteString(fmt.Sprintf("\n\n%s\n%s",
				fmt.Sprintf(MsgScheduleOnIn, formatDuration(duration), nextOn.In(s.location).Format("15:04")),
				MsgScheduleSource))
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
		logger.Debug("Failed to check pause state: %v", err)
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
			logger.Error("Failed to send notification to chat %d: %v", chatID, err)
			lastErr = err
		} else {
			logger.Debug("Sent notification to chat %d", chatID)
		}
	}

	return lastErr
}

// SendCustomMessage sends a custom message to notification chats
func (s *Service) SendCustomMessage(text string) error {
	return s.sendToAllChats(text)
}

// NotifyScheduleChanged sends notification when schedule changes
// scheduleType: "on" for next power on, "off" for next power off
func (s *Service) NotifyScheduleChanged(ctx context.Context, scheduleType string, oldTime, newTime *time.Time) error {
	if s.isPaused(ctx) {
		logger.Debug("Notifications paused, skipping schedule change notification")
		return nil
	}

	now := time.Now().In(s.location)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n\n", MsgScheduleUpdate))

	if scheduleType == "on" {
		if newTime != nil {
			duration := newTime.Sub(now)
			sb.WriteString(fmt.Sprintf("%s\n",
				fmt.Sprintf(MsgScheduleOnIn, formatDuration(duration), newTime.In(s.location).Format("15:04"))))
		}
	} else {
		if newTime != nil {
			duration := newTime.Sub(now)
			sb.WriteString(fmt.Sprintf("%s\n",
				fmt.Sprintf(MsgScheduleOffIn, formatDuration(duration), newTime.In(s.location).Format("15:04"))))
		}
	}

	sb.WriteString(MsgScheduleSource)

	return s.sendToAllChats(sb.String())
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

// NotifyYasnoScheduleTomorrow sends notification about tomorrow's Yasno schedule
func (s *Service) NotifyYasnoScheduleTomorrow(ctx context.Context, events []homeassistant.CalendarEvent, groupName string) error {
	if s.isPaused(ctx) {
		logger.Debug("Notifications paused, skipping Yasno schedule notification")
		return nil
	}

	tomorrow := time.Now().In(s.location).Add(24 * time.Hour)
	dateStr := formatYasnoDate(tomorrow, s.location)

	// Add group name if available
	groupStr := ""
	if groupName != "" {
		groupStr = fmt.Sprintf(", %s", groupName)
	}

	// Filter events with description="Definite" (actual outages)
	var outages []homeassistant.CalendarEvent
	for _, event := range events {
		if event.Description == "Definite" {
			outages = append(outages, event)
		}
	}

	// Build message
	var sb strings.Builder

	if len(outages) == 0 {
		// No outages tomorrow
		sb.WriteString(fmt.Sprintf(MsgYasnoNoOutagesTomorrow, dateStr, groupStr))
	} else {
		// Has outages
		sb.WriteString(fmt.Sprintf(MsgYasnoScheduleTomorrow, dateStr, groupStr))
		sb.WriteString("\n")

		// Format each outage interval
		for _, outage := range outages {
			startTime, err := time.Parse(time.RFC3339, outage.Start)
			if err != nil {
				logger.Warn("Failed to parse outage start time %s: %v", outage.Start, err)
				continue
			}
			endTime, err := time.Parse(time.RFC3339, outage.End)
			if err != nil {
				logger.Warn("Failed to parse outage end time %s: %v", outage.End, err)
				continue
			}

			startTime = startTime.In(s.location)
			endTime = endTime.In(s.location)

			sb.WriteString(fmt.Sprintf("\n"+MsgYasnoOutageInterval,
				startTime.Format("15:04"),
				endTime.Format("15:04")))
		}
	}

	return s.sendToAllChatsWithQuietHours(ctx, sb.String())
}

// NotifyYasnoEmergencyShutdown sends notification about emergency shutdowns
func (s *Service) NotifyYasnoEmergencyShutdown(ctx context.Context) error {
	if s.isPaused(ctx) {
		logger.Debug("Notifications paused, skipping Yasno emergency notification")
		return nil
	}

	return s.sendToAllChats(MsgYasnoEmergencyShutdown)
}

// NotifyYasnoScheduleRestored sends notification when schedule is restored after emergency
func (s *Service) NotifyYasnoScheduleRestored(ctx context.Context) error {
	if s.isPaused(ctx) {
		logger.Debug("Notifications paused, skipping Yasno schedule restored notification")
		return nil
	}

	return s.sendToAllChats(MsgYasnoScheduleRestored)
}

// GetYasnoScheduleState extracts schedule state from entity attributes
func (s *Service) GetYasnoScheduleState(entity *homeassistant.Entity, attributeName string) (string, error) {
	if entity == nil || entity.Attributes == nil {
		return "", fmt.Errorf("entity or attributes are nil")
	}

	state, ok := entity.Attributes[attributeName].(string)
	if !ok {
		return "", fmt.Errorf("attribute %s not found or not a string", attributeName)
	}

	return state, nil
}

// GetYasnoGroupName extracts group name from entity attributes
func (s *Service) GetYasnoGroupName(entity *homeassistant.Entity) string {
	if entity == nil || entity.Attributes == nil {
		return ""
	}

	// Try different possible attribute names for group
	possibleAttrs := []string{"group", "group_name", "queue", "queue_name"}

	for _, attr := range possibleAttrs {
		if value, ok := entity.Attributes[attr]; ok {
			if groupStr, ok := value.(string); ok && groupStr != "" {
				return fmt.Sprintf("група %s", groupStr)
			}
		}
	}

	return ""
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
	now := time.Now().In(s.location)
	hour := now.Hour()

	// Quiet hours: 23:00 (23) to 06:00 (6)
	disableNotification := hour >= 23 || hour < 6

	var lastErr error

	for _, chatID := range s.config.NotificationChatIDs {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.DisableNotification = disableNotification

		if _, err := s.bot.Send(msg); err != nil {
			logger.Error("Failed to send notification to chat %d: %v", chatID, err)
			lastErr = err
		} else {
			if disableNotification {
				logger.Debug("Sent quiet notification to chat %d (hour: %d)", chatID, hour)
			} else {
				logger.Debug("Sent notification to chat %d", chatID)
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
	} else if isSameDay(date, now.Add(24*time.Hour)) {
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
