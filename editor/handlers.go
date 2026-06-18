// Package editor wires the canvas: a long-lived SSE read connection plus short
// write endpoints that mutate the graph and broadcast authoritative patches.
package editor

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/starfederation/datastar-go/datastar"

	"diffy/domain"
	"diffy/flows"
	"diffy/hub"
	"diffy/render"
	"diffy/store"
)

// template is a reusable custom-node definition surfaced in the kind dropdown.
// Selecting its Value in the toolbar stamps out a fresh node from it.
type template struct {
	Value  string // dropdown value, e.g. "tmpl:t1"
	Title  string
	Ports  []domain.Port
	Config map[string]string
}

// Editor holds the dependencies shared by the canvas handlers.
type Editor struct {
	Store   *store.Memory
	Hub     *hub.Hub
	Flows   *flows.Registry
	GraphID string

	nodeSeq atomic.Int64
	cliSeq  atomic.Int64
	tmplSeq atomic.Int64

	mu        sync.Mutex
	templates []*template // custom-node templates, in creation order
}

// New constructs an Editor over the given store/hub/graph and saved-flow registry.
func New(s *store.Memory, h *hub.Hub, graphID string, fl *flows.Registry) *Editor {
	return &Editor{Store: s, Hub: h, Flows: fl, GraphID: graphID}
}

// kindOptionsHTML renders the dropdown's <option> list (built-ins + templates).
func (e *Editor) kindOptionsHTML() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	opts := make([]render.KindOption, len(e.templates))
	for i, t := range e.templates {
		opts[i] = render.KindOption{Value: t.Value, Label: t.Title}
	}
	return render.RenderKindOptions(opts)
}

// broadcastKindOptions repaints the kind dropdown on every tab (inner-morph the
// whole option list, so reconnects/duplicate creations never stack options).
func (e *Editor) broadcastKindOptions() {
	html := e.kindOptionsHTML()
	e.Hub.Broadcast(func(sse *datastar.ServerSentEventGenerator) error {
		return sse.PatchElements(html,
			datastar.WithSelector("#kind-select"),
			datastar.WithModeInner())
	})
}

// savedFlowsHTML renders the saved-flows panel (name + trigger keywords).
func (e *Editor) savedFlowsHTML() string {
	saved := e.Flows.All()
	items := make([]render.SavedFlow, len(saved))
	for i, s := range saved {
		items[i] = render.SavedFlow{Name: s.Name, Keywords: s.Keywords}
	}
	return render.RenderSavedFlows(items)
}

// broadcastSavedFlows repaints the saved-flows panel on every tab (inner-morph
// the whole list, so reconnects/duplicate saves never stack entries).
func (e *Editor) broadcastSavedFlows() {
	html := e.savedFlowsHTML()
	e.Hub.Broadcast(func(sse *datastar.ServerSentEventGenerator) error {
		return sse.PatchElements(html,
			datastar.WithSelector("#saved-flows"),
			datastar.WithModeInner())
	})
}

