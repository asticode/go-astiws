package astiws

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/asticode/go-astikit"
	"github.com/gorilla/websocket"
)

type ClientAdapter func(c *Client) error

type OnServeError func(*Client, error)

type Server struct {
	clientAdapter ClientAdapter
	cs            map[*Client]bool
	l             astikit.CompleteLogger
	m             *sync.Mutex
	onServeError  OnServeError
	timeout       time.Duration
	Upgrader      websocket.Upgrader
}

type ServerOptions struct {
	CheckOrigin    func(r *http.Request) bool
	ClientAdapter  ClientAdapter
	Logger         astikit.StdLogger
	MaxMessageSize int
	OnServeError   OnServeError
	Timeout        time.Duration
}

func NewServer(o ServerOptions) *Server {
	// Create server
	s := &Server{
		clientAdapter: o.ClientAdapter,
		cs:            make(map[*Client]bool),
		l:             astikit.AdaptStdLogger(o.Logger),
		m:             &sync.Mutex{},
		onServeError:  o.OnServeError,
		timeout:       o.Timeout,
		Upgrader: websocket.Upgrader{
			CheckOrigin:     o.CheckOrigin,
			ReadBufferSize:  o.MaxMessageSize,
			WriteBufferSize: o.MaxMessageSize,
		},
	}

	// Default serve error callback
	if s.onServeError == nil {
		s.onServeError = s.defaultOnServeError
	}
	return s
}

func (s *Server) Close() error {
	// Get clients
	var cs []*Client
	s.m.Lock()
	for c := range s.cs {
		cs = append(cs, c)
	}
	s.m.Unlock()

	// Loop through clients
	for _, c := range cs {
		c.Close()
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// We use a closure to ease error handling
	c, err := func() (c *Client, err error) {
		// Create client
		c = NewClientWithContext(r.Context(), ClientConfiguration{
			MaxMessageSize: s.Upgrader.WriteBufferSize,
			Timeout:        s.timeout,
		}, s.l)

		// Add client
		s.m.Lock()
		s.cs[c] = true
		s.m.Unlock()

		// Make sure to remove client
		defer func() {
			s.m.Lock()
			delete(s.cs, c)
			s.m.Unlock()
		}()

		// Upgrade connection
		if c.conn, err = s.Upgrader.Upgrade(w, r, nil); err != nil {
			err = fmt.Errorf("astiws: upgrading conn failed: %w", err)
			return
		}

		// Adapt client
		if s.clientAdapter != nil {
			if err = s.clientAdapter(c); err != nil {
				err = fmt.Errorf("astiws: adapting client failed: %w", err)
				return
			}
		}

		// Read
		if err = c.read(c.handlePingServer); err != nil {
			err = fmt.Errorf("astiws: reading failed: %w", err)
			return
		}
		return
	}()
	if err != nil {
		s.onServeError(c, err)
		return
	}
}

func (s *Server) defaultOnServeError(c *Client, err error) {
	if err != nil {
		var e *websocket.CloseError
		if ok := errors.As(err, &e); !ok || (e.Code != websocket.CloseNoStatusReceived && e.Code != websocket.CloseNormalClosure) {
			s.l.WarnC(c.Context(), fmt.Errorf("astiws: serving websocket failed: %w", err))
		}
		return
	}
}

func (s *Server) Clients() []*Client {
	// Lock
	s.m.Lock()
	defer s.m.Unlock()

	// Loop through clients
	var cs []*Client
	for c := range s.cs {
		cs = append(cs, c)
	}
	return cs
}
