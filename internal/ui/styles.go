package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
)

// clingon's palette. Every colour is one of the 256 standard terminal
// colours, so it looks the same everywhere: Terminal.app has no 24-bit
// colour, and inside tmux any other colour is rounded to the nearest of
// these, which is how near-misses of one purple end up as two. The shell's
// prompt (internal/shell), tmux (tmux.conf) and pack colouring (internal/mdcolor) use the same ones by
// number. The background and plain text are the terminal's own, from
// clingon's window profile (internal/window): that ink is exact.
var (
	accent      = lipgloss.Color(PinkHex)   // 212 pink: the cursor, marks, what you act on
	purple      = lipgloss.Color("#875fff") // 99: the menu's headings, the prompt's folder
	ghostPurple = lipgloss.Color("#5f5faf") // 61: the headings' purple, dimmed: a form's "+ ADD MODULE", where one would go
	ghostRed    = lipgloss.Color("#af5f5f") // 131: the errors' red, muted: a form's "− REMOVE", as ghostPurple is its ADD
	soft        = lipgloss.Color("#afafff") // 147 periwinkle: headings and keys in the shell's output, far enough from the prompt's purple; flags
	grey        = lipgloss.Color(greyHex)   // 103 lavender: secondary text, the prompt's branch
	faint       = lipgloss.Color("#5f5f87") // 60: chrome: rules, frames, placeholders
	raised      = lipgloss.Color("0")       // ANSI black, which clingon's window makes a step lighter than its background, in its violet (internal/window): the cursor's row, the command block, bars
	text        = lipgloss.Color("#e4e4e4") // 254: text on coloured bands
	orange      = lipgloss.Color(orangeHex) // 215: cautions
	red         = lipgloss.Color(redHex)    // 204: errors, danger; the prompt after a failure
	green       = lipgloss.Color("#5fd7af") // 79: values, all's well
	sky         = lipgloss.Color("#87d7ff") // 117: a command's blanks, waiting to be filled: apart from the pinks and purples (what you act on) and the flags' periwinkle
	ink         = lipgloss.Color("#121212") // 233: dark text on bright bands
)

// greyHex, orangeHex and redHex are the risk glyphs' colours, for where a
// colour's given as a hex.
const (
	greyHex   = "#8787af"
	orangeHex = "#ffaf5f"
	redHex    = "#ff5f87"
)

// PinkHex is clingon's pink, for a pack's color field.
const PinkHex = "#ff87d7"

// The palette, for clingon's other screens (welcome, check).
var Pink, Soft, Grey, Faint, Red, Green, Orange = accent, soft, grey, faint, red, green, orange

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
	// groupLabelStyle starts each group's line in a section's summary, in the
	// headings' purple so the groups stand apart from their commands.
	groupLabelStyle = headerStyle
	valueStyle      = lipgloss.NewStyle().Foreground(green)
	pendingStyle    = lipgloss.NewStyle().Foreground(accent).Underline(true)
	stepStyle       = lipgloss.NewStyle().Foreground(ink).Background(accent).Bold(true) // a command's place in the recipe
)

const (
	warnGlyph   = "⚠" // risk: warn, and other cautions
	dangerGlyph = "☠" // risk: danger
)

// iconStyle colours a section's icon: its pack's colour (a brand's, say),
// else grey, so the few brand colours stand out.
// warnHead is a caution's heading: the ⚠, then text in bold amber. The mark
// stays out of the bold: clingon's font has none, and a faked one smears it.
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
	tip := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Bold(true)
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

// onBg puts a whole line, already coloured, on background bg and pads it to
// w cells. (A style's background alone stops at the first reset inside.)
// cursorBar is the bar at the start of the cursor's row: a cell filled with
// st's colour. A filled cell, not a ▌, because some terminals smear a block
// character a pixel into the row below and never repaint it when the cursor
// moves up. Without colour (a plain terminal, tests) it's a ▌.
func cursorBar(st lipgloss.Style) string {
	cell := lipgloss.NewStyle().Background(st.GetForeground()).Render(" ")
	if cell == " " {
		return st.Render("▌")
	}
	return cell
}

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
