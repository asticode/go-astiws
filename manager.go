package astiws

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/asticode/go-astikit"
	"github.com/gorilla/websocket"
)

// ClientAdapter represents a client adapter func
type ClientAdapter func(c *Client) error

// Manager represents a websocket manager
type Manager struct {
	clients  map[interface{}]*Client
	l        astikit.CompleteLogger
	mutex    *sync.RWMutex
	timeout  time.Duration
	Upgrader websocket.Upgrader
}

// ManagerConfiguration represents a manager configuration
type ManagerConfiguration struct {
	CheckOrigin    func(r *http.Request) bool `toml:"-"`
	MaxMessageSize int                        `toml:"max_message_size"`
	// Timeout after which connections are closed. If Timeout <= 0, default timeout is used.
	Timeout time.Duration `toml:"timeout"`
}

// NewManager creates a new manager
func NewManager(c ManagerConfiguration, l astikit.StdLogger) *Manager {
	return &Manager{
		clients: make(map[interface{}]*Client),
		l:       astikit.AdaptStdLogger(l),
		mutex:   &sync.RWMutex{},
		timeout: c.Timeout,
		Upgrader: websocket.Upgrader{
			CheckOrigin:     c.CheckOrigin,
			ReadBufferSize:  c.MaxMessageSize,
			WriteBufferSize: c.MaxMessageSize,
		},
	}
}

// AutoRegisterClient auto registers a new client
func (m *Manager) AutoRegisterClient(c *Client) {
	m.RegisterClient(c, c)
}

// AutoUnregisterClient auto unregisters a client
func (m *Manager) AutoUnregisterClient(c *Client) {
	m.UnregisterClient(c)
}

// Client returns the client stored with the specific key
func (m *Manager) Client(k interface{}) (c *Client, ok bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	c, ok = m.clients[k]
	return
}

// Clients executes a function on every client. It stops if an error is returned.
func (m *Manager) Clients(fn func(k interface{}, c *Client) error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	for k, c := range m.clients {
		if err := fn(k, c); err != nil {
			return
		}
	}
}

// Close implements the io.Closer interface
func (m *Manager) Close() error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.l.Debugf("astiws: closing astiws manager %p", m)
	for k, c := range m.clients {
		c.Close()
		delete(m.clients, k)
	}
	return nil
}

// CountClients returns the number of connected clients
func (m *Manager) CountClients() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.clients)
}

// Loop loops in clients and execute a function for each of them
func (m *Manager) Loop(fn func(k interface{}, c *Client)) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for k, c := range m.clients {
		fn(k, c)
	}
}

// RegisterClient registers a new client
func (m *Manager) RegisterClient(k interface{}, c *Client) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.l.Debugf("astiws: registering client %p in astiws manager %p with key %+v", c, m, k)
	m.clients[k] = c
}

// ServeHTTP handles an HTTP request and returns an error unlike an http.Handler
// We don't want to register the client yet, since we may want to index the map of clients with an information we don't
// have yet
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request, a ClientAdapter) (err error) {
	// Create client
	var c = NewClientWithContext(r.Context(), ClientConfiguration{
		MaxMessageSize: m.Upgrader.WriteBufferSize,
		Timeout:        m.timeout,
	}, m.l)

	// Upgrade connection
	if c.conn, err = m.Upgrader.Upgrade(w, r, nil); err != nil {
		err = fmt.Errorf("astiws: upgrading conn failed: %w", err)
		return
	}

	// Adapt client
	if err = a(c); err != nil {
		err = fmt.Errorf("astiws: adapting client failed: %w", err)
		return
	}

	// Read
	if err = c.read(c.handlePingManager); err != nil {
		err = fmt.Errorf("astiws: reading failed: %w", err)
		return
	}
	return
}

// UnregisterClient unregisters a client
// astiws.disconnected event is a good place to call this function
func (m *Manager) UnregisterClient(k interface{}) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.l.Debugf("astiws: unregistering client in astiws manager %p with key %+v", m, k)
	delete(m.clients, k)
}
