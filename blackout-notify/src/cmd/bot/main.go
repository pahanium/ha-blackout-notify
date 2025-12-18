package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourusername/haaddon/telegram-bot/internal/bot"
	"github.com/yourusername/haaddon/telegram-bot/internal/config"
	"github.com/yourusername/haaddon/telegram-bot/internal/homeassistant"
	"github.com/yourusername/haaddon/telegram-bot/internal/logger"
	"github.com/yourusername/haaddon/telegram-bot/internal/notifications"
	"github.com/yourusername/haaddon/telegram-bot/internal/watcher"
)

func main() {
	// Load configuration from environment variables
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load config: %v", err)
	}

	// Initialize logger with configured level
	logger.SetLevel(cfg.LogLevel)
	logger.Info("Starting Telegram Bot for Home Assistant")
	logger.Debug("Config loaded: HA URL=%s, Polling=%ds", cfg.HAApiURL, cfg.PollingInterval)

	// Create context with cancellation support
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Home Assistant REST client
	haClient := homeassistant.NewClient(cfg.HAApiURL, cfg.HAToken)

	// Check connection to Home Assistant
	if err := haClient.CheckConnection(ctx); err != nil {
		logger.Fatal("Failed to connect to Home Assistant: %v", err)
	}
	logger.Info("Successfully connected to Home Assistant")

	// Initialize Telegram bot
	telegramBot, err := bot.New(cfg, haClient)
	if err != nil {
		logger.Fatal("Failed to create Telegram bot: %v", err)
	}

	// Handle OS signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start Telegram bot command handler if enabled
	if cfg.IsBotCommandsEnabled() {
		logger.Info("Bot commands enabled for %d chat(s)", len(cfg.AllowedChatIDs))
		go func() {
			if err := telegramBot.Start(ctx); err != nil {
				logger.Error("Bot error: %v", err)
				cancel()
			}
		}()
	} else {
		logger.Info("Bot commands disabled (allowed_chat_ids is empty)")
	}

	// Initialize power monitoring if configured
	var powerWatcher *watcher.Watcher
	if cfg.IsPowerMonitoringEnabled() {
		logger.Info("Power monitoring enabled for entity: %s", cfg.WatchedEntityID)

		// Initialize WebSocket client for real-time events
		wsClient := homeassistant.NewWSClient(cfg.HAApiURL, cfg.HAToken)

		// Initialize notification service
		notifSvc, err := notifications.NewService(telegramBot.GetAPI(), cfg, haClient)
		if err != nil {
			logger.Fatal("Failed to create notification service: %v", err)
		}

		// Initialize power watcher
		powerWatcher = watcher.NewWatcher(cfg, wsClient, haClient, notifSvc)

		// Start power watcher in a separate goroutine
		go func() {
			if err := powerWatcher.Start(ctx); err != nil {
				logger.Error("Power watcher error: %v", err)
			}
		}()

		logger.Info("Power monitoring started")
	} else {
		logger.Info("Power monitoring not configured, skipping")
	}

	logger.Info("Bot is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutdown signal received, stopping services...")
	cancel()

	// Stop power watcher if running
	if powerWatcher != nil {
		powerWatcher.Stop()
		logger.Info("Power watcher stopped")
	}

	// Stop Telegram bot
	telegramBot.Stop()
	logger.Info("Bot stopped successfully")
}
