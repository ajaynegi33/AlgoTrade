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
	connectedClients map[*websocket.Conn]bool
	clientCount      atomic.Int64
	broadcastChan    chan interface{} // Buffered channel size 1 for drop-oldest snapshot
	registerChan     chan *websocket.Conn
	unregisterChan   chan *websocket.Conn
	mu               sync.Mutex
	bufferPool       sync.Pool
	isClosed         atomic.Bool
}

// NewClientHub creates a new frontend WebSocket hub
func NewClientHub() *ClientHub {
	hub := &ClientHub{
		connectedClients: make(map[*websocket.Conn]bool),
		broadcastChan:    make(chan interface{}, 1),
		registerChan:     make(chan *websocket.Conn),
		unregisterChan:   make(chan *websocket.Conn),
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
func (hub *ClientHub) ClientCount() int64 {
	return hub.clientCount.Load()
}

// Broadcast pushes a snapshot to all connected browser clients.
// Non-blocking: if the hub is busy, older frame is dropped. The caller NEVER blocks.
func (hub *ClientHub) Broadcast(snapshot interface{}) {
	if hub.clientCount.Load() == 0 || hub.isClosed.Load() {
		return // Zero connected clients: zero allocation, zero CPU work
	}

	select {
	case hub.broadcastChan <- snapshot:
	default:
		// Hub busy: drop frame to ensure engine Actor is never blocked
	}
}

// ServeWS upgrades the HTTP request to WebSocket and registers the client
func (hub *ClientHub) ServeWS(writer http.ResponseWriter, request *http.Request) {
	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		slog.Warn("Failed to upgrade websocket connection", "error", err)
		return
	}

	hub.registerChan <- conn

	// Reader pump to detect client disconnects & handle pongs
	go func() {
		defer func() {
			hub.unregisterChan <- conn
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

func (hub *ClientHub) run() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case conn := <-hub.registerChan:
			hub.mu.Lock()
			hub.connectedClients[conn] = true
			hub.clientCount.Store(int64(len(hub.connectedClients)))
			hub.mu.Unlock()
			slog.Info("Frontend browser client connected", "active_clients", hub.clientCount.Load())

		case conn := <-hub.unregisterChan:
			hub.mu.Lock()
			if _, ok := hub.connectedClients[conn]; ok {
				delete(hub.connectedClients, conn)
				_ = conn.Close()
				hub.clientCount.Store(int64(len(hub.connectedClients)))
			}
			hub.mu.Unlock()
			slog.Info("Frontend browser client disconnected", "active_clients", hub.clientCount.Load())

		case snapshot := <-hub.broadcastChan:
			hub.broadcastSnapshot(snapshot)

		case <-ticker.C:
			hub.pingClients()
		}
	}
}

func (hub *ClientHub) broadcastSnapshot(snapshot interface{}) {
	hub.mu.Lock()
	if len(hub.connectedClients) == 0 {
		hub.mu.Unlock()
		return
	}

	// Copy client pointers to avoid holding mutex during I/O
	activeClients := make([]*websocket.Conn, 0, len(hub.connectedClients))
	for conn := range hub.connectedClients {
		activeClients = append(activeClients, conn)
	}
	hub.mu.Unlock()

	// Get reusable buffer from pool
	buffer := hub.bufferPool.Get().(*bytes.Buffer)
	buffer.Reset()
	defer hub.bufferPool.Put(buffer)

	jsonEncoder := json.NewEncoder(buffer)
	if err := jsonEncoder.Encode(snapshot); err != nil {
		slog.Error("Failed to encode snapshot JSON", "error", err)
		return
	}
	payload := buffer.Bytes()

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
		hub.mu.Lock()
		for _, conn := range deadClients {
			if _, ok := hub.connectedClients[conn]; ok {
				delete(hub.connectedClients, conn)
				_ = conn.Close()
			}
		}
		hub.clientCount.Store(int64(len(hub.connectedClients)))
		hub.mu.Unlock()
		slog.Warn("Evicted slow/disconnected browser clients", "evicted", len(deadClients), "remaining", hub.clientCount.Load())
	}
}

func (hub *ClientHub) pingClients() {
	hub.mu.Lock()
	if len(hub.connectedClients) == 0 {
		hub.mu.Unlock()
		return
	}
	activeClients := make([]*websocket.Conn, 0, len(hub.connectedClients))
	for conn := range hub.connectedClients {
		activeClients = append(activeClients, conn)
	}
	hub.mu.Unlock()

	var deadClients []*websocket.Conn
	for _, conn := range activeClients {
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			deadClients = append(deadClients, conn)
		}
	}

	if len(deadClients) > 0 {
		hub.mu.Lock()
		for _, conn := range deadClients {
			if _, ok := hub.connectedClients[conn]; ok {
				delete(hub.connectedClients, conn)
				_ = conn.Close()
			}
		}
		hub.clientCount.Store(int64(len(hub.connectedClients)))
		hub.mu.Unlock()
	}
}
