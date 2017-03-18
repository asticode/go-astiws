package main

import (
	"context"
	"flag"
	"net/http"

	"github.com/asticode/go-astilog"
	"github.com/asticode/go-astiws"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/xlog"
)

// Constants
const (
	contextKeyManager = "manager"
)

// Flags
var (
	addr = flag.String("a", "localhost:4000", "addr")
)

func main() {
	// Parse flags
	flag.Parse()

	// Init logger
	var l = xlog.New(astilog.NewConfig(astilog.FlagConfig()))

	// Init manager
	var m = astiws.NewManager(1024)
	defer m.Close()
	m.Logger = l

	// Init router
	var r = httprouter.New()
	r.GET("/", Handle)

	// Serve
	l.Debugf("Listening and serving on %s", *addr)
	if err := http.ListenAndServe(*addr, AdaptHandler(r, m)); err != nil {
		l.Fatal(err)
	}
}

// Handle returns the handler
func Handle(rw http.ResponseWriter, r *http.Request, p httprouter.Params) {
	// Retrieve manager
	var m = ManagerFromContext(r.Context())

	// Serve
	if err := m.ServeHTTP(rw, r, AdaptClient); err != nil {
		m.Logger.Error(err)
		return
	}
}

// AdaptHandle adapts a handler
func AdaptHandler(h http.Handler, m *astiws.Manager) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.Background())
		r = r.WithContext(NewContextWithManager(r.Context(), m))
		h.ServeHTTP(rw, r)
	})
}

// NewContextWithManager creates a context with the manager
func NewContextWithManager(ctx context.Context, m *astiws.Manager) context.Context {
	return context.WithValue(ctx, contextKeyManager, m)
}

// ManagerFromContext retrieves the manager from the context
func ManagerFromContext(ctx context.Context) *astiws.Manager {
	if l, ok := ctx.Value(contextKeyManager).(*astiws.Manager); ok {
		return l
	}
	return &astiws.Manager{}
}

// AdaptClient adapts a client
func AdaptClient(c *astiws.Client) {
	// Set up listeners
	c.SetListener("asticode", HandleAsticode)
}

// HandleAsticode handles asticode events
func HandleAsticode(c *astiws.Client, eventName string, payload interface{}) (err error) {
	// Assert payload
	var b, ok bool
	if b, ok = payload.(bool); !ok {
		c.Logger.Errorf("Payload %+v is not a bool", payload)
		return
	}

	// Answer
	if b {
		if err = c.Write("asticoded", 2); err != nil {
			c.Logger.Error(err)
			return
		}
	} else {
		c.Logger.Error("Payload should be true")
	}
	return
}
