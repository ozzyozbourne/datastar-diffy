package render

import (
	"fmt"
	"html"
	"strings"

	"diffy/domain"
)

func statusClass(s domain.NodeStatus) (label, cls string) {
	switch s {
	case domain.StatusRunning:
		return "running", "bg-sky-600 text-white"
	case domain.StatusAwaiting:
		return "awaiting approval", "bg-amber-500 text-black"
	case domain.StatusDelaying:
		return "delaying", "bg-purple-600 text-white"
	case domain.StatusDone:
		return "done", "bg-emerald-600 text-white"
	case domain.StatusFailed:
		return "failed", "bg-red-600 text-white"
	case domain.StatusRejected:
		return "rejected", "bg-red-700 text-white"
	default:
		return "pending", "bg-gray-600 text-gray-200"
	}
}

func statusOf(v *domain.RunView, nodeID string) domain.NodeStatus {
	if n, ok := v.Nodes[nodeID]; ok {
		return n.Status
	}
	return domain.StatusPending
}

// RenderStatusBadge renders one node's status pill (keyed for incremental patch).
func RenderStatusBadge(runID, nodeID string, s domain.NodeStatus) string {
	label, cls := statusClass(s)
	return fmt.Sprintf(
		`<span id="run-%s-node-%s-status" class="text-xs px-2 py-0.5 rounded %s">%s</span>`,
		runID, nodeID, cls, label)
}

// RenderOutput renders one node's output box.
func RenderOutput(runID, nodeID, text string) string {
	body := html.EscapeString(text)
	if body == "" {
		body = `<span class="text-gray-600">—</span>`
	}
	return fmt.Sprintf(
		`<div id="run-%s-node-%s-output" class="mt-1 text-sm text-gray-300 font-mono whitespace-pre-wrap">%s</div>`,
		runID, nodeID, body)
}

func nodeRow(runID string, n *domain.Node, v *domain.RunView) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div id="run-%s-node-%s" class="rounded border border-gray-700 bg-gray-800 p-3">`, runID, n.ID)
	b.WriteString(`<div class="flex items-center gap-2">`)
	fmt.Fprintf(&b, `<span class="font-semibold">%s</span>`, html.EscapeString(n.Title))
	fmt.Fprintf(&b, `<span class="text-xs text-gray-500">%s</span>`, html.EscapeString(string(n.Kind)))
	b.WriteString(`<span class="ml-auto">`)
	b.WriteString(RenderStatusBadge(runID, n.ID, statusOf(v, n.ID)))
	b.WriteString(`</span></div>`)
	out := ""
	if nv, ok := v.Nodes[n.ID]; ok {
		out = nv.Output
	}
	b.WriteString(RenderOutput(runID, n.ID, out))
	b.WriteString(`</div>`)
	return b.String()
}

// RenderApprovalPanel renders the approval gate (or an empty slot when none).
func RenderApprovalPanel(runID string, p *domain.PendingApproval) string {
	if p == nil {
		return fmt.Sprintf(`<div id="run-%s-approval"></div>`, runID)
	}
	approve := fmt.Sprintf("$approving=true;@post('/runs/%s/nodes/%s/approve')", runID, p.NodeID)
	reject := fmt.Sprintf("$approving=true;@post('/runs/%s/nodes/%s/reject')", runID, p.NodeID)
	return fmt.Sprintf(`<div id="run-%s-approval" data-signals="{approving:false}" class="rounded border border-amber-500 bg-amber-500/10 p-3">
  <div class="font-medium text-amber-300 mb-2">%s</div>
  <div class="flex gap-2">
    <button data-on:click="%s" data-attr:disabled="$approving" class="rounded bg-emerald-600 hover:bg-emerald-700 px-3 py-1 text-sm disabled:opacity-50 cursor-pointer">Approve</button>
    <button data-on:click="%s" data-attr:disabled="$approving" class="rounded bg-red-600 hover:bg-red-700 px-3 py-1 text-sm disabled:opacity-50 cursor-pointer">Reject</button>
    <span data-show="$approving" class="text-sm text-amber-300 self-center">submitting…</span>
  </div>
</div>`, runID, html.EscapeString(p.Prompt), html.EscapeString(approve), html.EscapeString(reject))
}

// RenderBanner renders the terminal run state (or an empty slot).
func RenderBanner(runID string, v *domain.RunView) string {
	switch {
	case v.Failed:
		return fmt.Sprintf(`<div id="run-%s-banner" class="rounded bg-red-700 p-3 font-medium">Flow failed: %s</div>`,
			runID, html.EscapeString(v.Error))
	case v.Finished:
		return fmt.Sprintf(`<div id="run-%s-banner" class="rounded bg-emerald-700 p-3 font-medium">Flow finished ✓</div>`, runID)
	default:
		return fmt.Sprintf(`<div id="run-%s-banner"></div>`, runID)
	}
}

// RenderRunLayout renders the full run view as one wrapper element (single root
// to keep morphing clean), ordered topologically when possible.
func RenderRunLayout(g *domain.Graph, v *domain.RunView) string {
	order, err := g.TopoSort()
	if err != nil {
		order = order[:0]
		for id := range g.Nodes {
			order = append(order, id)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div id="run-inner" class="space-y-3">`)
	b.WriteString(RenderBanner(v.RunID, v))
	b.WriteString(RenderApprovalPanel(v.RunID, v.Pending))
	for _, id := range order {
		b.WriteString(nodeRow(v.RunID, g.Nodes[id], v))
	}
	b.WriteString(`</div>`)
	return b.String()
}
