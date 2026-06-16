package domain

import "fmt"

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
