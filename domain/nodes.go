package domain

// NewNodeOfKind builds a node of the given kind with sensible default ports and
// config. Ports chain on the "text" type so a linear flow connects end to end.
func NewNodeOfKind(id string, kind NodeKind, x, y float64) *Node {
	n := &Node{ID: id, Kind: kind, X: x, Y: y, Config: map[string]string{}}
	switch kind {
	case KindInput:
		n.Title = "Input"
		n.Ports = []Port{{ID: "out", Name: "out", Dir: PortOut, Type: TypeText}}
		n.Config["text"] = "hello"
	case KindAgent:
		n.Title = "Agent"
		n.Ports = []Port{
			{ID: "in", Name: "in", Dir: PortIn, Type: TypeText},
			{ID: "out", Name: "out", Dir: PortOut, Type: TypeText},
		}
		n.Config["prompt"] = "summarize"
	case KindApproval:
		n.Title = "Approval"
		n.Ports = []Port{
			{ID: "in", Name: "in", Dir: PortIn, Type: TypeText},
			{ID: "out", Name: "out", Dir: PortOut, Type: TypeText},
		}
		n.Config["prompt"] = "Approve this step?"
	case KindDelay:
		n.Title = "Delay"
		n.Ports = []Port{
			{ID: "in", Name: "in", Dir: PortIn, Type: TypeText},
			{ID: "out", Name: "out", Dir: PortOut, Type: TypeText},
		}
		n.Config["seconds"] = "10"
	case KindOutput:
		n.Title = "Output"
		n.Ports = []Port{{ID: "in", Name: "in", Dir: PortIn, Type: TypeText}}
	}
	return n
}
