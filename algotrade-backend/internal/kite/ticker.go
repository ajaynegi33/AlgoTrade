package kite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	tickerURL              = "wss://ws.kite.trade"
	pingPeriod             = 5 * time.Second
	writeWait              = 5 * time.Second
	readWait               = 10 * time.Second
	maxReconnectDelay      = 10 * time.Second
	rawChannelBufferSize   = 2048
)

// TickerStats holds operational metrics for health checks
type TickerStats struct {
	IsConnected      bool      `json:"is_connected"`
	SubscribedTokens int       `json:"subscribed_tokens"`
	PacketsReceived  uint64    `json:"packets_received"`
	LastPacketTime   time.Time `json:"last_packet_time"`
	ReconnectCount   uint64    `json:"reconnect_count"`
}

// TickerClient manages the single shared Kite Ticker WebSocket connection to Zerodha
type TickerClient struct {
	service          *Service
	conn             *websocket.Conn
	mu               sync.Mutex
	tokensMu         sync.RWMutex
	subscribedTokens map[uint32]bool

	rawChan          chan []byte
	isClosed         atomic.Bool
	isConnected      atomic.Bool
	packetsReceived  atomic.Uint64
	reconnectCount   atomic.Uint64
	lastPacketEpoch  atomic.Int64

	callbackMu       sync.RWMutex
	onOrderUpdate    func([]byte)
	onReconnect      func()

	ctx              context.Context
	cancel           context.CancelFunc
}

// NewTickerClient creates a new shared Kite Ticker instance
func NewTickerClient(service *Service) *TickerClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &TickerClient{
		service:          service,
		subscribedTokens: make(map[uint32]bool),
		rawChan:          make(chan []byte, rawChannelBufferSize),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Start begins the auto-reconnecting websocket loop
func (client *TickerClient) Start() {
	go client.connectionLoop()
}

// Stop gracefully shuts down the ticker
func (client *TickerClient) Stop() {
	if client.isClosed.CompareAndSwap(false, true) {
		client.cancel()
		client.mu.Lock()
		if client.conn != nil {
			_ = client.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(writeWait))
			_ = client.conn.Close()
		}
		client.mu.Unlock()
		client.isConnected.Store(false)
		slog.Info("Kite Ticker client stopped")
	}
}

// SubscribeRaw returns the receive-only channel for raw binary frames
func (client *TickerClient) SubscribeRaw() <-chan []byte {
	return client.rawChan
}

// SetOrderUpdateHandler registers a listener for JSON text frames (order postbacks) from Kite Ticker
func (client *TickerClient) SetOrderUpdateHandler(orderUpdateHandler func([]byte)) {
	client.callbackMu.Lock()
	client.onOrderUpdate = orderUpdateHandler
	client.callbackMu.Unlock()
}

// SetReconnectHandler registers a listener invoked whenever Kite Ticker reconnects after a drop
func (client *TickerClient) SetReconnectHandler(reconnectHandler func()) {
	client.callbackMu.Lock()
	client.onReconnect = reconnectHandler
	client.callbackMu.Unlock()
}

// IsConnected returns current connection status
func (client *TickerClient) IsConnected() bool {
	return client.isConnected.Load()
}

// GetStats returns operational health metrics
func (client *TickerClient) GetStats() TickerStats {
	client.tokensMu.RLock()
	tokenCount := len(client.subscribedTokens)
	client.tokensMu.RUnlock()

	lastEpoch := client.lastPacketEpoch.Load()
	var lastTime time.Time
	if lastEpoch > 0 {
		lastTime = time.Unix(lastEpoch, 0)
	}

	return TickerStats{
		IsConnected:      client.isConnected.Load(),
		SubscribedTokens: tokenCount,
		PacketsReceived:  client.packetsReceived.Load(),
		LastPacketTime:   lastTime,
		ReconnectCount:   client.reconnectCount.Load(),
	}
}

// Subscribe adds instrument tokens and sends subscription command to Kite
func (client *TickerClient) Subscribe(tokens []uint32) error {
	if len(tokens) == 0 {
		return nil
	}

	client.tokensMu.Lock()
	for _, token := range tokens {
		client.subscribedTokens[token] = true
	}
	client.tokensMu.Unlock()

	if !client.isConnected.Load() {
		return nil // Stored in map; will be sent automatically upon connection
	}

	return client.sendSubscribeAndMode(tokens)
}

