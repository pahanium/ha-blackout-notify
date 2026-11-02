package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	_ "time/tzdata" // embed timezone database so TIMEZONE works without OS tzdata

	"github.com/joho/godotenv"
	"github.com/pahanium/ha-blackout-notify/internal/bot"
	"github.com/pahanium/ha-blackout-notify/internal/config"
	"github.com/pahanium/ha-blackout-notify/internal/homeassistant"
	"github.com/pahanium/ha-blackout-notify/internal/notifications"
	"github.com/pahanium/ha-blackout-notify/internal/stats"
	"github.com/pahanium/ha-blackout-notify/internal/watcher"
)

func main() {
	// Log at info level until the configured level is known
	slog.SetDefault(newLogger("info"))

	// Load .env for local development (production gets env vars from the add-on run script)
	loadDotEnv()

	// Load configuration from environment variables
	cfg, err := config.Load()
	if err != nil {
		fatal("Failed to load config", "error", err)
	}

	slog.SetDefault(newLogger(cfg.LogLevel))
	slog.Info("Starting Blackout Notify")
	slog.Debug("Config loaded", "ha_url", cfg.HAApiURL, "timezone", cfg.Timezone)

	// Create context with cancellation support
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Home Assistant REST client
	haClient := homeassistant.NewClient(cfg.HAApiURL, cfg.HAToken)

	// Check connection to Home Assistant
	if err := haClient.CheckConnection(ctx); err != nil {
		fatal("Failed to connect to Home Assistant", "error", err)
	}
	slog.Info("Connected to Home Assistant")

	// Initialize Telegram bot
	telegramBot, err := bot.New(cfg, haClient)
	if err != nil {
		fatal("Failed to create Telegram bot", "error", err)
	}

	// Handle OS signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start Telegram bot command handler if enabled
	if cfg.IsBotCommandsEnabled() {
		slog.Info("Bot commands enabled", "chats", len(cfg.AllowedChatIDs))
		go func() {
			if err := telegramBot.Start(ctx); err != nil {
				slog.Error("Bot stopped with error", "error", err)
				cancel()
			}
		}()
	} else {
		slog.Info("Bot commands disabled (allowed_chat_ids is empty)")
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
			fatal("Failed to create notification service", "error", err)
		}
		// Provide notification service to bot for /calendar command
		telegramBot.SetNotificationService(notifSvc)
		slog.Info("Notification service initialized")
	}

	if cfg.IsPowerMonitoringEnabled() {
		slog.Info("Power monitoring enabled", "entity", cfg.WatchedEntityID)

		// Initialize statistics database (always enabled)
		slog.Info("Opening statistics database", "path", cfg.StatsDBPath)
		statsDB, err = stats.NewDB(cfg.StatsDBPath)
		if err != nil {
			fatal("Failed to initialize statistics database", "error", err)
		}
		statsRecorder := stats.NewRecorder(statsDB, cfg.WatchedEntityID)

		// Provide stats to bot for /stats command
		telegramBot.SetStatsProvider(statsRecorder, statsDB)

		// Initialize WebSocket client for real-time events
		wsClient := homeassistant.NewWSClient(cfg.HAApiURL, cfg.HAToken)

		// Initialize power watcher
		powerWatcher = watcher.NewWatcher(cfg, wsClient, haClient, notifSvc, statsRecorder, stats.NewScheduleLog(statsDB))

		// Start power watcher in a separate goroutine
		go func() {
			if err := powerWatcher.Start(ctx); err != nil {
				slog.Error("Power watcher stopped with error", "error", err)
			}
		}()

		slog.Info("Power monitoring started")
	} else {
		slog.Info("Power monitoring not configured, skipping")
	}

	slog.Info("Bot is running")

	// Wait for shutdown signal
	<-sigChan
	slog.Info("Shutdown signal received, stopping services")
	cancel()

	// Stop power watcher if running
	if powerWatcher != nil {
		powerWatcher.Stop()
		slog.Info("Power watcher stopped")
	}

	// Close statistics database if open
	if statsDB != nil {
		if err := statsDB.Close(); err != nil {
			slog.Error("Failed to close statistics database", "error", err)
		} else {
			slog.Info("Statistics database closed")
		}
	}

	// Stop Telegram bot
	telegramBot.Stop()
	slog.Info("Bot stopped")
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
				slog.Warn("Failed to load .env", "path", path, "error", err)
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

// newLogger creates a text logger on stdout with the given level (debug, info, warn, error)
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

// fatal logs an error and exits
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
