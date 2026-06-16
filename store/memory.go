package store

import (
	"fmt"
	"sync"

	"diffy/domain"
)

// Memory is an in-memory GraphStore + RunStore guarded by a mutex. It satisfies
// both interfaces so a DB-backed store can be slotted in later.
type Memory struct {
	mu     sync.RWMutex
	graphs map[string]*domain.Graph
	runs   map[string][]domain.Event
	runSeq int
}

var (
	_ GraphStore = (*Memory)(nil)
	_ RunStore   = (*Memory)(nil)
)

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		graphs: map[string]*domain.Graph{},
		runs:   map[string][]domain.Event{},
	}
}

// PutGraph installs a graph (used for seeding).
func (m *Memory) PutGraph(g *domain.Graph) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.graphs[g.ID] = g
}

func (m *Memory) GetGraph(id string) (*domain.Graph, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.graphs[id]
	if !ok {
		return nil, fmt.Errorf("graph %q not found", id)
	}
	return g, nil
}

func (m *Memory) AddNode(graphID string, n *domain.Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.graphs[graphID]
	if !ok {
		return fmt.Errorf("graph %q not found", graphID)
	}
	g.Nodes[n.ID] = n
	g.Version++
	return nil
}

func (m *Memory) SetPositions(graphID string, pos map[string]Position) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.graphs[graphID]
	if !ok {
		return fmt.Errorf("graph %q not found", graphID)
	}
	for id, p := range pos {
		if n, ok := g.Nodes[id]; ok {
			n.X, n.Y = p.X, p.Y
		}
	}
	g.Version++
	return nil
}

func (m *Memory) DeleteNode(graphID, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.graphs[graphID]
	if !ok {
		return fmt.Errorf("graph %q not found", graphID)
	}
	delete(g.Nodes, nodeID)
	for id, e := range g.Edges {
		if e.FromNode == nodeID || e.ToNode == nodeID {
			delete(g.Edges, id)
		}
	}
	g.Version++
	return nil
}

func (m *Memory) AddEdge(graphID string, e *domain.Edge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.graphs[graphID]
	if !ok {
		return fmt.Errorf("graph %q not found", graphID)
	}
	g.Edges[e.ID] = e
	g.Version++
	return nil
}

func (m *Memory) DeleteEdge(graphID, edgeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.graphs[graphID]
	if !ok {
		return fmt.Errorf("graph %q not found", graphID)
	}
	delete(g.Edges, edgeID)
	g.Version++
	return nil
}

func (m *Memory) CreateRun(graphID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runSeq++
	runID := fmt.Sprintf("run-%d", m.runSeq)
	m.runs[runID] = []domain.Event{}
	return runID, nil
}

func (m *Memory) AppendEvent(runID string, e domain.Event) (domain.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	log, ok := m.runs[runID]
	if !ok {
		return domain.Event{}, fmt.Errorf("run %q not found", runID)
	}
	e.RunID = runID
	e.Seq = len(log) + 1
	m.runs[runID] = append(log, e)
	return e, nil
}

func (m *Memory) Events(runID string) ([]domain.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	log, ok := m.runs[runID]
	if !ok {
		return nil, fmt.Errorf("run %q not found", runID)
	}
	out := make([]domain.Event, len(log))
	copy(out, log)
	return out, nil
}

func (m *Memory) EventsSince(runID string, seq int) ([]domain.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	log, ok := m.runs[runID]
	if !ok {
		return nil, fmt.Errorf("run %q not found", runID)
	}
	var out []domain.Event
	for _, e := range log {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out, nil
}
