package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPacksAndStarterAreValid(t *testing.T) {
	pkgs, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no packs embedded")
	}
	for _, p := range pkgs {
		cfg, err := loadPack(p.Name)
		if err != nil {
			t.Fatal(err)
		}
		if errs := cfg.Validate(); len(errs) > 0 {
			t.Errorf("%s: %v", p.Name, errs)
		}
		if p.Description == "" || p.Commands == 0 {
			t.Errorf("%s: needs a description and commands", p.Name)
		}
		for _, m := range p.Modules {
			if m.Description == "" {
				t.Errorf("%s/%s: needs a description, shown when you pick it in Add packs", p.Name, m.Slug)
			}
		}
		if len(p.Requires) == 0 {
			for _, s := range cfg.Sections {
				for _, c := range s.All() { // a group's come down to its commands
					if len(c.Requires) == 0 {
						t.Errorf("%s: needs requires, on the pack or on every group", p.Name)
					}
				}
			}
		}
		for _, s := range cfg.Sections {
			if s.Icon == "" || s.Icon == DefaultIcon {
				t.Errorf("%s: section %s needs an icon", p.Name, s.Name)
			}
			for _, c := range s.All() {
				if c.Desc == "" {
					t.Errorf("%s: %q needs a desc", p.Name, c.Name)
				}
			}
		}
	}
	info, err := parsePackFile(PackInfoFile, StarterInfo)
	if err != nil {
		t.Fatal(err)
	}
	module, err := parsePackFile("examples.md", Starter)
	if err != nil {
		t.Fatal(err)
	}
	if errs := assemble("starter", []parsedFile{info, module}).Validate(); len(errs) > 0 {
		t.Fatal(errs)
	}
}

// userDir points the config at a temp folder with no built-in packs
// installed, and returns the folder your own packs go in.
func userDir(t *testing.T) string {
	dir := t.TempDir()
	t.Setenv("CLINGON_CONFIG", dir)
	for _, d := range []string{remoteDir(), localDir()} { // remote/ existing but empty: nothing installed
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return localDir()
}

func TestFirstRunInstallsDefaults(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := EnsureDefaults(); err != nil {
		t.Fatal(err)
	}
	names, err := Installed()
	if err != nil || strings.Join(names, ",") != "files,network,package-managers,ssh,system" {
		t.Fatalf("got %v, %v", names, err)
	}
	for _, n := range names {
		_ = Uninstall(n)
	}
	if err := EnsureDefaults(); err != nil { // remote/ exists, even empty: leave it be
		t.Fatal(err)
	}
	for _, n := range []string{"git", "aws"} {
		if err := Install(n); err != nil {
			t.Fatal(err)
		}
	}
	cfg, files, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("installed packs aren't the user's: %v", files)
	}
	if len(cfg.Sections) != 2 || cfg.Sections[0].Name != "AWS" || cfg.Sections[1].Name != "Git" {
		t.Fatalf("got %+v", cfg.Sections)
	}
}

