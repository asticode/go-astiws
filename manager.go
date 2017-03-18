package astiws

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/xlog"
)

// Manager represents a websocket manager
type Manager struct {
	clients  map[*Client]bool
	Logger   xlog.Logger
	mutex    *sync.RWMutex
	Upgrader websocket.Upgrader
}

// NewManager creates a new manager
func NewManager(maxMessageSize int) *Manager {
	return &Manager{
		clients: make(map[*Client]bool),
		Logger:  xlog.NopLogger,
		mutex:   &sync.RWMutex{},
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  maxMessageSize,
			WriteBufferSize: maxMessageSize,
		},
	}
}

// CountClients returns the number of connected clients
func (m *Manager) CountClients() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.clients)
}

// Close closes the manager properly
func (m *Manager) Close() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.Logger.Debugf("Closing astiws manager %p", m)
	for c := range m.clients {
		c.Close()
	}
}

// registerClient registers a new client
func (m *Manager) registerClient(c *Client) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.Logger.Debugf("Registering client %p in astiws manager %p", c, m)
	m.clients[c] = true
}

// ServeHTTP handles an HTTP request and returns an error unlike an http.Handler
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request, a func(c *Client)) (err error) {
	// Init client
	var c = NewClient(m.Upgrader.WriteBufferSize)
	defer c.Close()
	c.Logger = m.Logger
	if c.conn, err = m.Upgrader.Upgrade(w, r, nil); err != nil {
		return
	}

	// Register client in the manager
	m.registerClient(c)
	defer m.unregisterClient(c)

	// Adapt client
	a(c)

	// Read
	return c.Read()
}

// unregisterClient unregisters a client
func (m *Manager) unregisterClient(c *Client) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.Logger.Debugf("Unregistering client %p in astiws manager %p", c, m)
	delete(m.clients, c)
}
