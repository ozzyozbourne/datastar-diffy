package store

import "diffy/domain"

// Position is a node's baseline layout coordinate (presentation, not part of
// the executable graph). Updated only on Save.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// GraphStore persists the editable flow document. Mutations bump Graph.Version.
type GraphStore interface {
	GetGraph(id string) (*domain.Graph, error)
	AddNode(graphID string, n *domain.Node) error
	SetNodeConfig(graphID, nodeID string, config map[string]string) error
	SetPositions(graphID string, pos map[string]Position) error
	DeleteNode(graphID, nodeID string) error
	AddEdge(graphID string, e *domain.Edge) error
	DeleteEdge(graphID, edgeID string) error
}

// RunStore is the append-only event log for runs (event sourcing).
type RunStore interface {
	CreateRun(graphID string) (runID string, err error)
	AppendEvent(runID string, e domain.Event) (domain.Event, error) // assigns Seq
	Events(runID string) ([]domain.Event, error)
	EventsSince(runID string, seq int) ([]domain.Event, error) // strictly > seq
}
