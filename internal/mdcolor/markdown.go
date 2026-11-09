// Package mdcolor colours a pack's Markdown for the terminal, in the menu's
// colours: the form's preview, `clingon pack describe` and the guide.
package mdcolor

import (
	_ "embed"
	"regexp"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

// Markdown colours a pack file for the terminal: it applies the rules in
// markdown.yaml in order, a later match winning, regions carrying over lines,
// in colours.txt's colours. It only adds colour: the text comes through as
// it is.
func Markdown(src string) string {
	syn := loadSyntax()
	var out strings.Builder
	var in *synRegion // a region left open carries on to the next line
	for i, line := range strings.Split(src, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		cells := make([]string, len(line))
		in = syn.paintLine(cells, line, in)
		for at := 0; at < len(line); {
			end := at + 1
			for end < len(line) && cells[end] == cells[at] {
				end++
			}
			if sgr := syn.colour(cells[at]); sgr != "" {
				out.WriteString("\x1b[" + sgr + "m" + line[at:end] + "\x1b[m")
			} else {
				out.WriteString(line[at:end])
			}
			at = end
		}
	}
	return out.String()
}

//go:embed markdown.yaml
var rulesFile string

//go:embed colours.txt
var coloursFile string

type synPattern struct {
	group string
	re    *regexp.Regexp
}

type synRegion struct {
	group      string
	start, end *regexp.Regexp
	patterns   []synPattern
}

type syntax struct {
	patterns []synPattern
	regions  []*synRegion
	colours  map[string]string // group → SGR
}

var (
	synOnce sync.Once
	syn     *syntax
)

func loadSyntax() *syntax {
	synOnce.Do(func() {
		syn = &syntax{colours: map[string]string{}}
		data := []byte(rulesFile)
		var doc struct {
			Rules []map[string]yaml.Node `yaml:"rules"`
		}
		_ = yaml.Unmarshal(data, &doc)
		syn.patterns, syn.regions = parseRules(doc.Rules)
		for _, m := range colorLinkRe.FindAllStringSubmatch(coloursFile, -1) {
			syn.colours[m[1]] = sgrFor(m[2])
		}
	})
	return syn
}

var colorLinkRe = regexp.MustCompile(`(?m)^color-link (\S+) "([^"]*)"`)

// sgrFor turns a colour scheme entry like "bold 212" into an SGR code.
func sgrFor(spec string) string {
	var parts []string
	for _, f := range strings.Fields(strings.Split(spec, ",")[0]) {
		switch f {
		case "bold":
			parts = append(parts, "1")
		case "underline":
			parts = append(parts, "4")
		case "default", "reverse":
		default:
			parts = append(parts, "38;5;"+f)
		}
	}
	return strings.Join(parts, ";")
}

// colour is a group's SGR, falling back to its parent: special.blank, then special.
func (s *syntax) colour(group string) string {
	for g := group; g != ""; {
		if c, ok := s.colours[g]; ok {
			return c
		}
		i := strings.LastIndex(g, ".")
		if i < 0 {
			break
		}
		g = g[:i]
	}
	return ""
}

func parseRules(rules []map[string]yaml.Node) (pats []synPattern, regs []*synRegion) {
	for _, r := range rules {
		for group, n := range r {
			if n.Kind == yaml.ScalarNode {
				pats = append(pats, synPattern{group, regexp.MustCompile(n.Value)})
				continue
			}
			var reg struct {
				Start string                 `yaml:"start"`
				End   string                 `yaml:"end"`
				Rules []map[string]yaml.Node `yaml:"rules"`
			}
			if err := n.Decode(&reg); err != nil {
				continue
			}
			inner, _ := parseRules(reg.Rules)
			regs = append(regs, &synRegion{group: group, start: regexp.MustCompile(reg.Start), end: regexp.MustCompile(reg.End), patterns: inner})
		}
	}
	return pats, regs
}

// paintLine fills cells with each byte's group and says which region, if
// any, is still open at the end of the line.
func (s *syntax) paintLine(cells []string, line string, in *synRegion) *synRegion {
	at := 0
	for at <= len(line) {
		if in != nil { // inside a region: up to and including its end
			end := len(line)
			if loc := in.end.FindStringIndex(line[at:]); loc != nil && !(at == 0 && loc[1] == 0) {
				end = at + loc[1]
			}
			for i := at; i < end; i++ {
				cells[i] = in.group
			}
			paint(cells, line, at, end, in.patterns)
			if end == len(line) && in.end.FindStringIndex(line[at:]) == nil {
				return in
			}
			at, in = end, nil
			continue
		}
		// Outside: the earliest region to start, and the patterns before it.
		start, next := len(line), (*synRegion)(nil)
		for _, r := range s.regions {
			if loc := r.start.FindStringIndex(line[at:]); loc != nil && at+loc[0] < start {
				start, next = at+loc[0], r
			}
		}
		paint(cells, line, at, start, s.patterns)
		if next == nil {
			return nil
		}
		loc := next.start.FindStringIndex(line[start:])
		for i := start; i < start+loc[1]; i++ {
			cells[i] = next.group
		}
		at, in = start+loc[1], next
		if at == len(line) {
			return in
		}
	}
	return in
}

// paint applies patterns to line[from:to], each in turn, a later match winning.
func paint(cells []string, line string, from, to int, pats []synPattern) {
	if from >= to {
		return
	}
	for _, p := range pats {
		for _, loc := range p.re.FindAllStringIndex(line, -1) {
			for i := max(loc[0], from); i < min(loc[1], to); i++ {
				cells[i] = p.group
			}
		}
	}
}
