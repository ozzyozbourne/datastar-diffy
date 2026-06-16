// Package render builds the HTML/SVG fragments streamed to the browser. All
// user-controlled text is escaped via html.EscapeString. Rendering is
// centralised here so a later swap to templ is mechanical.
package render

import (
	"fmt"
	"html"
	"strings"

	"diffy/domain"
)

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
	case domain.KindApproval:
		return "#5f3a1e"
	case domain.KindDelay:
		return "#3a1e5f"
	case domain.KindInput:
		return "#1e5f3a"
	case domain.KindOutput:
		return "#5f1e3a"
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

	fmt.Fprintf(&b, `<g id="node-%s" transform="%s" data-attr:transform="%s" style="cursor:grab">`,
		n.ID, baseline, html.EscapeString(posExpr))

	// Body: dragging this rect starts a node drag. Capture the pointer to the
	// canvas so move/up keep tracking even if the cursor leaves the node. Grab
	// offset is taken from the current $pos so the box doesn't jump.
	down := fmt.Sprintf(
		"evt.stopPropagation();const s=evt.currentTarget.closest('svg');s.setPointerCapture(evt.pointerId);const r=s.getBoundingClientRect();"+
			"$drag.id='%s';$drag.active=true;$drag.offx=evt.clientX-r.left-$pos.%s.x;$drag.offy=evt.clientY-r.top-$pos.%s.y",
		n.ID, n.ID, n.ID)
	fmt.Fprintf(&b,
		`<rect width="%g" height="%g" rx="8" fill="%s" stroke="#64748b" stroke-width="1.5" data-on:pointerdown="%s"/>`,
		NodeW, NodeH, nodeFill(n.Kind), html.EscapeString(down))

	fmt.Fprintf(&b,
		`<text x="12" y="24" fill="#e5e7eb" font-size="14" font-weight="600" style="pointer-events:none">%s</text>`,
		html.EscapeString(n.Title))
	fmt.Fprintf(&b,
		`<text x="12" y="44" fill="#94a3b8" font-size="11" style="pointer-events:none">%s</text>`,
		html.EscapeString(string(n.Kind)))

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
