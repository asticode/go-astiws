package hub

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/xlog"
)

// Constants
const (
	maxMessageSize = 1024 * 1024
)

// Hub represents a websocket Hub
type Hub struct {
	clients        map[*Client]bool
	HandlerWelcome func(r *http.Request, c *Client) error
	Logger         xlog.Logger
	mutex          *sync.RWMutex
	Upgrader       websocket.Upgrader
}

// New creates a new hub
func New() *Hub {
	return &Hub{
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
func (h *Hub) CountClients() int {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return len(h.clients)
}

// Close closes the hub properly
func (h *Hub) Close() {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	h.Logger.Debugf("Closing the hub %p", h)
	for c := range h.clients {
		c.Close()
	}
}

// registerClient registers a new client
func (h *Hub) registerClient(c *Client) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.Logger.Debugf("Registering client %p", c)
	h.clients[c] = true
}

// ServeHTTP handles an HTTP request and returns an error unlike an http.Handler
func (h *Hub) ServeHTTP(rw http.ResponseWriter, r *http.Request) (err error) {
	// Init client
	var c = newClient(h.Logger)
	if c.conn, err = h.Upgrader.Upgrade(rw, r, nil); err != nil {
		return
	}
	defer c.Close()

	// Register client in the hub
	h.registerClient(c)
	defer h.unregisterClient(c)

	// Read
	go c.read()

	// Welcome client
	if h.HandlerWelcome != nil {
		if err = h.HandlerWelcome(r, c); err != nil {
			return
		}
	}

	// Wait is the blocking pattern
	return c.wait()
}

// unregisterClient unregisters a client
func (h *Hub) unregisterClient(c *Client) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.Logger.Debugf("Unregistering client %p", c)
	delete(h.clients, c)
}
