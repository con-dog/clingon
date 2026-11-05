package main

import (
	"os"
	"testing"

	"github.com/con-dog/clingon/internal/config"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "v0.1.9", true},
		{"v1.0.0", "0.9.9", true},
		{"v0.10.0", "v0.9.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"", "v0.1.0", false},
		{"v0.2.0", "dev", false},
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestTmuxTooOld(t *testing.T) {
	for v, want := range map[string]bool{"tmux 3.8": false, "tmux 3.2a": false, "tmux 3.1c": true, "tmux 2.9": true, "tmux master": false, "tmux next-3.6": false} {
		if tmuxTooOld(v) != want {
			t.Errorf("tmuxTooOld(%q) != %v", v, want)
		}
	}
}

// Every menu clingon shows is in natural alphabetical order.
func TestEveryMenuIsSorted(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	for _, n := range mustPacks(t) {
		if err := config.Install(n); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	var sections []*config.Section
	for i := range cfg.Sections {
		sections = append(sections, &cfg.Sections[i])
	}
	sections = append(sections, builtinSections()...)
	check := func(where string, names []string) {
		for i := 1; i < len(names); i++ {
			if config.NaturalLess(names[i], names[i-1]) {
				t.Errorf("%s: %q comes before %q", where, names[i], names[i-1])
			}
		}
	}
	var builtinNames []string
	for _, s := range builtinSections() {
		builtinNames = append(builtinNames, s.Name)
	}
	check("Clingon sections", builtinNames)
	for _, s := range sections {
		var groups, cmds []string
		for _, c := range s.Commands {
			cmds = append(cmds, c.Name)
		}
		check(s.Name, cmds)
		for _, g := range s.Groups {
			groups = append(groups, g.Name)
			var gc []string
			for _, c := range g.Commands {
				gc = append(gc, c.Name)
			}
			check(s.Name+" › "+g.Name, gc)
		}
		check(s.Name+" groups", groups)
		for _, c := range s.All() {
			for _, a := range c.Args {
				var opts []string
				for _, o := range a.Options {
					opts = append(opts, o.Value)
				}
				check(s.Name+" › "+c.Name+" › "+a.Name, opts)
			}
		}
	}
}

func mustPacks(t *testing.T) []string {
	ps, err := config.Packs()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return names
}

// Clingon's own sections use group headings, like every pack.
func TestBuiltinSectionsAreGrouped(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	for _, s := range builtinSections() {
		if len(s.Commands) > 0 || len(s.Groups) == 0 {
			t.Errorf("%s: put its commands in groups", s.Name)
		}
	}
}

func TestReleaseCacheRoundTrip(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if c := readReleaseCache(); !c.Checked.IsZero() || c.Latest != "" {
		t.Fatalf("no file: nothing known, got %+v", c)
	}
	if err := os.MkdirAll(config.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releaseCachePath(), []byte("1767225600 v1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := readReleaseCache(); c.Checked.Unix() != 1767225600 || c.Latest != "v1.2.3" {
		t.Fatalf("got %+v", c)
	}
	if err := os.WriteFile(releaseCachePath(), []byte("1767225600 \n"), 0o644); err != nil { // checked: no release yet
		t.Fatal(err)
	}
	if c := readReleaseCache(); c.Checked.IsZero() || c.Latest != "" {
		t.Fatalf("got %+v", c)
	}
}
