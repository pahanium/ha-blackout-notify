package homeassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient("http://localhost:8123/api", "test_token")

	if client.baseURL != "http://localhost:8123/api" {
		t.Errorf("baseURL = %v, want http://localhost:8123/api", client.baseURL)
	}

	if client.token != "test_token" {
		t.Errorf("token = %v, want test_token", client.token)
	}
}

func TestGetState(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authorization
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/states/light.test" {
			entity := Entity{
				EntityID: "light.test",
				State:    "on",
				Attributes: map[string]interface{}{
					"friendly_name": "Test Light",
					"brightness":    255.0,
				},
			}
			json.NewEncoder(w).Encode(entity)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test_token")
	ctx := context.Background()

	// Test successful retrieval
	entity, err := client.GetState(ctx, "light.test")
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}

	if entity.EntityID != "light.test" {
		t.Errorf("EntityID = %v, want light.test", entity.EntityID)
	}

	if entity.State != "on" {
		t.Errorf("State = %v, want on", entity.State)
	}

	// Test entity not found
	_, err = client.GetState(ctx, "light.nonexistent")
	if err == nil {
		t.Error("GetState() expected error for nonexistent entity")
	}
}

func TestGetDomain(t *testing.T) {
	tests := []struct {
		entityID string
		want     string
	}{
		{"light.living_room", "light"},
		{"switch.bedroom_fan", "switch"},
		{"sensor.temperature", "sensor"},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.entityID, func(t *testing.T) {
			got := getDomain(tt.entityID)
			if got != tt.want {
				t.Errorf("getDomain(%q) = %q, want %q", tt.entityID, got, tt.want)
			}
		})
	}
}

func TestCallService(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.URL.Path == "/services/light/turn_on" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("[]"))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test_token")
	ctx := context.Background()

	err := client.CallService(ctx, "light", "turn_on", "light.test")
	if err != nil {
		t.Errorf("CallService() error = %v", err)
	}
}
