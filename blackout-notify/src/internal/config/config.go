package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all application settings
type Config struct {
	// Telegram settings
	TelegramToken  string
	AllowedChatIDs []int64

	// Home Assistant settings
	HAApiURL string
	HAToken  string

	// General settings
	LogLevel        string
	PollingInterval int

	// Power monitoring settings
	NotificationChatIDs []int64 // Chat IDs for power notifications (can be channels)
	WatchedEntityID     string  // Entity ID of power sensor (e.g., binary_sensor.power)
	NextOnSensorID      string  // Entity ID of sensor with next power on time
	NextOffSensorID     string  // Entity ID of sensor with next power off time
	PauseEntityID       string  // Entity ID of input_boolean to pause notifications

	// Timezone for formatting
	Timezone string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		TelegramToken:   os.Getenv("TELEGRAM_TOKEN"),
		HAApiURL:        getEnvOrDefault("HA_API_URL", "http://supervisor/core/api"),
		HAToken:         os.Getenv("HA_TOKEN"),
		LogLevel:        getEnvOrDefault("LOG_LEVEL", "info"),
		PollingInterval: getEnvAsInt("POLLING_INTERVAL", 30),

		// Power monitoring settings
		WatchedEntityID: os.Getenv("WATCHED_ENTITY_ID"),
		NextOnSensorID:  os.Getenv("NEXT_ON_SENSOR_ID"),
		NextOffSensorID: os.Getenv("NEXT_OFF_SENSOR_ID"),
		PauseEntityID:   getEnvOrDefault("PAUSE_ENTITY_ID", "input_boolean.pause_power_notifications"),
		Timezone:        getEnvOrDefault("TIMEZONE", "Europe/Kyiv"),
	}

	// Parse allowed chat IDs
	chatIDsStr := os.Getenv("ALLOWED_CHAT_IDS")
	if chatIDsStr != "" {
		cfg.AllowedChatIDs = parseChatIDs(chatIDsStr)
	}

	// Parse notification chat IDs (for power notifications)
	notifChatIDsStr := os.Getenv("NOTIFICATION_CHAT_IDS")
	if notifChatIDsStr != "" {
		cfg.NotificationChatIDs = parseChatIDs(notifChatIDsStr)
	}

	return cfg, nil
}

// IsChatAllowed checks if chat ID is allowed
func (c *Config) IsChatAllowed(chatID int64) bool {
	// If list is empty - allow all (unsafe!)
	if len(c.AllowedChatIDs) == 0 {
		return true
	}

	for _, allowed := range c.AllowedChatIDs {
		if allowed == chatID {
			return true
		}
	}
	return false
}

// IsPowerMonitoringEnabled checks if power monitoring is configured
func (c *Config) IsPowerMonitoringEnabled() bool {
	return c.WatchedEntityID != "" && len(c.NotificationChatIDs) > 0
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func parseChatIDs(str string) []int64 {
	// Format: "[123,456,789]" or "123,456,789"
	str = strings.Trim(str, "[]")
	parts := strings.Split(str, ",")

	var ids []int64
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if id, err := strconv.ParseInt(part, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
