package astiws

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/asticode/go-astilog"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// Constants
const (
	EventNameDisconnect = "astiws.disconnect"
	pingPeriod          = (pingWait * 9) / 10
	pingWait            = 60 * time.Second
)

// ListenerFunc represents a listener callback
type ListenerFunc func(c *Client, eventName string, payload json.RawMessage) error

// Client represents a hub client
type Client struct {
	channelStopPing chan bool
	conn            *websocket.Conn
	listeners       map[string][]ListenerFunc
	maxMessageSize  int
	mutex           *sync.RWMutex
}

// NewClient creates a new client
func NewClient(maxMessageSize int) *Client {
	return &Client{
		channelStopPing: make(chan bool),
		listeners:       make(map[string][]ListenerFunc),
		maxMessageSize:  maxMessageSize,
		mutex:           &sync.RWMutex{},
	}
}

// Close closes the client properly
func (c *Client) Close() {
	astilog.Debugf("Closing astiws client %p", c)
	if c.conn != nil {
		c.conn.Close()
	}
	if c.channelStopPing != nil {
		close(c.channelStopPing)
		c.channelStopPing = nil
	}
}

// Dial dials an addr
func (c *Client) Dial(addr string) (err error) {
	// Make sure previous connections is closed
	if c.conn != nil {
		c.conn.Close()
	}

	// Dial
	astilog.Debugf("Dialing %s with client %p", addr, c)
	if c.conn, _, err = websocket.DefaultDialer.Dial(addr, nil); err != nil {
		err = errors.Wrapf(err, "dialing %s failed", addr)
		return
	}
	return
}

// BodyMessageRead represents the body of a message for read purposes
// Indeed when reading the body, we need the payload to be a json.RawMessage
type BodyMessageRead struct {
	BodyMessage
	Payload json.RawMessage `json:"payload"`
}

// ping writes a ping message in the connection
func (c *Client) ping() {
	var t = time.NewTicker(pingPeriod)
	defer t.Stop()
	for {
		select {
		case <-c.channelStopPing:
			return
		case <-t.C:
			c.mutex.Lock()
			if err := c.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				astilog.Error(errors.Wrap(err, "sending ping message failed"))
			}
			c.mutex.Unlock()
		}
	}
}

// Read reads from the client
func (c *Client) Read() (err error) {
	defer c.executeListeners(EventNameDisconnect, json.RawMessage{})

	// Update conn
	c.conn.SetReadLimit(int64(c.maxMessageSize))
	c.conn.SetReadDeadline(time.Now().Add(pingWait))
	c.conn.SetPingHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pingWait)); return nil })

	// Ping
	go c.ping()

	// Loop
	for {
		// Read message
		var m []byte
		if _, m, err = c.conn.ReadMessage(); err != nil {
			// We need to close the connection here since we want the client to know the connection is not
			// usable anymore for writing
			c.conn.Close()
			err = errors.Wrap(err, "reading message failed")
			return
		}

		// Unmarshal
		var b BodyMessageRead
		if err = json.Unmarshal(m, &b); err != nil {
			err = errors.Wrap(err, "unmarshaling message failed")
			return
		}

		// Execute listener callbacks
		c.executeListeners(b.EventName, b.Payload)
	}
	return
}

// executeListeners executes listeners for a specific event
func (c *Client) executeListeners(eventName string, payload json.RawMessage) (err error) {
	if fs, ok := c.listeners[eventName]; ok {
		for _, f := range fs {
			if err = f(c, eventName, payload); err != nil {
				err = errors.Wrapf(err, "executing listener for event %s failed", eventName)
				return
			}
		}
	}
	return
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
		err = errors.Wrap(err, "marshaling message failed")
		return
	}

	// Write message
	astilog.Debugf("Writing %s to astiws client %p", string(b), c)
	if err = c.conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		err = errors.Wrap(err, "writing message failed")
		return
	}
	return
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
