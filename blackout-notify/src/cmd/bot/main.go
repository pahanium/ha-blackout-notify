package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/pahanium/ha-blackout-notify/internal/bot"
	"github.com/pahanium/ha-blackout-notify/internal/config"
	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
	"github.com/pahanium/ha-blackout-notify/internal/logger"
	"github.com/pahanium/ha-blackout-notify/internal/notifications"
	"github.com/pahanium/ha-blackout-notify/internal/stats"
	"github.com/pahanium/ha-blackout-notify/internal/watcher"
)

func main() {
	// Load .env for local development (production gets env vars from the add-on run script)
	loadDotEnv()

	// Load configuration from environment variables
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load config: %v", err)
	}

	// Initialize logger with configured level
	logger.SetLevel(cfg.LogLevel)
	logger.Info("Starting Telegram Bot for Home Assistant")
	logger.Debug("Config loaded: HA URL=%s", cfg.HAApiURL)

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
	var statsDB *stats.DB
	var notifSvc *notifications.Service

	// Initialize notification service if Yasno calendar is configured
	// This allows /calendar command to work even without power monitoring
	if cfg.YasnoCalendarID != "" || cfg.IsPowerMonitoringEnabled() {
		notifSvc, err = notifications.NewService(telegramBot.GetAPI(), cfg, haClient)
		if err != nil {
			logger.Fatal("Failed to create notification service: %v", err)
		}
		// Provide notification service to bot for /calendar command
		telegramBot.SetNotificationService(notifSvc)
		logger.Info("Notification service initialized")
	}

	if cfg.IsPowerMonitoringEnabled() {
		logger.Info("Power monitoring enabled for entity: %s", cfg.WatchedEntityID)

		// Initialize statistics database (always enabled)
		logger.Info("Initializing statistics database: %s", cfg.StatsDBPath)
		statsDB, err = stats.NewDB(cfg.StatsDBPath)
		if err != nil {
			logger.Fatal("Failed to initialize statistics database: %v", err)
		}
		statsRecorder := stats.NewRecorder(statsDB, cfg.WatchedEntityID)

		// Provide stats to bot for /stats command
		telegramBot.SetStatsProvider(statsRecorder, statsDB)

		// Initialize WebSocket client for real-time events
		wsClient := homeassistant.NewWSClient(cfg.HAApiURL, cfg.HAToken)

		// Initialize power watcher
		powerWatcher = watcher.NewWatcher(cfg, wsClient, haClient, notifSvc, statsRecorder)

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

	// Close statistics database if open
	if statsDB != nil {
		if err := statsDB.Close(); err != nil {
			logger.Error("Failed to close statistics database: %v", err)
		} else {
			logger.Info("Statistics database closed")
		}
	}

	// Stop Telegram bot
	telegramBot.Stop()
	logger.Info("Bot stopped successfully")
}

// loadDotEnv loads the nearest .env file found in the current directory or its parents,
// so the bot can be started from the repo root or from src/. Existing env vars win.
func loadDotEnv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for {
		path := filepath.Join(dir, ".env")
		if _, err := os.Stat(path); err == nil {
			if err := godotenv.Load(path); err != nil {
				logger.Warn("Failed to load %s: %v", path, err)
			}
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}
