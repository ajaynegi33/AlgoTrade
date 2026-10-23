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
func (t *TickerClient) Start() {
	go t.connectionLoop()
}

// Stop gracefully shuts down the ticker
func (t *TickerClient) Stop() {
	if t.isClosed.CompareAndSwap(false, true) {
		t.cancel()
		t.mu.Lock()
		if t.conn != nil {
			_ = t.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(writeWait))
			_ = t.conn.Close()
		}
		t.mu.Unlock()
		t.isConnected.Store(false)
		slog.Info("Kite Ticker client stopped")
	}
}

// SubscribeRaw returns the receive-only channel for raw binary frames
func (t *TickerClient) SubscribeRaw() <-chan []byte {
	return t.rawChan
}

// IsConnected returns current connection status
func (t *TickerClient) IsConnected() bool {
	return t.isConnected.Load()
}

// GetStats returns operational health metrics
func (t *TickerClient) GetStats() TickerStats {
	t.tokensMu.RLock()
	tokenCount := len(t.subscribedTokens)
	t.tokensMu.RUnlock()

	lastEpoch := t.lastPacketEpoch.Load()
	var lastTime time.Time
	if lastEpoch > 0 {
		lastTime = time.Unix(lastEpoch, 0)
	}

	return TickerStats{
		IsConnected:      t.isConnected.Load(),
		SubscribedTokens: tokenCount,
		PacketsReceived:  t.packetsReceived.Load(),
		LastPacketTime:   lastTime,
		ReconnectCount:   t.reconnectCount.Load(),
	}
}

// Subscribe adds instrument tokens and sends subscription command to Kite
func (t *TickerClient) Subscribe(tokens []uint32) error {
	if len(tokens) == 0 {
		return nil
	}

	t.tokensMu.Lock()
	for _, tok := range tokens {
		t.subscribedTokens[tok] = true
	}
	t.tokensMu.Unlock()

	if !t.isConnected.Load() {
		return nil // Stored in map; will be sent automatically upon connection
	}

	return t.sendSubscribeAndMode(tokens)
}

// Unsubscribe removes instrument tokens and sends unsubscribe command
func (t *TickerClient) Unsubscribe(tokens []uint32) error {
	if len(tokens) == 0 {
		return nil
	}

	t.tokensMu.Lock()
	for _, tok := range tokens {
		delete(t.subscribedTokens, tok)
	}
	t.tokensMu.Unlock()

	if !t.isConnected.Load() {
		return nil
	}

	payload := map[string]interface{}{
		"a": "unsubscribe",
		"v": tokens,
	}
	return t.writeJSON(payload)
}

func (t *TickerClient) connectionLoop() {
	backoff := 1 * time.Second

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		session := t.service.GetSession()
		if session == nil || session.AccessToken == "" {
			// No active session yet; wait 3 seconds and retry
			time.Sleep(3 * time.Second)
			continue
		}

		slog.Info("Connecting to Zerodha Kite Ticker WebSocket...", "user_id", session.UserID)
		err := t.connectAndListen(session)
		if err != nil {
			t.isConnected.Store(false)
			if !t.isClosed.Load() {
				t.reconnectCount.Add(1)
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

func (t *TickerClient) connectAndListen(session *SessionInfo) error {
	u, err := url.Parse(tickerURL)
	if err != nil {
		return fmt.Errorf("invalid ticker url: %w", err)
	}

	q := u.Query()
	q.Set("api_key", session.APIKey)
	q.Set("access_token", session.AccessToken)
	u.RawQuery = q.Encode()

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(t.ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()

	t.isConnected.Store(true)
	slog.Info("Connected to Zerodha Kite Ticker WebSocket successfully")

	// Resubscribe to all tokens stored in map
	t.tokensMu.RLock()
	activeTokens := make([]uint32, 0, len(t.subscribedTokens))
	for tok := range t.subscribedTokens {
		activeTokens = append(activeTokens, tok)
	}
	t.tokensMu.RUnlock()

	if len(activeTokens) > 0 {
		if err := t.sendSubscribeAndMode(activeTokens); err != nil {
			slog.Error("Failed to resubscribe tokens on reconnect", "error", err)
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
	go t.pingPump(conn, pingDone)
	defer close(pingDone)

	for {
		msgType, message, readErr := conn.ReadMessage()
		if readErr != nil {
			return readErr
		}

		_ = conn.SetReadDeadline(time.Now().Add(readWait))

		if msgType == websocket.BinaryMessage {
			t.packetsReceived.Add(1)
			t.lastPacketEpoch.Store(time.Now().Unix())

			// Non-blocking send to rawChan
			select {
			case t.rawChan <- message:
			default:
				// Channel buffer full: drop frame to never block the reader
			}
		}
	}
}

func (t *TickerClient) pingPump(conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.mu.Lock()
			err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait))
			t.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (t *TickerClient) sendSubscribeAndMode(tokens []uint32) error {
	// Step 1: Subscribe
	subPayload := map[string]interface{}{
		"a": "subscribe",
		"v": tokens,
	}
	if err := t.writeJSON(subPayload); err != nil {
		return fmt.Errorf("failed to send subscribe message: %w", err)
	}

	// Step 2: Set Mode to "full" for full 184-byte equity depth & 32-byte index packets
	modePayload := map[string]interface{}{
		"a": "mode",
		"v": []interface{}{"full", tokens},
	}
	if err := t.writeJSON(modePayload); err != nil {
		return fmt.Errorf("failed to send mode message: %w", err)
	}

	slog.Info("Sent subscription & modeFull to Kite Ticker", "token_count", len(tokens))
	return nil
}

func (t *TickerClient) writeJSON(v interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn == nil {
		return errors.New("websocket connection is not open")
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("json marshal error: %w", err)
	}

	_ = t.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return t.conn.WriteMessage(websocket.TextMessage, data)
}
