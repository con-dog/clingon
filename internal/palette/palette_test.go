package palette_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/palette"
)

// root is the repository's top, from this package's folder.
const root = "../.."

// Each colour's number is the hex it says, in the terminal's 256.
func TestNumbersAreTheirHex(t *testing.T) {
	for n, c := range palette.All {
		if got := xterm(c.Hex); got != n {
			t.Errorf("colour %d says %s, which is %d", n, c.Hex, got)
		}
	}
}

// The files that can't import the palette use its numbers and no others:
// tmux's styles, the shells' prompts, pack colouring. The prompt's brand
// glyphs (git's, AWS's) are their logos' colours, as the menu's are.
func TestFilesUseThePalette(t *testing.T) {
	ok := map[int]bool{}
	for n := range palette.All {
		ok[n] = true
	}
	for _, logo := range []string{"git", "amazon"} {
		ok[xterm(config.LogoColor(logo))] = true
	}
	for file, re := range map[string]*regexp.Regexp{
		"internal/tmux/tmux.conf":          regexp.MustCompile(`colour(\d+)`),
		"internal/shell/files/zsh/.zshrc":  regexp.MustCompile(`%F\{(\d+)`),
		"internal/shell/files/bash/bashrc": regexp.MustCompile(`38;5;(\d+)`),
		"internal/mdcolor/colours.txt":     regexp.MustCompile(`"(?:(?:bold|underline|italic) )*(\d+)"`),
	} {
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		found := re.FindAllStringSubmatch(string(b), -1)
		if len(found) == 0 {
			t.Errorf("%s: no colours found: has its format changed?", file)
		}
		for _, m := range found {
			if n, _ := strconv.Atoi(m[1]); !ok[n] {
				t.Errorf("%s uses colour %d, which isn't in internal/palette", file, n)
			}
		}
	}
}

// No Go code writes a colour of its own: every one is the palette's. Pack
// tints and logo colours (a pack's choice, or a brand's) and the window's 16
// terminal colours are the only others.
func TestGoUsesThePalette(t *testing.T) {
	allowed := map[string]bool{
		"internal/palette/palette.go": true,
		"internal/config/layout.go":   true, // pack tints and logo colours
		"internal/window/window.go":   true, // the terminal's own 16, and its background
	}
	inline := regexp.MustCompile(`"#[0-9a-fA-F]{6}"|lipgloss\.Color\("|38;5;\d|48;5;\d|"colour\d`)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") && rel != ".") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || allowed[rel] {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if code, _, _ := strings.Cut(line, "//"); inline.MatchString(code) {
				t.Errorf("%s:%d writes a colour of its own: use internal/palette\n\t%s", rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// xterm is a hex's number in the terminal's 256: its 6×6×6 cube, or its
// greys.
func xterm(hex string) int {
	v, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	r, g, b := int(v>>16), int(v>>8&0xff), int(v&0xff)
	if r == g && g == b && (r-8)%10 == 0 && r >= 8 && r <= 238 {
		return 232 + (r-8)/10
	}
	level := func(c int) int {
		for i, l := range []int{0, 95, 135, 175, 215, 255} {
			if l == c {
				return i
			}
		}
		return -1
	}
	lr, lg, lb := level(r), level(g), level(b)
	if lr < 0 || lg < 0 || lb < 0 {
		return -1
	}
	return 16 + 36*lr + 6*lg + lb
}
