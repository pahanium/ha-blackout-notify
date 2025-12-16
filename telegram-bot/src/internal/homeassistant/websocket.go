package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yourusername/haaddon/telegram-bot/internal/logger"
)

// WebSocket message types
const (
	MsgTypeAuthRequired  = "auth_required"
	MsgTypeAuth          = "auth"
	MsgTypeAuthOK        = "auth_ok"
	MsgTypeAuthInvalid   = "auth_invalid"
	MsgTypeSubscribe     = "subscribe_events"
	MsgTypeEvent         = "event"
	MsgTypeResult        = "result"
	EventTypeStateChange = "state_changed"
)

// WSMessage represents a WebSocket message
type WSMessage struct {
	ID          int             `json:"id,omitempty"`
	Type        string          `json:"type"`
	AccessToken string          `json:"access_token,omitempty"`
	EventType   string          `json:"event_type,omitempty"`
	Event       *WSEvent        `json:"event,omitempty"`
	Success     bool            `json:"success,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       *WSError        `json:"error,omitempty"`
}

// WSError represents a WebSocket error
type WSError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WSEvent represents a WebSocket event
type WSEvent struct {
	EventType string           `json:"event_type"`
	Data      *StateChangeData `json:"data,omitempty"`
	Origin    string           `json:"origin"`
	TimeFired string           `json:"time_fired"`
}

// StateChangeData represents state change event data
type StateChangeData struct {
	EntityID string  `json:"entity_id"`
	OldState *Entity `json:"old_state"`
	NewState *Entity `json:"new_state"`
}

// StateChangeHandler is a callback for state changes
type StateChangeHandler func(entityID string, oldState, newState *Entity)

// WSClient is a WebSocket client for Home Assistant
type WSClient struct {
	baseURL        string
	token          string
	conn           *websocket.Conn
	mu             sync.Mutex
	msgID          int
	handlers       map[string][]StateChangeHandler
	handlersMu     sync.RWMutex
	reconnectDelay time.Duration
	maxReconnect   time.Duration
	stopChan       chan struct{}
	connected      bool
}

// NewWSClient creates a new WebSocket client
func NewWSClient(baseURL, token string) *WSClient {
	return &WSClient{
		baseURL:        baseURL,
		token:          token,
		handlers:       make(map[string][]StateChangeHandler),
		reconnectDelay: 5 * time.Second,
		maxReconnect:   5 * time.Minute,
		stopChan:       make(chan struct{}),
	}
}

// getWSURL converts HTTP URL to WebSocket URL
func (c *WSClient) getWSURL() string {
	wsURL := c.baseURL
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.TrimSuffix(wsURL, "/api")
	wsURL = strings.TrimSuffix(wsURL, "/")

	u, err := url.Parse(wsURL)
	if err != nil {
		return wsURL + "/api/websocket"
	}
	u.Path = "/api/websocket"
	return u.String()
}

// Connect establishes WebSocket connection
func (c *WSClient) Connect(ctx context.Context) error {
	wsURL := c.getWSURL()
	logger.Debug("Connecting to WebSocket: %s", wsURL)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.msgID = 0
	c.mu.Unlock()

	// Read auth_required message
	var authReq WSMessage
	if err := conn.ReadJSON(&authReq); err != nil {
		conn.Close()
		return fmt.Errorf("failed to read auth_required: %w", err)
	}

	if authReq.Type != MsgTypeAuthRequired {
		conn.Close()
		return fmt.Errorf("unexpected message type: %s", authReq.Type)
	}

	// Send authentication
	authMsg := WSMessage{
		Type:        MsgTypeAuth,
		AccessToken: c.token,
	}
	if err := conn.WriteJSON(authMsg); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send auth: %w", err)
	}

	// Read auth response
	var authResp WSMessage
	if err := conn.ReadJSON(&authResp); err != nil {
		conn.Close()
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	if authResp.Type == MsgTypeAuthInvalid {
		conn.Close()
		return fmt.Errorf("authentication failed: invalid token")
	}

	if authResp.Type != MsgTypeAuthOK {
		conn.Close()
		return fmt.Errorf("unexpected auth response: %s", authResp.Type)
	}

	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	logger.Info("WebSocket connected and authenticated")
	return nil
}

// SubscribeStateChanges subscribes to state change events
func (c *WSClient) SubscribeStateChanges(ctx context.Context) error {
	c.mu.Lock()
	c.msgID++
	msgID := c.msgID
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := WSMessage{
		ID:        msgID,
		Type:      MsgTypeSubscribe,
		EventType: EventTypeStateChange,
	}

	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// Read subscription result
	var result WSMessage
	if err := conn.ReadJSON(&result); err != nil {
		return fmt.Errorf("failed to read subscription result: %w", err)
	}

	if result.Type == MsgTypeResult && !result.Success {
		errMsg := "unknown error"
		if result.Error != nil {
			errMsg = result.Error.Message
		}
		return fmt.Errorf("subscription failed: %s", errMsg)
	}

	logger.Info("Subscribed to state_changed events")
	return nil
}

// OnStateChange registers a handler for specific entity state changes
func (c *WSClient) OnStateChange(entityID string, handler StateChangeHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[entityID] = append(c.handlers[entityID], handler)
}

// OnAnyStateChange registers a handler for all state changes
func (c *WSClient) OnAnyStateChange(handler StateChangeHandler) {
	c.OnStateChange("*", handler)
}

// Listen starts listening for events
func (c *WSClient) Listen(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopChan:
			return nil
		default:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				return fmt.Errorf("not connected")
			}

			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return nil
				}
				return fmt.Errorf("read error: %w", err)
			}

			if msg.Type == MsgTypeEvent && msg.Event != nil {
				c.handleEvent(msg.Event)
			}
		}
	}
}

// handleEvent processes incoming events
func (c *WSClient) handleEvent(event *WSEvent) {
	if event.EventType != EventTypeStateChange || event.Data == nil {
		return
	}

	entityID := event.Data.EntityID
	oldState := event.Data.OldState
	newState := event.Data.NewState

	c.handlersMu.RLock()
	defer c.handlersMu.RUnlock()

	// Call specific entity handlers
	if handlers, ok := c.handlers[entityID]; ok {
		for _, h := range handlers {
			go h(entityID, oldState, newState)
		}
	}

	// Call wildcard handlers
	if handlers, ok := c.handlers["*"]; ok {
		for _, h := range handlers {
			go h(entityID, oldState, newState)
		}
	}
}

// RunWithReconnect runs the client with automatic reconnection
func (c *WSClient) RunWithReconnect(ctx context.Context) error {
	delay := c.reconnectDelay

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopChan:
			return nil
		default:
		}

		// Connect
		if err := c.Connect(ctx); err != nil {
			logger.Error("WebSocket connection failed: %v, retrying in %v", err, delay)
			time.Sleep(delay)
			delay = min(delay*2, c.maxReconnect)
			continue
		}

		// Reset delay on successful connection
		delay = c.reconnectDelay

		// Subscribe to events
		if err := c.SubscribeStateChanges(ctx); err != nil {
			logger.Error("Failed to subscribe: %v", err)
			c.Close()
			continue
		}

		// Listen for events
		if err := c.Listen(ctx); err != nil {
			logger.Error("WebSocket listen error: %v, reconnecting...", err)
			c.Close()
			continue
		}
	}
}

// Close closes the WebSocket connection
func (c *WSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// Stop stops the client
func (c *WSClient) Stop() {
	close(c.stopChan)
	c.Close()
}

// IsConnected returns connection status
func (c *WSClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
