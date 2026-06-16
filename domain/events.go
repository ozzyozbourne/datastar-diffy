package domain

import "time"

// EventType enumerates the append-only run events. A run's state is the fold of
// its event log (event sourcing).
type EventType string

const (
	EvNodeStarted       EventType = "NodeStarted"
	EvNodeOutput        EventType = "NodeOutput"
	EvApprovalRequested EventType = "ApprovalRequested"
	EvApprovalGranted   EventType = "ApprovalGranted"
	EvApprovalRejected  EventType = "ApprovalRejected"
	EvDelayScheduled    EventType = "DelayScheduled"
	EvDelayElapsed      EventType = "DelayElapsed"
	EvNodeCompleted     EventType = "NodeCompleted"
	EvFlowFinished      EventType = "FlowFinished"
	EvFlowFailed        EventType = "FlowFailed"
)

// Event is a single immutable fact about a run. Seq is monotonic per run and is
// assigned by the store on append; it doubles as the SSE event id for replay.
type Event struct {
	Seq    int            `json:"seq"`
	RunID  string         `json:"runId"`
	NodeID string         `json:"nodeId,omitempty"`
	Type   EventType      `json:"type"`
	At     time.Time      `json:"at"`
	Data   map[string]any `json:"data,omitempty"`
}

// Str reads a string value from the event payload.
func (e Event) Str(key string) string {
	if e.Data == nil {
		return ""
	}
	if v, ok := e.Data[key].(string); ok {
		return v
	}
	return ""
}

// Time reads a time value from the event payload. JSON round-trips times as
// strings, so accept both time.Time and RFC3339 strings.
func (e Event) Time(key string) (time.Time, bool) {
	if e.Data == nil {
		return time.Time{}, false
	}
	switch v := e.Data[key].(type) {
	case time.Time:
		return v, true
	case string:
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