func (client *TickerClient) connectionLoop() {
	backoff := 1 * time.Second

	for {
		select {
		case <-client.ctx.Done():
			return
		default:
		}

		session := client.service.GetSession()
		if session == nil || session.AccessToken == "" {
			// No active session yet; wait 3 seconds and retry
			time.Sleep(3 * time.Second)
			continue
		}

		slog.Info("Connecting to Zerodha Kite Ticker WebSocket...", "user_id", session.UserID)
		err := client.connectAndListen(session)
		if err != nil {
			client.isConnected.Store(false)
			if !client.isClosed.Load() {
				client.reconnectCount.Add(1)
				slog.Warn("Kite Ticker disconnected, retrying with backoff...", "error", err, "backoff", backoff)
				time.Sleep(backoff)
				backoff *= 2
				if backoff > maxReconnectDelay {
					backoff = maxReconnectDelay
				}
			}
		} else {
			backoff = 1 * time.Second
		}
	}
}

func (client *TickerClient) connectAndListen(session *SessionInfo) error {
	parsedURL, err := url.Parse(tickerURL)
	if err != nil {
		return fmt.Errorf("invalid ticker url: %w", err)
	}

	queryParams := parsedURL.Query()
	queryParams.Set("api_key", session.APIKey)
	queryParams.Set("access_token", session.AccessToken)
	parsedURL.RawQuery = queryParams.Encode()

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(client.ctx, parsedURL.String(), nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	client.mu.Lock()
	client.conn = conn
	client.mu.Unlock()

	client.isConnected.Store(true)
	slog.Info("Connected to Zerodha Kite Ticker WebSocket successfully")

	// Resubscribe to all tokens stored in map
	client.tokensMu.RLock()
	activeTokens := make([]uint32, 0, len(client.subscribedTokens))
	for token := range client.subscribedTokens {
		activeTokens = append(activeTokens, token)
	}
	client.tokensMu.RUnlock()

	if len(activeTokens) > 0 {
		if err := client.sendSubscribeAndMode(activeTokens); err != nil {
			slog.Error("Failed to resubscribe tokens on reconnect", "error", err)
		}
	}

	if client.reconnectCount.Load() > 0 {
		client.callbackMu.RLock()
		onReconnectCallback := client.onReconnect
		client.callbackMu.RUnlock()
		if onReconnectCallback != nil {
			go onReconnectCallback()
		}
	}

	// Read pump
	conn.SetReadLimit(65536)
	_ = conn.SetReadDeadline(time.Now().Add(readWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(readWait))
		return nil
	})

	// Start ping sender in background
	pingDone := make(chan struct{})
	go client.pingPump(conn, pingDone)
	defer close(pingDone)

	for {
		msgType, message, readErr := conn.ReadMessage()
		if readErr != nil {
			return readErr
		}

		_ = conn.SetReadDeadline(time.Now().Add(readWait))

		if msgType == websocket.BinaryMessage && len(message) > 2 {
			client.packetsReceived.Add(1)
			client.lastPacketEpoch.Store(time.Now().Unix())

			// Non-blocking send to rawChan
			select {
			case client.rawChan <- message:
			default:
				// Channel buffer full: drop frame to never block the reader
			}
		} else if msgType == websocket.TextMessage && len(message) > 0 {
			client.callbackMu.RLock()
			onOrderUpdateCallback := client.onOrderUpdate
			client.callbackMu.RUnlock()
			if onOrderUpdateCallback != nil {
				messageCopy := make([]byte, len(message))
				copy(messageCopy, message)
				go onOrderUpdateCallback(messageCopy)
			}
		}
	}
}

func (client *TickerClient) pingPump(conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-client.ctx.Done():
			return
		case <-ticker.C:
			client.mu.Lock()
			err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait))
			client.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (client *TickerClient) sendSubscribeAndMode(tokens []uint32) error {
	// Step 1: Subscribe
	subscriptionPayload := map[string]interface{}{
		"a": "subscribe",
		"v": tokens,
	}
	if err := client.writeJSON(subscriptionPayload); err != nil {
		return fmt.Errorf("failed to send subscribe message: %w", err)
	}

	// Step 2: Set Mode to "full" for full 184-byte equity depth & 32-byte index packets
	modePayload := map[string]interface{}{
		"a": "mode",
		"v": []interface{}{"full", tokens},
	}
	if err := client.writeJSON(modePayload); err != nil {
		return fmt.Errorf("failed to send mode message: %w", err)
	}

	slog.Info("Sent subscription & modeFull to Kite Ticker", "token_count", len(tokens))
	return nil
}

func (client *TickerClient) writeJSON(payload interface{}) error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.conn == nil {
		return errors.New("websocket connection is not open")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("json marshal error: %w", err)
	}

	_ = client.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return client.conn.WriteMessage(websocket.TextMessage, data)
}
