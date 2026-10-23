package ws

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 250 * time.Millisecond // Strict 250ms write deadline for slow browser tabs
	pongWait       = 10 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 65536
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 32768,
	CheckOrigin: func(r *http.Request) bool {
		// Allow local development and dashboard origin
		return true
	},
}

// ClientHub manages connected browser WebSocket tabs
type ClientHub struct {
	clients        map[*websocket.Conn]bool
	clientCount    atomic.Int64
	broadcastChan  chan interface{} // Buffered channel size 1 for drop-oldest snapshot
	registerChan   chan *websocket.Conn
	unregisterChan chan *websocket.Conn
	mu             sync.Mutex
	bufferPool     sync.Pool
	isClosed       atomic.Bool
}

// NewClientHub creates a new frontend WebSocket hub
func NewClientHub() *ClientHub {
	hub := &ClientHub{
		clients:        make(map[*websocket.Conn]bool),
		broadcastChan:  make(chan interface{}, 1),
		registerChan:   make(chan *websocket.Conn),
		unregisterChan: make(chan *websocket.Conn),
		bufferPool: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, 32768))
			},
		},
	}
	go hub.run()
	return hub
}

// ClientCount returns the number of currently connected browser clients
func (h *ClientHub) ClientCount() int64 {
	return h.clientCount.Load()
}

// Broadcast pushes a snapshot to all connected browser clients.
// Non-blocking: if the hub is busy, older frame is dropped. The caller NEVER blocks.
func (h *ClientHub) Broadcast(snapshot interface{}) {
	if h.clientCount.Load() == 0 || h.isClosed.Load() {
		return // Zero connected clients: zero allocation, zero CPU work
	}

	select {
	case h.broadcastChan <- snapshot:
	default:
		// Hub busy: drop frame to ensure engine Actor is never blocked
	}
}

// ServeWS upgrades the HTTP request to WebSocket and registers the client
func (h *ClientHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("Failed to upgrade websocket connection", "error", err)
		return
	}

	h.registerChan <- conn

	// Reader pump to detect client disconnects & handle pongs
	go func() {
		defer func() {
			h.unregisterChan <- conn
			_ = conn.Close()
		}()

		conn.SetReadLimit(maxMessageSize)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		for {
			_, _, readErr := conn.ReadMessage()
			if readErr != nil {
				break
			}
		}
	}()
}

func (h *ClientHub) run() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case conn := <-h.registerChan:
			h.mu.Lock()
			h.clients[conn] = true
			h.clientCount.Store(int64(len(h.clients)))
			h.mu.Unlock()
			slog.Info("Frontend browser client connected", "active_clients", h.clientCount.Load())

		case conn := <-h.unregisterChan:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				_ = conn.Close()
				h.clientCount.Store(int64(len(h.clients)))
			}
			h.mu.Unlock()
			slog.Info("Frontend browser client disconnected", "active_clients", h.clientCount.Load())

		case snapshot := <-h.broadcastChan:
			h.broadcastSnapshot(snapshot)

		case <-ticker.C:
			h.pingClients()
		}
	}
}

func (h *ClientHub) broadcastSnapshot(snapshot interface{}) {
	h.mu.Lock()
	if len(h.clients) == 0 {
		h.mu.Unlock()
		return
	}

	// Copy client pointers to avoid holding mutex during I/O
	activeClients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		activeClients = append(activeClients, conn)
	}
	h.mu.Unlock()

	// Get reusable buffer from pool
	buf := h.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer h.bufferPool.Put(buf)

	enc := json.NewEncoder(buf)
	if err := enc.Encode(snapshot); err != nil {
		slog.Error("Failed to encode snapshot JSON", "error", err)
		return
	}
	payload := buf.Bytes()

	// Broadcast with strict 250ms deadline per client
	var deadClients []*websocket.Conn
	for _, conn := range activeClients {
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			deadClients = append(deadClients, conn)
		}
	}

	// Clean up dead or slow clients
	if len(deadClients) > 0 {
		h.mu.Lock()
		for _, conn := range deadClients {
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				_ = conn.Close()
			}
		}
		h.clientCount.Store(int64(len(h.clients)))
		h.mu.Unlock()
		slog.Warn("Evicted slow/disconnected browser clients", "evicted", len(deadClients), "remaining", h.clientCount.Load())
	}
}

func (h *ClientHub) pingClients() {
	h.mu.Lock()
	if len(h.clients) == 0 {
		h.mu.Unlock()
		return
	}
	activeClients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		activeClients = append(activeClients, conn)
	}
	h.mu.Unlock()

	var deadClients []*websocket.Conn
	for _, conn := range activeClients {
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			deadClients = append(deadClients, conn)
		}
	}

	if len(deadClients) > 0 {
		h.mu.Lock()
		for _, conn := range deadClients {
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				_ = conn.Close()
			}
		}
		h.clientCount.Store(int64(len(h.clients)))
		h.mu.Unlock()
	}
}
