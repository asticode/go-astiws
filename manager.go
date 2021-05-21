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
	cs       map[*Client]bool
	l        astikit.CompleteLogger
	m        *sync.Mutex
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
		cs:      make(map[*Client]bool),
		l:       astikit.AdaptStdLogger(l),
		m:       &sync.Mutex{},
		timeout: c.Timeout,
		Upgrader: websocket.Upgrader{
			CheckOrigin:     c.CheckOrigin,
			ReadBufferSize:  c.MaxMessageSize,
			WriteBufferSize: c.MaxMessageSize,
		},
	}
}

// Close implements the io.Closer interface
func (m *Manager) Close() error {
	// Get clients
	var cs []*Client
	m.m.Lock()
	for c := range m.cs {
		cs = append(cs, c)
	}
	m.m.Unlock()

	// Loop through clients
	for _, c := range cs {
		c.Close()
	}
	return nil
}

// ServeHTTP handles an HTTP request and returns an error unlike an http.Handler
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request, a ClientAdapter) (err error) {
	// Create client
	var c = NewClientWithContext(r.Context(), ClientConfiguration{
		MaxMessageSize: m.Upgrader.WriteBufferSize,
		Timeout:        m.timeout,
	}, m.l)

	// Add client
	m.m.Lock()
	m.cs[c] = true
	m.m.Unlock()

	// Make sure to remove client
	defer func() {
		m.m.Lock()
		delete(m.cs, c)
		m.m.Unlock()
	}()

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
