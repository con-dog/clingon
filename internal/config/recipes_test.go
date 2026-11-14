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
	// Replacing one keeps its description, written by hand.
	if err := os.WriteFile(recipeFile("Zed"), []byte("# Zed\n\nMy notes.\n\n1. [A](clingon:a-00000000)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceRecipe("Zed", []string{"b-11111111", "a-00000000"}, nil); err != nil {
		t.Fatal(err)
	}
	if rs, _ := Recipes(); !slices.ContainsFunc(rs, func(r Recipe) bool {
		return r.Name == "Zed" && r.Description == "My notes." && slices.Equal(r.Steps, []string{"b-11111111", "a-00000000"})
	}) {
		t.Errorf("replaced, its description kept: %+v", rs)
	}
	if err := ReplaceRecipe("Nobody", []string{"a-00000000"}, nil); err == nil {
		t.Error("replacing what isn't there should be refused")
	}
	if err := ReplaceRecipe("Zed", []string{"a-00000000"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecipe("Zed", []string{"a-00000000"}, nil); err == nil {
		t.Error("after replacing, there's still one of that name")
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

// A recipe's step can be a command in a folder whose name has spaces or
// brackets (a project's own pack, in "My Project (v2)"): its link is written
// so it reads back as the same step, and never so the recipes can't be read.
func TestRecipeStepsInFoldersWithSpaces(t *testing.T) {
	userDir(t)
	steps := []string{
		"/Users/me/My Project (v2)/.clingon/packs/deploy/build.md#run",
		"mine/my tools/100% done.md#do-it",
		"remote/code/git/everyday.md#status",
		"clingon-12345678",
	}
	if err := SaveRecipe("Ship it", steps, nil); err != nil {
		t.Fatal(err)
	}
	rs, err := Recipes()
	if err != nil {
		t.Fatalf("the recipes can't be read back: %v", err)
	}
	if len(rs) != 1 || !slices.Equal(rs[0].Steps, steps) {
		t.Fatalf("read back %+v, want steps %q", rs, steps)
	}
	// And once more over itself: still readable, the same steps.
	if err := ReplaceRecipe("Ship it", steps[:2], nil); err != nil {
		t.Fatal(err)
	}
	if rs, err = Recipes(); err != nil || len(rs) != 1 || !slices.Equal(rs[0].Steps, steps[:2]) {
		t.Fatalf("replaced: %+v, %v", rs, err)
	}
}

// Recipes are read once and given again while nothing's changed, and read
// afresh the moment anything has: one added, removed, or edited in place (by
// hand, where its folder's own time doesn't move). What's handed out is the
// caller's own: changing it changes nothing kept.
func TestRecipesAreReadAgainOnlyWhenChanged(t *testing.T) {
	userDir(t)
	names := func() string {
		rs, err := Recipes()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rs {
			out = append(out, r.Name+":"+strings.Join(r.Steps, "+"))
		}
		return strings.Join(out, " ")
	}
	if names() != "" || names() != "" {
		t.Fatal("none yet")
	}
	if err := SaveRecipe("One", []string{"a-00000000"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := names(); got != "One:a-00000000" {
		t.Fatalf("added: %q", got)
	}
	rs, _ := Recipes()
	rs[0].Name, rs[0].Steps[0] = "Changed", "changed"
	if got := names(); got != "One:a-00000000" {
		t.Fatalf("what's handed out isn't what's kept: %q", got)
	}
	// Edited in place, as an editor writing over the file does: same folder
	// entry, a new size.
	file := recipeFile("One")
	if err := os.WriteFile(file, []byte("# One\n\n1. [A](clingon:a-00000000)\n2. [B](clingon:b-11111111)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := names(); got != "One:a-00000000+b-11111111" {
		t.Fatalf("edited in place: %q", got)
	}
	if err := SaveRecipe("Two", []string{"b-11111111"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := names(); got != "One:a-00000000+b-11111111 Two:b-11111111" {
		t.Fatalf("a second added: %q", got)
	}
	if err := RemoveRecipe("One"); err != nil {
		t.Fatal(err)
	}
	if got := names(); got != "Two:b-11111111" {
		t.Fatalf("one removed: %q", got)
	}
	// Another config folder is another set of recipes.
	userDir(t)
	if got := names(); got != "" {
		t.Fatalf("a different folder's: %q", got)
	}
}
