package chat

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/starfederation/datastar-go/datastar"

	"diffy/hub"
)

// Chat holds the chat handlers' dependencies. It owns its own hub instance,
// independent of the editor's canvas hub.
type Chat struct {
	Room *Room
	Hub  *hub.Hub

	cliSeq atomic.Int64
}

// New constructs a Chat over the given room and (chat-dedicated) hub.
func New(r *Room, h *hub.Hub) *Chat { return &Chat{Room: r, Hub: h} }

// snapshot paints the full message history into #messages (inner-reset so a
// reconnect doesn't duplicate), then scrolls to the latest.
func (c *Chat) snapshot(sse *datastar.ServerSentEventGenerator) error {
	if err := sse.PatchElements(RenderMessages(c.Room.All()),
		datastar.WithSelector("#messages"),
		datastar.WithModeInner()); err != nil {
		return err
	}
	return scrollToBottom(sse)
}

// Updates is the long-lived chat read connection (GET /chat/updates): snapshot
// on connect (also handling reconnect) then drain broadcast patches until the
// client disconnects. Mirrors editor.Updates.
func (c *Chat) Updates(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	id := fmt.Sprintf("chat-%d", c.cliSeq.Add(1))
	client := c.Hub.Add(id)
	defer c.Hub.Remove(id)

	if err := c.snapshot(sse); err != nil {
		return
	}

	for {
		select {
		case <-sse.Context().Done():
			return
		case p := <-client.Send:
			if client.Stale() {
				client.ClearStale()
				if err := c.snapshot(sse); err != nil {
					return
				}
				continue
			}
			if err := p(sse); err != nil {
				return
			}
		}
	}
}

// signals mirrors the chat client signals sent with each write.
type signals struct {
	Chat struct {
		User    string `json:"user"`
		Message string `json:"message"`
	} `json:"chat"`
}

// Send appends a message and broadcasts it to every connected tab (POST /chat).
// Returns 204 with no body — the poster sees the message arrive over its own
// /chat/updates stream (the CQRS split, like editor.AddNode).
func (c *Chat) Send(w http.ResponseWriter, r *http.Request) {
	var s signals
	if err := datastar.ReadSignals(r, &s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(s.Chat.Message)
	if text == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	user := strings.TrimSpace(s.Chat.User)
	if user == "" {
		user = "anon"
	}

	m := c.Room.Add(user, text)
	c.Hub.Broadcast(func(sse *datastar.ServerSentEventGenerator) error {
		if err := sse.PatchElements(RenderMessage(m),
			datastar.WithSelector("#messages"),
			datastar.WithModeAppend()); err != nil {
			return err
		}
		return scrollToBottom(sse)
	})
	w.WriteHeader(http.StatusNoContent)
}

// scrollToBottom keeps the newest message in view after a patch. Written as a
// single declaration-free expression: each ExecuteScript runs as its own global
// <script>, so a top-level `const` would clash ("already declared") on the next.
func scrollToBottom(sse *datastar.ServerSentEventGenerator) error {
	return sse.ExecuteScript(`document.getElementById('messages')?.scrollTo(0,1e9)`)
}
