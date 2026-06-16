// Package hub fans server-side changes out to all connected editor SSE clients.
package hub

import (
	"sync"
	"sync/atomic"

	"github.com/starfederation/datastar-go/datastar"
)

// Patch is a unit of work applied to one client's SSE stream.
type Patch func(*datastar.ServerSentEventGenerator) error

// Client is one connected browser tab. Patches are delivered through a buffered
// channel drained by a single goroutine, so writes to the SSE never interleave.
type Client struct {
	ID    string
	Send  chan Patch
	stale atomic.Bool // set when Send overflowed; consumer should re-snapshot
}

// MarkStale flags that this client missed a patch and needs a full snapshot.
func (c *Client) MarkStale()    { c.stale.Store(true) }
func (c *Client) Stale() bool   { return c.stale.Load() }
func (c *Client) ClearStale()   { c.stale.Store(false) }

// Hub holds the set of connected clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

// New returns an empty hub.
func New() *Hub { return &Hub{clients: map[string]*Client{}} }

// Add registers a client with a buffered send channel.
func (h *Hub) Add(id string) *Client {
	c := &Client{ID: id, Send: make(chan Patch, 64)}
	h.mu.Lock()
	h.clients[id] = c
	h.mu.Unlock()
	return c
}

// Remove deregisters a client.
func (h *Hub) Remove(id string) {
	h.mu.Lock()
	delete(h.clients, id)
	h.mu.Unlock()
}

// Broadcast pushes a patch to every client without blocking. A client whose
// buffer is full is marked stale (back-pressure safety): its drain loop will
// recover by re-sending a full snapshot.
func (h *Hub) Broadcast(p Patch) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		select {
		case c.Send <- p:
		default:
			c.MarkStale()
		}
	}
}
