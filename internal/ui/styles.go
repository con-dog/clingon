package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/palette"
)

// clingon's palette, from internal/palette, where each colour is defined
// and what it means is said.
var (
	accent      = palette.Pink.Color()
	purple      = palette.Purple.Color()
	ghostPurple = palette.GhostPurple.Color()
	ghostRed    = palette.GhostRed.Color()
	soft        = palette.Soft.Color()
	grey        = palette.Grey.Color()
	faint       = palette.Faint.Color()
	raised      = palette.Raised
	text        = palette.Text.Color()
	orange      = palette.Orange.Color()
	red         = palette.Red.Color()
	green       = palette.Green.Color()
	sky         = palette.Sky.Color()
	tan         = palette.Tan.Color()
	ink         = palette.Ink.Color()
	gold        = palette.Gold.Color()
)

var (
	logoStyle    = lipgloss.NewStyle().Foreground(accent).Bold(true)
	crumbStyle   = lipgloss.NewStyle().Foreground(text).Bold(true)
	headerStyle  = lipgloss.NewStyle().Foreground(purple).Bold(true)
	labelStyle   = headerStyle                                     // COMMAND, DESCRIPTION: headings, as the list's are
	blurredStyle = lipgloss.NewStyle().Foreground(grey).Bold(true) // the cursor while focus is elsewhere
	cursorStyle  = lipgloss.NewStyle().Foreground(accent).Bold(true)
	accentStyle  = lipgloss.NewStyle().Foreground(accent)
	dimStyle     = lipgloss.NewStyle().Foreground(grey)
	faintStyle   = lipgloss.NewStyle().Foreground(faint)
	warnStyle    = lipgloss.NewStyle().Foreground(orange)
	errStyle     = lipgloss.NewStyle().Foreground(red)
	dangerStyle  = lipgloss.NewStyle().Foreground(red).Bold(true)
	valueStyle   = lipgloss.NewStyle().Foreground(green)
	pendingStyle = lipgloss.NewStyle().Foreground(sky).Underline(true)              // the blank being filled in, in the command: a blank's sky
	stepStyle    = lipgloss.NewStyle().Foreground(ink).Background(green).Bold(true) // a command's place in the recipe: green, as its name is
	keyStyle     = lipgloss.NewStyle().Foreground(text)                             // a key, wherever it's said: the status bar, help; bright, above what it does in grey
	chosenStyle  = lipgloss.NewStyle().Foreground(green)                            // what's chosen: a tick, a run row
)

const (
	warnGlyph   = "⚠" // risk: warn, and other cautions
	dangerGlyph = "☠" // risk: danger
)

// iconStyle colours a section's icon: its pack's colour (a brand's, say),
// else grey, so the few brand colours stand out.
// warnHead is a caution's heading: the ⚠, then text in bold amber. The mark
// stays out of the bold: it's a mark, and reads the same in every heading.
func warnHead(text string) string {
	return warnStyle.Render(warnGlyph) + " " + warnStyle.Bold(true).Render(text)
}

func iconStyle(color string) lipgloss.Style {
	if color == "" {
		return dimStyle
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

// slotIcon is the icon as drawn in its two-cell slot: a symbol's centred
// twin in clingon's font, so it lines up with the logos (also drawn across
// two cells) and emoji (two cells wide already).
func slotIcon(icon string) string {
	if c, ok := config.Centred[icon]; ok {
		return c
	}
	return icon
}

// Logo is clingon's banner, shared by the menu and `clingon welcome`.
const Logo = `█▀▀ █   █ █▄ █ █▀▀ █▀█ █▄ █
█▄▄ █▄▄ █ █ ▀█ █▄█ █▄█ █ ▀█`

// Banner renders the logo with up to three lines of text beside it. Text
// that doesn't fit whole is left out; below the logo's width it's just a name.
func Banner(width int, side ...string) string { return banner(width, -1, side...) }

// LogoWidth is the logo's width in cells.
var LogoWidth = ansi.StringWidth(strings.SplitN(Logo, "\n", 2)[0])

// banner draws the logo's first reveal columns (all of it if reveal < 0),
// the last of them white: the tip of a pen drawing it in.
func banner(width, reveal int, side ...string) string {
	logo := strings.Split(Logo, "\n")
	logoW := LogoWidth
	if width < logoW {
		return logoStyle.Render("clingon") + "\n\n"
	}
	tip := lipgloss.NewStyle().Foreground(palette.White.Color()).Bold(true)
	for i := range logo {
		line := logoStyle.Render(logo[i])
		if reveal >= 0 && reveal < logoW {
			r := []rune(logo[i]) // block characters: one cell a rune
			drawn := logoStyle.Render(string(r[:max(reveal-1, 0)]))
			if reveal > 0 {
				drawn += tip.Render(string(r[reveal-1]))
			}
			line = drawn + strings.Repeat(" ", logoW-reveal)
		}
		if i < len(side) && side[i] != "" && logoW+3+ansi.StringWidth(side[i]) <= width {
			line += "   " + side[i]
		}
		logo[i] = line
	}
	return strings.Join(logo, "\n")
}

// cursorBar is the bar at the start of the cursor's row: a cell filled with
// st's colour. A filled cell, not a ▌: Terminal.app smears a block character
// a pixel into the rows beside it and never repaints it when the cursor
// moves, leaving pink marks down the list. (Tried again on 2026-11-13 with
// the font's blocks fitted to the cell exactly: it still did.) Without colour
// (a plain terminal) it's a ▌.
func cursorBar(st lipgloss.Style) string {
	cell := lipgloss.NewStyle().Background(st.GetForeground()).Render(" ")
	if cell == " " {
		return st.Render("▌")
	}
	return cell
}

// onBg puts a whole line, already coloured, on background bg and pads it to
// w cells. (A style's background alone stops at the first reset inside.)
func onBg(line string, bg color.Color, w int) string {
	start, end, ok := strings.Cut(lipgloss.NewStyle().Background(bg).Render("\x00"), "\x00")
	if pad := w - ansi.StringWidth(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	if !ok || start == "" { // no colour here (tests, a plain terminal)
		return line
	}
	// Styles end with a reset, long or short form, and a band in this one
	// with a background reset: the background goes back on after each.
	return start + strings.NewReplacer("\x1b[0m", "\x1b[0m"+start, "\x1b[m", "\x1b[m"+start, "\x1b[49m", "\x1b[49m"+start).Replace(line) + end
}
