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

// GetLocation returns the configured timezone location
func (s *Service) GetLocation() *time.Location {
	return s.location
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
