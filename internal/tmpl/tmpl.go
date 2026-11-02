// Package tmpl fills <name> placeholders in command templates.
//
// Only names declared as args count as placeholders, so shell syntax like
// `cat < file` or `{{.Names}}` passes through untouched.
package tmpl

import (
	"regexp"
	"strings"
)

var placeholder = regexp.MustCompile(`<([A-Za-z_][A-Za-z0-9_-]*)>`)

// Seg is either literal Text or a placeholder for Arg.
type Seg struct {
	Text string
	Arg  string
}

func Parse(t string, known func(string) bool) []Seg {
	var segs []Seg
	last := 0
	for _, m := range placeholder.FindAllStringSubmatchIndex(t, -1) {
		name := t[m[2]:m[3]]
		if !known(name) {
			continue
		}
		if m[0] > last {
			segs = append(segs, Seg{Text: t[last:m[0]]})
		}
		segs = append(segs, Seg{Arg: name})
		last = m[1]
	}
	if last < len(t) {
		segs = append(segs, Seg{Text: t[last:]})
	}
	return segs
}

// Render substitutes vals into t. An empty value also swallows the space
// before it, so optional args don't leave gaps.
func Render(t string, known func(string) bool, vals map[string]string) string {
	var b strings.Builder
	for _, s := range Parse(t, known) {
		if s.Arg == "" {
			b.WriteString(s.Text)
			continue
		}
		v := vals[s.Arg]
		if v == "" {
			str := b.String()
			if strings.HasSuffix(str, " ") {
				b.Reset()
				b.WriteString(str[:len(str)-1])
			}
		}
		b.WriteString(v)
	}
	return strings.TrimSpace(b.String())
}
