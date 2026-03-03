// Package hub manages WebSocket connections from SmartHub Pis and buffers
// pending commands for hubs that are temporarily offline.
package hub

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// pendingCmdTTL is how long a command is buffered for an offline hub.
	pendingCmdTTL  = 5 * time.Minute
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 50 * time.Second   // must be < pongWait
	maxMessageSize = 512 * 1024         // 512 KiB per message
)

// PendingCmd is a command buffered while a hub is offline.
type PendingCmd struct {
	Payload   []byte
	ExpiresAt time.Time
}

// Connection represents one active WebSocket session with a hub.
type Connection struct {
	HubID string
	Conn  *websocket.Conn
	Send  chan []byte
}

// Manager tracks live hub connections and pending command buffers.
// All exported methods are safe for concurrent use.
type Manager struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	pending     map[string][]PendingCmd
	log         *zap.Logger
}

// NewManager creates a ready-to-use Manager.
func NewManager(log *zap.Logger) *Manager {
	return &Manager{
		connections: make(map[string]*Connection),
		pending:     make(map[string][]PendingCmd),
		log:         log,
	}
}

// Register adds a new hub WebSocket connection to the manager.
// Returns the Connection and any unexpired pending commands to drain.
func (m *Manager) Register(hubID string, conn *websocket.Conn) (*Connection, []PendingCmd) {
	c := &Connection{
		HubID: hubID,
		Conn:  conn,
		Send:  make(chan []byte, 64),
	}
	m.mu.Lock()
	m.connections[hubID] = c
	pending := m.drainPending(hubID) // clears the buffer
	m.mu.Unlock()

	m.log.Info("hub connected", zap.String("hub_id", hubID))
	return c, pending
}

// Unregister removes a hub connection when the WebSocket closes.
func (m *Manager) Unregister(hubID string) {
	m.mu.Lock()
	delete(m.connections, hubID)
	m.mu.Unlock()
	m.log.Info("hub disconnected", zap.String("hub_id", hubID))
}

// SendCommand delivers payload to a hub immediately if online, or buffers it
// for pendingCmdTTL if offline.
func (m *Manager) SendCommand(hubID string, payload []byte) {
	m.mu.RLock()
	c, online := m.connections[hubID]
	m.mu.RUnlock()

	if online {
		select {
		case c.Send <- payload:
		default:
			m.log.Warn("hub send buffer full, dropping command",
				zap.String("hub_id", hubID))
		}
		return
	}

	// Hub offline — buffer with TTL.
	m.mu.Lock()
	m.pending[hubID] = append(m.pending[hubID], PendingCmd{
		Payload:   payload,
		ExpiresAt: time.Now().Add(pendingCmdTTL),
	})
	m.mu.Unlock()
}

// drainPending returns non-expired pending commands and removes them.
// Caller must hold m.mu.Lock.
func (m *Manager) drainPending(hubID string) []PendingCmd {
	all := m.pending[hubID]
	delete(m.pending, hubID)

	now := time.Now()
	var valid []PendingCmd
	for _, cmd := range all {
		if now.Before(cmd.ExpiresAt) {
			valid = append(valid, cmd)
		}
	}
	return valid
}
