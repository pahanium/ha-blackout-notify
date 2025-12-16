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

	// Initialize Home Assistant client
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

	// Start bot in a separate goroutine
	go func() {
		if err := telegramBot.Start(ctx); err != nil {
			logger.Error("Bot error: %v", err)
			cancel()
		}
	}()

	logger.Info("Bot is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutdown signal received, stopping bot...")
	cancel()

	// Allow time for graceful shutdown
	telegramBot.Stop()
	logger.Info("Bot stopped successfully")
}
