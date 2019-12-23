package astiws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/asticode/go-astikit"
	"github.com/asticode/go-astilog"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// Constants
const (
	EventNameDisconnect = "astiws.disconnect"
	PingPeriod          = (pingWait * 9) / 10
	pingWait            = 60 * time.Second
)

// ListenerFunc represents a listener callback
type ListenerFunc func(c *Client, eventName string, payload json.RawMessage) error

// MessageHandler represents a message handler
type MessageHandler func(m []byte) error

// Client represents a websocket client
type Client struct {
	c         ClientConfiguration
	conn      *websocket.Conn
	ctx       context.Context
	listeners map[string][]ListenerFunc
	mh        MessageHandler
	ml        *sync.Mutex // Lock listeners
	mw        *sync.Mutex // Lock write to avoid panics "concurrent write to websocket connection"
	wg        *sync.WaitGroup
}

// ClientConfiguration represents a client configuration
type ClientConfiguration struct {
	MaxMessageSize int `toml:"max_message_size"`
}

// BodyMessage represents the body of a message
type BodyMessage struct {
	EventName string      `json:"event_name"`
	Payload   interface{} `json:"payload"`
}

// BodyMessageRead represents the body of a message for read purposes
// Indeed when reading the body, we need the payload to be a json.RawMessage
type BodyMessageRead struct {
	BodyMessage
	Payload json.RawMessage `json:"payload"`
}

// NewClient creates a new client
func NewClient(c ClientConfiguration) *Client {
	return NewClientWithContext(context.Background(), c)
}

// NewClientWithContext creates a new client with a context
func NewClientWithContext(ctx context.Context, cfg ClientConfiguration) (c *Client) {
	c = &Client{
		c:         cfg,
		ctx:       ctx,
		listeners: make(map[string][]ListenerFunc),
		ml:        &sync.Mutex{},
		mw:        &sync.Mutex{},
		wg:        &sync.WaitGroup{},
	}
	c.SetMessageHandler(c.defaultMessageHandler)
	return
}

// Context return the client's context
func (c *Client) Context() context.Context {
	return c.ctx
}

// WithContext updates the client's context
func (c *Client) WithContext(ctx context.Context) *Client {
	c.ctx = ctx
	return c
}

// Close closes the client properly
func (c *Client) Close() (err error) {
	// Log
	astilog.DebugCf(c.ctx, "astiws: closing astiws client %p", c)

	// There's a connection to close
	if c.conn != nil {
		// Send a close frame
		astilog.DebugCf(c.ctx, "astiws: sending close frame")
		if err = c.write(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
			err = errors.Wrap(err, "astiws: sending close frame failed")
			return
		}

		// Wait for the connection to be really closed
		c.wg.Wait()
	}
	return
}

// Dial dials an addr
func (c *Client) Dial(addr string) error {
	return c.DialWithHeaders(addr, nil)
}

// DialWithHeader dials an addr with specific headers
func (c *Client) DialWithHeaders(addr string, h http.Header) (err error) {
	// Make sure previous connections is closed
	if c.conn != nil {
		c.conn.Close()
	}

	// Dial
	astilog.DebugCf(c.ctx, "astiws: dialing %s with client %p", addr, c)
	if c.conn, _, err = websocket.DefaultDialer.Dial(addr, h); err != nil {
		err = errors.Wrapf(err, "astiws: dialing %s failed", addr)
		return
	}
	return
}

// Read reads from the client
func (c *Client) Read() error {
	return c.read(c.handlePingClient)
}

func (c *Client) read(handlePing func(ctx context.Context)) (err error) {
	// Make sure the connection is properly closed
	c.wg.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		c.conn.Close()
		c.conn = nil
		cancel()
		c.wg.Done()
		c.executeListeners(EventNameDisconnect, json.RawMessage{})
	}()

	// Update conn
	if c.c.MaxMessageSize > 0 {
		c.conn.SetReadLimit(int64(c.c.MaxMessageSize))
	}

	// Extend connection
	if err = c.ExtendConnection(); err != nil {
		err = errors.Wrap(err, "astiws: extending connection failed")
		return
	}

	// Handle ping
	if handlePing != nil {
		handlePing(ctx)
	}

	// Loop
	for {
		// Read message
		var m []byte
		if _, m, err = c.conn.ReadMessage(); err != nil {
			err = errors.Wrap(err, "astiws: reading message failed")
			return
		}

		// Handle message
		if err = c.mh(m); err != nil {
			astilog.ErrorC(c.ctx, errors.Wrap(err, "astiws: handling message failed"))
			continue
		}
	}
}

func (c *Client) handlePingClient(ctx context.Context) {
	// Handle pong message
	c.conn.SetPongHandler(func(string) (err error) {
		// Extend connection
		if err = c.ExtendConnection(); err != nil {
			err = errors.Wrap(err, "astiws: extending connection failed")
			return
		}
		return
	})

	// Send ping at constant interval
	go c.ping(ctx)
}

// ping writes a ping message in the connection
func (c *Client) ping(ctx context.Context) {
	var t = time.NewTicker(PingPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.write(websocket.PingMessage, nil); err != nil {
				astilog.ErrorC(c.ctx, errors.Wrap(err, "astiws: sending ping message failed"))
			}
		}
	}
}

