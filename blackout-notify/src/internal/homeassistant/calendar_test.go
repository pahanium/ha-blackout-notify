package homeassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetCalendarEvents(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		if r.URL.Path != "/services/calendar/get_events" {
			t.Errorf("Expected path /services/calendar/get_events, got %s", r.URL.Path)
		}

		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test_token" {
			t.Errorf("Expected auth header 'Bearer test_token', got %s", auth)
		}

		// Parse request body
		var req CalendarEventsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		// Verify request fields
		if req.EntityID != "calendar.yasno" {
			t.Errorf("Expected entity_id 'calendar.yasno', got %s", req.EntityID)
		}

		// Send response
		response := CalendarEventsResponse{
			"calendar.yasno": {
				Events: []CalendarEvent{
					{
						Start:       "2026-02-07T00:00:00+00:00",
						End:         "2026-02-07T04:00:00+00:00",
						Summary:     "Outage",
						Description: "Definite",
					},
					{
						Start:       "2026-02-07T07:30:00+00:00",
						End:         "2026-02-07T14:30:00+00:00",
						Summary:     "Outage",
						Description: "Definite",
					},
					{
						Start:       "2026-02-07T18:00:00+00:00",
						End:         "2026-02-08T00:00:00+00:00",
						Summary:     "Outage",
						Description: "Definite",
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := NewClient(server.URL, "test_token")

	// Get calendar events
	events, err := client.GetCalendarEvents(
		context.Background(),
		"calendar.yasno",
		"2026-02-07 00:00:00",
		"2026-02-07 23:59:59",
	)

	if err != nil {
		t.Fatalf("GetCalendarEvents() error = %v", err)
	}

	// Verify events
	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}

	// Verify first event
	if events[0].Description != "Definite" {
		t.Errorf("Expected description 'Definite', got %s", events[0].Description)
	}

	if events[0].Start != "2026-02-07T00:00:00+00:00" {
		t.Errorf("Expected start '2026-02-07T00:00:00+00:00', got %s", events[0].Start)
	}
}

func TestGetCalendarEventsEmpty(t *testing.T) {
	// Create test server that returns empty events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := CalendarEventsResponse{
			"calendar.yasno": {
				Events: []CalendarEvent{},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test_token")

	events, err := client.GetCalendarEvents(
		context.Background(),
		"calendar.yasno",
		"2026-02-07 00:00:00",
		"2026-02-07 23:59:59",
	)

	if err != nil {
		t.Fatalf("GetCalendarEvents() error = %v", err)
	}

	if len(events) != 0 {
		t.Errorf("Expected 0 events, got %d", len(events))
	}
}

func TestGetCalendarEventsNotFound(t *testing.T) {
	// Create test server that returns response without the calendar
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := CalendarEventsResponse{}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test_token")

	events, err := client.GetCalendarEvents(
		context.Background(),
		"calendar.yasno",
		"2026-02-07 00:00:00",
		"2026-02-07 23:59:59",
	)

	if err != nil {
		t.Fatalf("GetCalendarEvents() error = %v", err)
	}

	// Should return empty slice when calendar not found
	if len(events) != 0 {
		t.Errorf("Expected 0 events, got %d", len(events))
	}
}

func TestGetCalendarEventsArrayFormat(t *testing.T) {
	// Create test server that returns array directly (when using ?return_response=true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify return_response query parameter
		if r.URL.Query().Get("return_response") != "true" {
			t.Errorf("Expected return_response=true query parameter")
		}

		// Send direct array response
		response := []CalendarEvent{
			{
				Start:       "2026-02-07T00:00:00+00:00",
				End:         "2026-02-07T04:00:00+00:00",
				Summary:     "Outage",
				Description: "Definite",
			},
			{
				Start:       "2026-02-07T07:30:00+00:00",
				End:         "2026-02-07T14:30:00+00:00",
				Summary:     "Outage",
				Description: "Possible",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := NewClient(server.URL, "test_token")

	// Get calendar events
	events, err := client.GetCalendarEvents(
		context.Background(),
		"calendar.yasno",
		"2026-02-07 00:00:00",
		"2026-02-07 23:59:59",
	)

	if err != nil {
		t.Fatalf("GetCalendarEvents() error = %v", err)
	}

	// Verify events
	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	// Verify first event
	if events[0].Description != "Definite" {
		t.Errorf("Expected description 'Definite', got %s", events[0].Description)
	}

	if events[1].Description != "Possible" {
		t.Errorf("Expected description 'Possible', got %s", events[1].Description)
	}
}

func TestGetCalendarEventsServiceResponse(t *testing.T) {
	// Create test server that returns new service_response format
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return response in new HA format with service_response wrapper
		response := ServiceCallResponse{
			ChangedStates: []interface{}{},
			ServiceResponse: CalendarEventsResponse{
				"calendar.yasno": {
					Events: []CalendarEvent{
						{
							Start:       "2026-02-21T02:00:00+02:00",
							End:         "2026-02-21T05:30:00+02:00",
							Summary:     "Відключення",
							Description: "Definite",
						},
						{
							Start:       "2026-02-21T12:00:00+02:00",
							End:         "2026-02-21T17:00:00+02:00",
							Summary:     "Відключення",
							Description: "Definite",
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := NewClient(server.URL, "test_token")

	// Get calendar events
	events, err := client.GetCalendarEvents(
		context.Background(),
		"calendar.yasno",
		"2026-02-21T00:00:00",
		"2026-02-22T00:00:00",
	)

	if err != nil {
		t.Fatalf("GetCalendarEvents() error = %v", err)
	}

	// Verify events
	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	// Verify event content
	if events[0].Summary != "Відключення" {
		t.Errorf("Expected summary 'Відключення', got %s", events[0].Summary)
	}

	if events[0].Description != "Definite" {
		t.Errorf("Expected description 'Definite', got %s", events[0].Description)
	}
}
