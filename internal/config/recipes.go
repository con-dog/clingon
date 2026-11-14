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
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxRecipeSteps is as many steps as there are number keys.
const MaxRecipeSteps = 9

// Recipe is a saved sequence of commands, by CommandID. On disk it's a
// Markdown file in recipes/: a # Name, an optional description, and a
// numbered list of links, each to a command's ## heading in its pack's file:
//
//	# Deploy check
//	Pull, then see what changed.
//
//	1. [Status](../packs/remote/code/git/everyday.md#status)
//	2. [Type a command](clingon:freeform-00000000)
//
// A command with no file (one of clingon's own) is a clingon: link to its ID.
type Recipe struct {
	Name        string
	Description string
	Steps       []string
	Labels      []string // what each step's link says: the command's name
}

var (
	notSlug     = regexp.MustCompile(`[^a-z0-9]+`)
	commandIDRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*-[0-9a-f]{8}$`)
	anchorDrop  = regexp.MustCompile(`[^a-z0-9 _-]`)
)

// CommandID is what a recipe calls one of the section's commands. A command
// from a pack's file is its file and heading, like
// remote/code/git/everyday.md#status: the same for everyone with the pack,
// and what a recipe's link points at. One of clingon's own says what it's
// called with ID; any other gets the one newCommandID gives it.
func (s *Section) CommandID(c *Command) string {
	if c.ID != "" {
		return c.ID
	}
	if filepath.IsAbs(c.Source) {
		return sourceRef(c.Source, c.Name)
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

// Anchor is a heading's link anchor, as GitHub makes it: Up (detached) is
// up-detached.
func Anchor(heading string) string {
	return strings.ReplaceAll(anchorDrop.ReplaceAllString(strings.ToLower(strings.TrimSpace(heading)), ""), " ", "-")
}

// sourceRef is a command's ID from its file: the file within packs/ (or, for a
// project's pack, its whole path) and its heading's anchor.
func sourceRef(path, name string) string {
	// What says where packs are (Dir), rather than the folder itself: this
	// is asked of every command at once, and joining a path each time adds up.
	key := [4]string{os.Getenv("CLINGON_CONFIG"), os.Getenv("HOME"), path, name}
	if ref, ok := sourceRefs.Load(key); ok {
		return ref.(string)
	}
	ref := path
	if rel, err := filepath.Rel(packsDir(), path); err == nil && !strings.HasPrefix(rel, "..") {
		ref = filepath.ToSlash(rel)
	}
	ref += "#" + Anchor(name)
	sourceRefs.Store(key, ref)
	return ref
}

// sourceRefs keeps what sourceRef worked out, by what it worked it out from
// (where packs are, the file, the name): the menu asks for every command's
// ID at once, as often as each time it draws a recipe.
var sourceRefs sync.Map

// newCommandID makes the ID for a command with no file or ID of its own. The
// group counts: two modules can each have a "Run tests".
func newCommandID(section, group, name string) string {
	sum := sha256.Sum256([]byte(section + "\x00" + group + "\x00" + name))
	slug := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(section), "-"), "-")
	if slug == "" {
		slug = "command"
	}
	return slug + "-" + hex.EncodeToString(sum[:4])
}

// RecipesDir is where saved recipes are, next to packs/.
func RecipesDir() string { return filepath.Join(Dir(), "recipes") }

// recipeFile is where the recipe called name is saved.
func recipeFile(name string) string {
	return filepath.Join(RecipesDir(), strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")+".md")
}

// recipeFiles are the recipes' files: every .md in recipes/ but those whose
// names start with _ or ., as in a pack's folder (a note of yours, or a file
// an editor or the Finder keeps beside one, which isn't a recipe and needn't
// read as one).
func recipeFiles() []string {
	return slices.DeleteFunc(mdFiles(RecipesDir()), func(f string) bool { return skippedName(filepath.Base(f)) })
}

// Recipes lists your saved recipes by name. The menu asks each time it draws
// them, so what was read last is kept and given again for as long as the
// folder and each file in it are as they were then (their sizes and times:
// a look, not a read).
func Recipes() ([]Recipe, error) {
	dir := RecipesDir()
	recipesRead.Lock()
	defer recipesRead.Unlock()
	if recipesRead.dir == dir && recipesRead.unchanged() {
		return cloneRecipes(recipesRead.rs), nil
	}
	recipesRead.dir, recipesRead.files = "", nil
	folder, statErr := os.Stat(dir) // before it's listed: changed since, and the next look says so
	var rs []Recipe
	var files []fileSeen
	for _, f := range recipeFiles() {
		st, err := os.Stat(f)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		r, err := parseRecipe(f, data)
		if err != nil {
			return nil, err
		}
		rs = append(rs, *r)
		files = append(files, fileSeen{f, st.Size(), st.ModTime()})
	}
	slices.SortStableFunc(rs, func(a, b Recipe) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	if statErr == nil {
		recipesRead.dir, recipesRead.at, recipesRead.files, recipesRead.rs = dir, folder.ModTime(), files, cloneRecipes(rs)
	}
	return rs, nil
}

// fileSeen is a file as it was when read: where, how big, and when written.
type fileSeen struct {
	path string
	size int64
	at   time.Time
}

// recipesRead is what Recipes last read, and what it read it from.
var recipesRead recipesSeen

type recipesSeen struct {
	sync.Mutex
	dir   string
	at    time.Time // the folder's own time: a file added, removed or replaced changes it
	files []fileSeen
	rs    []Recipe
}

// unchanged says whether the folder and its files are as they were read.
// Called with the lock held.
func (c *recipesSeen) unchanged() bool {
	if st, err := os.Stat(c.dir); err != nil || !st.ModTime().Equal(c.at) {
		return false
	}
	for _, f := range c.files {
		if st, err := os.Stat(f.path); err != nil || st.Size() != f.size || !st.ModTime().Equal(f.at) {
			return false
		}
	}
	return true
}

// cloneRecipes copies rs through, so what's kept is never what's handed out.
func cloneRecipes(rs []Recipe) []Recipe {
	out := slices.Clone(rs)
	for i := range out {
		out[i].Steps, out[i].Labels = slices.Clone(out[i].Steps), slices.Clone(out[i].Labels)
	}
	return out
}

var recipeStepRe = regexp.MustCompile(`^\d+[.)] \[([^\]]*)\]\(([^)\s]+)\)\s*$`)

// parseRecipe reads a recipe's file.
func parseRecipe(path string, data []byte) (*Recipe, error) {
	m := &mdLines{path: path, lines: stripComments(strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"))}
	name, desc, err := m.title()
	if err != nil {
		return nil, err
	}
	r := &Recipe{Name: name, Description: desc}
	for ; m.at < len(m.lines); m.at++ {
		l := strings.TrimSpace(m.lines[m.at])
		if l == "" {
			continue
		}
		s := recipeStepRe.FindStringSubmatch(l)
		if s == nil {
			return nil, m.errf(m.at, "a recipe's steps are a numbered list of links to commands, like 1. [Status](../packs/remote/code/git/everyday.md#status)")
		}
		r.Steps, r.Labels = append(r.Steps, stepRef(s[2])), append(r.Labels, s[1])
	}
	if len(r.Steps) == 0 || len(r.Steps) > MaxRecipeSteps {
		return nil, fmt.Errorf("%s: a recipe has 1 to %d steps (not %d)", path, MaxRecipeSteps, len(r.Steps))
	}
	return r, nil
}

// stepRef is the CommandID a recipe's link points at.
func stepRef(target string) string {
	target = linkUnescape(target)
	if id, ok := strings.CutPrefix(target, "clingon:"); ok {
		return id
	}
	file, anchor, _ := strings.Cut(target, "#")
	if !filepath.IsAbs(file) {
		file = filepath.Join(RecipesDir(), file)
	}
	return sourceRef(filepath.Clean(file), "") + anchor
}

// stepLink is the link a recipe's file has for a CommandID.
func stepLink(ref string) string {
	file, anchor, ok := strings.Cut(ref, "#")
	switch {
	case !ok:
		return linkEscape("clingon:" + ref)
	case filepath.IsAbs(file):
		return linkEscape(ref)
	}
	return linkEscape("../packs/" + file + "#" + anchor)
}

// linkEscape writes a link's target so Markdown takes it as one, and
// parseRecipe reads it back: a space or a bracket in it (a folder called
// "My Project (v2)") would end the link early, so each is written as its
// code, %20 for a space, as a web address would have it.
func linkEscape(target string) string {
	var b strings.Builder
	for i := 0; i < len(target); i++ {
		if c := target[i]; c <= ' ' || c == '%' || c == '(' || c == ')' || c == 0x7f {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// linkUnescape is the target linkEscape wrote: each %20 the character it
// stands for. Anything that isn't such a code stays as it is.
func linkUnescape(target string) string {
	if !strings.Contains(target, "%") {
		return target
	}
	var b strings.Builder
	for i := 0; i < len(target); i++ {
		if target[i] == '%' && i+2 < len(target) {
			if c, err := strconv.ParseUint(target[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(c))
				i += 2
				continue
			}
		}
		b.WriteByte(target[i])
	}
	return b.String()
}

// HasRecipe says whether rs has one by that name, whatever its capitals.
func HasRecipe(rs []Recipe, name string) bool {
	return slices.ContainsFunc(rs, func(r Recipe) bool { return strings.EqualFold(r.Name, name) })
}

// SaveRecipe adds a recipe, as a file of its own. It won't replace one of the
// same name. labels are what each step's link says (the command's name); a
// missing one is made from the step.
func SaveRecipe(name string, steps, labels []string) error {
	return saveRecipe(name, steps, labels, false)
}

// ReplaceRecipe saves a recipe over the one of that name, keeping its
// description: what Edit, then Add as… under the same name, does.
func ReplaceRecipe(name string, steps, labels []string) error {
	return saveRecipe(name, steps, labels, true)
}

func saveRecipe(name string, steps, labels []string, replace bool) error {
	if name = strings.TrimSpace(name); name == "" {
		return errors.New("a recipe needs a name")
	}
	if !nameCharsRe.MatchString(name) {
		return fmt.Errorf("recipe names use only letters, digits, spaces and ( ) (not %q)", name)
	}
	if len(steps) == 0 || len(steps) > MaxRecipeSteps {
		return fmt.Errorf("a recipe has 1 to %d commands (not %d)", MaxRecipeSteps, len(steps))
	}
	rs, err := Recipes()
	if err != nil {
		return err
	}
	path := recipeFile(name)
	desc, replaced := "", ""
	if replace {
		i := slices.IndexFunc(rs, func(r Recipe) bool { return strings.EqualFold(r.Name, name) })
		if i < 0 {
			return fmt.Errorf("you don't have a recipe called %q to replace", name)
		}
		desc, replaced = rs[i].Description, rs[i].Name
	} else if _, err := os.Stat(path); HasRecipe(rs, name) || err == nil {
		return fmt.Errorf("you already have a recipe called %q", name)
	}
	var b strings.Builder
	b.WriteString("# " + name + "\n\n")
	if desc != "" {
		b.WriteString(desc + "\n\n")
	}
	for i, st := range steps {
		label := ""
		if i < len(labels) {
			label = labels[i]
		}
		if label == "" {
			_, a, _ := strings.Cut(st, "#")
			label = sectionTitle(a)
			if a == "" {
				label = st
			}
		}
		fmt.Fprintf(&b, "%d. [%s](%s)\n", i+1, strings.NewReplacer("[", "(", "]", ")").Replace(label), stepLink(st))
	}
	// Only what reads back is written: one recipe that can't be read hides
	// them all. The one it replaces goes once that's known, not before.
	if _, err := parseRecipe(path, []byte(b.String())); err != nil {
		return err
	}
	if replaced != "" {
		if err := RemoveRecipe(replaced); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(RecipesDir(), 0o755); err != nil {
		return err
	}
	return writeWhole(path, []byte(b.String()))
}

// RemoveRecipe deletes the recipe called name.
func RemoveRecipe(name string) error {
	for _, f := range recipeFiles() {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if r, err := parseRecipe(f, data); err == nil && strings.EqualFold(r.Name, name) {
			return os.Remove(f)
		}
	}
	return fmt.Errorf("you don't have a recipe called %q", name)
}
