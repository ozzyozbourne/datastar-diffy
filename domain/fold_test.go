package domain

import (
	"testing"
	"time"
)

func ev(seq int, t EventType, node string, data map[string]any) Event {
	return Event{Seq: seq, RunID: "run-1", NodeID: node, Type: t, At: time.Now(), Data: data}
}

func TestFold_NodeLifecycle(t *testing.T) {
	v := Fold([]Event{
		ev(1, EvNodeStarted, "a", nil),
		ev(2, EvNodeOutput, "a", map[string]any{"text": "hello"}),
		ev(3, EvNodeCompleted, "a", nil),
	})
	n := v.Nodes["a"]
	if n == nil {
		t.Fatal("node a missing")
	}
	if n.Status != StatusDone {
		t.Errorf("status = %q, want done", n.Status)
	}
	if n.Output != "hello" {
		t.Errorf("output = %q, want hello", n.Output)
	}
	if v.LastSeq != 3 {
		t.Errorf("lastSeq = %d, want 3", v.LastSeq)
	}
}

func TestFold_ApprovalPending(t *testing.T) {
	v := Fold([]Event{
		ev(1, EvNodeStarted, "gate", nil),
		ev(2, EvApprovalRequested, "gate", map[string]any{"prompt": "ok?"}),
	})
	if v.Pending == nil {
		t.Fatal("expected pending approval")
	}
	if v.Pending.NodeID != "gate" || v.Pending.Prompt != "ok?" {
		t.Errorf("pending = %+v", v.Pending)
	}
	if v.Nodes["gate"].Status != StatusAwaiting {
		t.Errorf("status = %q, want awaiting_approval", v.Nodes["gate"].Status)
	}
}

func TestFold_ApprovalGrantedClearsPending(t *testing.T) {
	v := Fold([]Event{
		ev(1, EvApprovalRequested, "gate", map[string]any{"prompt": "ok?"}),
		ev(2, EvApprovalGranted, "gate", nil),
	})
	if v.Pending != nil {
		t.Errorf("pending should be cleared, got %+v", v.Pending)
	}
	if v.Nodes["gate"].Status != StatusRunning {
		t.Errorf("status = %q, want running", v.Nodes["gate"].Status)
	}
}

func TestFold_DelayScheduledThenElapsed(t *testing.T) {
	until := time.Now().Add(10 * time.Second)
	v := Fold([]Event{
		ev(1, EvDelayScheduled, "wait", map[string]any{"until": until}),
	})
	if v.Nodes["wait"].Status != StatusDelaying || v.Nodes["wait"].DelayUntil == nil {
		t.Fatalf("expected delaying with DelayUntil, got %+v", v.Nodes["wait"])
	}
	ApplyEvent(&v, ev(2, EvDelayElapsed, "wait", nil))
	if v.Nodes["wait"].Status != StatusRunning || v.Nodes["wait"].DelayUntil != nil {
		t.Errorf("after elapse: %+v", v.Nodes["wait"])
	}
}

func TestFold_FlowFailed(t *testing.T) {
	v := Fold([]Event{
		ev(1, EvFlowFailed, "", map[string]any{"error": "boom"}),
	})
	if !v.Failed || !v.Finished || v.Error != "boom" {
		t.Errorf("failed view = %+v", v)
	}
}

// Fold must equal repeated ApplyEvent over an empty view.
func TestFold_AgreesWithApplyEvent(t *testing.T) {
	events := []Event{
		ev(1, EvNodeStarted, "a", nil),
		ev(2, EvNodeOutput, "a", map[string]any{"text": "x"}),
		ev(3, EvNodeCompleted, "a", nil),
		ev(4, EvNodeStarted, "b", nil),
	}
	folded := Fold(events)

	var manual RunView
	for _, e := range events {
		ApplyEvent(&manual, e)
	}
	if manual.LastSeq != folded.LastSeq {
		t.Errorf("lastSeq mismatch: %d vs %d", manual.LastSeq, folded.LastSeq)
	}
	for id, fn := range folded.Nodes {
		mn := manual.Nodes[id]
		if mn == nil || mn.Status != fn.Status || mn.Output != fn.Output {
			t.Errorf("node %q mismatch: %+v vs %+v", id, mn, fn)
		}
	}
}
