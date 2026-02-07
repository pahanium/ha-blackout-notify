package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// CalendarEvent represents a single calendar event
type CalendarEvent struct {
	Start       string `json:"start"`
	End         string `json:"end"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

// CalendarEventsRequest represents the request to get calendar events
type CalendarEventsRequest struct {
	EntityID      string `json:"entity_id"`
	StartDateTime string `json:"start_date_time"`
	EndDateTime   string `json:"end_date_time"`
}

// CalendarEventsResponse represents the response from calendar.get_events service
// Response structure: {"calendar.entity_id": {"events": [...]}}
type CalendarEventsResponse map[string]struct {
	Events []CalendarEvent `json:"events"`
}

// GetCalendarEvents retrieves calendar events for a specific date range
func (c *Client) GetCalendarEvents(ctx context.Context, calendarID, startDateTime, endDateTime string) ([]CalendarEvent, error) {
	url := fmt.Sprintf("%s/services/calendar/get_events", c.baseURL)

	// Create request payload
	payload := CalendarEventsRequest{
		EntityID:      calendarID,
		StartDateTime: startDateTime,
		EndDateTime:   endDateTime,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call calendar service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("calendar service call failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var response CalendarEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode calendar response: %w", err)
	}

	// Extract events for the requested calendar
	if calendarData, ok := response[calendarID]; ok {
		return calendarData.Events, nil
	}

	return []CalendarEvent{}, nil
}
