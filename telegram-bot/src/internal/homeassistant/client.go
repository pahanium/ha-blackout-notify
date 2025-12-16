package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a Home Assistant API client
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Entity represents a Home Assistant entity
type Entity struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	Attributes  map[string]interface{} `json:"attributes"`
	LastChanged string                 `json:"last_changed"`
	LastUpdated string                 `json:"last_updated"`
}

// HAStatus represents Home Assistant status
type HAStatus struct {
	Message string `json:"message"`
}

// ServiceCall represents a service call payload
type ServiceCall struct {
	EntityID string `json:"entity_id"`
}

// NewClient creates a new Home Assistant client
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CheckConnection checks connection to Home Assistant
func (c *Client) CheckConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// GetStates gets all entity states
func (c *Client) GetStates(ctx context.Context) ([]Entity, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/states", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get states: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var entities []Entity
	if err := json.NewDecoder(resp.Body).Decode(&entities); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return entities, nil
}

// GetState gets state of a specific entity
func (c *Client) GetState(ctx context.Context, entityID string) (*Entity, error) {
	url := fmt.Sprintf("%s/states/%s", c.baseURL, entityID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("entity not found: %s", entityID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var entity Entity
	if err := json.NewDecoder(resp.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &entity, nil
}

// CallService calls a Home Assistant service
func (c *Client) CallService(ctx context.Context, domain, service, entityID string) error {
	url := fmt.Sprintf("%s/services/%s/%s", c.baseURL, domain, service)

	payload := ServiceCall{EntityID: entityID}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.setAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("service call failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// TurnOn turns on an entity
func (c *Client) TurnOn(ctx context.Context, entityID string) error {
	domain := getDomain(entityID)
	return c.CallService(ctx, domain, "turn_on", entityID)
}

// TurnOff turns off an entity
func (c *Client) TurnOff(ctx context.Context, entityID string) error {
	domain := getDomain(entityID)
	return c.CallService(ctx, domain, "turn_off", entityID)
}

// Toggle toggles an entity
func (c *Client) Toggle(ctx context.Context, entityID string) error {
	domain := getDomain(entityID)
	return c.CallService(ctx, domain, "toggle", entityID)
}

// GetEntitiesByDomain returns entities of a specific domain
func (c *Client) GetEntitiesByDomain(ctx context.Context, domain string) ([]Entity, error) {
	allEntities, err := c.GetStates(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []Entity
	prefix := domain + "."
	for _, entity := range allEntities {
		if strings.HasPrefix(entity.EntityID, prefix) {
			filtered = append(filtered, entity)
		}
	}

	return filtered, nil
}

func (c *Client) setAuthHeader(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
}

func getDomain(entityID string) string {
	parts := strings.SplitN(entityID, ".", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return "homeassistant"
}
