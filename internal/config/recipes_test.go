package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A recipe keeps working, for anyone with the pack, only if its commands are
// called the same thing everywhere: a built-in command's ID comes from its
// section, module and name, so no two may share all three.
func TestPackCommandIDsAreUnique(t *testing.T) {
	pks, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, p := range pks {
		c, err := loadPack(p.Name)
		if err != nil {
			t.Fatal(err)
		}
		for i := range c.Sections {
			s := &c.Sections[i]
			for _, cmd := range s.All() {
				where := p.Name + " › " + s.Name + " › " + cmd.Name
				id := s.CommandID(cmd)
				if !commandIDRe.MatchString(id) {
					t.Errorf("%s: id %q doesn't look like an ID", where, id)
				}
				if had, ok := seen[id]; ok {
					t.Errorf("%s has the same id as %s: %s", where, had, id)
				}
				seen[id] = where
			}
		}
	}
	if len(seen) < 400 {
		t.Errorf("only %d commands: are the packs being read?", len(seen))
	}
}

func TestCommandIDs(t *testing.T) {
	s := &Section{Name: "My AWS!", Commands: []Command{{Name: "ls"}, {Name: "renamed", ID: "aws-1a2b3c4d"}},
		Groups: []Group{{Name: "npm", Commands: []Command{{Name: "Run tests"}}}, {Name: "pnpm", Commands: []Command{{Name: "Run tests"}}}}}
	if id := s.CommandID(&s.Commands[1]); id != "aws-1a2b3c4d" {
		t.Errorf("its own id: got %q", id)
	}
	id := s.CommandID(&s.Commands[0])
	if !commandIDRe.MatchString(id) || !strings.HasPrefix(id, "my-aws-") {
		t.Errorf("worked out: got %q", id)
	}
	if a, b := s.CommandID(&s.Groups[0].Commands[0]), s.CommandID(&s.Groups[1].Commands[0]); a == b {
		t.Errorf("same name in two groups got the same id: %s", a)
	}
	bad := &Config{Sections: []Section{{Name: "S", Icon: "•", Category: Other, Commands: []Command{{Name: "x", Cmd: "x", ID: "Not An ID"}}}}}
	if errs := bad.Validate(); len(errs) == 0 {
		t.Error("a malformed id should be refused")
	}
}

func TestAddedCommandsGetAnID(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := AddCommand("mine", "Deploy", "Tail logs", "kubectl logs", ""); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s := &cfg.Sections[0]
	if cs := s.All(); len(cs) != 1 {
		t.Fatalf("got %d commands", len(cs))
	}
	// A command from a file is called after its file and heading, as a
	// recipe's link to it is.
	if id := s.CommandID(s.All()[0]); id != "mine/mine/deploy.md#tail-logs" {
		t.Errorf("id %q", id)
	}
}

func TestSavedRecipes(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if rs, err := Recipes(); err != nil || len(rs) != 0 {
		t.Fatalf("none yet: %v %v", rs, err)
	}
	if err := SaveRecipe("Zed", []string{"a-00000000"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecipe("alpha", []string{"a-00000000", "b-11111111"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecipe("ZED", []string{"b-11111111"}, nil); err == nil {
		t.Error("a second recipe of the same name should be refused")
	}
	if err := SaveRecipe("none", nil, nil); err == nil {
		t.Error("a recipe with no steps should be refused")
	}
	rs, err := Recipes()
	if err != nil || len(rs) != 2 || rs[0].Name != "alpha" || len(rs[0].Steps) != 2 {
		t.Fatalf("got %+v %v", rs, err)
	}
	if err := RemoveRecipe("nope"); err == nil {
		t.Error("removing what isn't there should say so")
	}
	if err := RemoveRecipe("alpha"); err != nil {
		t.Fatal(err)
	}
	if rs, _ := Recipes(); len(rs) != 1 || rs[0].Name != "Zed" {
		t.Errorf("got %+v", rs)
	}
}

// A recipe is a Markdown file of links to commands' headings: it reads back
// to the same steps, and its links work from recipes/ like any relative link.
func TestRecipeFiles(t *testing.T) {
	dir := userDir(t)
	writePack(t, "work/deploy", "# Deploy\n\n## Ship it\n\n```sh\n./ship.sh\n```\n\n## Up (detached)\n\n```sh\nup\n```\n")
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s := &cfg.Sections[0]
	var ids []string
	for _, c := range s.All() {
		ids = append(ids, s.CommandID(c))
	}
	ids = append(ids, "freeform-00000000") // one of clingon's own: no file
	if err := SaveRecipe("Deploy check", ids, []string{"Ship it", "Up (detached)", "Type a command"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(RecipesDir(), "deploy-check.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "# Deploy check\n\n1. [Ship it](../packs/mine/work/deploy.md#ship-it)\n2. [Up (detached)](../packs/mine/work/deploy.md#up-detached)\n3. [Type a command](clingon:freeform-00000000)\n"
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	// Each link reaches the file it names, from where the recipe is.
	if _, err := os.Stat(filepath.Join(RecipesDir(), "../packs/mine/work/deploy.md")); err != nil {
		t.Errorf("the link's file: %v", err)
	}
	// A description added by hand is kept; the steps read back as they were.
	if err := os.WriteFile(filepath.Join(RecipesDir(), "deploy-check.md"), []byte(strings.Replace(string(data), "\n\n", "\nShips, then starts it.\n\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := Recipes()
	if err != nil || len(rs) != 1 {
		t.Fatalf("got %+v %v", rs, err)
	}
	if r := rs[0]; r.Name != "Deploy check" || r.Description != "Ships, then starts it." || !slices.Equal(r.Steps, ids) {
		t.Errorf("got %+v, want steps %v", r, ids)
	}
	if err := SaveRecipe("Bad: name", ids, nil); err == nil {
		t.Error("recipe names follow the naming rule")
	}
	if err := os.WriteFile(filepath.Join(RecipesDir(), "broken.md"), []byte("# Broken\n\n- not numbered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Recipes(); err == nil || !strings.Contains(err.Error(), "numbered list of links") {
		t.Errorf("a broken recipe says why: %v", err)
	}
	_ = dir
}
