package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/pahanium/ha-blackout-notify/internal/config"
)

func TestHandleStatsWithoutStatsProvider(t *testing.T) {
	b := &Bot{config: &config.Config{Timezone: "Europe/Kyiv"}}

	response, err := b.handleStats(context.Background(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(response, "not available") {
		t.Errorf("expected 'not available' message, got %q", response)
	}
}