// lookupTemplate returns the template a dropdown value refers to, if any.
func (e *Editor) lookupTemplate(value string) (*template, bool) {
	if !strings.HasPrefix(value, "tmpl:") {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range e.templates {
		if t.Value == value {
			return t, true
		}
	}
	return nil, false
}

func clonePorts(ps []domain.Port) []domain.Port {
	return append([]domain.Port(nil), ps...)
}

func cloneConfig(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
	// Seed the kind dropdown with built-ins + any custom-node templates.
	if err := sse.PatchElements(e.kindOptionsHTML(),
		datastar.WithSelector("#kind-select"),
		datastar.WithModeInner()); err != nil {
		return err
	}
	// Seed the saved-flows panel so new/reconnecting tabs see existing flows.
	if err := sse.PatchElements(e.savedFlowsHTML(),
		datastar.WithSelector("#saved-flows"),
		datastar.WithModeInner()); err != nil {
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
	Draft struct {
		Title      string `json:"title"`
		PortsText  string `json:"portsText"`
		ConfigText string `json:"configText"`
	} `json:"draft"`
	Inspect struct {
		ID         string `json:"id"`
		ConfigText string `json:"configText"`
	} `json:"inspect"`
	Flow struct {
		Name string `json:"name"`
	} `json:"flow"`
}

// nextPlacement allocates a unique node id and a staggered baseline position so
// freshly added nodes don't stack exactly on top of one another.
func (e *Editor) nextPlacement() (id string, x, y float64) {
	seq := e.nodeSeq.Add(1)
	return fmt.Sprintf("n%d", seq), 80.0 + float64((seq%5)*40), 80.0 + float64((seq%7)*40)
}

// broadcastNew paints a newly created node onto every connected tab: it seeds
// the node's baseline position into each client's $pos (only if absent) then
// appends the node element. The write itself returns 204; this is the visible
// change arriving over each tab's /updates stream (the CQRS split).
func (e *Editor) broadcastNew(n *domain.Node) {
	e.Hub.Broadcast(func(sse *datastar.ServerSentEventGenerator) error {
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
}

// AddNode creates a node of the selected kind (POST /nodes).
func (e *Editor) AddNode(w http.ResponseWriter, r *http.Request) {
	var s signals
	if err := datastar.ReadSignals(r, &s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, x, y := e.nextPlacement()

	// A "tmpl:" value selects a saved custom-node template; stamp a fresh node
	// from it (own copies of ports/config so instances stay independent).
	if def, ok := e.lookupTemplate(s.NewKind); ok {
		n := domain.NewCustomNode(id, def.Title, clonePorts(def.Ports), cloneConfig(def.Config), x, y)
		if err := e.Store.AddNode(e.GraphID, n); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		e.broadcastNew(n)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	kind := domain.NodeKind(s.NewKind)
	if kind == "" {
		kind = domain.KindAgent
	}
	n := domain.NewNodeOfKind(id, kind, x, y)

	if err := e.Store.AddNode(e.GraphID, n); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e.broadcastNew(n)
	w.WriteHeader(http.StatusNoContent)
}

// AddCustomNode creates a user-defined node from the draft modal (POST
// /nodes/custom): title + a line-based ports spec + key=value config. It runs
// as an agent at execution time. A malformed ports spec returns 422 and adds
// nothing.
func (e *Editor) AddCustomNode(w http.ResponseWriter, r *http.Request) {
	// Read signals BEFORE opening the SSE (NewSSE consumes the request body).
	var s signals
	if err := datastar.ReadSignals(r, &s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Respond over SSE: validation failures come back as a $draft.error signal
	// shown in the modal (Datastar discards non-2xx bodies, so we can't use a
	// plain HTTP error), and success closes the modal on the posting tab.
	sse := datastar.NewSSE(w, r)
	draftErr := func(msg string) error {
		return sse.MarshalAndPatchSignals(map[string]any{"draft": map[string]any{"error": msg}})
	}

	ports, err := domain.ParsePorts(s.Draft.PortsText)
	if err != nil {
		draftErr(err.Error())
		return
	}
	config := domain.ParseConfig(s.Draft.ConfigText)
	id, x, y := e.nextPlacement()
	n := domain.NewCustomNode(id, s.Draft.Title, ports, config, x, y)

	if err := e.Store.AddNode(e.GraphID, n); err != nil {
		draftErr(err.Error())
		return
	}

	// Register a reusable template (own copies, independent of this instance) and
	// repaint the dropdown so the new kind is selectable on every tab.
	def := &template{
		Value:  fmt.Sprintf("tmpl:t%d", e.tmplSeq.Add(1)),
		Title:  n.Title, // normalized (defaults to "Custom")
		Ports:  clonePorts(ports),
		Config: cloneConfig(config),
	}
	e.mu.Lock()
	e.templates = append(e.templates, def)
	e.mu.Unlock()

	e.broadcastNew(n)
	e.broadcastKindOptions()

	// Success: clear any prior error and close the modal on the posting tab.
	sse.MarshalAndPatchSignals(map[string]any{"draft": map[string]any{"error": "", "open": false}})
}

// ConfigNode updates a node's config from the inspector (POST /nodes/{id}/config).
// The config text is key=value lines (same grammar as the custom-node modal).
// On success it repaints the node (so the summary/inspector seed update) and
// closes the modal on the posting tab.
func (e *Editor) ConfigNode(w http.ResponseWriter, r *http.Request) {
	var s signals
	if err := datastar.ReadSignals(r, &s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nodeID := chi.URLParam(r, "id")
	config := domain.ParseConfig(s.Inspect.ConfigText)

	sse := datastar.NewSSE(w, r)
	if err := e.Store.SetNodeConfig(e.GraphID, nodeID, config); err != nil {
		sse.MarshalAndPatchSignals(map[string]any{"inspect": map[string]any{"error": err.Error()}})
		return
	}

	g, _ := e.Store.GetGraph(e.GraphID)
	if n := g.Nodes[nodeID]; n != nil {
		e.Hub.Broadcast(func(sse *datastar.ServerSentEventGenerator) error {
			return sse.PatchElements(render.RenderNode(n), datastar.WithNamespaceSVG())
		})
	}
	sse.MarshalAndPatchSignals(map[string]any{"inspect": map[string]any{"error": "", "open": false}})
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

// SaveFlow snapshots the current canvas as a named agentic flow in the in-memory
// registry (POST /flows). Saved flows are what chat triggers fire — so we
// require a name and at least one trigger node with a keyword (otherwise the
// flow could never be fired). Validation failures come back as a $flow.error
// signal shown in the modal (mirroring AddCustomNode's draftErr).
func (e *Editor) SaveFlow(w http.ResponseWriter, r *http.Request) {
	var s signals
	if err := datastar.ReadSignals(r, &s); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sse := datastar.NewSSE(w, r)
	flowErr := func(msg string) error {
		return sse.MarshalAndPatchSignals(map[string]any{"flow": map[string]any{"error": msg}})
	}

	name := strings.TrimSpace(s.Flow.Name)
	if name == "" {
		flowErr("name is required")
		return
	}
	g, err := e.Store.GetGraph(e.GraphID)
	if err != nil {
		flowErr(err.Error())
		return
	}
	if !hasTriggerKeyword(g) {
		flowErr("add a Trigger node with a keyword so chat can fire this flow")
		return
	}

	saved := e.Flows.Save(name, g)
	e.broadcastSavedFlows()

	// Success: clear any prior error, close the modal, and confirm on the posting
	// tab which keyword(s) will fire the flow.
	sse.MarshalAndPatchSignals(map[string]any{"flow": map[string]any{
		"error": "",
		"open":  false,
		"saved": fmt.Sprintf("saved %q ✓ — fires on: %s", saved.Name, strings.Join(saved.Keywords, ", ")),
	}})
}

// hasTriggerKeyword reports whether the graph has at least one trigger node with
// a non-empty keyword (same config access as domain.MatchTriggers).
func hasTriggerKeyword(g *domain.Graph) bool {
	for _, n := range g.Nodes {
		if n.Kind == domain.KindTrigger && strings.TrimSpace(n.Config["keyword"]) != "" {
			return true
		}
	}
	return false
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
