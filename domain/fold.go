package domain

import "time"

// NodeStatus is the projected execution state of a single node.
type NodeStatus string

const (
	StatusPending   NodeStatus = "pending"
	StatusRunning   NodeStatus = "running"
	StatusAwaiting  NodeStatus = "awaiting_approval"
	StatusDelaying  NodeStatus = "delaying"
	StatusDone      NodeStatus = "done"
	StatusFailed    NodeStatus = "failed"
	StatusRejected  NodeStatus = "rejected"
)

// RunNodeView is the projected state of one node within a run.
type RunNodeView struct {
	NodeID     string
	Status     NodeStatus
	Output     string
	DelayUntil *time.Time
}

// PendingApproval describes an outstanding approval gate.
type PendingApproval struct {
	NodeID string
	Prompt string
}

// RunView is the full projection of a run, derived purely from its events.
type RunView struct {
	RunID    string
	Nodes    map[string]*RunNodeView
	Pending  *PendingApproval
	Finished bool
	Failed   bool
	Error    string
	LastSeq  int
}

// node returns (creating if needed) the view for a node.
func (v *RunView) node(id string) *RunNodeView {
	if v.Nodes == nil {
		v.Nodes = map[string]*RunNodeView{}
	}
	n, ok := v.Nodes[id]
	if !ok {
		n = &RunNodeView{NodeID: id, Status: StatusPending}
		v.Nodes[id] = n
	}
	return n
}

// ApplyEvent folds a single event into the view in place. Fold == repeated
// ApplyEvent over an empty view; the two must always agree.
func ApplyEvent(v *RunView, e Event) {
	if v.RunID == "" {
		v.RunID = e.RunID
	}
	if e.Seq > v.LastSeq {
		v.LastSeq = e.Seq
	}

	switch e.Type {
	case EvNodeStarted:
		v.node(e.NodeID).Status = StatusRunning
	case EvNodeOutput:
		v.node(e.NodeID).Output = e.Str("text")
	case EvApprovalRequested:
		v.node(e.NodeID).Status = StatusAwaiting
		v.Pending = &PendingApproval{NodeID: e.NodeID, Prompt: e.Str("prompt")}
	case EvApprovalGranted:
		v.node(e.NodeID).Status = StatusRunning
		if v.Pending != nil && v.Pending.NodeID == e.NodeID {
			v.Pending = nil
		}
	case EvApprovalRejected:
		v.node(e.NodeID).Status = StatusRejected
		if v.Pending != nil && v.Pending.NodeID == e.NodeID {
			v.Pending = nil
		}
	case EvDelayScheduled:
		n := v.node(e.NodeID)
		n.Status = StatusDelaying
		if until, ok := e.Time("until"); ok {
			n.DelayUntil = &until
		}
	case EvDelayElapsed:
		n := v.node(e.NodeID)
		n.Status = StatusRunning
		n.DelayUntil = nil
	case EvNodeCompleted:
		v.node(e.NodeID).Status = StatusDone
	case EvFlowFinished:
		v.Finished = true
	case EvFlowFailed:
		v.Failed = true
		v.Finished = true
		v.Error = e.Str("error")
	}
}

// Fold reconstructs a run view from its full (ordered) event log.
func Fold(events []Event) RunView {
	v := RunView{Nodes: map[string]*RunNodeView{}}
	for _, e := range events {
		ApplyEvent(&v, e)
	}
	return v
}
