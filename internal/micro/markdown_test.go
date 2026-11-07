package micro

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Colour is all it adds: every pack comes through otherwise as it is.
func TestMarkdownKeepsTheText(t *testing.T) {
	files, _ := filepath.Glob("../config/packs/*/*.md")
	if len(files) < 50 {
		t.Fatalf("only %d pack files found", len(files))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if got := ansi.Strip(Markdown(string(b))); got != string(b) {
			t.Errorf("%s changed", f)
		}
	}
}

// The same colours micro uses for the file (checked against micro itself):
// the command is coloured as the menu colours it.
func TestMarkdownColoursWhatMatters(t *testing.T) {
	src := "---\nwhen: .git\n---\n# Undo\n\n## Hard reset\n> [!CAUTION]\n> Throws it away.\n\n```sh needs=git\ngit reset --hard | grep \"<branch>\"\n```\n\n- `<branch>` Which branch\n  - (x) `main`\n"
	got := Markdown(src)
	for _, want := range []string{
		"\x1b[38;5;147mwhen:\x1b[m",
		"\x1b[38;5;60m## \x1b[m\x1b[1;38;5;212mHard reset\x1b[m",
		"\x1b[1;38;5;204m> [!CAUTION]\x1b[m",
		"\x1b[38;5;147mneeds=\x1b[m",
		"\x1b[38;5;79mgit\x1b[m",
		"\x1b[38;5;147m --hard\x1b[m",
		"\x1b[38;5;103m|\x1b[m",
		"\x1b[38;5;79m grep\x1b[m",
		"\x1b[38;5;180m\"\x1b[m\x1b[38;5;212m<branch>\x1b[m\x1b[38;5;180m\"\x1b[m",
		"\x1b[38;5;79m`<branch>`\x1b[m",
		"\x1b[38;5;103m  - \x1b[m\x1b[38;5;147m(x)\x1b[m",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}
