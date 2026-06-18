package domain

import "testing"

func TestParsePorts(t *testing.T) {
	t.Run("valid with defaults and blank lines", func(t *testing.T) {
		ports, err := ParsePorts("in input text\n\n  out result  \nout extra agent\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []Port{
			{ID: "input", Name: "input", Dir: PortIn, Type: TypeText},
			{ID: "result", Name: "result", Dir: PortOut, Type: TypeText}, // type defaults to text
			{ID: "extra", Name: "extra", Dir: PortOut, Type: TypeAgent},
		}
		if len(ports) != len(want) {
			t.Fatalf("got %d ports, want %d: %+v", len(ports), len(want), ports)
		}
		for i := range want {
			if ports[i] != want[i] {
				t.Errorf("port %d = %+v, want %+v", i, ports[i], want[i])
			}
		}
	})

	t.Run("duplicate names get unique ids", func(t *testing.T) {
		ports, err := ParsePorts("out result text\nout result text")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ports[0].ID != "result" || ports[1].ID != "result1" {
			t.Errorf("ids = %q, %q; want result, result1", ports[0].ID, ports[1].ID)
		}
	})

	t.Run("errors", func(t *testing.T) {
		for _, spec := range []string{
			"sideways x text", // bad direction
			"in prompt bogus",  // bad type
			"in",               // missing name
		} {
			if _, err := ParsePorts(spec); err == nil {
				t.Errorf("ParsePorts(%q) = nil error, want error", spec)
			}
		}
	})
}

func TestParseConfig(t *testing.T) {
	got := ParseConfig("prompt = translate to french\n\nmodel=opus\nnoequals\nurl=https://x?a=b")
	want := map[string]string{
		"prompt": "translate to french",
		"model":  "opus",
		"url":    "https://x?a=b", // value may contain '='
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d: %+v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("config[%q] = %q, want %q", k, got[k], v)
		}
	}
}
