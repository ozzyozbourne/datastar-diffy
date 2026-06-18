package chat

import (
	"fmt"
	"html"
	"strings"
)

// RenderMessage renders one message bubble keyed id="msg-{Seq}". All
// user-controlled text is escaped (injection safety, same rule as render/).
func RenderMessage(m Message) string {
	return fmt.Sprintf(
		`<div id="msg-%d" class="rounded bg-gray-700/60 px-2 py-1 text-sm">`+
			`<span class="font-semibold text-sky-300">%s</span> `+
			`<span class="text-gray-400 text-xs">%s</span>`+
			`<div class="text-gray-100 whitespace-pre-wrap break-words">%s</div></div>`,
		m.Seq,
		html.EscapeString(m.User),
		m.At.Format("15:04"),
		html.EscapeString(m.Text),
	)
}

// RenderMessages concatenates bubbles for the connect snapshot (oldest first).
func RenderMessages(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(RenderMessage(m))
	}
	return b.String()
}

// ApprovalCardID is the element id for a pending approval card in the chat panel.
func ApprovalCardID(runID, nodeID string) string {
	return fmt.Sprintf("approval-%s-%s", runID, nodeID)
}

// RenderApprovalCard renders a pending approval inline in the chat panel, with
// Approve/Reject buttons that post to the run's existing decide endpoints. The
// card is keyed so it can be removed once the decision is made.
func RenderApprovalCard(runID, nodeID, prompt string) string {
	id := ApprovalCardID(runID, nodeID)
	approve := fmt.Sprintf("@post('/runs/%s/nodes/%s/approve')", runID, nodeID)
	reject := fmt.Sprintf("@post('/runs/%s/nodes/%s/reject')", runID, nodeID)
	return fmt.Sprintf(`<div id="%s" class="rounded border border-amber-500 bg-amber-500/10 p-2 text-sm">`+
		`<div class="font-medium text-amber-300 mb-1">%s</div>`+
		`<div class="flex gap-1">`+
		`<button data-on:click="%s" class="rounded bg-emerald-600 hover:bg-emerald-700 px-2 py-0.5 text-xs cursor-pointer">Approve</button>`+
		`<button data-on:click="%s" class="rounded bg-red-600 hover:bg-red-700 px-2 py-0.5 text-xs cursor-pointer">Reject</button>`+
		`</div></div>`,
		id, html.EscapeString(prompt), html.EscapeString(approve), html.EscapeString(reject))
}
