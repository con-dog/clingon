package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The pack form writes what it edits in the format packs are read in: every
// built-in pack, written back out, reads back the same.
func TestWritingPacksReadsBackTheSame(t *testing.T) {
	strip := func(g *Group) {
		for i := range g.Commands {
			g.Commands[i].Source = ""
		}
	}
	n := 0
	for _, pack := range builtinPackNames() {
		for _, f := range builtinFiles(pack) {
			if f == PackInfoFile {
				continue
			}
			data, _ := builtinFile(pack, f)
			part, err := parsePackFile(f, data)
			if err != nil {
				t.Fatal(err)
			}
			again, err := parsePackFile(f, []byte(moduleMarkdown(part.group)))
			if err != nil {
				t.Fatalf("%s/%s written out: %v\n%s", pack, f, err, moduleMarkdown(part.group))
			}
			strip(part.group)
			strip(again.group)
			if !reflect.DeepEqual(part.group, again.group) {
				t.Errorf("%s/%s reads back differently", pack, f)
			}
			n += len(part.group.Commands)
		}
	}
	if n < 400 {
		t.Errorf("only %d commands checked", n)
	}
}

func TestEditablePack(t *testing.T) {
	userDir(t)
	if err := NewPackWithLogo("work", "rocket", "orange"); err != nil {
		t.Fatal(err)
	}
	p, err := LoadEditable("work")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Work" || p.Logo != "rocket" || p.Color != "orange" || len(p.Files) != 1 || p.Section != "other" {
		t.Fatalf("loaded %+v", p)
	}
	// A new command's blanks come from its command.
	c := Command{Name: "Ship it", Cmd: "./ship.sh <env> <token>"}
	SyncBlanks(&c)
	if len(c.Args) != 2 || c.Args[0].Prompt != "" || c.Args[0].Label() != "Env" || c.Args[1].Kind() != "secret" { // no question yet: asked by its name
		t.Fatalf("blanks %+v", c.Args)
	}
	p.Files = append(p.Files, EditableFile{Slug: p.NewSlug("Deploy"), Group: Group{Name: "Deploy", Commands: []Command{c}}})
	p.Section = "infrastructure"
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(localDir(), "work", "deploy.md")); err != nil {
		t.Fatalf("saved in its folder: %v", err)
	}
	if info, _ := os.ReadFile(filepath.Join(localDir(), "work", PackInfoFile)); !strings.HasPrefix(string(info), "---\nmenu: infrastructure\n") {
		t.Errorf("the section is in _pack.md: %q", info)
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s := cfg.Sections[0]; s.Category != "Infrastructure" || len(s.All()) != 2 {
		t.Errorf("loads back: %+v", s)
	}
	if again, err := LoadEditable("work"); err != nil || again.Section != "infrastructure" {
		t.Errorf("the form reads the section back: %v %+v", err, again)
	}
	// What wouldn't load isn't saved.
	p.Files[1].Group.Commands[0].Name = "Ship it!"
	if err := p.Save(); err == nil || !strings.Contains(err.Error(), "callout") {
		t.Errorf("a bad name: %v", err)
	}
}

// Files in a pack's folder that aren't the pack's (a note of yours, an
// editor's lock file, the Finder's ._ twin) are nothing to the pack form: it
// opens the pack as the menu does, and saving leaves them where they are. The
// same in recipes/: one beside your recipes doesn't hide them.
func TestFilesThatArentThePacksAreLeftAlone(t *testing.T) {
	mine := userDir(t)
	if err := NewPackWithLogo("demo", "rocket", "orange"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(mine, "demo")
	strays := map[string]string{"_notes.md": "to do: tidy this\n", ".#demo.md": "not a pack file at all", "._demo.md": "\x00\x05\x16\x07"}
	for name, text := range strays {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := LoadEditable("demo")
	if err != nil {
		t.Fatalf("the form can't open a pack the menu can: %v", err)
	}
	for _, f := range p.Files {
		if skippedName(f.Slug) {
			t.Errorf("%s was taken as a module", f.Slug)
		}
	}
	p.Description = "Changed"
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	for name, text := range strays {
		if got, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(got) != text {
			t.Errorf("%s should be left as it was: %q, %v", name, got, err)
		}
	}

	if err := SaveRecipe("Mine", []string{"a-00000000"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"_ideas.md", "._mine.md"} {
		if err := os.WriteFile(filepath.Join(RecipesDir(), name), []byte("not a recipe"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if rs, err := Recipes(); err != nil || len(rs) != 1 || rs[0].Name != "Mine" {
		t.Errorf("recipes: %+v, %v", rs, err)
	}
	if err := RemoveRecipe("Mine"); err != nil {
		t.Error(err)
	}
}
