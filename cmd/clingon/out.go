package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/palette"
	"golang.org/x/term"
)

// How clingon's commands print, in its palette: in a terminal, everything's in a
// card, single results and errors too. Colour and cards go when the output isn't
// a terminal (lipgloss sees to that for colour): scripts get plain lines.

var (
	nameStyle  = lipgloss.NewStyle().Foreground(palette.Green.Color()).Bold(true) // clingon, as a program is in a command
	headStyle  = lipgloss.NewStyle().Foreground(palette.Soft.Color()).Bold(true)
	markStyle  = lipgloss.NewStyle().Foreground(palette.Green.Color()) // worth doing, what runs: as the menu's
	greyStyle  = lipgloss.NewStyle().Foreground(palette.Grey.Color())
	faintStyle = lipgloss.NewStyle().Foreground(palette.Faint.Color())
	okStyle    = lipgloss.NewStyle().Foreground(palette.Green.Color())
	failStyle  = lipgloss.NewStyle().Foreground(palette.Red.Color())
	warnStyle  = lipgloss.NewStyle().Foreground(palette.Orange.Color())
)

// A mark says what a row or line is: done or fine, failed, worth a look,
// worth doing (its own glyph, in green), or just so.
type mark int

const (
	ok mark = iota
	fail
	warn
	todo
	info
)

func (k mark) render(glyph string) string {
	switch k {
	case ok:
		return okStyle.Render("✓")
	case fail:
		return failStyle.Render("✗")
	case warn:
		return warnStyle.Render("!")
	case todo:
		return markStyle.Render(glyph)
	}
	return greyStyle.Render("·")
}

// stdout is where clingon's commands print: styles render full colour, and
// this rounds them to what the terminal shows (Terminal.app has 256), or
// drops colour when it isn't a terminal. Lip Gloss's own writer, unless
// os.Stdout has changed since it was made (tests swap it).
func stdout() io.Writer {
	if f, ok := lipgloss.Writer.Forward.(*os.File); ok && f == os.Stdout {
		return lipgloss.Writer
	}
	return colorprofile.NewWriter(os.Stdout, os.Environ())
}

// stderr is stdout's counterpart for errors and usage.
func stderr() io.Writer { return colorprofile.NewWriter(os.Stderr, os.Environ()) }

func outln(a ...any) {
	flushSaid()
	_, _ = fmt.Fprintln(stdout(), a...)
}

func outf(format string, a ...any) {
	flushSaid()
	_, _ = fmt.Fprintf(stdout(), format, a...)
}

func errf(format string, a ...any) { _, _ = fmt.Fprintf(stderr(), format, a...) }

// said is what say has held for one card, and saidTitle what that's headed.
var (
	said      []string
	saidTitle = "Clingon"
)

// say gives a single result: `✓ added git`. In a terminal, a command's results
// share a card, drawn by flushSaid.
func say(k mark, format string, a ...any) {
	line := k.render("") + " " + fmt.Sprintf(format, a...)
	if !isTerm() {
		_, _ = fmt.Fprintln(stdout(), line)
		return
	}
	said = append(said, line)
}

// flushSaid draws what say has held. It runs before anything else prints, before
// a command hands the terminal to another program, and as clingon exits.
func flushSaid() {
	if len(said) == 0 {
		return
	}
	lines := said
	said = nil
	_, _ = fmt.Fprintln(stdout(), boxed(headStyle.Render(saidTitle), lines, cardWidth()))
}

// commandTitle heads a command's card: `Pack add` from `clingon pack add git`.
func commandTitle(args []string) string {
	if len(args) == 0 {
		return "Clingon"
	}
	title := args[0]
	switch args[0] {
	case "ai": // an initialism: AI, not Ai
		return "AI"
	case "pack", "packs", "command", "commands", "cmd", "recipe", "recipes":
		if len(args) > 1 {
			title = args[0] + " " + args[1]
		}
	}
	return strings.ToUpper(title[:1]) + title[1:]
}

// boxed draws lines as a titled card, wrapping any too long for it: unlike a
// card's details, none of this may be cut short.
func boxed(title string, lines []string, w int) string {
	var out []string
	for _, l := range lines {
		out = append(out, strings.Split(ansi.Wrap(l, max(w-4, 1), ""), "\n")...)
	}
	return titled(title, out, w)
}

// sayBoxed prints text as a titled card in a terminal, and as it is anywhere else.
func sayBoxed(title, text string) {
	text = strings.TrimRight(text, "\n")
	if !isTerm() {
		outln(text)
		return
	}
	outln(boxed(headStyle.Render(title), strings.Split(text, "\n"), cardWidth()))
}

// step prints what's about to run, for commands that run others.
func step(text string) {
	line := markStyle.Render("→") + " " + text
	if !isTerm() {
		outln(line)
		return
	}
	said = append(said, line)
	flushSaid() // what it's about to run may want the terminal
}

