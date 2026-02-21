package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/yourusername/haaddon/telegram-bot/internal/config"
	"github.com/yourusername/haaddon/telegram-bot/internal/homeassistant"
	"github.com/yourusername/haaddon/telegram-bot/internal/logger"
	"github.com/yourusername/haaddon/telegram-bot/internal/notifications"
	"github.com/yourusername/haaddon/telegram-bot/internal/stats"
)

// NotificationService interface for accessing notification formatting
type NotificationService interface {
	FormatYasnoSchedule(events []homeassistant.CalendarEvent, date time.Time, groupName string) string
	GetLocation() *time.Location
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
					b.sendMessage(update.Message.Chat.ID, "⛔ Bot commands are disabled. Configure allowed_chat_ids to enable.")
				} else {
					b.sendMessage(update.Message.Chat.ID, "⛔ Access denied. Your chat ID is not in the allowed list.")
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
		b.sendMessage(message.Chat.ID, "Please use commands. Type /help for available commands.")
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
	case "entities":
		response, err = b.handleEntities(ctx, args)
	case "state":
		response, err = b.handleState(ctx, args)
	case "turn_on", "on":
		response, err = b.handleTurnOn(ctx, args)
	case "turn_off", "off":
		response, err = b.handleTurnOff(ctx, args)
	case "toggle":
		response, err = b.handleToggle(ctx, args)
	case "chatid":
		response = fmt.Sprintf("Your chat ID: `%d`", message.Chat.ID)
	default:
		response = fmt.Sprintf("Unknown command: /%s\nType /help for available commands.", command)
	}

	if err != nil {
		response = fmt.Sprintf("❌ Error: %s", err.Error())
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
	return `🏠 *Welcome to Home Assistant Telegram Bot!*

I can help you control your smart home devices.

Use /help to see available commands.`
}

func (b *Bot) handleHelp() string {
	return `📋 *Available Commands:*

*General:*
/status - Home Assistant status
/stats [period] - Power statistics (today/week/month)
/calendar [tomorrow] - View Yasno schedule (today or tomorrow)
/chatid - Show your chat ID

*Entities:*
/entities [domain] - List entities (optionally filter by domain)
/state <entity_id> - Get entity state

*Control:*
/turn_on <entity_id> - Turn on entity
/turn_off <entity_id> - Turn off entity
/toggle <entity_id> - Toggle entity

*Examples:*
` + "`/entities light`" + `
` + "`/state light.living_room`" + `
` + "`/turn_on switch.bedroom_fan`" + `
` + "`/stats week`" + `
` + "`/calendar tomorrow`"
}

func (b *Bot) handleStatus(ctx context.Context) (string, error) {
	err := b.haClient.CheckConnection(ctx)
	if err != nil {
		return "", fmt.Errorf("Home Assistant is not reachable: %v", err)
	}

	entities, err := b.haClient.GetStates(ctx)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`✅ *Home Assistant Status*

🔗 Connected: Yes
📊 Total entities: %d`, len(entities)), nil
}

func (b *Bot) handleEntities(ctx context.Context, args string) (string, error) {
	var entities []homeassistant.Entity
	var err error

	if args != "" {
		entities, err = b.haClient.GetEntitiesByDomain(ctx, args)
	} else {
		entities, err = b.haClient.GetStates(ctx)
	}

	if err != nil {
		return "", err
	}

	if len(entities) == 0 {
		return "No entities found.", nil
	}

	// Group by domains
	domains := make(map[string]int)
	for _, e := range entities {
		domain := strings.SplitN(e.EntityID, ".", 2)[0]
		domains[domain]++
	}

	var sb strings.Builder
	sb.WriteString("📋 *Entities Summary:*\n\n")

	if args != "" {
		sb.WriteString(fmt.Sprintf("Domain: `%s`\n\n", args))
		// Show first 20 entities
		count := 0
		for _, e := range entities {
			if count >= 20 {
				sb.WriteString(fmt.Sprintf("\n... and %d more", len(entities)-20))
				break
			}
			icon := getStateIcon(e.State)
			sb.WriteString(fmt.Sprintf("%s `%s`: %s\n", icon, e.EntityID, e.State))
			count++
		}
	} else {
		for domain, count := range domains {
			sb.WriteString(fmt.Sprintf("• %s: %d\n", domain, count))
		}
		sb.WriteString(fmt.Sprintf("\nTotal: %d entities", len(entities)))
		sb.WriteString("\n\nUse `/entities <domain>` to list specific domain")
	}

	return sb.String(), nil
}

func (b *Bot) handleState(ctx context.Context, entityID string) (string, error) {
	if entityID == "" {
		return "", fmt.Errorf("please provide entity_id: /state <entity_id>")
	}

	entity, err := b.haClient.GetState(ctx, entityID)
	if err != nil {
		return "", err
	}

	icon := getStateIcon(entity.State)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s *%s*\n\n", icon, entity.EntityID))
	sb.WriteString(fmt.Sprintf("State: `%s`\n", entity.State))

	// Show main attributes
	if name, ok := entity.Attributes["friendly_name"].(string); ok {
		sb.WriteString(fmt.Sprintf("Name: %s\n", name))
	}
	if brightness, ok := entity.Attributes["brightness"].(float64); ok {
		sb.WriteString(fmt.Sprintf("Brightness: %.0f%%\n", brightness/255*100))
	}
	if temp, ok := entity.Attributes["temperature"].(float64); ok {
		sb.WriteString(fmt.Sprintf("Temperature: %.1f\n", temp))
	}
	if unit, ok := entity.Attributes["unit_of_measurement"].(string); ok {
		sb.WriteString(fmt.Sprintf("Unit: %s\n", unit))
	}

	return sb.String(), nil
}

