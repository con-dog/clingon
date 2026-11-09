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
	if len(c.Args) != 2 || c.Args[0].Prompt != "Env" || c.Args[1].Kind() != "secret" {
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