func TestValidateCatchesMistakes(t *testing.T) {
	src := `
sections:
  - name: S
    commands:
      - name: unused arg
        cmd: echo hi
        args: [{name: x}]
      - name: choose without options
        cmd: echo <x>
        args: [{name: x, type: choose}]
      - name: bad type
        cmd: echo <x>
        args: [{name: x, type: nope}]
      - name: no cmd
`
	cfg, err := parse("t", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, e := range cfg.Validate() {
		msgs = append(msgs, e.Error())
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"never used", "choose needs options", `unknown type "nope"`, "missing cmd"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
}

// md is a module file: a title, and a command for each name: cmd pair.
func md(title string, cmds ...string) string {
	out := "# " + title + "\n"
	for i := 0; i+1 < len(cmds); i += 2 {
		out += "\n## " + cmds[i] + "\n\n```sh\n" + cmds[i+1] + "\n```\n"
	}
	return out
}

func TestUnknownSettingIsAnError(t *testing.T) {
	if _, err := parsePackFile("s.md", []byte("---\nneed: git\n---\n"+md("S", "a", "ls"))); err == nil {
		t.Fatal("a typo'd setting should fail")
	}
	if _, err := parsePackFile(PackInfoFile, []byte("---\nversion: 1.0.0\n---\n# S\n")); err == nil {
		t.Fatal("a typo'd setting in _pack.md should fail too")
	}
}

func TestLoadMergesFiles(t *testing.T) {
	dir := userDir(t)
	write := func(pack, name, body string) {
		if err := os.MkdirAll(filepath.Join(dir, pack), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, pack, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Two packs called the same thing in the menu, and one that's not.
	write("a", "_pack.md", "# Git\n")
	write("a", "g.md", md("G", "one", "git status"))
	write("b", "_pack.md", "# Git\n")
	write("b", "g.md", md("G", "two", "git log"))
	write("other", "h.md", md("H", "three", "ls"))
	cfg, files, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || len(cfg.Sections) != 2 || len(cfg.Sections[0].Groups[0].Commands) != 2 {
		t.Fatalf("bad merge: files=%v sections=%+v", files, cfg.Sections)
	}
	if o := cfg.Sections[1]; o.Name != "Other" || o.Groups[0].Name != "H" {
		t.Fatalf("with no _pack.md, the folder names the pack: %+v", o)
	}
}

func TestLoadSortsAlphabetically(t *testing.T) {
	dir := userDir(t)
	for f, src := range map[string]string{
		"zed/later.md": md("Later", "b", "ls", "A", "ls"),
		"zed/early.md": md("early", "x", "ls"),
		"alpha/one.md": md("One", "c", "ls"),
		"zed/_pack.md": "# zed\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	z := cfg.Sections[1]
	if cfg.Sections[0].Name != "Alpha" || z.Groups[0].Name != "early" || z.Groups[1].Commands[0].Name != "A" {
		t.Fatalf("not sorted: %+v", cfg.Sections)
	}
}

func TestGroupsNeedTheirTools(t *testing.T) {
	dir := userDir(t)
	for f, src := range map[string]string{
		"a/here.md": "---\nneeds: sh\n---\n" + md("here", "a", "ls"),
		"a/gone.md": "---\nneeds: no-such-tool-xyz\n---\n" + md("gone", "b", "ls"),
		"b/gone.md": "---\nneeds: no-such-tool-xyz\n---\n" + md("gone", "c", "ls"),
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sections) != 1 || len(cfg.Sections[0].Groups) != 1 || cfg.Sections[0].Groups[0].Name != "here" {
		t.Fatalf("got %+v", cfg.Sections)
	}
}

func TestCategories(t *testing.T) {
	cfg, err := parse("t", []byte("sections:\n  - {name: A, category: system, commands: [{name: c, cmd: ls}]}\n  - {name: B, commands: [{name: c, cmd: ls}]}\n  - {name: C, category: Infra, commands: [{name: c, cmd: ls}]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	errs := cfg.Validate()
	if cfg.Sections[0].Category != "System" || cfg.Sections[1].Category != Other {
		t.Fatalf("got %q, %q", cfg.Sections[0].Category, cfg.Sections[1].Category)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `unknown category "Infra"`) {
		t.Fatalf("want one unknown-category error, got %v", errs)
	}
}

func TestOptionForms(t *testing.T) {
	cfg, err := parse("t", []byte("sections:\n  - name: S\n    commands:\n      - name: c\n        cmd: x <f>\n        args:\n          - name: f\n            type: flags\n            options: [-a, {value: -b, label: bee}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	opts := cfg.Sections[0].Commands[0].Args[0].Options
	if opts[0].Value != "-a" || opts[1].Value != "-b" || opts[1].Label != "bee" {
		t.Fatalf("got %+v", opts)
	}
}

func TestNaturalLess(t *testing.T) {
	in := []string{"64", "all", "Bash", "16", "100", "1000", "2", "zsh", "sh"}
	sort.SliceStable(in, func(i, j int) bool { return NaturalLess(in[i], in[j]) })
	if strings.Join(in, " ") != "2 16 64 100 1000 all Bash sh zsh" {
		t.Fatalf("got %v", in)
	}
}

func TestHistoryBang(t *testing.T) {
	for cmd, want := range map[string]string{
		`echo "Hello, <who>!"`:  `!"`, // zsh deletes it, leaving the quote open
		`echo hi !!`:            `!!`,
		`git show !$`:           `!$`,
		`echo 'Hello!'`:         ``, // single quotes: left alone
		`[ ! -f x ] && echo no`: ``,
		`awk '$1 != 0'`:         ``,
		`test "$a" != "$b"`:     ``,
		`kill $!`:               ``,
		`echo "done\!"`:         ``,
		`echo wow!`:             ``,
		`echo "a"'!'"b"`:        ``,
	} {
		if got := historyBang(cmd); got != want {
			t.Errorf("historyBang(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestLogoIsTheIcon(t *testing.T) {
	src := `
sections:
  - name: Rust
    logo: rust
    icon: ⚙
    commands: [{name: Build, cmd: cargo build}]
  - name: Gone
    logo: no-such-logo
    icon: ▲
    commands: [{name: Hi, cmd: echo hi}]
`
	cfg, err := parse("t", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		t.Fatalf("an unknown logo shouldn't stop clingon starting: %v", errs)
	}
	if cfg.Sections[0].Icon != Logos["rust"] {
		t.Errorf("the logo should win over the icon, got %q", cfg.Sections[0].Icon)
	}
	if cfg.Sections[1].Icon != "▲" {
		t.Errorf("an unknown logo should leave the icon, got %q", cfg.Sections[1].Icon)
	}
	if u := cfg.UnknownLogos(); len(u) != 1 || !strings.Contains(u[0], `there's no logo "no-such-logo"`) {
		t.Errorf("check should hear about it, got %v", u)
	}
}

func TestSectionAndGroupNamesCantHideThings(t *testing.T) {
	cmds := []Command{{Name: "a", Cmd: "echo a"}}
	for name, s := range map[string]Section{
		"section": {Name: "x\x1b[2Jy", Commands: cmds},
		"group":   {Name: "S", Groups: []Group{{Name: "x\x1b[2Jy", Commands: cmds}}},
		"icon":    {Name: "S", Icon: "\x1b[2J", Commands: cmds},
	} {
		if errs := (&Config{Sections: []Section{s}}).Validate(); len(errs) == 0 {
			t.Errorf("%s: an escape code should be refused", name)
		}
	}
	ok := Section{Name: "S", Icon: "👩\u200d💻", Commands: cmds}
	if errs := (&Config{Sections: []Section{ok}}).Validate(); len(errs) != 0 {
		t.Errorf("an emoji with a joiner is one glyph: %v", errs)
	}
}
