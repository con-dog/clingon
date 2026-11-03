package config

import (
	"os"
	"strings"
	"testing"
)

// A recipe keeps working, for anyone with the pack, only if its commands are
// called the same thing everywhere and for good: so every built-in command
// says what it's called, and no two say the same.
func TestPackCommandsHaveTheirOwnIDs(t *testing.T) {
	pks, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, p := range pks {
		data, _ := packFS.ReadFile("packs/" + p.Name + ".yaml")
		c, err := parse(p.Name, data)
		if err != nil {
			t.Fatal(err)
		}
		for i := range c.Sections {
			s := &c.Sections[i]
			for _, cmd := range s.All() {
				where := p.Name + " › " + s.Name + " › " + cmd.Name
				if !commandIDRe.MatchString(cmd.ID) {
					t.Errorf("%s: id %q: give it id: %s", where, cmd.ID, s.CommandID(&Command{Name: cmd.Name}))
				}
				if had, ok := seen[cmd.ID]; ok {
					t.Errorf("%s has the same id as %s: %s", where, had, cmd.ID)
				}
				seen[cmd.ID] = where
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
	var id string
	cs, _ := YourCommands()
	if len(cs) != 1 {
		t.Fatalf("got %d commands", len(cs))
	}
	data, _ := readDocText(t)
	for _, l := range strings.Split(data, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "id: "); ok {
			id = v
		}
	}
	if !commandIDRe.MatchString(id) || !strings.HasPrefix(id, "deploy-") {
		t.Errorf("id %q in:\n%s", id, data)
	}
}

func TestSavedRecipes(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if rs, err := Recipes(); err != nil || len(rs) != 0 {
		t.Fatalf("none yet: %v %v", rs, err)
	}
	if err := SaveRecipe("Zed", []string{"a-00000000"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecipe("alpha", []string{"a-00000000", "b-11111111"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecipe("ZED", []string{"b-11111111"}); err == nil {
		t.Error("a second recipe of the same name should be refused")
	}
	if err := SaveRecipe("none", nil); err == nil {
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

func readDocText(t *testing.T) (string, error) {
	t.Helper()
	b, err := os.ReadFile(PackPath("mine"))
	return string(b), err
}