// sayErr prints an error, to stderr: as a card, if that's a terminal.
func sayErr(err error) {
	flushSaid()
	if term.IsTerminal(int(os.Stderr.Fd())) {
		w := cardWidth()
		_, _ = fmt.Fprintln(stderr(), boxed(failStyle.Render("✗ Problem"), strings.Split(err.Error(), "\n"), w))
		return
	}
	_, _ = fmt.Fprintln(stderr(), failStyle.Render("✗")+" "+err.Error())
}

// errUsage prints usage to stderr: as a card, if that's a terminal.
func errUsage(text string) {
	text = strings.TrimRight(text, "\n")
	if term.IsTerminal(int(os.Stderr.Fd())) {
		_, _ = fmt.Fprintln(stderr(), boxed(headStyle.Render("Usage"), strings.Split(text, "\n"), cardWidth()))
		return
	}
	errf("%s\n", text)
}

// A row of a card.
type row struct {
	mark   mark
	glyph  string // todo's mark
	colour string // the glyph's, where it's a section's logo (#rrggbb): as the menu draws it; else green
	label  string
	detail string // cut short to fit
	note   string // at the right, faint
	nudge  bool   // worth doing, but not news: left out of the menu's status bar
}

// isTerm reports whether stdout is a terminal: lists print as cards there,
// plain for scripts.
func isTerm() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// cardWidth is how wide cards are drawn: all of the terminal's width.
func cardWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w >= 40 {
		return w
	}
	return 80
}

// slot is a todo mark as the card draws it: in clingon's window, where
// one can be a logo two cells wide, a symbol is its centred twin there, so
// the marks line up down the card. Elsewhere it's left as it is.
func slot(glyph string) string {
	if c, ok := config.Centred[glyph]; ok && os.Getenv("CLINGON") != "" {
		return c
	}
	return glyph
}

// card draws rows as a titled card w cells wide: a row each, its mark,
// label, detail (cut short) and note.
func card(title string, rows []row, w int) string {
	labelW, noteW := 0, 0
	for _, r := range rows {
		labelW = max(labelW, ansi.StringWidth(r.label))
		noteW = max(noteW, ansi.StringWidth(r.note))
	}
	inner := w - 4 // two cells in from each side
	detailW := inner - 3 - labelW - 2 - noteW - 2
	if detailW < 12 { // too narrow for notes: the details matter more
		noteW, detailW = 0, inner-3-labelW-2
	}
	var lines []string
	for _, r := range rows {
		mk := r.mark.render(slot(r.glyph))
		if r.colour != "" && r.mark == todo {
			mk = lipgloss.NewStyle().Foreground(lipgloss.Color(r.colour)).Render(slot(r.glyph))
		}
		line := mk + "  " + r.label + strings.Repeat(" ", labelW-ansi.StringWidth(r.label)) + "  "
		line += greyStyle.Render(fit(r.detail, max(detailW, 0)))
		if noteW > 0 {
			line += "  " + faintStyle.Render(fmt.Sprintf("%*s", noteW, r.note))
		}
		lines = append(lines, line)
	}
	return titled(headStyle.Render(title), lines, w)
}

// titled draws lines, at most w-4 cells each, as a card: a rule w cells wide
// with the title in it, and the lines under it, set in two cells. No sides
// and no foot: clingon's window frames the shell already, and a box in a
// frame is lines for their own sake. The next card's rule, or the prompt,
// ends it.
func titled(title string, lines []string, w int) string {
	out := []string{faintStyle.Render("── ") + title + faintStyle.Render(" "+strings.Repeat("─", max(w-4-ansi.StringWidth(title), 0)))}
	for _, l := range lines {
		out = append(out, cardIndent+l)
	}
	return strings.Join(out, "\n")
}

// cardIndent sets a card's lines in under its title's rule.
const cardIndent = "  "

// fit pads or cuts s to exactly w cells.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", w-ansi.StringWidth(s))
}

// styleUsage colours a usage text: its first line's name, each `clingon …`
// line's command in green, as a program is, and what it does in grey.
func styleUsage(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		switch {
		case i == 0 && strings.HasPrefix(l, "clingon"):
			name, rest, _ := strings.Cut(l, " ")
			lines[i] = nameStyle.Render(name) + " " + greyStyle.Render(rest)
		case strings.HasPrefix(l, "  clingon"):
			cmd, what := l, ""
			if j := strings.Index(l[2:], "   "); j >= 0 {
				cmd, what = l[:j+2], l[j+2:]
			}
			lines[i] = markStyle.Render(cmd) + greyStyle.Render(what)
		case strings.HasSuffix(l, ":") && !strings.HasPrefix(l, " "):
			lines[i] = headStyle.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}
