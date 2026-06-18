package domain

import "fmt"

// PortDir is the direction of a port.
type PortDir string

const (
	PortIn  PortDir = "in"
	PortOut PortDir = "out"
)

// PortType is the data type carried by a port. Connections are only allowed
// between ports of the same type (out -> in).
type PortType string

const (
	TypeTrigger PortType = "trigger" // control-flow start signal
	TypeText    PortType = "text"    // textual data
	TypeAgent   PortType = "agent"   // agent message/result
)

// Port is a typed connection point on a node.
type Port struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Dir  PortDir  `json:"dir"`
	Type PortType `json:"type"`
}

// NodeKind selects the executor behaviour for a node.
type NodeKind string

const (
	KindInput    NodeKind = "input"
	KindAgent    NodeKind = "agent"
	KindTrigger  NodeKind = "trigger"
	KindApproval NodeKind = "approval"
	KindDelay    NodeKind = "delay"
	KindReply    NodeKind = "reply"
	KindOutput   NodeKind = "output"
	KindCustom   NodeKind = "custom"
)

// Node is a single box on the canvas.
type Node struct {
	ID     string            `json:"id"`
	Kind   NodeKind          `json:"kind"`
	Title  string            `json:"title"`
	X      float64           `json:"x"`
	Y      float64           `json:"y"`
	Ports  []Port            `json:"ports"`
	Config map[string]string `json:"config"`
}

// Port looks up a port by id, returning nil if absent.
func (n *Node) Port(id string) *Port {
	for i := range n.Ports {
		if n.Ports[i].ID == id {
			return &n.Ports[i]
		}
	}
	return nil
}

// Edge connects an output port to an input port.
type Edge struct {
	ID       string `json:"id"`
	FromNode string `json:"fromNode"`
	FromPort string `json:"fromPort"`
	ToNode   string `json:"toNode"`
	ToPort   string `json:"toPort"`
}

// Graph is the editable flow document. Version is bumped on every mutation.
type Graph struct {
	ID      string           `json:"id"`
	Version int              `json:"version"`
	Nodes   map[string]*Node `json:"nodes"`
	Edges   map[string]*Edge `json:"edges"`
}

// NewGraph returns an empty graph.
func NewGraph(id string) *Graph {
	return &Graph{
		ID:    id,
		Nodes: map[string]*Node{},
		Edges: map[string]*Edge{},
	}
}

// Clone returns a deep copy of the graph: fresh Nodes/Edges maps with copied
// Node structs (own Ports slice and Config map) and Edge structs. Used to freeze
// a snapshot that stays independent of later edits to the live graph.
func (g *Graph) Clone() *Graph {
	out := &Graph{
		ID:      g.ID,
		Version: g.Version,
		Nodes:   make(map[string]*Node, len(g.Nodes)),
		Edges:   make(map[string]*Edge, len(g.Edges)),
	}
	for id, n := range g.Nodes {
		cn := *n
		cn.Ports = append([]Port(nil), n.Ports...)
		cn.Config = make(map[string]string, len(n.Config))
		for k, v := range n.Config {
			cn.Config[k] = v
		}
		out.Nodes[id] = &cn
	}
	for id, e := range g.Edges {
		ce := *e
		out.Edges[id] = &ce
	}
	return out
}

// CanConnect validates a proposed edge: ports must exist, be out->in, share a
// type, not form a self-loop, and not duplicate an existing edge. This is where
// typed ports earn their keep.
func (g *Graph) CanConnect(fromNode, fromPort, toNode, toPort string) error {
	if fromNode == toNode {
		return fmt.Errorf("cannot connect a node to itself")
	}
	from, ok := g.Nodes[fromNode]
	if !ok {
		return fmt.Errorf("from node %q not found", fromNode)
	}
	to, ok := g.Nodes[toNode]
	if !ok {
		return fmt.Errorf("to node %q not found", toNode)
	}
	fp := from.Port(fromPort)
	if fp == nil {
		return fmt.Errorf("from port %q not found", fromPort)
	}
	tp := to.Port(toPort)
	if tp == nil {
		return fmt.Errorf("to port %q not found", toPort)
	}
	if fp.Dir != PortOut {
		return fmt.Errorf("from port %q is not an output", fromPort)
	}
	if tp.Dir != PortIn {
		return fmt.Errorf("to port %q is not an input", toPort)
	}
	if fp.Type != tp.Type {
		return fmt.Errorf("type mismatch: %s -> %s", fp.Type, tp.Type)
	}
	for _, e := range g.Edges {
		if e.FromNode == fromNode && e.FromPort == fromPort &&
			e.ToNode == toNode && e.ToPort == toPort {
			return fmt.Errorf("edge already exists")
		}
	}
	return nil
}

// Outgoing returns edges whose source is nodeID.
func (g *Graph) Outgoing(nodeID string) []*Edge {
	var out []*Edge
	for _, e := range g.Edges {
		if e.FromNode == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// Incoming returns edges whose target is nodeID.
func (g *Graph) Incoming(nodeID string) []*Edge {
	var in []*Edge
	for _, e := range g.Edges {
		if e.ToNode == nodeID {
			in = append(in, e)
		}
	}
	return in
}