func (c *Client) handlePingManager(ctx context.Context) {
	// Handle ping message
	c.conn.SetPingHandler(func(string) (err error) {
		// Extend connection
		if err = c.ExtendConnection(); err != nil {
			err = errors.Wrap(err, "astiws: extending connection failed")
			return
		}

		// Send pong
		if err = c.write(websocket.PongMessage, nil); err != nil {
			err = errors.Wrap(err, "astiws: sending pong message failed")
			return
		}
		return
	})
}

// ExtendConnection extends the connection
func (c *Client) ExtendConnection() error {
	return c.conn.SetReadDeadline(time.Now().Add(pingWait))
}

// executeListeners executes listeners for a specific event
func (c *Client) executeListeners(eventName string, payload json.RawMessage) {
	// Get listeners
	c.ml.Lock()
	fs, ok := c.listeners[eventName]
	c.ml.Unlock()

	// No listeners
	if !ok {
		return
	}

	// Loop through listeners
	for _, f := range fs {
		// Execute listener
		if err := f(c, eventName, payload); err != nil {
			astilog.ErrorC(c.ctx, errors.Wrapf(err, "astiws: executing listener for event %s failed", eventName))
			continue
		}
	}
}

// Write writes a message to the client
func (c *Client) Write(eventName string, payload interface{}) error {
	return c.WriteJSON(BodyMessage{EventName: eventName, Payload: payload})
}

// WriteJSON writes a JSON message to the client
func (c *Client) WriteJSON(m interface{}) (err error) {
	// Marshal
	var b []byte
	if b, err = json.Marshal(m); err != nil {
		err = errors.Wrap(err, "astiws: marshaling message failed")
		return
	}

	// Write text message
	if err = c.WriteText(b); err != nil {
		err = errors.Wrap(err, "astiws: writing text message failed")
		return
	}
	return
}

// WriteText writes a text message to the client
func (c *Client) WriteText(m []byte) (err error) {
	// Write message
	if err = c.write(websocket.TextMessage, m); err != nil {
		err = errors.Wrap(err, "astiws: writing message failed")
		return
	}
	return
}

func (c *Client) write(messageType int, data []byte) (err error) {
	// Connection is not set
	if c.conn == nil {
		err = fmt.Errorf("astiws: connection is not set for astiws client %p", c)
		return
	}

	// Write
	c.mw.Lock()
	defer c.mw.Unlock()
	return c.conn.WriteMessage(messageType, data)
}

// AddListener adds a listener
func (c *Client) AddListener(eventName string, f ListenerFunc) {
	c.ml.Lock()
	defer c.ml.Unlock()
	c.listeners[eventName] = append(c.listeners[eventName], f)
}

// DelListener deletes a listener
func (c *Client) DelListener(eventName string) {
	c.ml.Lock()
	defer c.ml.Unlock()
	delete(c.listeners, eventName)
}

// SetListener sets a listener
func (c *Client) SetListener(eventName string, f ListenerFunc) {
	c.ml.Lock()
	defer c.ml.Unlock()
	c.listeners[eventName] = []ListenerFunc{f}
}

// SetMessageHandler sets the message handler
func (c *Client) SetMessageHandler(h MessageHandler) {
	c.mh = h
}

func (c *Client) defaultMessageHandler(m []byte) (err error) {
	// Unmarshal
	var b BodyMessageRead
	if err = json.Unmarshal(m, &b); err != nil {
		err = errors.Wrap(err, "astiws: unmarshaling message failed")
		return
	}

	// Execute listener callbacks
	c.executeListeners(b.EventName, b.Payload)
	return
}

// DialAndReadOptions represents dial and read options
type DialAndReadOptions struct {
	Addr        string
	Header      http.Header
	OnDial      func() error
	OnReadError func(err error)
}

// DialAndReadOptions dials and read with options
// It's the responsibility of the caller to close the Client
func (c *Client) DialAndRead(w *astikit.Worker, o DialAndReadOptions) {
	// Execute in a task
	w.NewTask().Do(func() {
		// Dial
		go func() {
			const sleepError = 5 * time.Second
			for {
				// Check context error
				if w.Context().Err() != nil {
					break
				}

				// Dial
				w.Logger().Infof("astiws: dialing %s", o.Addr)
				if err := c.DialWithHeaders(o.Addr, o.Header); err != nil {
					w.Logger().Error(errors.Wrapf(err, "astiws: dialing %s failed", o.Addr))
					time.Sleep(sleepError)
					continue
				}

				// Custom callback
				if o.OnDial != nil {
					if err := o.OnDial(); err != nil {
						w.Logger().Error(errors.Wrapf(err, "astiws: custom on dial callback on %s failed", o.Addr))
						time.Sleep(sleepError)
						continue
					}
				}

				// Read
				if err := c.Read(); err != nil {
					if o.OnReadError != nil {
						o.OnReadError(err)
					} else {
						w.Logger().Error(errors.Wrapf(err, "astiws: reading on %s failed", o.Addr))
					}
					time.Sleep(sleepError)
					continue
				}
			}
		}()

		// Wait for context to be done
		<-w.Context().Done()
	})

}
