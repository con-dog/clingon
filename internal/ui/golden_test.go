package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
)

// The menu's main screens, as text, at a few sizes, kept in testdata/golden:
// what's on each row and where. A change to a screen's layout shows up here
// as a diff to look at, not as something noticed in the window weeks later.
// Colour isn't in them: the palette's own test and the screens' tests have
// that.
//
// After changing a screen on purpose: UPDATE_GOLDEN=1 go test ./internal/ui/
// and read the diff of testdata/golden before keeping it.
func TestGoldenScreens(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir()) // no settings of yours
	menu := func(w, h int) *Model {
		cfg := &config.Config{Sections: []config.Section{
			{Name: "Git", Icon: "•", Category: "Code", Desc: "Everyday Git, branches and undo.", Groups: []config.Group{
				{Name: "Everyday", Commands: []config.Command{
					{Name: "Status", Cmd: "git status", Desc: "Shows what's changed."},
					{Name: "New branch", Cmd: "git switch -c <name>", Desc: "Makes a branch and switches to it.", Args: []config.Arg{{Name: "name"}}},
					{Name: "Push this branch", Cmd: "git push -u origin HEAD", Desc: "Sends this branch to the remote.", Risk: config.RiskWarn},
				}},
				{Name: "Undo", Commands: []config.Command{
					{Name: "Discard all changes", Cmd: "git reset --hard && git clean -fd", Desc: "Throws away every change that isn't committed.", Risk: config.RiskDanger},
				}},
			}},
			{Name: "Files", Icon: "•", Category: "System", Desc: "Find, copy and tidy files.", Commands: []config.Command{
				{Name: "Folder tree", Cmd: "tree -L 2", Desc: "Draws the folder as a tree, two levels deep."},
				{Name: "Count lines of code", Cmd: "git ls-files | xargs wc -l", Desc: "Counts the lines in every tracked file."},
			}},
		}}
		m := New(Options{Config: cfg, Version: "v1.2.3"})
		m.intro = -1 // the logo, drawn in
		m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		return m
	}
	press := func(m *Model, keys ...tea.KeyPressMsg) {
		for _, k := range keys {
			m.Update(k)
		}
	}
	right, down, enter := tea.KeyPressMsg{Code: tea.KeyRight}, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyEnter}
	screens := []struct {
		name string
		to   func(m *Model)
	}{
		{"home", func(m *Model) {}},
		{"pack", func(m *Model) { press(m, right) }},
		{"search", func(m *Model) { press(m, typed("/"), typed("c"), typed("o"), down) }},
		{"blank", func(m *Model) { press(m, right, down, enter) }}, // New branch, asking for its name
		{"risky", func(m *Model) { press(m, right, down, down, enter) }},
		{"help", func(m *Model) { press(m, typed("?")) }},
	}
	sizes := [][2]int{{48, 40}, {40, 24}, {72, 50}}
	update := os.Getenv("UPDATE_GOLDEN") != ""
	for _, sc := range screens {
		for _, size := range sizes {
			w, h := size[0], size[1]
			name := fmt.Sprintf("%s-%dx%d", sc.name, w, h)
			t.Run(name, func(t *testing.T) {
				m := menu(w, h)
				sc.to(m)
				var rows []string
				for _, l := range strings.Split(ansi.Strip(m.view()), "\n") {
					if got := ansi.StringWidth(l); got > w {
						t.Errorf("a row %d cells wide in a pane of %d: %q", got, w, l)
					}
					rows = append(rows, strings.TrimRight(l, " "))
				}
				if len(rows) != h {
					t.Errorf("%d rows in a pane of %d", len(rows), h)
				}
				got := strings.Join(rows, "\n") + "\n"
				path := filepath.Join("testdata", "golden", name+".txt")
				if update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%v: run UPDATE_GOLDEN=1 go test ./internal/ui/ and look at what it wrote", err)
				}
				if got != string(want) {
					gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
					for i := 0; i < max(len(gl), len(wl)); i++ {
						g, w := "", ""
						if i < len(gl) {
							g = gl[i]
						}
						if i < len(wl) {
							w = wl[i]
						}
						if g != w {
							t.Errorf("row %d\n  now: %q\n  was: %q", i, g, w)
						}
					}
					t.Errorf("the screen's changed: if that's meant, UPDATE_GOLDEN=1 go test ./internal/ui/ and read the diff of %s", path)
				}
			})
		}
	}
}
