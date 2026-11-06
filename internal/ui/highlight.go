package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Minimal shell highlighting for the Command area, in the colours used for a
// command everywhere (micro, `clingon pack guide`, the shell's highlighting
// plugin): program names green, flags periwinkle, quoted strings tan,
// operators grey. Placeholders and filled
// values keep their own styles (see preview).
var (
	hlProgram  = lipgloss.NewStyle().Foreground(green)
	hlFlag     = lipgloss.NewStyle().Foreground(soft)
	hlString   = lipgloss.NewStyle().Foreground(lipgloss.Color("#d7af87"))
	hlOperator = lipgloss.NewStyle().Foreground(grey)
)

// wrappers take a program after them: `sudo apt …`, `exec $SHELL`.
var wrappers = map[string]bool{"sudo": true, "exec": true, "time": true, "nohup": true, "xargs": true, "env": true}

// highlighter colours shell text a piece at a time, so a command split
// around placeholders keeps its state (inside a quote, expecting a program).
type highlighter struct {
	quote     byte
	expectCmd bool
}

func newHighlighter() *highlighter { return &highlighter{expectCmd: true} }

// highlight colours a whole command.
func highlight(cmd string) string { return newHighlighter().text(cmd) }

func (h *highlighter) text(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case h.quote != 0: // inside a quote: up to and including its end
			j := strings.IndexByte(s[i:], h.quote)
			if j < 0 {
				b.WriteString(hlString.Render(s[i:]))
				return b.String()
			}
			b.WriteString(hlString.Render(s[i : i+j+1]))
			h.quote, i = 0, i+j+1
			h.expectCmd = false
		case c == '\'' || c == '"':
			j := strings.IndexByte(s[i+1:], c)
			if j < 0 { // the quote closes after a placeholder
				b.WriteString(hlString.Render(s[i:]))
				h.quote = c
				return b.String()
			}
			b.WriteString(hlString.Render(s[i : i+j+2]))
			i += j + 2
			h.expectCmd = false
		case c == ' ' || c == '\t' || c == '\n':
			b.WriteByte(c)
			i++
		case strings.HasPrefix(s[i:], "$("):
			b.WriteString(hlOperator.Render("$("))
			h.expectCmd, i = true, i+2
		case strings.IndexByte("|&;<>()", c) >= 0:
			j := i
			for j < len(s) && strings.IndexByte("|&;<>()", s[j]) >= 0 {
				j++
			}
			op := s[i:j]
			b.WriteString(hlOperator.Render(op))
			if op != ">" && op != ">>" && op != "<" && op != ")" {
				h.expectCmd = true
			}
			i = j
		default:
			j := i
			for j < len(s) && strings.IndexByte(" \t\n'\"|&;<>()", s[j]) < 0 {
				j++
			}
			word := s[i:j]
			switch {
			case h.expectCmd && strings.Contains(word, "=") && !strings.HasPrefix(word, "-"):
				b.WriteString(word) // NAME=value before a program
			case h.expectCmd:
				b.WriteString(hlProgram.Render(word))
				h.expectCmd = wrappers[word]
			case strings.HasPrefix(word, "-"):
				b.WriteString(hlFlag.Render(word))
			default:
				b.WriteString(word)
			}
			i = j
		}
	}
	return b.String()
}

// placeholder tells the highlighter a placeholder or value came next, so
// whatever follows isn't taken for a program name.
func (h *highlighter) placeholder() {
	if h.quote == 0 {
		h.expectCmd = false
	}
}
