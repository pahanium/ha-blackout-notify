package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Set test environment variables
	os.Setenv("TELEGRAM_TOKEN", "test_token")
	os.Setenv("HA_TOKEN", "ha_test_token")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("POLLING_INTERVAL", "60")
	os.Setenv("ALLOWED_CHAT_IDS", "123,456,789")
	defer func() {
		os.Unsetenv("TELEGRAM_TOKEN")
		os.Unsetenv("HA_TOKEN")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("POLLING_INTERVAL")
		os.Unsetenv("ALLOWED_CHAT_IDS")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TelegramToken != "test_token" {
		t.Errorf("TelegramToken = %v, want test_token", cfg.TelegramToken)
	}

	if cfg.HAToken != "ha_test_token" {
		t.Errorf("HAToken = %v, want ha_test_token", cfg.HAToken)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}

	if cfg.PollingInterval != 60 {
		t.Errorf("PollingInterval = %v, want 60", cfg.PollingInterval)
	}

	if len(cfg.AllowedChatIDs) != 3 {
		t.Errorf("AllowedChatIDs length = %v, want 3", len(cfg.AllowedChatIDs))
	}
}

func TestIsChatAllowed(t *testing.T) {
	tests := []struct {
		name       string
		allowedIDs []int64
		chatID     int64
		want       bool
	}{
		{
			name:       "allowed chat",
			allowedIDs: []int64{123, 456, 789},
			chatID:     456,
			want:       true,
		},
		{
			name:       "not allowed chat",
			allowedIDs: []int64{123, 456, 789},
			chatID:     999,
			want:       false,
		},
		{
			name:       "empty list denies all (security)",
			allowedIDs: []int64{},
			chatID:     999,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{AllowedChatIDs: tt.allowedIDs}
			if got := cfg.IsChatAllowed(tt.chatID); got != tt.want {
				t.Errorf("IsChatAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBotCommandsEnabled(t *testing.T) {
	tests := []struct {
		name       string
		allowedIDs []int64
		want       bool
	}{
		{
			name:       "enabled with chat IDs",
			allowedIDs: []int64{123, 456},
			want:       true,
		},
		{
			name:       "disabled when empty",
			allowedIDs: []int64{},
			want:       false,
		},
		{
			name:       "disabled when nil",
			allowedIDs: nil,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{AllowedChatIDs: tt.allowedIDs}
			if got := cfg.IsBotCommandsEnabled(); got != tt.want {
				t.Errorf("IsBotCommandsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseChatIDs(t *testing.T) {
	tests := []struct {
		input string
		want  []int64
	}{
		{"123,456,789", []int64{123, 456, 789}},
		{"[123,456,789]", []int64{123, 456, 789}},
		{"123", []int64{123}},
		{"", nil},
		{"abc,123,xyz", []int64{123}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseChatIDs(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseChatIDs(%q) length = %d, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseChatIDs(%q)[%d] = %d, want %d", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
