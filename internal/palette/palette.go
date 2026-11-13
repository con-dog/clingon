// Package palette is clingon's colours: the one place each is defined. The
// menu, the CLI's cards, the pack form, tmux's borders, the shell's prompt
// and pack colouring all use these, by name in Go and by number elsewhere
// (tmux.conf, the shells' rc files, mdcolor's colours.txt), and a test checks
// those files use no other numbers.
//
// Every colour is one of the 256 standard terminal colours, so it looks the
// same everywhere: Terminal.app has no 24-bit colour, and inside tmux any
// other colour is rounded to the nearest of these, which is how near-misses of
// one purple end up as two. The background and plain text are the terminal's
// own, from clingon's window profile (internal/window).
//
// What each colour means is the same on every screen:
//
//   - Pink is where you are: the cursor, the text cursor and the ❯ of the box
//     you're typing in, the shell's ❯, the AI's spinner while it works. And
//     clingon's logo. Nothing else, so the cursor is always the pink thing.
//   - Purple is headings (the menu's, the form's, a blank's question, a
//     Markdown file's) and the frame of the pane you're in.
//   - Periwinkle (Soft) is flags: what you pass. Keys are the bright Text,
//     their letters bold, over grey for what they do.
//   - Sky is a blank, waiting to be filled.
//   - Green is what's chosen, what runs and what's good: values, ticks,
//     programs in a command, recipe steps, a run row, something worth doing.
//   - Tan is a literal: a quoted string in a command, a number in code.
//   - Orange is a caution, or something that needs you; Red an error or
//     danger.
//   - Gold is the AI at work: its AI DRIVING chip, the focused pane's frame
//     while it drives, and what it's typing as it types it. So who has the
//     keys, you or the AI, shows at a glance.
//   - Grey is secondary text; Faint is chrome: rules, hints, the path beside
//     a search result; Shade is the frame of a pane you're not in.
package palette

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

// Colour is one of the 256 standard terminal colours.
type Colour struct {
	Num int    // its number, 16–255
	Hex string // its exact value, #rrggbb
}

// Color is the colour for Lip Gloss.
func (c Colour) Color() color.Color { return lipgloss.Color(c.Hex) }

// SGR is its foreground as an escape's parameters: "38;5;212".
func (c Colour) SGR() string { return fmt.Sprintf("38;5;%d", c.Num) }

// Tmux is its name in a tmux style: "colour212".
func (c Colour) Tmux() string { return fmt.Sprintf("colour%d", c.Num) }

var (
	Pink        = Colour{212, "#ff87d7"} // where you are: the cursor; the logo
	Purple      = Colour{99, "#875fff"}  // headings; the frame of the pane you're in; the prompt's folder
	GhostPurple = Colour{61, "#5f5faf"}  // the headings' purple, dimmed: a form's "+ ADD MODULE", where one would go
	GhostRed    = Colour{131, "#af5f5f"} // the errors' red, muted: a form's "− REMOVE", as GhostPurple is its ADD
	Soft        = Colour{147, "#afafff"} // periwinkle: flags; headings in the shell's output; the prompt's folders above the one you're in; the quiet chips: where you are, the AI's model
	Sky         = Colour{117, "#87d7ff"} // a blank, waiting to be filled
	Green       = Colour{79, "#5fd7af"}  // chosen, runs, all's well
	Tan         = Colour{180, "#d7af87"} // a literal: a quoted string in a command, a number in code
	Orange      = Colour{215, "#ffaf5f"} // cautions; something that needs you
	Red         = Colour{204, "#ff5f87"} // errors, danger; the prompt after a failure
	Gold        = Colour{221, "#ffd75f"} // the AI at work: its chip, the frame while it drives, what it's typing
	Grey        = Colour{103, "#8787af"} // lavender: secondary text, the prompt's branch
	Faint       = Colour{60, "#5f5f87"}  // chrome: rules, frames, placeholders, hints
	Shade       = Colour{238, "#444444"} // the quietest chrome: the frame of a pane without focus, so the focused one's stands out
	Text        = Colour{254, "#e4e4e4"} // text on coloured bands; keys, over grey for what they do
	Ink         = Colour{233, "#121212"} // dark text on bright bands
	White       = Colour{231, "#ffffff"} // the tip of the logo's pen as it's drawn in
)

// Raised is ANSI black, the terminal's own colour 0, which clingon's window
// makes a step lighter than its background, in its violet (internal/window):
// the cursor's row, the command block, bars. Not one of the 256: the window's.
var Raised = lipgloss.Color("0")

// All is every colour, by number: what the files that can't import this may
// use.
var All = map[int]Colour{}

func init() {
	for _, c := range []Colour{Pink, Purple, GhostPurple, GhostRed, Soft, Sky, Green, Tan, Orange, Red, Gold, Grey, Faint, Shade, Text, Ink, White} {
		All[c.Num] = c
	}
}
