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

// Icons for messages
const (
	IconPowerOn  = "💡"
	IconPowerOff = "🔌"
	IconTime     = "🕐"
	IconSchedule = "📅"
	IconWarning  = "⚠️"
	IconPause    = "⏸️"
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
func (s *Service) NotifyPowerOn(ctx context.Context) error {
	if s.isPaused(ctx) {
		logger.Debug("Notifications paused, skipping power on notification")
		return nil
	}

	now := time.Now().In(s.location)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s *Світло повернулось!* (%s)\n\n", IconPowerOn, now.Format("15:04")))

	// Get next scheduled off time
	if s.config.NextOffSensorID != "" {
		nextOff, err := s.getScheduledTime(ctx, s.config.NextOffSensorID)
		if err != nil {
			logger.Warn("Failed to get next off time: %v", err)
		} else if nextOff != nil {
			duration := nextOff.Sub(now)
			sb.WriteString(fmt.Sprintf("%s Наступне планове відключення через *%s* (%s)\n",
				IconSchedule,
				formatDuration(duration),
				nextOff.In(s.location).Format("15:04")))
		}
	}

	return s.sendToAllChats(sb.String())
}

// NotifyPowerOff sends notification when power is lost
func (s *Service) NotifyPowerOff(ctx context.Context) error {
	if s.isPaused(ctx) {
		logger.Debug("Notifications paused, skipping power off notification")
		return nil
	}

	now := time.Now().In(s.location)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s *Світло вимкнено* (%s)\n\n", IconPowerOff, now.Format("15:04")))

	// Get next scheduled on time
	if s.config.NextOnSensorID != "" {
		nextOn, err := s.getScheduledTime(ctx, s.config.NextOnSensorID)
		if err != nil {
			logger.Warn("Failed to get next on time: %v", err)
		} else if nextOn != nil {
			duration := nextOn.Sub(now)
			sb.WriteString(fmt.Sprintf("%s Наступне планове включення через *%s* (%s)\n",
				IconSchedule,
				formatDuration(duration),
				nextOn.In(s.location).Format("15:04")))
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

	for _, format := range formats {
		parsedTime, parseErr = time.Parse(format, entity.State)
		if parseErr == nil {
			break
		}
	}

	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse time '%s': %w", entity.State, parseErr)
	}

	// If only time was parsed (no date), use today's date
	if parsedTime.Year() == 0 {
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

// formatDuration formats duration in human-readable Ukrainian
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "невідомо"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours == 0 {
		return fmt.Sprintf("%d хв", minutes)
	}

	if minutes == 0 {
		return fmt.Sprintf("%d год", hours)
	}

	return fmt.Sprintf("%d год %d хв", hours, minutes)
}
