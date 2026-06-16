# Understanding Diffy

A study guide to the codebase: **what each piece does and why it exists.** Read this top-to-bottom once, then keep it open while you read the actual files.

---

## 1. The big idea (read this first)

Diffy is a **node-based agentic-flow editor + runner** — like React Flow, but with one deliberate inversion:

> **The server owns the state. The browser is just a projection of it.**

In React Flow, the graph lives in the browser (React state) and you sync a copy to the server. Diffy does the opposite: the **graph lives on the server**, and the browser is a thin view that the server "paints" by streaming HTML/state down a long-lived connection. We do this because Diffy runs **agent workflows** — the execution engine, validation, and (eventually) persistence already live server-side, so keeping the graph there means **one source of truth and no client/server sync bugs.**

The one exception is **node positions (layout)**, which we deliberately moved to the *client* — see §7. That exception is itself a lesson in "what is actually source-of-truth vs. what is just presentation."

The tech that makes server-owned UI practical is **Datastar** (a hypermedia framework): the server streams DOM patches and signal updates over **SSE** (Server-Sent Events); the browser applies them. We use the **open-source** Datastar core + its Go SDK — *not* "Rocket" (Datastar Pro's closed-source component layer), which we don't need.

**Two mental models you must hold:**
1. **CQRS** — *reads* are one long-lived SSE stream (server → browser); *writes* are short HTTP POSTs (browser → server). A write never renders its own response; it mutates state and the change comes back over the existing SSE stream.
2. **Event sourcing (for runs)** — a flow *execution* is an append-only log of events; the run UI is a pure fold over that log.

---

## 2. The 30-second request lifecycle

```
Browser                              Server
  │  GET /            ───────────►   serve index.html (has the Datastar <script>)
  │  GET /updates     ───────────►   open SSE; push snapshot; keep streaming  ◄── long-lived READ
  │                                    │
  │  POST /nodes      ───────────►   mutate graph, broadcast to all SSE clients  ◄── short WRITE
  │  ◄─────────────────────────────  (the new node arrives on the /updates stream)
```

The browser almost never "renders" anything itself. It sends intents (POSTs) and **displays whatever the server streams back.**

---

## 3. Directory map (what each package is for)

```
main.go            Wires everything together: store + hub + engine, and all HTTP routes.
web/               The two HTML shells the browser loads (editor + run view).
domain/            Pure data + rules. No HTTP, no Datastar. The "truth" of what a graph/run IS.
store/             Persistence (currently in-memory) behind interfaces, so a DB can slot in later.
render/            Turns domain objects into HTML/SVG strings to stream to the browser.
hub/               Fan-out: broadcasts a change to every connected editor tab.
editor/            HTTP handlers for the canvas (the long-lived stream + the write endpoints).
engine/            The run executor: walks the graph, emits events, suspends on approvals.
runs/              HTTP handlers for watching a run (its own SSE projection) + approve/reject.
```

The dependency direction is clean: `domain` depends on nothing; everything else depends on `domain`; `main` depends on everything.

---

## 4. `domain/` — the source of truth (start your code reading here)

This package has **no framework code** — it's just Go types and rules. If you understand `domain/`, you understand what Diffy *is*.

- **`graph.go`** — `Node`, `Edge`, `Port`, `Graph`.
  - A `Node` has a `Kind` (input/agent/approval/delay/output), a `Config` map, an `X/Y` (baseline layout — see §7), and typed `Port`s.
  - **`CanConnect(...)`** is the key rule: an edge is only valid if it goes output→input, the port *types* match, it's not a self-loop, and not a duplicate. *Why:* typed ports are what make an agent flow meaningful — you can't wire incompatible things together.
  - `Incoming/Outgoing` — used by the executor to pass data along edges.
- **`nodes.go`** — `NewNodeOfKind`: the default ports/config for each node kind. *Why separate:* keeps the "what does an Agent node look like" knowledge in one place.
- **`topo.go`** — `TopoSort`: returns nodes in dependency order (and detects cycles). *Why:* the executor must run a node only after its inputs are ready.
- **`events.go`** — `Event` + the `EventType` constants (`NodeStarted`, `ApprovalRequested`, `DelayElapsed`, `FlowFinished`, …). This is the **vocabulary of a run**. `Seq` is a per-run monotonic number that doubles as the SSE event id (for reconnection replay).
- **`fold.go`** — **the heart of event sourcing.** `Fold(events) → RunView` reconstructs the current state of a run purely from its event log. `ApplyEvent` folds one event in place. *Why this matters:* the run UI is never "stored" — it's always *derived* from events. That's what gives us replay, reconnection, and durability for free. The invariant: `Fold(events) == ApplyEvent applied repeatedly`. (`fold_test.go` proves it.)

> **Study exercise:** read `events.go` then `fold.go`, and trace what `RunView` looks like after `[NodeStarted, NodeOutput, ApprovalRequested]`. That single fold is the whole run-UI model.

---

## 5. `store/` — persistence behind an interface

- **`store.go`** — two interfaces: `GraphStore` (CRUD on the graph) and `RunStore` (append-only event log). *Why interfaces:* today it's in-memory; tomorrow it's SQLite/Postgres. Nothing else in the app changes when we swap it.
- **`memory.go`** — a `map` + `sync.RWMutex` implementation of both. `AppendEvent` assigns the next `Seq` under lock. `SetPositions` bulk-updates baseline layout (used by Save).

> **Note:** because it's in-memory, restarting the server loses everything. That's an accepted, temporary limitation — the event-sourced design means a DB-backed `RunStore` will later give durable, resumable runs with no executor changes.

---

## 6. The editor: `editor/` + `hub/` + `render/` + `web/index.html`

This is the canvas. Four pieces cooperate:

- **`web/index.html`** — the shell. It loads Datastar from a CDN, declares the client **signals** (`pos`, `drag`, `connect`, `newKind`), and on load runs `data-init="@get('/updates', {retry:'always'})"` to open the long-lived stream. The `<svg id="canvas">` is where everything is painted.
- **`editor/handlers.go`**:
  - **`Updates`** (`GET /updates`) — the long-lived READ. On connect it calls `snapshot` (seed `$pos`, then paint the full canvas), registers with the hub, then loops forwarding broadcast patches until the browser disconnects. *Reconnect is free:* every (re)connect re-paints the authoritative snapshot.
  - **`AddNode` / `DeleteNode` / `AddEdge` / `DeleteEdge`** — the WRITEs. Each mutates the store, then `hub.Broadcast(...)` paints the change to *every* connected tab. They return `204` with no body — the visible change arrives over each tab's `/updates` stream. *This is the CQRS split in action.*
  - **`Save`** — the only endpoint that learns node positions (see §7).
- **`hub/hub.go`** — keeps the set of connected clients; `Broadcast` pushes a patch to each. Each client has a buffered channel drained by one goroutine, so patches to one tab never interleave; a slow tab is marked "stale" and recovers with a fresh snapshot instead of stalling everyone.
- **`render/render.go`** — builds the SVG strings: `RenderNode` (a `<g>` with the box + ports), `RenderEdge` (a bezier `<path>`), `RenderEdgesLayer` (all edges + the connection "ghost" line). **All user text is escaped** (injection safety). This is where the Datastar `data-*` attributes get baked into the markup.

**Two interactions worth tracing in `render.go` + `index.html`:**
- **Dragging a node** — `pointerdown` on the box captures the pointer and records a grab offset; `pointermove` on the canvas writes the live position into `$pos[$drag.id]`; the node's `transform` is bound to `$pos`, so it follows instantly. *No server call* (see §7).
- **Connecting two nodes** — `pointerdown` on a cyan output port sets `$connect` and seeds the "ghost" line's start point; `pointermove` updates the ghost's end point (the dashed rubber-band that follows your cursor); `pointerup` hit-tests with `document.elementFromPoint` to find the input port under the cursor, then POSTs `/edges`. *Why hit-testing:* pressing a port triggers browser "pointer capture," so the release event lands on the canvas, not the target port — we find the real target by coordinates.

---

## 7. Why node positions are *client*-owned (an important design lesson)

The executor never reads `X/Y` — **position is presentation, not part of the agent flow.** So we keep layout in a client signal `$pos` and let dragging write to it **with zero network calls**. Benefits:
- Dragging is instant and can't "jitter" (there's no round-trip gap to flicker through).
- We deleted a whole category of code (a per-drag POST, a broadcast, and the optimistic-position machinery that compensated for the round-trip).

