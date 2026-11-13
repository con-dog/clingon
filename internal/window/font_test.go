package window

import (
	"encoding/binary"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/con-dog/clingon/internal/config"
)

// The menu draws logos and centred symbols by codepoint, from tables
// (config.Logos, config.Centred) generated alongside the font; the window
// asks the terminal for the font by name (fontname.go). Each has drifted
// from the font before: logos showed as the wrong logo, or as "?" boxes.
// These read the font that's really embedded and check it against them.

func TestEmbeddedFontIsTheOneNamed(t *testing.T) {
	if got := fontName(t, fontFile, 6); got != fontPS {
		t.Errorf("the embedded font is %q but the window asks for %q: run internal/window/font/build.py", got, fontPS)
	}
	if got := fontName(t, fontFile, 1); got != fontFamily {
		t.Errorf("the embedded font's family is %q but the window asks for %q", got, fontFamily)
	}
	if !strings.HasPrefix(fontFileName, fontPS) {
		t.Errorf("the font is installed as %q, not under its own name %q", fontFileName, fontPS)
	}
}

// The bold is the same family (so a terminal finds it as the family's bold,
// and never fakes one), under a name of its own, with every logo, twin and
// drawn character the regular has: a logo in a bold heading is still the logo.
func TestBoldIsTheSameFamilyWithTheSameCharacters(t *testing.T) {
	if got := fontName(t, fontBoldFile, 1); got != fontFamily {
		t.Errorf("the bold's family is %q, not the regular's %q", got, fontFamily)
	}
	if got := fontName(t, fontBoldFile, 2); got != "Bold" {
		t.Errorf("the bold's style is %q", got)
	}
	if got := fontName(t, fontBoldFile, 6); got != fontPS+"-Bold" || !strings.HasPrefix(fontBoldFileName, got) {
		t.Errorf("the bold's PostScript name is %q, its file %q", got, fontBoldFileName)
	}
	regular, bold := fontCmap(t, fontFile), fontCmap(t, fontBoldFile)
	for r, g := range regular {
		if g != 0 && bold[r] == 0 {
			t.Errorf("U+%04X is in the regular but not the bold", r)
		}
	}
}

func TestEveryLogoAndSymbolIsInTheFont(t *testing.T) {
	cmap := fontCmap(t, fontFile)
	if cmap['A'] == 0 || cmap[0xE7FF] != 0 { // the reader itself: a letter's there, an unused codepoint isn't
		t.Fatalf("reading the character map went wrong: A=%d U+E7FF=%d", cmap['A'], cmap[0xE7FF])
	}
	glyphs := map[uint16]string{}
	check := func(what, name, s string) {
		r := []rune(s)
		if len(r) != 1 {
			t.Fatalf("%s %q: %q isn't one character", what, name, s)
		}
		g, ok := cmap[r[0]]
		if !ok || g == 0 {
			t.Errorf("%s %q (U+%04X) isn't in the embedded font: it shows as a box", what, name, r[0])
			return
		}
		if other, dup := glyphs[g]; dup {
			t.Errorf("%s %q and %s draw the same glyph", what, name, other)
		}
		glyphs[g] = what + " " + name
	}
	for name, s := range config.Logos {
		check("logo", name, s)
	}
	for sym, s := range config.Centred {
		check("centred", sym, s)
	}
}