func (b *Bot) handleTurnOn(ctx context.Context, entityID string) (string, error) {
	if entityID == "" {
		return "", fmt.Errorf("please provide entity_id: /turn_on <entity_id>")
	}

	if err := b.haClient.TurnOn(ctx, entityID); err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ Turned ON: `%s`", entityID), nil
}

func (b *Bot) handleTurnOff(ctx context.Context, entityID string) (string, error) {
	if entityID == "" {
		return "", fmt.Errorf("please provide entity_id: /turn_off <entity_id>")
	}

	if err := b.haClient.TurnOff(ctx, entityID); err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ Turned OFF: `%s`", entityID), nil
}

func (b *Bot) handleToggle(ctx context.Context, entityID string) (string, error) {
	if entityID == "" {
		return "", fmt.Errorf("please provide entity_id: /toggle <entity_id>")
	}

	if err := b.haClient.Toggle(ctx, entityID); err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ Toggled: `%s`", entityID), nil
}

func getStateIcon(state string) string {
	switch strings.ToLower(state) {
	case "on":
		return "🟢"
	case "off":
		return "⚫"
	case "unavailable":
		return "❓"
	case "unknown":
		return "❔"
	case "home":
		return "🏠"
	case "not_home", "away":
		return "🚶"
	default:
		return "📍"
	}
}

func (b *Bot) handleStats(ctx context.Context, args string) (string, error) {
	// Parse period argument
	period := strings.ToLower(strings.TrimSpace(args))
	if period == "" {
		period = "today"
	}

	// Get timezone from config
	location, err := time.LoadLocation(b.config.Timezone)
	if err != nil {
		logger.Warn("Failed to load timezone %s, using UTC: %v", b.config.Timezone, err)
		location = time.UTC
	}

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
		return fmt.Sprintf("❌ Невідомий період: %s\n\nВикористовуйте: today, week, month", period), nil
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
		periodName = "Сьогодні"
	case "week", "тиждень":
		periodName = "За тиждень"
	case "month", "місяць":
		periodName = "За місяць"
	}

	response := fmt.Sprintf("📊 *Статистика: %s*\n\n", periodName)

	// Power on statistics
	if periodStats.TotalOnSeconds > 0 {
		response += fmt.Sprintf("⚡ *Світло було:* %s\n", stats.FormatDuration(periodStats.TotalOnSeconds))
		if periodStats.OnCount > 0 {
			avgOn := periodStats.TotalOnSeconds / int64(periodStats.OnCount)
			response += fmt.Sprintf("   Середня тривалість: %s (%d разів)\n", stats.FormatDuration(avgOn), periodStats.OnCount)
		}
	} else {
		response += "⚡ *Світло було:* немає даних\n"
	}

	response += "\n"

	// Power off statistics
	if periodStats.TotalOffSeconds > 0 {
		response += fmt.Sprintf("🔌 *Світла не було:* %s\n", stats.FormatDuration(periodStats.TotalOffSeconds))
		if periodStats.OffCount > 0 {
			avgOff := periodStats.TotalOffSeconds / int64(periodStats.OffCount)
			response += fmt.Sprintf("   Середня тривалість: %s (%d разів)\n", stats.FormatDuration(avgOff), periodStats.OffCount)
		}
	} else {
		response += "🔌 *Світла не було:* немає даних\n"
	}

	// Current state
	if currentState != "unknown" && currentDuration > 0 {
		response += "\n"
		stateIcon := "⚡"
		stateName := "Світло є"
		if currentState == "off" {
			stateIcon = "🔌"
			stateName = "Світла немає"
		}
		currentDurationSeconds := int64(currentDuration.Seconds())
		response += fmt.Sprintf("%s *%s:* %s\n", stateIcon, stateName, stats.FormatDuration(currentDurationSeconds))
	}

	// Add usage instructions
	response += "\n_Використання:_\n`/stats today` - за сьогодні\n`/stats week` - за тиждень\n`/stats month` - за місяць"

	return response, nil
}

func (b *Bot) handleCalendar(ctx context.Context, args string) (string, error) {
	// Check if notification service is available
	if b.notifService == nil {
		return "❌ Notification service not available", nil
	}

	// Check if calendar is configured
	if b.config.YasnoCalendarID == "" {
		return "❌ Yasno calendar not configured. Please set YASNO_CALENDAR_ID in config.", nil
	}

	// Parse arguments: /calendar or /calendar tomorrow
	args = strings.ToLower(strings.TrimSpace(args))

	location := b.notifService.GetLocation()
	var targetDate time.Time

	switch args {
	case "", "today", "сьогодні":
		targetDate = time.Now().In(location)
	case "tomorrow", "завтра":
		targetDate = time.Now().In(location).Add(24 * time.Hour)
	default:
		return "❌ Невірний аргумент. Використовуйте:\n`/calendar` - графік на сьогодні\n`/calendar tomorrow` - графік на завтра", nil
	}

	// Format date range for the entire day
	startOfDay := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, location)
	endOfDay := startOfDay.Add(24 * time.Hour)

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
