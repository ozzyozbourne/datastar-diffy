package domain

import (
	"fmt"
	"strings"
)

// ParsePorts parses a line-based port spec into typed ports. Each non-empty
// line is "dir name [type]":
//
//	in  prompt  text
//	out result  text
//
// dir must be "in" or "out"; type defaults to "text" and must be one of the
// known PortTypes. Each port's ID is a slug of its name, de-duplicated with a
// numeric suffix so CanConnect's lookups stay stable and unique.
func ParsePorts(spec string) ([]Port, error) {
	var ports []Port
	seen := map[string]int{}
	for i, raw := range strings.Split(spec, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("line %d: expected %q, got %q", i+1, "dir name [type]", line)
		}

		var dir PortDir
		switch fields[0] {
		case "in":
			dir = PortIn
		case "out":
			dir = PortOut
		default:
			return nil, fmt.Errorf("line %d: direction must be \"in\" or \"out\", got %q", i+1, fields[0])
		}

		name := fields[1]
		typ := TypeText
		if len(fields) >= 3 {
			typ = PortType(fields[2])
			switch typ {
			case TypeTrigger, TypeText, TypeAgent:
			default:
				return nil, fmt.Errorf("line %d: unknown port type %q", i+1, fields[2])
			}
		}

		id := slug(name)
		if id == "" {
			id = string(dir)
		}
		if seen[id] > 0 {
			id = fmt.Sprintf("%s%d", id, seen[id])
		}
		seen[slug(name)]++

		ports = append(ports, Port{ID: id, Name: name, Dir: dir, Type: typ})
	}
	return ports, nil
}

// ParseConfig parses "key=value" lines into a config map. Blank lines are
// skipped; values may contain "=". Lines without "=" are ignored.
func ParseConfig(spec string) map[string]string {
	config := map[string]string{}
	for _, raw := range strings.Split(spec, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		config[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return config
}

// slug lowercases name and keeps only [a-z0-9], collapsing everything else away,
// yielding a stable port id.
func slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}
