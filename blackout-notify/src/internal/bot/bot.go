package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/pahanium/ha-blackout-notify/internal/config"
	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
	"github.com/pahanium/ha-blackout-notify/internal/logger"
	"github.com/pahanium/ha-blackout-notify/internal/notifications"
	"github.com/pahanium/ha-blackout-notify/internal/stats"
)

// NotificationService interface for accessing notification formatting
type NotificationService interface {
	FormatYasnoSchedule(events []homeassistant.CalendarEvent, date time.Time, groupName string) string
}

// Bot represents a Telegram bot
type Bot struct {
	api           *tgbotapi.BotAPI
	config        *config.Config
	haClient      *homeassistant.Client
	statsRecorder *stats.Recorder
	statsDB       *stats.DB
	notifService  NotificationService
	stopChan      chan struct{}
}

// New creates a new Telegram bot
func New(cfg *config.Config, haClient *homeassistant.Client) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	logger.Info("Authorized on account %s", api.Self.UserName)

	return &Bot{
		api:      api,
		config:   cfg,
		haClient: haClient,
		stopChan: make(chan struct{}),
	}, nil
}

// GetAPI returns the underlying Telegram Bot API for use by other services
func (b *Bot) GetAPI() *tgbotapi.BotAPI {
	return b.api
}

// SetStatsProvider sets the statistics provider for the bot
func (b *Bot) SetStatsProvider(recorder *stats.Recorder, db *stats.DB) {
	b.statsRecorder = recorder
	b.statsDB = db
}

// SetNotificationService sets the notification service for the bot
func (b *Bot) SetNotificationService(notifService NotificationService) {
	b.notifService = notifService
}

// Start starts processing messages
func (b *Bot) Start(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.stopChan:
			return nil
		case update := <-updates:
			if update.Message == nil {
				continue
			}

			// Check if chat ID is allowed
			if !b.config.IsChatAllowed(update.Message.Chat.ID) {
				logger.Warn("Unauthorized access attempt from chat ID: %d", update.Message.Chat.ID)
				if len(b.config.AllowedChatIDs) == 0 {
					b.sendMessage(update.Message.Chat.ID, MsgCommandsDisabled)
				} else {
					b.sendMessage(update.Message.Chat.ID, MsgAccessDenied)
				}
				continue
			}

			// Handle the message
			go b.handleMessage(ctx, update.Message)
		}
	}
}

// Stop stops the bot
func (b *Bot) Stop() {
	close(b.stopChan)
	b.api.StopReceivingUpdates()
}

func (b *Bot) handleMessage(ctx context.Context, message *tgbotapi.Message) {
	if !message.IsCommand() {
		b.sendMessage(message.Chat.ID, MsgNotACommand)
		return
	}

	command := message.Command()
	args := message.CommandArguments()

	logger.Debug("Received command: /%s %s from chat %d", command, args, message.Chat.ID)

	var response string
	var err error

	switch command {
	case "start":
		response = b.handleStart()
	case "help":
		response = b.handleHelp()
	case "status":
		response, err = b.handleStatus(ctx)
	case "stats":
		response, err = b.handleStats(ctx, args)
	case "calendar":
		response, err = b.handleCalendar(ctx, args)
	case "chatid":
		response = fmt.Sprintf(MsgChatID, message.Chat.ID)
	default:
		response = fmt.Sprintf(MsgUnknownCommand, tgbotapi.EscapeText(tgbotapi.ModeMarkdown, command))
	}

	if err != nil {
		response = fmt.Sprintf(MsgError, tgbotapi.EscapeText(tgbotapi.ModeMarkdown, err.Error()))
		logger.Error("Command /%s failed: %v", command, err)
	}

	b.sendMessage(message.Chat.ID, response)
}

func (b *Bot) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown

	if _, err := b.api.Send(msg); err != nil {
		logger.Error("Failed to send message: %v", err)
	}
}

func (b *Bot) handleStart() string {
	return MsgStart
}

func (b *Bot) handleHelp() string {
	return MsgHelp
}

func (b *Bot) handleStatus(ctx context.Context) (string, error) {
	if err := b.haClient.CheckConnection(ctx); err != nil {
		logger.Warn("Home Assistant is not reachable: %v", err)
		return MsgStatusUnreachable, nil
	}

	return MsgStatusOK, nil
}

