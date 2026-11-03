package micro

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// What real micro 2.0.15 drew for these lines, captured with clingon's config.
func TestYAMLMatchesMicro(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`# TODO something`, "\x1b[38;5;103m# \x1b[m\x1b[1;38;5;215mTODO\x1b[m\x1b[38;5;103m something\x1b[m"},
		{`version: 1.1.0`, "\x1b[38;5;99mversion\x1b[m\x1b[38;5;103m: \x1b[m1.1.0"},
		{`on: true`, "\x1b[38;5;99mon\x1b[m\x1b[38;5;103m: \x1b[m\x1b[38;5;212mtrue\x1b[m"},
		{`  - name: Node`, "\x1b[38;5;103m  - \x1b[m\x1b[38;5;99mname\x1b[m\x1b[38;5;103m: \x1b[mNode"},
		{`url: http://x.y/z`, "\x1b[38;5;99murl\x1b[m\x1b[38;5;103m: \x1b[m\x1b[38;5;99mhttp\x1b[m\x1b[38;5;103m:\x1b[m//x.\x1b[38;5;212my\x1b[m/z"},
		{`a: "b # c"`, "\x1b[38;5;99ma\x1b[m\x1b[38;5;103m:\x1b[m\x1b[38;5;180m \"b # c\"\x1b[m"},
		{`d: "x \"y\" z"`, "\x1b[38;5;99md\x1b[m\x1b[38;5;103m:\x1b[m\x1b[38;5;180m \"x \x1b[m\x1b[38;5;212m\\\"\x1b[m\x1b[38;5;180my\x1b[m\x1b[38;5;212m\\\"\x1b[m\x1b[38;5;180m z\"\x1b[m"},
		// micro's slip: the string starts at 4 and its end is 4 along, so no pink.
		{`d: 'a \"'`, "\x1b[38;5;99md\x1b[m\x1b[38;5;103m:\x1b[m\x1b[38;5;180m 'a \\\"'\x1b[m"},
		{`   e: 'a \"'`, "   \x1b[38;5;99me\x1b[m\x1b[38;5;103m:\x1b[m\x1b[38;5;180m 'a \x1b[m\x1b[38;5;212m\\\"\x1b[m\x1b[38;5;180m'\x1b[m"},
	} {
		if got := YAML(c.src); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.src, got, c.want)
		}
	}
	// A string left open carries on to the next line.
	if got := YAML("a: \"b\nc: d\"\ne: f"); !strings.Contains(got, "\x1b[38;5;180mc: d\"\x1b[m") || !strings.Contains(got, "\x1b[38;5;99me\x1b[m") {
		t.Errorf("open string: %q", got)
	}
}

// Colour is all it adds: every pack comes through otherwise as it is.
func TestYAMLKeepsTheText(t *testing.T) {
	files, _ := filepath.Glob("../config/packs/*.yaml")
	if len(files) == 0 {
		t.Fatal("no packs found")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if got := ansi.Strip(YAML(string(b))); got != string(b) {
			t.Errorf("%s changed", f)
		}
	}
}

func TestSchemeHasEveryColourUsed(t *testing.T) {
	groups := []string{"constant.string", "comment", "todo", "type", "constant", "statement", "identifier", "special"}
	for _, g := range groups {
		if colours[g] == "" {
			t.Errorf("clingon.micro has no %s", g)
		}
	}
}
