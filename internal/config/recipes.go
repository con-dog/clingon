package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// MaxRecipeSteps is as many steps as there are number keys.
const MaxRecipeSteps = 9

// Recipe is a saved sequence of commands, by CommandID.
type Recipe struct {
	Name  string   `yaml:"name"`
	Steps []string `yaml:"steps"`
}

var (
	notSlug     = regexp.MustCompile(`[^a-z0-9]+`)
	commandIDRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*-[0-9a-f]{8}$`)
)

// CommandID is what a recipe calls one of the section's commands, like
// aws-1a2b3c4d: the same for everyone with the pack. A pack's command says so
// itself with id:, so it's kept whatever the command is renamed to. One that
// doesn't (written by hand) gets the one newCommandID gives it.
func (s *Section) CommandID(c *Command) string {
	if c.ID != "" {
		return c.ID
	}
	group := ""
	for i := range s.Groups {
		for j := range s.Groups[i].Commands {
			if &s.Groups[i].Commands[j] == c {
				group = s.Groups[i].Name
			}
		}
	}
	return newCommandID(s.Name, group, c.Name)
}

// newCommandID makes the ID for a command with no id: of its own. The group
// counts: Node has a "Run tests" under both npm and pnpm.
func newCommandID(section, group, name string) string {
	sum := sha256.Sum256([]byte(section + "\x00" + group + "\x00" + name))
	slug := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(section), "-"), "-")
	if slug == "" {
		slug = "command"
	}
	return slug + "-" + hex.EncodeToString(sum[:4])
}

func recipesPath() string { return filepath.Join(Dir(), "recipes.yaml") }

// Recipes lists your saved recipes by name.
func Recipes() ([]Recipe, error) {
	data, err := os.ReadFile(recipesPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Recipes []Recipe `yaml:"recipes"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", recipesPath(), err)
	}
	slices.SortStableFunc(file.Recipes, func(a, b Recipe) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return file.Recipes, nil
}

func writeRecipes(rs []Recipe) error {
	data, err := yaml.Marshal(struct {
		Recipes []Recipe `yaml:"recipes"`
	}{rs})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(recipesPath(), data, 0o644)
}

// HasRecipe says whether rs has one by that name, whatever its capitals.
func HasRecipe(rs []Recipe, name string) bool {
	return slices.ContainsFunc(rs, func(r Recipe) bool { return strings.EqualFold(r.Name, name) })
}

// SaveRecipe adds a recipe. It won't replace one of the same name.
func SaveRecipe(name string, steps []string) error {
	if name = strings.TrimSpace(name); name == "" {
		return errors.New("a recipe needs a name")
	}
	if len(steps) == 0 || len(steps) > MaxRecipeSteps {
		return fmt.Errorf("a recipe has 1 to %d steps (not %d)", MaxRecipeSteps, len(steps))
	}
	rs, err := Recipes()
	if err != nil {
		return err
	}
	if HasRecipe(rs, name) {
		return fmt.Errorf("you already have a recipe called %q", name)
	}
	return writeRecipes(append(rs, Recipe{name, steps}))
}

// RemoveRecipe deletes the recipe called name.
func RemoveRecipe(name string) error {
	rs, err := Recipes()
	if err != nil {
		return err
	}
	if !HasRecipe(rs, name) {
		return fmt.Errorf("you don't have a recipe called %q", name)
	}
	return writeRecipes(slices.DeleteFunc(rs, func(r Recipe) bool { return strings.EqualFold(r.Name, name) }))
}
