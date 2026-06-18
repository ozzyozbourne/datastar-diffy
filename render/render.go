// Package render builds the HTML/SVG fragments streamed to the browser. All
// user-controlled text is escaped via html.EscapeString. Rendering is
// centralised here so a later swap to templ is mechanical.
package render

import (
	"fmt"
	"html"
	"sort"
	"strings"

	"diffy/domain"
)

// ConfigText serialises a node's config as sorted "key=value" lines — the form
// shown in the inspector textarea and parsed back by domain.ParseConfig.
func ConfigText(n *domain.Node) string {
	keys := make([]string, 0, len(n.Config))
	for k := range n.Config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+n.Config[k])
	}
	return strings.Join(parts, "\n")
}

// jsString escapes s for embedding inside a single-quoted JS string literal in a
// data-on attribute (html.EscapeString is applied afterwards by the caller).
func jsString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// KindOption is one entry in the toolbar's node-kind dropdown.
type KindOption struct{ Value, Label string }

// builtinKinds are the always-present node kinds, in display order.
var builtinKinds = []KindOption{
	{"input", "Input"},
	{"agent", "Agent"},
	{"trigger", "Trigger"},
	{"approval", "Approval"},
	{"delay", "Wait"},
	{"reply", "Reply"},
	{"output", "Output"},
}

// RenderKindOptions renders the <option> list for the kind dropdown: the
// built-in kinds followed by any user-defined custom-node templates. Rendered
// server-side (and inner-patched whole) so reconnects never duplicate options.
func RenderKindOptions(custom []KindOption) string {
	var b strings.Builder
	for _, o := range append(append([]KindOption{}, builtinKinds...), custom...) {
		fmt.Fprintf(&b, `<option value="%s">%s</option>`,
			html.EscapeString(o.Value), html.EscapeString(o.Label))
	}
	return b.String()
}

// SavedFlow is one entry in the saved-flows panel: a flow's name and the trigger
// keywords that fire it from chat.
type SavedFlow struct {
	Name     string
	Keywords []string
}

// RenderSavedFlows renders the saved-flows list (inner-patched whole, like
// RenderKindOptions, so reconnects/repaints never duplicate). Each row shows the
// flow name and its trigger keyword(s).
func RenderSavedFlows(flows []SavedFlow) string {
	if len(flows) == 0 {
		return `<span class="text-xs text-gray-500">no saved flows yet</span>`
	}
	var b strings.Builder
	for _, f := range flows {
		kw := "(no keyword)"
		if len(f.Keywords) > 0 {
			kw = strings.Join(f.Keywords, ", ")
		}
		fmt.Fprintf(&b,
			`<span class="rounded bg-gray-700 px-2 py-1 text-xs"><span class="font-medium">%s</span> <span class="text-gray-400">▶ %s</span></span>`,
			html.EscapeString(f.Name), html.EscapeString(kw))
	}
	return b.String()
}

// Node geometry (SVG user units == pixels; the canvas is rendered 1:1).
const (
	NodeW = 170.0
	NodeH = 64.0
	PortR = 7.0
)

// localPortPos returns a port's position relative to the node origin.
func localPortPos(n *domain.Node, p domain.Port) (x, y float64) {
	// Count siblings on the same side to distribute vertically.
	var sides []domain.Port
	for _, q := range n.Ports {
		if q.Dir == p.Dir {
			sides = append(sides, q)
		}
	}
	idx := 0
	for i, q := range sides {
		if q.ID == p.ID {
			idx = i
		}
	}
	step := NodeH / float64(len(sides)+1)
	y = step * float64(idx+1)
	if p.Dir == domain.PortIn {
		x = 0
	} else {
		x = NodeW
	}
	return x, y
}

// AbsPortPos returns a port's absolute canvas coordinates.
func AbsPortPos(n *domain.Node, p domain.Port) (x, y float64) {
	lx, ly := localPortPos(n, p)
	return n.X + lx, n.Y + ly
}

func nodeFill(kind domain.NodeKind) string {
	switch kind {
	case domain.KindAgent:
		return "#1e3a5f"
	case domain.KindTrigger:
		return "#5f5f1e"
	case domain.KindReply:
		return "#1e5f5f"
	case domain.KindApproval:
		return "#5f3a1e"
	case domain.KindDelay:
		return "#3a1e5f"
	case domain.KindInput:
		return "#1e5f3a"
	case domain.KindOutput:
		return "#5f1e3a"
	case domain.KindCustom:
		return "#3a3a5f"
	default:
		return "#374151"
	}
}

