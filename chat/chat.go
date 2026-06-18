// Package chat is an in-memory group chat that rides on diffy's CQRS+SSE
// pattern: a long-lived read stream (GET /chat/updates) paints history and then
// drains broadcast patches, while a short write (POST /chat) appends a message
// and fans it out to every connected tab. No database — history lives in a
// capped slice and is lost on restart (same trade-off as the graph/runs).
package chat

import (
	"sync"
	"time"
)

// maxHistory bounds in-memory retention to the most recent messages.
const maxHistory = 200

// Message is one chat line. Seq is a per-room monotonic id (also the element id).
type Message struct {
	Seq  int
	User string
	Text string
	At   time.Time
}

// Room is the in-memory message log (the SQLite-table replacement).
type Room struct {
	mu   sync.Mutex
	seq  int
	msgs []Message
}

// NewRoom returns an empty room.
func NewRoom() *Room { return &Room{} }

// Add appends a message (assigning the next Seq), trims to the last maxHistory,
// and returns the stored message.
func (r *Room) Add(user, text string) Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	m := Message{Seq: r.seq, User: user, Text: text, At: time.Now()}
	r.msgs = append(r.msgs, m)
	if len(r.msgs) > maxHistory {
		r.msgs = r.msgs[len(r.msgs)-maxHistory:]
	}
	return m
}

// All returns a copy of the current history (oldest first).
func (r *Room) All() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Message, len(r.msgs))
	copy(out, r.msgs)
	return out
}
