package astiws

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/xlog"
)

// ListenerFunc represents a listener callback
type ListenerFunc func(c *Client, eventName string, payload interface{}) error

// Client represents a hub client
type Client struct {
	conn           *websocket.Conn
	listeners      map[string][]ListenerFunc
	Logger         xlog.Logger
	maxMessageSize int
	mutex          *sync.RWMutex
}

// NewClient creates a new client
func NewClient(maxMessageSize int) *Client {
	return &Client{
		listeners:      make(map[string][]ListenerFunc),
		Logger:         xlog.NopLogger,
		maxMessageSize: maxMessageSize,
		mutex:          &sync.RWMutex{},
	}
}

// Close closes the client properly
func (c *Client) Close() {
	c.Logger.Debugf("Closing astiws client %p", c)
	if c.conn != nil {
		c.conn.Close()
	}
}

// Dial dials an addr
func (c *Client) Dial(addr string) (err error) {
	// Make sure previous connections is closed
	if c.conn != nil {
		c.conn.Close()
	}

	// Dial
	c.Logger.Debugf("Dialing %s with client %p", addr, c)
	c.conn, _, err = websocket.DefaultDialer.Dial(addr, nil)
	return
}

// Read reads from the client
func (c *Client) Read() (err error) {
	c.conn.SetReadLimit(int64(c.maxMessageSize))
	for {
		// Read message
		var m []byte
		if _, m, err = c.conn.ReadMessage(); err != nil {
			// We need to close the connection here since we want the client to know the connection is not
			// usable anymore for writing
			c.conn.Close()
			return
		}

		// Unmarshal
		var b BodyMessage
		if err = json.Unmarshal(m, &b); err != nil {
			return
		}

		// Execute listener callbacks
		if fs, ok := c.listeners[b.EventName]; ok {
			for _, f := range fs {
				if err = f(c, b.EventName, b.Payload); err != nil {
					return
				}
			}
		}
	}
}

// BodyMessage represents the body of a message
type BodyMessage struct {
	EventName string      `json:"event_name"`
	Payload   interface{} `json:"payload"`
}

// Write writes a message to the client
func (c *Client) Write(eventName string, payload interface{}) (err error) {
	// Init
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Connection is not set
	if c.conn == nil {
		return fmt.Errorf("Connection is not set for astiws client %p", c)
	}

	// Marshal
	var b []byte
	if b, err = json.Marshal(BodyMessage{EventName: eventName, Payload: payload}); err != nil {
		return
	}

	// Write message
	c.Logger.Debugf("Writing %s to astiws client %p", string(b), c)
	return c.conn.WriteMessage(websocket.BinaryMessage, b)
}

// AddListener adds a listener
func (c *Client) AddListener(eventName string, f ListenerFunc) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.listeners[eventName] = append(c.listeners[eventName], f)
}

// SetListener sets a listener
func (c *Client) SetListener(eventName string, f ListenerFunc) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.listeners[eventName] = []ListenerFunc{f}
}
