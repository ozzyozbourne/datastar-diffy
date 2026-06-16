// Package runs serves the run view: start, the page shell, the projection SSE,
// and approve/reject writes.
package runs

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/starfederation/datastar-go/datastar"

	"diffy/domain"
	"diffy/engine"
	"diffy/render"
)

// Runs holds the run handlers' dependencies.
type Runs struct {
	Engine   *engine.Engine
	GraphID  string
	pageTmpl string
}

// New constructs Runs with the embedded run-page template.
func New(eng *engine.Engine, graphID, pageTemplate string) *Runs {
	return &Runs{Engine: eng, GraphID: graphID, pageTmpl: pageTemplate}
}

// Start launches a run and redirects the client to its page (POST /runs).
func (rs *Runs) Start(w http.ResponseWriter, r *http.Request) {
	runID, err := rs.Engine.StartRun(rs.GraphID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sse := datastar.NewSSE(w, r)
	sse.Redirect("/runs/" + runID)
}

// Page serves the run view shell (GET /runs/{id}).
func (rs *Runs) Page(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(strings.ReplaceAll(rs.pageTmpl, "__RUNID__", runID)))
}

// Approve resolves an approval gate (POST /runs/{id}/nodes/{nid}/approve).
func (rs *Runs) Approve(w http.ResponseWriter, r *http.Request) {
	rs.decide(w, r, engine.Decision{Approved: true})
}

// Reject rejects an approval gate (POST /runs/{id}/nodes/{nid}/reject).
func (rs *Runs) Reject(w http.ResponseWriter, r *http.Request) {
	rs.decide(w, r, engine.Decision{Approved: false, Reason: "rejected by user"})
}

func (rs *Runs) decide(w http.ResponseWriter, r *http.Request, d engine.Decision) {
	runID := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "nid")
	if err := rs.Engine.Decide(runID, nodeID, d); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Updates is the long-lived run projection (GET /runs/{id}/updates). It folds
// the existing log into a full layout on connect (replay / reconnect) then
// streams incremental patches, deduping against the snapshot by Seq.
func (rs *Runs) Updates(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	g, err := rs.Engine.Graph(rs.GraphID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Subscribe before reading existing events so nothing emitted in between is
	// lost; the snapshot's LastSeq dedupes any overlap.
	stream, cancel := rs.Engine.Subscribe(runID)
	defer cancel()

	sse := datastar.NewSSE(w, r)

	existing, _ := rs.Engine.Events(runID)
	view := domain.Fold(existing)
	view.RunID = runID
	lastSeq := view.LastSeq

	if err := sse.PatchElements(render.RenderRunLayout(g, &view),
		datastar.WithSelector("#run"), datastar.WithModeInner()); err != nil {
		return
	}

	for {
		select {
		case <-sse.Context().Done():
			return
		case ev := <-stream:
			if ev.Seq <= lastSeq {
				continue // already in the snapshot
			}
			domain.ApplyEvent(&view, ev)
			lastSeq = ev.Seq
			if err := rs.patchEvent(sse, g, &view, ev); err != nil {
				return
			}
		}
	}
}

// patchEvent emits the minimal patches for one event. Badge + output for the
// node, plus the (idempotent) approval panel and banner, cover all transitions.
func (rs *Runs) patchEvent(sse *datastar.ServerSentEventGenerator, g *domain.Graph, v *domain.RunView, ev domain.Event) error {
	id := strconv.Itoa(ev.Seq)
	if ev.NodeID != "" {
		nv := v.Nodes[ev.NodeID]
		if err := sse.PatchElements(render.RenderStatusBadge(v.RunID, ev.NodeID, nv.Status),
			datastar.WithPatchElementsEventID(id)); err != nil {
			return err
		}
		if err := sse.PatchElements(render.RenderOutput(v.RunID, ev.NodeID, nv.Output)); err != nil {
			return err
		}
	}
	if err := sse.PatchElements(render.RenderApprovalPanel(v.RunID, v.Pending)); err != nil {
		return err
	}
	return sse.PatchElements(render.RenderBanner(v.RunID, v))
}