The server keeps `Node.X/Y` only as a **baseline** (where a node first appears, or the last saved layout) and seeds `$pos` from it on connect. An explicit **💾 Save layout** button (`POST /save`) is the *only* time the backend learns positions. Trade-off we accepted: layout isn't live-shared across tabs (graph *structure* still is). *Lesson:* not everything the UI shows is "state" — separate the true source-of-truth (the graph) from presentation (the layout).

---

## 8. The runner: `engine/` + `runs/` + `render/run_render.go` + `web/run.html`

Clicking **▶ Run** starts an execution. This is where event sourcing earns its keep.

- **`engine/engine.go`** — owns running flows. `StartRun` creates a run and launches the executor in a goroutine. `emit` does two things for every event: **persist it** (append to the `RunStore`) **and publish it** to live subscribers. `Subscribe` lets a run-view connection receive the live event stream. The `waits` map holds the channels that approval gates block on.
- **`engine/executor.go`** — walks the graph in topological order and, per node, emits `NodeStarted` → does the work → `NodeCompleted`:
  - **agent** → calls the (stubbed) `AgentFunc` and emits `NodeOutput`. *Why a stub:* lets us build the whole machine before wiring real LLM calls; real agents slot in behind the interface with no executor changes.
  - **approval** → emits `ApprovalRequested`, then **blocks on a channel** until a human approves/rejects. *This is the killer feature:* the run suspends, and because the truth is the event log, it can wait indefinitely.
  - **delay** → emits `DelayScheduled`, sleeps, emits `DelayElapsed`.
