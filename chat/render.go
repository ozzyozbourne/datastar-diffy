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