// RenderNode renders a single node as an SVG <g> keyed by id="node-{id}".
// Its transform binds to the client-owned $pos signal (layout is client state);
// the baked static transform is the server baseline used until $pos is seeded.
func RenderNode(n *domain.Node) string {
	var b strings.Builder
	baseline := fmt.Sprintf("translate(%g,%g)", n.X, n.Y)
	// Guard against the brief window before $pos is seeded (first paint).
	posExpr := fmt.Sprintf(
		"($pos.%s && $pos.%s.x!=null) ? ('translate('+$pos.%s.x+','+$pos.%s.y+')') : '%s'",
		n.ID, n.ID, n.ID, n.ID, baseline)

	// Title + config are stashed on the group so a click on the body (handled in
	// the canvas pointerup, since pointer capture swallows the element's own
	// click) can seed the inspector without baking it into the rect handler.
	fmt.Fprintf(&b, `<g id="node-%s" transform="%s" data-attr:transform="%s" data-title="%s" data-cfgtext="%s" style="cursor:grab">`,
		n.ID, baseline, html.EscapeString(posExpr),
		html.EscapeString(n.Title), html.EscapeString(ConfigText(n)))

	// Body: dragging this rect starts a node drag. Capture the pointer to the
	// canvas so move/up keep tracking even if the cursor leaves the node. Grab
	// offset is taken from the current $pos so the box doesn't jump.
	down := fmt.Sprintf(
		"evt.stopPropagation();const s=evt.currentTarget.closest('svg');s.setPointerCapture(evt.pointerId);const r=s.getBoundingClientRect();"+
			"$drag.id='%s';$drag.active=true;$drag.moved=false;$drag.offx=evt.clientX-r.left-$pos.%s.x;$drag.offy=evt.clientY-r.top-$pos.%s.y",
		n.ID, n.ID, n.ID)
	fmt.Fprintf(&b,
		`<rect width="%g" height="%g" rx="8" fill="%s" stroke="#64748b" stroke-width="1.5" data-on:pointerdown="%s"/>`,
		NodeW, NodeH, nodeFill(n.Kind), html.EscapeString(down))

	fmt.Fprintf(&b,
		`<text x="12" y="24" fill="#e5e7eb" font-size="14" font-weight="600" style="pointer-events:none">%s</text>`,
		html.EscapeString(n.Title))
	fmt.Fprintf(&b,
		`<text x="12" y="42" fill="#94a3b8" font-size="11" style="pointer-events:none">%s</text>`,
		html.EscapeString(string(n.Kind)))

	// Config summary (one compact line) so keyword/seconds/message are visible
	// without opening the inspector.
	if summary := ConfigText(n); summary != "" {
		summary = strings.ReplaceAll(summary, "\n", " · ")
		if len(summary) > 26 {
			summary = summary[:25] + "…"
		}
		fmt.Fprintf(&b,
			`<text x="12" y="57" fill="#64748b" font-size="10" style="pointer-events:none">%s</text>`,
			html.EscapeString(summary))
	}

	// Ports. Each carries data-* attributes for hit-testing on canvas pointerup.
	// Output ports begin a connection (and capture the pointer to the canvas);
	// input ports are passive drop targets resolved via elementFromPoint.
	for _, p := range n.Ports {
		lx, ly := localPortPos(n, p)
		dir, fillCol, attrs := "in", "#a78bfa", ""
		if p.Dir == domain.PortOut {
			dir, fillCol = "out", "#22d3ee"
			down := fmt.Sprintf(
				"evt.stopPropagation();evt.currentTarget.closest('svg').setPointerCapture(evt.pointerId);"+
					"$connect.fromNode='%s';$connect.fromPort='%s';"+
					"$connect.sx=$pos.%s.x+%g;$connect.sy=$pos.%s.y+%g;$connect.cx=$connect.sx;$connect.cy=$connect.sy",
				n.ID, p.ID, n.ID, lx, n.ID, ly)
			attrs = ` data-on:pointerdown="` + html.EscapeString(down) + `"`
		}
		fmt.Fprintf(&b,
			`<circle cx="%g" cy="%g" r="%g" fill="%s" stroke="#0f172a" stroke-width="1.5" data-node="%s" data-port="%s" data-dir="%s" style="cursor:crosshair"%s/>`,
			lx, ly, PortR, fillCol, n.ID, p.ID, dir, attrs)
	}

	// Config button: small ⚙ circle that opens the inspector seeded with this
	// node's current title + config. We set the bound signal AND the textarea's
	// value directly: Datastar reflects signal→input on init but not always on a
	// later programmatic change, so seeding the DOM guarantees the existing config
	// is visible for editing (data-bind still carries edits back on input).
	jsCfg := jsString(ConfigText(n))
	cfg := fmt.Sprintf(
		"evt.stopPropagation();$inspect.id='%s';$inspect.title='%s';$inspect.error='';$inspect.configText='%s';$inspect.open=true;"+
			"document.getElementById('inspect-config').value='%s'",
		n.ID, jsString(n.Title), jsCfg, jsCfg)
	fmt.Fprintf(&b,
		`<circle cx="%g" cy="10" r="8" fill="#475569" stroke="#0f172a" stroke-width="1" style="cursor:pointer" data-on:click="%s"/>`,
		NodeW-30, html.EscapeString(cfg))
	fmt.Fprintf(&b,
		`<text x="%g" y="14" text-anchor="middle" fill="white" font-size="10" style="pointer-events:none">&#x2699;</text>`,
		NodeW-30)

	// Delete button: small ✕ circle in the top-right corner of the node.
	del := fmt.Sprintf("evt.stopPropagation();@delete('/nodes/%s')", n.ID)
	fmt.Fprintf(&b,
		`<circle cx="%g" cy="10" r="8" fill="#ef4444" stroke="#0f172a" stroke-width="1" style="cursor:pointer" data-on:click="%s"/>`,
		NodeW-10, html.EscapeString(del))
	fmt.Fprintf(&b,
		`<text x="%g" y="14" text-anchor="middle" fill="white" font-size="11" font-weight="700" style="pointer-events:none">&#x2715;</text>`,
		NodeW-10)

	b.WriteString(`</g>`)
	return b.String()
}

