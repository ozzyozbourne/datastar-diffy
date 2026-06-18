package engine

import (
	"strconv"
	"time"

	"diffy/domain"
)

// run executes a graph node-by-node in topological order, emitting events. An
// approval node suspends the goroutine on a channel until Decide unparks it.
//
// The walk is intentionally derivable from folded state (each node's effect is a
// function of its inputs + events), so a future durable store can resume a run
// by re-driving from Fold(events) rather than always from the top.
func (e *Engine) run(runID string, g *domain.Graph, startNodeID, seed string) {
	order, err := g.TopoSort()
	if err != nil {
		e.emit(runID, domain.Event{Type: domain.EvFlowFailed, Data: map[string]any{"error": err.Error()}})
		return
	}

	// Scope the walk to one flow: when started from a trigger, only run the nodes
	// reachable from it (the rest of the canvas belongs to other flows).
	var scope map[string]bool
	if startNodeID != "" {
		scope = g.Reachable(startNodeID)
	}

	outputs := map[string]string{} // node outputs, for passing downstream

	gather := func(nodeID string) string {
		var in string
		for _, edge := range g.Incoming(nodeID) {
			if v, ok := outputs[edge.FromNode]; ok {
				if in != "" {
					in += "\n"
				}
				in += v
			}
		}
		return in
	}

	for _, nodeID := range order {
		if scope != nil && !scope[nodeID] {
			continue // belongs to another flow on the shared canvas
		}
		n := g.Nodes[nodeID]
		e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvNodeStarted})

		switch n.Kind {
		case domain.KindInput:
			out := n.Config["text"]
			outputs[nodeID] = out
			e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvNodeOutput, Data: map[string]any{"text": out}})

		case domain.KindTrigger:
			outputs[nodeID] = seed // the chat message that fired this flow
			e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvNodeOutput, Data: map[string]any{"text": seed}})

		case domain.KindReply:
			msg := n.Config["message"]
			if msg == "" {
				msg = gather(nodeID)
			}
			outputs[nodeID] = msg
			e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvNodeOutput, Data: map[string]any{"text": msg}})
			if e.OnReply != nil {
				e.OnReply(msg)
			}

		case domain.KindAgent, domain.KindCustom:
			out := e.Agent(n, gather(nodeID))
			outputs[nodeID] = out
			e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvNodeOutput, Data: map[string]any{"text": out}})

		case domain.KindApproval:
			prompt := n.Config["prompt"]
			e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvApprovalRequested, Data: map[string]any{"prompt": prompt}})
			if e.OnApproval != nil {
				e.OnApproval(runID, nodeID, prompt)
			}
			ch := e.registerWait(runID, nodeID)
			d := <-ch // suspend until Decide
			if e.OnApprovalDone != nil {
				e.OnApprovalDone(runID, nodeID)
			}
			if !d.Approved {
				e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvApprovalRejected, Data: map[string]any{"reason": d.Reason}})
				e.emit(runID, domain.Event{Type: domain.EvFlowFailed, Data: map[string]any{"error": "rejected at " + n.Title}})
				return
			}
			e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvApprovalGranted})
			outputs[nodeID] = gather(nodeID) // pass input through

		case domain.KindDelay:
			secs, _ := strconv.Atoi(n.Config["seconds"])
			if secs <= 0 {
				secs = 1
			}
			until := time.Now().Add(time.Duration(secs) * time.Second)
			e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvDelayScheduled, Data: map[string]any{"until": until}})
			time.Sleep(time.Until(until))
			e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvDelayElapsed})
			outputs[nodeID] = gather(nodeID)

		case domain.KindOutput:
			out := gather(nodeID)
			outputs[nodeID] = out
			e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvNodeOutput, Data: map[string]any{"text": out}})
		}

		e.emit(runID, domain.Event{NodeID: nodeID, Type: domain.EvNodeCompleted})
	}

	e.emit(runID, domain.Event{Type: domain.EvFlowFinished})
}
