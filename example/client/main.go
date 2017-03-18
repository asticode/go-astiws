package main

import (
	"flag"
	"math/rand"
	"sync"
	"time"

	"encoding/json"

	"github.com/asticode/go-astilog"
	"github.com/asticode/go-astiws"
	"github.com/rs/xlog"
)

// Flags
var (
	clientsCount = flag.Int("c", 2, "number of clients")
	managerAddr  = flag.String("m", "ws://localhost:4000", "manager addr")
	sleepError   = flag.Duration("s", 10*time.Second, "sleep duration before retrying")
	ticker       = flag.Duration("t", 5*time.Second, "ticker duration")
)

func main() {
	// Parse flags
	flag.Parse()

	// Init logger
	var l = xlog.New(astilog.NewConfig(astilog.FlagConfig()))

	// Init clients
	var clients, mutex, wg = make(map[int]*astiws.Client), &sync.RWMutex{}, &sync.WaitGroup{}
	for i := 0; i < *clientsCount; i++ {
		wg.Add(1)
		go func(i int) {
			// Init client
			var c = astiws.NewClient(1024)
			defer c.Close()
			c.Logger = l

			// Set up listeners
			c.AddListener("asticoded", HandleAsticodedFirst)
			c.AddListener("asticoded", HandleAsticodedSecond)

			// Add client
			mutex.Lock()
			clients[i] = c
			mutex.Unlock()
			wg.Done()

			// Infinite loop to handle reconnection
			var err error
			for {
				// Dial
				if err = c.Dial(*managerAddr); err != nil {
					l.Errorf("%s while dialing %s, sleeping %s before retrying", err, *managerAddr, *sleepError)
					time.Sleep(*sleepError)
					continue
				}

				// Read
				if err = c.Read(); err != nil {
					l.Errorf("%s while reading, sleeping %s before retrying", err, *sleepError)
					time.Sleep(*sleepError)
					continue
				}
			}
		}(i)
	}
	wg.Wait()

	// Send random messages
	var t = time.NewTicker(*ticker)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			// Get random client
			var c = clients[rand.Intn(len(clients))]

			// Random payload
			var b bool
			if rand.Intn(2) == 1 {
				b = true
			}

			// Send asticode event
			if err := c.Write("asticode", b); err != nil {
				l.Error(err)
			}
		}
	}
}

// HandleAsticodedFirst handles asticoded events
func HandleAsticodedFirst(c *astiws.Client, eventName string, payload json.RawMessage) (err error) {
	c.Logger.Debugf("Client %p is handling an asticoded event (1/2)", c)
	return
}

// HandleAsticodedSecond handles asticoded events
func HandleAsticodedSecond(c *astiws.Client, eventName string, payload json.RawMessage) (err error) {
	c.Logger.Debugf("Client %p is handling an asticoded event (2/2)", c)
	return
}