// RenderEdge renders one edge as a bezier <path> keyed by id="edge-{id}".
func RenderEdge(g *domain.Graph, e *domain.Edge) string {
	from := g.Nodes[e.FromNode]
	to := g.Nodes[e.ToNode]
	if from == nil || to == nil {
		return ""
	}
	fp := from.Port(e.FromPort)
	tp := to.Port(e.ToPort)
	if fp == nil || tp == nil {
		return ""
	}
	flx, fly := localPortPos(from, *fp)
	tlx, tly := localPortPos(to, *tp)
	x1, y1 := from.X+flx, from.Y+fly
	x2, y2 := to.X+tlx, to.Y+tly
	dx := 60.0
	d := fmt.Sprintf("M %g %g C %g %g, %g %g, %g %g", x1, y1, x1+dx, y1, x2-dx, y2, x2, y2)

	// data-attr:d binds both endpoints to the client-owned $pos signal, so the
	// path follows live as either node is dragged (drag writes $pos). Falls back
	// to the server baseline per endpoint until $pos is seeded.
	expr := fmt.Sprintf(
		"(()=>{"+
			"const fx=($pos.%s&&$pos.%s.x!=null?$pos.%s.x:%g)+%g,fy=($pos.%s&&$pos.%s.y!=null?$pos.%s.y:%g)+%g,"+
			"tx=($pos.%s&&$pos.%s.x!=null?$pos.%s.x:%g)+%g,ty=($pos.%s&&$pos.%s.y!=null?$pos.%s.y:%g)+%g;"+
			"return 'M '+fx+' '+fy+' C '+(fx+%g)+' '+fy+', '+(tx-%g)+' '+ty+', '+tx+' '+ty;})()",
		e.FromNode, e.FromNode, e.FromNode, from.X, flx,
		e.FromNode, e.FromNode, e.FromNode, from.Y, fly,
		e.ToNode, e.ToNode, e.ToNode, to.X, tlx,
		e.ToNode, e.ToNode, e.ToNode, to.Y, tly,
		dx, dx)

	return fmt.Sprintf(
		`<path id="edge-%s" d="%s" data-attr:d="%s" fill="none" stroke="#64748b" stroke-width="2" style="cursor:pointer" data-on:click="@delete('/edges/%s')"/>`,
		e.ID, d, html.EscapeString(expr), e.ID)
}

// connectGhost is the live "rubber-band" path shown while dragging a new
// connection from an output port. It is bound to $connect (client-only) and
// hidden unless a connection is in progress. pointer-events:none so it never
// blocks the input-port hit-test on release.
// Note: keep spaces around the "-" — Datastar's signal parser otherwise reads
// "$connect.cx-60" as a kebab signal path ("$['connect']['cx-60']").
const connectGhost = `<path id="connect-ghost" data-show="$connect.fromNode" ` +
	`data-attr:d="'M '+$connect.sx+' '+$connect.sy+' C '+($connect.sx + 60)+' '+$connect.sy+', '+($connect.cx - 60)+' '+$connect.cy+', '+$connect.cx+' '+$connect.cy" ` +
	`fill="none" stroke="#22d3ee" stroke-width="2" stroke-dasharray="5,5" style="pointer-events:none"/>`

// RenderEdgesLayer renders the whole <g id="edges"> layer (real edges plus the
// connection ghost). Re-rendered wholesale on connect/delete.
func RenderEdgesLayer(g *domain.Graph) string {
	var b strings.Builder
	b.WriteString(`<g id="edges">`)
	for _, e := range g.Edges {
		b.WriteString(RenderEdge(g, e))
	}
	b.WriteString(connectGhost)
	b.WriteString(`</g>`)
	return b.String()
}
