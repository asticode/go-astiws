package astiws

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/xlog"
)

// ClientAdapter represents a client adapter func
type ClientAdapter func(c *Client)

// Manager represents a websocket manager
type Manager struct {
	clients  map[interface{}]*Client
	Logger   xlog.Logger
	mutex    *sync.RWMutex
	Upgrader websocket.Upgrader
}

// NewManager creates a new manager
func NewManager(maxMessageSize int) *Manager {
	return &Manager{
		clients: make(map[interface{}]*Client),
		Logger:  xlog.NopLogger,
		mutex:   &sync.RWMutex{},
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  maxMessageSize,
			WriteBufferSize: maxMessageSize,
		},
	}
}

// Client returns the client stored with the specific key
func (m *Manager) Client(k interface{}) (c *Client, ok bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	c, ok = m.clients[k]
	return
}

// Close closes the manager properly
func (m *Manager) Close() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.Logger.Debugf("Closing astiws manager %p", m)
	for _, c := range m.clients {
		c.Close()
	}
}

// CountClients returns the number of connected clients
func (m *Manager) CountClients() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.clients)
}

// RegisterClient registers a new client
func (m *Manager) RegisterClient(k interface{}, c *Client) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.Logger.Debugf("Registering client %p in astiws manager %p with key %+v", c, m, k)
	m.clients[k] = c
}

// ServeHTTP handles an HTTP request and returns an error unlike an http.Handler
// We don't want to register the client yet, since we may want to index the map of clients with an information we don't
// have yet
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request, a ClientAdapter) (err error) {
	// Init client
	var c = NewClient(m.Upgrader.WriteBufferSize)
	defer c.Close()
	c.Logger = m.Logger
	if c.conn, err = m.Upgrader.Upgrade(w, r, nil); err != nil {
		return
	}

	// Adapt client
	a(c)

	// Read
	return c.Read()
}

// UnregisterClient unregisters a client
// astiws.disconnected event is a good place to call this function
func (m *Manager) UnregisterClient(k interface{}) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.Logger.Debugf("Unregistering client in astiws manager %p with key %+v", m, k)
	delete(m.clients, k)
}
