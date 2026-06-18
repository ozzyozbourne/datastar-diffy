package main

import (
	_ "embed"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"diffy/chat"
	"diffy/domain"
	"diffy/editor"
	"diffy/engine"
	"diffy/flows"
	"diffy/hub"
	"diffy/runs"
	"diffy/store"
)

//go:embed web/landing.html
var landingHTML []byte

//go:embed web/index.html
var indexHTML []byte

//go:embed web/run.html
var runHTML string

const graphID = "main"

func main() {
	mem := store.NewMemory()
	mem.PutGraph(domain.NewGraph(graphID))

	h := hub.New()
	fl := flows.New()
	ed := editor.New(mem, h, graphID, fl)
	eng := engine.New(mem, mem)
	rn := runs.New(eng, graphID, runHTML)

	// Chat: an in-memory group chat on its own hub + SSE stream.
	ch := chat.New(chat.NewRoom(), hub.New())

	// Bridge flow side-effects into the chat: replies post as "bot", and approval
	// gates surface as inline cards that resolve via the run decide endpoints.
	eng.OnReply = func(text string) { ch.Post("bot", text) }
	eng.OnApproval = func(runID, nodeID, prompt string) { ch.BroadcastApproval(runID, nodeID, prompt) }
	eng.OnApprovalDone = func(runID, nodeID string) { ch.RemoveApproval(runID, nodeID) }

	// A human chat message fires every saved flow whose trigger keyword it
	// matches; each starts a run on that flow's frozen snapshot, scoped to the
	// matched trigger and seeded with the message text. Only saved flows fire —
	// the live canvas is a draft until "Save agentic flow" registers it.
	ch.Trigger = func(user, text string) {
		if user == "bot" {
			return // don't let replies re-trigger flows
		}
		for _, f := range fl.All() {
			for _, t := range f.Graph.MatchTriggers(text) {
				eng.StartRunOnGraph(f.Graph, t.ID, text)
			}
		}
	}

	r := chi.NewRouter()

	// Landing page is the public front door; the editor lives at /editor.
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(landingHTML)
	})

	r.Get("/editor", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	// Editor: one long-lived read connection + short writes.
	r.Get("/updates", ed.Updates)
	r.Post("/nodes", ed.AddNode)
	r.Post("/nodes/custom", ed.AddCustomNode)
	r.Post("/nodes/{id}/config", ed.ConfigNode)
	r.Delete("/nodes/{id}", ed.DeleteNode)
	r.Post("/edges", ed.AddEdge)
	r.Delete("/edges/{id}", ed.DeleteEdge)
	r.Post("/save", ed.Save)
	r.Post("/flows", ed.SaveFlow)

	// Chat: long-lived read + short write, same CQRS split as the editor.
	r.Get("/chat/updates", ch.Updates)
	r.Post("/chat", ch.Send)

	// Runs: start, page shell, projection SSE, approve/reject.
	r.Post("/runs", rn.Start)
	r.Get("/runs/{id}", rn.Page)
	r.Get("/runs/{id}/updates", rn.Updates)
	r.Post("/runs/{id}/nodes/{nid}/approve", rn.Approve)
	r.Post("/runs/{id}/nodes/{nid}/reject", rn.Reject)

	addr := ":1337"
	log.Printf("diffy listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}