- **`runs/handlers.go`**:
  - **`Updates`** (`GET /runs/{id}/updates`) — the run's own long-lived projection. It subscribes, **folds the existing events into the full view** (this is the replay-on-reconnect), paints it, then streams incremental patches for each new event. Uses `Last-Event-ID` so a reconnect resumes exactly.
  - **`Approve` / `Reject`** — unblock the suspended executor (sends on the channel in `engine.waits`). The resulting event flows down the SSE stream and updates the UI.
- **`render/run_render.go`** — paints the run view: a status badge + output per node, the approval panel (with Approve/Reject buttons), and the finished/failed banner.

> **Study exercise:** trace one approval. `Run` → executor reaches the approval node → `emit(ApprovalRequested)` → UI shows the panel → you POST `/approve` → `engine.Decide` sends on the channel → executor unblocks → `emit(ApprovalGranted)` → UI removes the panel → next node runs. Notice the UI is *only ever* reacting to events.

---

## 9. Datastar concepts you'll see everywhere (a tiny glossary)

- **Signals** (`$name`) — reactive client state. We use them sparingly (the philosophy: most state lives on the server). Ours: `$pos` (layout), `$drag`/`$connect` (gesture bookkeeping), `$newKind` (the toolbar dropdown).
- **`data-*` attributes** — the reactive bindings baked into server-rendered HTML: `data-on:click="@post('/nodes')"`, `data-attr:transform="..."` (compute an attribute from signals), `data-show`, `data-bind`, `data-init`.
- **`@post` / `@get` / `@delete`** — fire an HTTP request *and* feed any SSE response back into the page. The request body automatically includes the current signals.
- **SSE patches** — what the Go SDK sends: `PatchElements` (morph/append/remove DOM by selector), `PatchSignals` (update client signals), and `Redirect`/`ExecuteScript` for the odd bit of client control.
- **Gotcha you'll hit:** in a `data-*` expression, Datastar reads `-` as part of a signal name (`$a.b-1` → signal `a.b-1`, not "minus one"). Put spaces around arithmetic: `$a.b - 1`.

---

## 10. Suggested reading order

1. `domain/graph.go` → `domain/events.go` → `domain/fold.go` (what things *are*).
2. `main.go` (how it's wired; skim the route list).
3. `editor/handlers.go` + `web/index.html` (the canvas: one stream, several writes).
4. `render/render.go` (how a node/edge becomes SVG; find the drag + connect expressions).
5. `engine/engine.go` → `engine/executor.go` → `runs/handlers.go` (how a run streams to the UI and suspends on approval).

## 11. Run it yourself
```bash
cd diffy && go run .       # http://localhost:1337
go test ./...              # fold tests prove the event-sourcing invariant
```
Open two browser tabs to watch structure sync live. Build Input → Agent → Approval → Delay → Output, connect them, and hit ▶ Run.
