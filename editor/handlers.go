// Package editor wires the canvas: a long-lived SSE read connection plus short
// write endpoints that mutate the graph and broadcast authoritative patches.
package editor

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/starfederation/datastar-go/datastar"

	"diffy/domain"
	"diffy/hub"
	"diffy/render"
	"diffy/store"
)

// Editor holds the dependencies shared by the canvas handlers.
type Editor struct {
	Store   *store.Memory
	Hub     *hub.Hub
	GraphID string

	nodeSeq atomic.Int64
	cliSeq  atomic.Int64
}

// New constructs an Editor over the given store/hub/graph.
func New(s *store.Memory, h *hub.Hub, graphID string) *Editor {
	return &Editor{Store: s, Hub: h, GraphID: graphID}
}

// snapshot patches the authoritative full canvas state. It first inner-resets
// #canvas to just the edges layer (a clean slate, so reconnects don't
// duplicate), then appends each node individually. Appending one root at a time
// avoids an idiomorph quirk where inner-morphing a multi-root SVG fragment drops
// sibling <g> nodes.
func (e *Editor) snapshot(sse *datastar.ServerSentEventGenerator) error {
	g, err := e.Store.GetGraph(e.GraphID)
	if err != nil {
		return err
	}
	// Seed client-owned layout from the server baseline. IfMissing so a
	// mid-session SSE reconnect preserves in-session drags, while a fresh page
	// load (empty $pos) gets the baseline → "reset on reload".
	posMap := make(map[string]store.Position, len(g.Nodes))
	for id, n := range g.Nodes {
		posMap[id] = store.Position{X: n.X, Y: n.Y}
	}
	if err := sse.MarshalAndPatchSignalsIfMissing(map[string]any{"pos": posMap}); err != nil {
		return err
	}
	if err := sse.PatchElements(
		render.RenderEdgesLayer(g),
		datastar.WithSelector("#canvas"),
		datastar.WithModeInner(),
		datastar.WithNamespaceSVG(),
	); err != nil {
		return err
	}
	for _, n := range g.Nodes {
		if err := sse.PatchElements(render.RenderNode(n),
			datastar.WithSelector("#canvas"),
			datastar.WithModeAppend(),
			datastar.WithNamespaceSVG()); err != nil {
			return err
		}
	}
	return nil
}

// Updates is the long-lived editor read connection (GET /updates). It sends a
// full snapshot on connect (also handling reconnect) then drains broadcast
// patches until the client disconnects.
func (e *Editor) Updates(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	id := fmt.Sprintf("cli-%d", e.cliSeq.Add(1))
	c := e.Hub.Add(id)
	defer e.Hub.Remove(id)

	if err := e.snapshot(sse); err != nil {
		return
	}

	for {
		select {
		case <-sse.Context().Done():
			return
		case p := <-c.Send:
			if c.Stale() {
				// Recover missed patches with a fresh authoritative snapshot.
				c.ClearStale()
				if err := e.snapshot(sse); err != nil {
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

// signals mirrors the client signal store sent with each write.
type signals struct {
	NewKind string                    `json:"newKind"`
	Pos     map[string]store.Position `json:"pos"`
	Connect struct {
		FromNode string `json:"fromNode"`
		FromPort string `json:"fromPort"`
		ToNode   string `json:"toNode"`
		ToPort   string `json:"toPort"`
	} `json:"connect"`
}

// AddNode creates a node of the selected kind (POST /nodes).
func (e *Editor) AddNode(w http.ResponseWriter, r *http.Request) {
	var s signals
	if err := datastar.ReadSignals(r, &s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	kind := domain.NodeKind(s.NewKind)
	if kind == "" {
		kind = domain.KindAgent
	}
	seq := e.nodeSeq.Add(1)
	id := fmt.Sprintf("n%d", seq)
	// Stagger new nodes so they don't stack exactly.
	x := 80.0 + float64((seq%5)*40)
	y := 80.0 + float64((seq%7)*40)
	n := domain.NewNodeOfKind(id, kind, x, y)

	if err := e.Store.AddNode(e.GraphID, n); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e.Hub.Broadcast(func(sse *datastar.ServerSentEventGenerator) error {
		// Seed the new node's baseline position into each client's $pos (only if
		// the client doesn't already have it), then append the node element.
		if err := sse.MarshalAndPatchSignalsIfMissing(map[string]any{
			"pos": map[string]store.Position{n.ID: {X: n.X, Y: n.Y}},
		}); err != nil {
			return err
		}
		return sse.PatchElements(render.RenderNode(n),
			datastar.WithSelector("#canvas"),
			datastar.WithModeAppend(),
			datastar.WithNamespaceSVG())
	})
	w.WriteHeader(http.StatusNoContent)
}

// Save persists the current client layout as the server baseline (POST /save).
// This is the only time the backend learns x/y. No broadcast: layout is
// client-owned, so other clients keep their own arrangement.
func (e *Editor) Save(w http.ResponseWriter, r *http.Request) {
	var s signals
	if err := datastar.ReadSignals(r, &s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := e.Store.SetPositions(e.GraphID, s.Pos); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sse := datastar.NewSSE(w, r)
	sse.PatchElements(`<span id="save-status" class="text-xs text-emerald-400">saved ✓</span>`)
}

// DeleteNode removes a node and its incident edges (DELETE /nodes/{id}).
func (e *Editor) DeleteNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	if err := e.Store.DeleteNode(e.GraphID, nodeID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	g, _ := e.Store.GetGraph(e.GraphID)
	e.Hub.Broadcast(func(sse *datastar.ServerSentEventGenerator) error {
		if err := sse.RemoveElementByID("node-" + nodeID); err != nil {
			return err
		}
		return sse.PatchElements(render.RenderEdgesLayer(g), datastar.WithNamespaceSVG())
	})
	w.WriteHeader(http.StatusNoContent)
}

// AddEdge validates and creates an edge (POST /edges). Invalid connections
// return 422 and broadcast nothing.
func (e *Editor) AddEdge(w http.ResponseWriter, r *http.Request) {
	var s signals
	if err := datastar.ReadSignals(r, &s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	g, err := e.Store.GetGraph(e.GraphID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	c := s.Connect
	if err := g.CanConnect(c.FromNode, c.FromPort, c.ToNode, c.ToPort); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	edge := &domain.Edge{
		ID:       fmt.Sprintf("%s.%s-%s.%s", c.FromNode, c.FromPort, c.ToNode, c.ToPort),
		FromNode: c.FromNode, FromPort: c.FromPort,
		ToNode: c.ToNode, ToPort: c.ToPort,
	}
	if err := e.Store.AddEdge(e.GraphID, edge); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	g, _ = e.Store.GetGraph(e.GraphID)
	e.Hub.Broadcast(func(sse *datastar.ServerSentEventGenerator) error {
		return sse.PatchElements(render.RenderEdgesLayer(g), datastar.WithNamespaceSVG())
	})
	w.WriteHeader(http.StatusNoContent)
}

// DeleteEdge removes an edge (DELETE /edges/{id}).
func (e *Editor) DeleteEdge(w http.ResponseWriter, r *http.Request) {
	edgeID := chi.URLParam(r, "id")
	if err := e.Store.DeleteEdge(e.GraphID, edgeID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	e.Hub.Broadcast(func(sse *datastar.ServerSentEventGenerator) error {
		return sse.RemoveElementByID("edge-" + edgeID)
	})
	w.WriteHeader(http.StatusNoContent)
}
