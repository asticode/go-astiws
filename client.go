package hub

import (
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/xlog"
)

// Client represents a hub client
type Client struct {
	channelClose chan error
	conn         *websocket.Conn
	logger       xlog.Logger
	mutex        *sync.RWMutex
}

// newClient creates a new client
func newClient(l xlog.Logger) *Client {
	return &Client{
		channelClose: make(chan error),
		logger:       l,
		mutex:        &sync.RWMutex{},
	}
}

// Close closes the client properly
func (c *Client) Close() {
	c.logger.Debugf("Closing client %p", c)
	close(c.channelClose)
	c.conn.Close()
}

// read reads from the client
func (c *Client) read() {
	c.conn.SetReadLimit(maxMessageSize)
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			c.channelClose <- fmt.Errorf("%s while reading message of client %p", err, c)
			return
		}

		// TODO Do something with the message
	}
}

// wait is a blocking pattern
func (c *Client) wait() (err error) {
	c.logger.Debugf("Client %p is waiting...", c)
	for {
		select {
		case err = <-c.channelClose:
			return
		}
	}
}

// write writes a message to the client
func (c *Client) write(mt int, b []byte) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.conn.WriteMessage(mt, b)
}