func (b *Bot) handleStats(ctx context.Context, args string) (string, error) {
	// Check if statistics are available (power monitoring may be disabled)
	if b.statsDB == nil || b.statsRecorder == nil {
		return MsgStatsUnavailable, nil
	}

	// Parse period argument
	period := strings.ToLower(strings.TrimSpace(args))
	if period == "" {
		period = "today"
	}

	location := b.config.Location
	var err error

	// Query statistics
	query := stats.NewQuery(b.statsDB, b.config.WatchedEntityID)
	var periodStats *stats.PeriodStats

	switch period {
	case "today", "сьогодні":
		periodStats, err = query.GetStatsToday(ctx, location)
		if err != nil {
			return "", fmt.Errorf("не вдалось отримати статистику: %w", err)
		}
	case "week", "тиждень":
		periodStats, err = query.GetStatsWeek(ctx, location)
		if err != nil {
			return "", fmt.Errorf("не вдалось отримати статистику: %w", err)
		}
	case "month", "місяць":
		periodStats, err = query.GetStatsMonth(ctx, location)
		if err != nil {
			return "", fmt.Errorf("не вдалось отримати статистику: %w", err)
		}
	default:
		return fmt.Sprintf(MsgStatsUnknownArg, tgbotapi.EscapeText(tgbotapi.ModeMarkdown, period)), nil
	}

	// Get current state duration
	currentState, currentDuration, err := b.statsRecorder.GetCurrentStateDuration(ctx)
	if err != nil {
		return "", fmt.Errorf("не вдалось отримати поточний стан: %w", err)
	}

	// Format response
	var periodName string
	switch period {
	case "today", "сьогодні":
		periodName = MsgStatsToday
	case "week", "тиждень":
		periodName = MsgStatsWeek
	case "month", "місяць":
		periodName = MsgStatsMonth
	}

	response := fmt.Sprintf(MsgStatsTitle, periodName)

	// Power on statistics
	if periodStats.TotalOnSeconds > 0 {
		response += fmt.Sprintf(MsgStatsOn, stats.FormatDuration(periodStats.TotalOnSeconds))
		if periodStats.OnCount > 0 {
			avgOn := periodStats.TotalOnSeconds / int64(periodStats.OnCount)
			response += fmt.Sprintf(MsgStatsAverage, stats.FormatDuration(avgOn), periodStats.OnCount)
		}
	} else {
		response += MsgStatsOnNoData
	}

	response += "\n"

	// Power off statistics
	if periodStats.TotalOffSeconds > 0 {
		response += fmt.Sprintf(MsgStatsOff, stats.FormatDuration(periodStats.TotalOffSeconds))
		if periodStats.OffCount > 0 {
			avgOff := periodStats.TotalOffSeconds / int64(periodStats.OffCount)
			response += fmt.Sprintf(MsgStatsAverage, stats.FormatDuration(avgOff), periodStats.OffCount)
		}
	} else {
		response += MsgStatsOffNoData
	}

	// Current state
	if currentState != "unknown" && currentDuration > 0 {
		response += "\n"
		stateIcon := "⚡"
		stateName := MsgStatsCurrentOn
		if currentState == "off" {
			stateIcon = "🔌"
			stateName = MsgStatsCurrentOff
		}
		currentDurationSeconds := int64(currentDuration.Seconds())
		response += fmt.Sprintf("%s *%s:* %s\n", stateIcon, stateName, stats.FormatDuration(currentDurationSeconds))
	}

	// Add usage instructions
	response += MsgStatsUsage

	return response, nil
}

func (b *Bot) handleCalendar(ctx context.Context, args string) (string, error) {
	// Check if notification service is available
	if b.notifService == nil {
		return MsgCalendarUnavailable, nil
	}

	// Check if calendar is configured
	if b.config.YasnoCalendarID == "" {
		return MsgCalendarNotConfigured, nil
	}

	// Parse arguments: /calendar or /calendar tomorrow
	args = strings.ToLower(strings.TrimSpace(args))

	location := b.config.Location
	var targetDate time.Time

	switch args {
	case "", "today", "сьогодні":
		targetDate = time.Now().In(location)
	case "tomorrow", "завтра":
		targetDate = time.Now().In(location).AddDate(0, 0, 1)
	default:
		return MsgCalendarUnknownArg, nil
	}

	// Format date range for the entire day
	startOfDay := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, location)
	endOfDay := startOfDay.AddDate(0, 0, 1)

	startDateTime := startOfDay.Format("2006-01-02T15:04:05")
	endDateTime := endOfDay.Format("2006-01-02T15:04:05")

	logger.Debug("Fetching calendar events: calendar=%s, start=%s, end=%s", b.config.YasnoCalendarID, startDateTime, endDateTime)

	// Get calendar events
	events, err := b.haClient.GetCalendarEvents(ctx, b.config.YasnoCalendarID, startDateTime, endDateTime)
	if err != nil {
		return "", fmt.Errorf("не вдалось отримати події календаря: %w", err)
	}

	logger.Debug("Retrieved %d events", len(events))

	// Extract group name from calendar entity ID
	// Example: calendar.yasno_kiiv_2_1_planned_outages -> "група 2.1"
	groupName := notifications.ExtractYasnoGroupFromCalendarID(b.config.YasnoCalendarID)
	logger.Debug("Group name extracted from calendar ID '%s': %s", b.config.YasnoCalendarID, groupName)

	// Format using the same formatter as automatic notifications
	response := b.notifService.FormatYasnoSchedule(events, targetDate, groupName)

	return response, nil
}
