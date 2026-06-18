package domain

import (
	"fmt"
	"strings"
)

// TopoSort returns node IDs in a topological order (sources first). It returns
// an error if the graph contains a cycle. Ordering among independent nodes is
// stabilised by insertion order of discovery.
func (g *Graph) TopoSort() ([]string, error) {
	indeg := make(map[string]int, len(g.Nodes))
	for id := range g.Nodes {
		indeg[id] = 0
	}
	for _, e := range g.Edges {
		if _, ok := g.Nodes[e.ToNode]; ok {
			indeg[e.ToNode]++
		}
	}

	// Seed the queue with all zero-indegree nodes.
	var queue []string
	for id := range g.Nodes {
		if indeg[id] == 0 {
			queue = append(queue, id)
		}
	}

	var order []string
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		order = append(order, n)
		for _, e := range g.Outgoing(n) {
			indeg[e.ToNode]--
			if indeg[e.ToNode] == 0 {
				queue = append(queue, e.ToNode)
			}
		}
	}

	if len(order) != len(g.Nodes) {
		return nil, fmt.Errorf("graph has a cycle")
	}
	return order, nil
}

// Reachable returns the set of node IDs reachable from startID by following
// outgoing edges (startID included). It scopes a run to a single flow when
// several trigger-rooted flows share one canvas.
func (g *Graph) Reachable(startID string) map[string]bool {
	seen := map[string]bool{}
	if _, ok := g.Nodes[startID]; !ok {
		return seen
	}
	queue := []string{startID}
	seen[startID] = true
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, e := range g.Outgoing(n) {
			if !seen[e.ToNode] {
				seen[e.ToNode] = true
				queue = append(queue, e.ToNode)
			}
		}
	}
	return seen
}

// MatchTriggers returns the trigger nodes whose configured keyword is a
// non-empty, case-insensitive substring of text. A chat message can fan out to
// every matching trigger (each starts its own run).
func (g *Graph) MatchTriggers(text string) []*Node {
	hay := strings.ToLower(text)
	var out []*Node
	for _, n := range g.Nodes {
		if n.Kind != KindTrigger {
			continue
		}
		kw := strings.ToLower(strings.TrimSpace(n.Config["keyword"]))
		if kw != "" && strings.Contains(hay, kw) {
			out = append(out, n)
		}
	}
	return out
}
