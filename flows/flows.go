// Package flows is an in-memory registry of named "agentic flows": frozen graph
// snapshots saved from the editor canvas. Chat triggers match against these
// snapshots (not the live canvas), so saving is what registers a flow to fire.
// No database — like the chat/graph, the registry is lost on restart.
package flows

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"diffy/domain"
)

// Saved is one registered flow: a frozen graph snapshot plus the trigger
// keywords that fire it (derived from the snapshot at save time).
type Saved struct {
	ID       string
	Name     string
	Graph    *domain.Graph
	Keywords []string
}

// Registry holds saved flows keyed by name, preserving first-save order.
type Registry struct {
	mu     sync.Mutex
	seq    int
	byName map[string]*Saved
	order  []string // names in first-save order
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{byName: map[string]*Saved{}}
}

// Save freezes a snapshot of g under name and returns the stored flow. Saving an
// existing name overwrites its snapshot (and keeps the same flow ID). The graph
// is deep-copied so later canvas edits don't mutate the saved flow.
func (r *Registry) Save(name string, g *domain.Graph) *Saved {
	r.mu.Lock()
	defer r.mu.Unlock()

	snap := g.Clone()
	kws := triggerKeywords(snap)

	if existing, ok := r.byName[name]; ok {
		snap.ID = existing.ID
		existing.Graph = snap
		existing.Keywords = kws
		return existing
	}

	r.seq++
	s := &Saved{ID: fmt.Sprintf("flow-%d", r.seq), Name: name, Graph: snap, Keywords: kws}
	snap.ID = s.ID
	r.byName[name] = s
	r.order = append(r.order, name)
	return s
}

// All returns the saved flows in first-save order.
func (r *Registry) All() []*Saved {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Saved, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.byName[name])
	}
	return out
}

// triggerKeywords collects the non-empty keywords of a graph's trigger nodes,
// deduped and sorted (same config access as domain.MatchTriggers).
func triggerKeywords(g *domain.Graph) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range g.Nodes {
		if n.Kind != domain.KindTrigger {
			continue
		}
		kw := strings.TrimSpace(n.Config["keyword"])
		if kw != "" && !seen[kw] {
			seen[kw] = true
			out = append(out, kw)
		}
	}
	sort.Strings(out)
	return out
}