// A character missing from the font is drawn from whatever font the system
// picks, which for some is an emoji one. This reads what clingon shows: the
// strings in its Go code (not its tests or comments), and its packs and the
// starter pack, which are Markdown files in internal/config.
func TestEverythingClingonShowsIsInTheFont(t *testing.T) {
	cmap := fontCmap(t, fontFile)
	root := filepath.Join("..", "..")
	seen := map[rune]string{}
	note := func(text, where string) {
		for _, r := range text {
			if r == '\u200d' { // never shown: config strips it from icons when checking them
				continue
			}
			if _, dup := seen[r]; r > 127 && !dup {
				seen[r] = where
			}
		}
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		switch {
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, lit := range goStrings(t, path, src) {
				note(lit, path)
			}
		case strings.HasSuffix(name, ".md") && strings.Contains(filepath.ToSlash(path), "internal/config/"):
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			note(string(src), path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 || seen['☠'] == "" {
		t.Fatalf("the scan found nothing to check: %d characters", len(seen))
	}
	// Never drawn in clingon's window, so never in its font.
	elsewhere := map[rune]string{
		'▷': "the greyed cursor's mark in the menu as text, for the AI to read",
		'⊞': "a pack suggestion's mark outside clingon's window, in the terminal's own font",
		'⊕': "the suggestion to make a pack, outside clingon's window too",
	}
	for r, where := range seen {
		if elsewhere[r] != "" {
			continue
		}
		if cmap[r] == 0 {
			t.Errorf("%c (U+%04X), used in %s, isn't in the embedded font: use one that is, or add it to DRAWN in build.py", r, r, where)
		}
	}
}

// goStrings is the string and character literals in Go source.
func goStrings(t *testing.T, path string, src []byte) []string {
	t.Helper()
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile(path, fset.Base(), len(src)), src, nil, 0)
	var out []string
	for {
		_, tok, lit := s.Scan()
		switch tok {
		case token.EOF:
			return out
		case token.STRING, token.CHAR:
			text, err := strconv.Unquote(lit)
			if err != nil {
				t.Fatalf("%s: %s: %v", path, lit, err)
			}
			out = append(out, text)
		}
	}
}

// fontTable finds a table in an OpenType file.
func fontTable(t *testing.T, font []byte, tag string) []byte {
	t.Helper()
	n := int(binary.BigEndian.Uint16(font[4:]))
	for i := 0; i < n; i++ {
		rec := font[12+16*i:]
		if string(rec[:4]) == tag {
			off, size := binary.BigEndian.Uint32(rec[8:]), binary.BigEndian.Uint32(rec[12:])
			return font[off : off+size]
		}
	}
	t.Fatalf("the font has no %s table", tag)
	return nil
}

// fontName reads a Windows-platform name (1 family, 6 PostScript name).
func fontName(t *testing.T, font []byte, id uint16) string {
	name := fontTable(t, font, "name")
	count, strs := int(binary.BigEndian.Uint16(name[2:])), int(binary.BigEndian.Uint16(name[4:]))
	for i := 0; i < count; i++ {
		rec := name[6+12*i:]
		if binary.BigEndian.Uint16(rec) == 3 && binary.BigEndian.Uint16(rec[6:]) == id {
			size, off := int(binary.BigEndian.Uint16(rec[8:])), int(binary.BigEndian.Uint16(rec[10:]))
			raw := name[strs+off : strs+off+size]
			u := make([]uint16, size/2)
			for j := range u {
				u[j] = binary.BigEndian.Uint16(raw[2*j:])
			}
			return string(utf16.Decode(u))
		}
	}
	t.Fatalf("the font has no name %d", id)
	return ""
}

// fontCmap reads the font's Unicode BMP character map (format 4), which
// holds the Private Use Area the logos are in.
func fontCmap(t *testing.T, font []byte) map[rune]uint16 {
	cmap := fontTable(t, font, "cmap")
	n := int(binary.BigEndian.Uint16(cmap[2:]))
	for i := 0; i < n; i++ {
		rec := cmap[4+8*i:]
		if binary.BigEndian.Uint16(rec) != 3 || binary.BigEndian.Uint16(rec[2:]) != 1 {
			continue
		}
		sub := cmap[binary.BigEndian.Uint32(rec[4:]):]
		if binary.BigEndian.Uint16(sub) != 4 {
			continue
		}
		segs := int(binary.BigEndian.Uint16(sub[6:])) / 2
		ends, starts := sub[14:], sub[16+2*segs:]
		deltas, ranges := sub[16+4*segs:], sub[16+6*segs:]
		out := map[rune]uint16{}
		for s := 0; s < segs; s++ {
			end, start := binary.BigEndian.Uint16(ends[2*s:]), binary.BigEndian.Uint16(starts[2*s:])
			delta, ro := binary.BigEndian.Uint16(deltas[2*s:]), int(binary.BigEndian.Uint16(ranges[2*s:]))
			for c := int(start); c <= int(end) && c != 0xFFFF; c++ {
				g := uint16(c) + delta
				if ro != 0 {
					at := 2*s + ro + 2*(c-int(start))
					if g = binary.BigEndian.Uint16(ranges[at:]); g != 0 {
						g += delta
					}
				}
				out[rune(c)] = g
			}
		}
		return out
	}
	t.Fatal("the font has no Unicode BMP character map")
	return nil
}
