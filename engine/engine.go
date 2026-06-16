// Package engine runs flows as append-only event logs (event sourcing). A run's
// UI is a pure projection of its events, streamed to subscribers over SSE.
package engine

import (
	"fmt"
	"sync"
	"time"

	"diffy/domain"
	"diffy/store"
)

// Decision resolves an approval gate.
type Decision struct {
	Approved bool
	Reason   string
}

// AgentFunc executes an agent node. Stubbed by default; real LLM calls slot in
// here without touching the executor.
type AgentFunc func(n *domain.Node, input string) string

// runHub fans a run's events out to its live subscribers.
type runHub struct {
	mu   sync.Mutex
	seq  int
	subs map[int]chan domain.Event
}

func newRunHub() *runHub { return &runHub{subs: map[int]chan domain.Event{}} }

func (h *runHub) add() (int, chan domain.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	ch := make(chan domain.Event, 256)
	h.subs[h.seq] = ch
	return h.seq, ch
}

func (h *runHub) remove(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, id)
}

func (h *runHub) publish(e domain.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- e:
		default: // slow subscriber; it will re-fold on reconnect
		}
	}
}

// Engine owns run execution and fan-out.
type Engine struct {
	graphs store.GraphStore
	runs   store.RunStore
	Agent  AgentFunc

	mu    sync.Mutex
	hubs  map[string]*runHub
	waits map[string]chan Decision // key: runID|nodeID
}

// New constructs an Engine.
func New(graphs store.GraphStore, runs store.RunStore) *Engine {
	return &Engine{
		graphs: graphs,
		runs:   runs,
		Agent:  stubAgent,
		hubs:   map[string]*runHub{},
		waits:  map[string]chan Decision{},
	}
}

func stubAgent(n *domain.Node, input string) string {
	time.Sleep(600 * time.Millisecond) // simulate work
	prompt := n.Config["prompt"]
	if input == "" {
		return fmt.Sprintf("[%s] %s", prompt, "(no input)")
	}
	return fmt.Sprintf("[%s] processed: %s", prompt, input)
}

func (e *Engine) hub(runID string) *runHub {
	e.mu.Lock()
	defer e.mu.Unlock()
	h, ok := e.hubs[runID]
	if !ok {
		h = newRunHub()
		e.hubs[runID] = h
	}
	return h
}

// emit persists an event (assigning Seq) then publishes it to subscribers.
func (e *Engine) emit(runID string, ev domain.Event) domain.Event {
	ev.At = time.Now()
	stored, err := e.runs.AppendEvent(runID, ev)
	if err != nil {
		return ev
	}
	e.hub(runID).publish(stored)
	return stored
}

// Subscribe registers a live event subscriber for a run; call cancel to stop.
func (e *Engine) Subscribe(runID string) (events <-chan domain.Event, cancel func()) {
	h := e.hub(runID)
	id, ch := h.add()
	return ch, func() { h.remove(id) }
}

// Events returns the full persisted log for a run.
func (e *Engine) Events(runID string) ([]domain.Event, error) {
	return e.runs.Events(runID)
}

// Graph returns the graph a run executes against.
func (e *Engine) Graph(graphID string) (*domain.Graph, error) {
	return e.graphs.GetGraph(graphID)
}

// StartRun creates a run and launches its executor goroutine.
func (e *Engine) StartRun(graphID string) (string, error) {
	g, err := e.graphs.GetGraph(graphID)
	if err != nil {
		return "", err
	}
	runID, err := e.runs.CreateRun(graphID)
	if err != nil {
		return "", err
	}
	go e.run(runID, g)
	return runID, nil
}

// registerWait creates the resume channel an approval node parks on.
func (e *Engine) registerWait(runID, nodeID string) chan Decision {
	ch := make(chan Decision, 1)
	e.mu.Lock()
	e.waits[runID+"|"+nodeID] = ch
	e.mu.Unlock()
	return ch
}

// Decide resolves an outstanding approval, unparking the executor.
func (e *Engine) Decide(runID, nodeID string, d Decision) error {
	e.mu.Lock()
	ch, ok := e.waits[runID+"|"+nodeID]
	if ok {
		delete(e.waits, runID+"|"+nodeID)
	}
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending approval for %s/%s", runID, nodeID)
	}
	ch <- d
	return nil
}
