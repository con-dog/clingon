package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// EditablePack is one of your packs as clingon's pack form edits it: plain
// data, written back to the pack's Markdown files by Save, in the format a
// pack is read in (see markdown.go). Comments in the files aren't kept.
type EditablePack struct {
	Folder      string // the pack's folder name: what `clingon pack` calls it
	Section     string // where in the menu, as menu: names it: ai, code, …, other
	Title       string // its name in the menu
	Description string
	Logo        string   // one of Logos
	Color       string   // one of PackColors by name, or "" for the logo's own
	Needs       []string // programs every command needs
	Files       []EditableFile

	dir string // where it was loaded from
}

// EditableFile is a module: its file's name (without .md) and its commands.
type EditableFile struct {
	Slug  string
	Group Group
}

// Dir is where the pack's folder is.
func (p *EditablePack) Dir() string { return filepath.Join(localDir(), p.Folder) }

// LoadEditable reads your pack called name for the pack form.
func LoadEditable(name string) (*EditablePack, error) {
	if err := checkPackName(name); err != nil {
		return nil, err
	}
	dir := packDirIn(localDir(), name)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("you don't have a pack called mine/%s", name)
	}
	p := &EditablePack{Folder: name, Section: "other", Title: sectionTitle(name), dir: dir, Logo: DefaultPackLogo}
	for _, f := range mdFiles(dir) {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		part, err := parsePackFile(f, data)
		if err != nil {
			return nil, err
		}
		if part.info != nil {
			p.Title, p.Description, p.Logo, p.Needs = part.info.Name, part.info.Description, part.info.Logo, part.info.Requires
			p.Section = part.info.Section
			for _, c := range PackColors {
				if c.Hex == part.info.Color {
					p.Color = c.Name
				}
			}
			continue
		}
		p.Files = append(p.Files, EditableFile{Slug: moduleSlug(f), Group: *part.group})
	}
	return p, nil
}

// SyncBlanks makes a command's blanks match the <blanks> in its command: a new
// one starts with its question empty (the pack form hints at one, and
// without one it's asked by its name), one no longer used goes, and the rest
// keep what they had, in the order the command has them.
func SyncBlanks(c *Command) {
	var args []Arg
	seen := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(c.Cmd, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if i := slices.IndexFunc(c.Args, func(a Arg) bool { return a.Name == name }); i >= 0 {
			args = append(args, c.Args[i])
			continue
		}
		a := Arg{Name: name, Optional: true}
		if IsSecretName(name) {
			a.Type, a.Quote = "secret", true
		}
		args = append(args, a)
	}
	c.Args = args
}

// blankNameRe is what a blank can be called, between its < >.
var blankNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// RenameBlank renames the command's blank old to name: in the command, as
// <name>, and as its own, so what it asks and offers stays with it.
func RenameBlank(c *Command, old, name string) error {
	name = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(name), "<"), ">")
	switch {
	case name == old:
		return nil
	case !blankNameRe.MatchString(name):
		return fmt.Errorf("a blank's name starts with a letter or _, then letters, digits, _ and - (not %q)", name)
	case slices.ContainsFunc(c.Args, func(a Arg) bool { return a.Name == name }):
		return fmt.Errorf("the command already has a blank called <%s>", name)
	}
	c.Cmd = strings.ReplaceAll(c.Cmd, "<"+old+">", "<"+name+">")
	for i := range c.Args {
		if c.Args[i].Name == old {
			c.Args[i].Name = name
		}
		if c.Args[i].IconFrom == old {
			c.Args[i].IconFrom = name
		}
	}
	return nil
}

// NewSlug is a file name for a new module called title, not already taken.
func (p *EditablePack) NewSlug(title string) string {
	base := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if base == "" {
		base = "commands"
	}
	slug := base
	for n := 2; slices.ContainsFunc(p.Files, func(f EditableFile) bool { return f.Slug == slug }); n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	return slug
}

// Markdown is each of the pack's files as Save writes them, by file name.
func (p *EditablePack) Markdown() map[string]string {
	out := map[string]string{PackInfoFile: packMarkdown(p)}
	for _, f := range p.Files {
		out[f.Slug+".md"] = moduleMarkdown(&f.Group)
	}
	return out
}

// Check finds what Save would refuse: each file is read back the way clingon
// reads packs, and the whole is checked as `clingon check` checks it.
func (p *EditablePack) Check() error {
	return p.standIns().check()
}

// standIns is a copy of the pack with what's still empty filled in by
// stand-ins, so a part not yet filled in doesn't stop a change elsewhere:
// checkFilled says what's empty, when it's saved.
func (p *EditablePack) standIns() *EditablePack {
	data, err := json.Marshal(p)
	c := &EditablePack{}
	if err != nil || json.Unmarshal(data, c) != nil {
		return p
	}
	c.dir = p.dir
	if strings.TrimSpace(c.Title) == "" {
		c.Title = "Unnamed"
	}
	for i := range c.Files {
		g := &c.Files[i].Group
		if strings.TrimSpace(g.Name) == "" {
			g.Name = fmt.Sprintf("Unnamed module %d", i+1)
		}
		for j := range g.Commands {
			cmd := &g.Commands[j]
			if strings.TrimSpace(cmd.Name) == "" {
				cmd.Name = fmt.Sprintf("Unnamed command %d", j+1)
			}
			if strings.TrimSpace(cmd.Cmd) == "" {
				cmd.Cmd = "true"
			}
			for a := range cmd.Args {
				for o := range cmd.Args[a].Options {
					if strings.TrimSpace(cmd.Args[a].Options[o].Value) == "" {
						cmd.Args[a].Options[o].Value = fmt.Sprintf("option-%d", o+1)
					}
				}
			}
		}
	}
	return c
}

func (p *EditablePack) check() error {
	var parts []parsedFile
	for name, text := range p.Markdown() {
		part, err := parsePackFile(filepath.Join(p.Dir(), name), []byte(text))
		if err != nil {
			return err
		}
		parts = append(parts, part)
	}
	if !nameCharsRe.MatchString(p.Title) {
		return fmt.Errorf("names use only letters, digits, spaces and ( ) (not %q)", p.Title)
	}
	if len(p.Files) == 0 {
		return errors.New("a pack needs a module with a command in it")
	}
	if errs := assemble(p.Folder, parts).Validate(); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// CheckFilled finds what's still empty that has to be filled in: the names,
// each command, each option's value.
func (p *EditablePack) CheckFilled() error {
	if strings.TrimSpace(p.Title) == "" {
		return errors.New("the pack needs a name")
	}
	for _, f := range p.Files {
		g := f.Group
		if strings.TrimSpace(g.Name) == "" {
			return errors.New("every module needs a name")
		}
		for _, c := range g.Commands {
			if strings.TrimSpace(c.Name) == "" {
				return fmt.Errorf("every command in %s needs a name", g.Name)
			}
			if strings.TrimSpace(c.Cmd) == "" {
				return fmt.Errorf("%s needs its command", c.Name)
			}
			for _, a := range c.Args {
				for _, o := range a.Options {
					if strings.TrimSpace(o.Value) == "" {
						return fmt.Errorf("%s: every option of <%s> needs what it types", c.Name, a.Name)
					}
				}
			}
		}
	}
	return nil
}

// Save writes the pack's files if they check out, removing any module's file
// that's gone.
func (p *EditablePack) Save() error {
	if err := p.CheckFilled(); err != nil { // said plainly: what's still to fill in
		return err
	}
	if err := p.Check(); err != nil {
		return err
	}
	dir := p.Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	files := p.Markdown()
	for _, f := range mdFiles(dir) {
		if _, keep := files[filepath.Base(f)]; !keep {
			if err := os.Remove(f); err != nil {
				return err
			}
		}
	}
	for name, text := range files {
		path := filepath.Join(dir, name)
		if have, err := os.ReadFile(path); err == nil && string(have) == text {
			continue
		}
		if err := writeWhole(path, []byte(text)); err != nil {
			return err
		}
	}
	p.dir = dir
	return nil
}

func packMarkdown(p *EditablePack) string {
	var b strings.Builder
	var fm []string
	fm = append(fm, "menu: "+p.Section)
	if p.Logo != "" {
		fm = append(fm, "logo: "+p.Logo)
	}
	if p.Color != "" {
		fm = append(fm, "tint: "+p.Color)
	}
	if len(p.Needs) > 0 {
		fm = append(fm, "needs: "+strings.Join(p.Needs, ", "))
	}
	if len(fm) > 0 {
		b.WriteString("---\n" + strings.Join(fm, "\n") + "\n---\n")
	}
	b.WriteString("# " + p.Title + "\n")
	if p.Description != "" {
		b.WriteString(p.Description + "\n")
	}
	return b.String()
}

func moduleMarkdown(g *Group) string {
	var b strings.Builder
	var fm []string
	if len(g.Requires) > 0 {
		fm = append(fm, "needs: "+strings.Join(g.Requires, ", "))
	}
	if len(g.When) > 0 {
		fm = append(fm, "when: "+strings.Join(g.When, ", "))
	}
	if len(fm) > 0 {
		b.WriteString("---\n" + strings.Join(fm, "\n") + "\n---\n")
	}
	b.WriteString("# " + g.Name + "\n")
	if g.Description != "" {
		b.WriteString(g.Description + "\n")
	}
	for _, c := range g.Commands {
		b.WriteString("\n" + commandMarkdown(&c))
	}
	return b.String()
}

func commandMarkdown(c *Command) string {
	var b strings.Builder
	b.WriteString("## " + c.Name + "\n")
	switch c.Risk {
	case RiskWarn:
		b.WriteString("> [!WARNING]\n")
	case RiskDanger:
		b.WriteString("> [!CAUTION]\n")
	}
	if c.Desc != "" {
		desc := c.Desc
		if c.Risk != "" { // every line of it in the callout
			desc = "> " + strings.ReplaceAll(desc, "\n", "\n> ")
		}
		b.WriteString(desc + "\n")
	}
	fence := "```sh"
	if len(c.Requires) > 0 {
		fence += " needs=" + strings.Join(c.Requires, ",")
	}
	if len(c.When) > 0 {
		fence += " when=" + strings.Join(c.When, ",")
	}
	b.WriteString("\n" + fence + "\n" + c.Cmd + "\n```\n")
	if len(c.Args) > 0 {
		b.WriteString("\n")
	}
	for _, a := range c.Args {
		b.WriteString("- `<" + a.Name + ">`")
		if a.Prompt != "" {
			b.WriteString(" " + a.Prompt)
		}
		b.WriteString("\n")
		switch a.Kind() {
		case "choose":
			for _, o := range a.Options {
				b.WriteString(choiceMarkdown("(", ")", o.Value == a.Default, o))
			}
		case "flags":
			on := a.Ticked(a.Default)
			for _, o := range a.Options {
				b.WriteString(choiceMarkdown("[", "]", slices.Contains(on, o.Value), o))
			}
		default:
			if a.Default != "" {
				b.WriteString("  - (x) `" + a.Default + "`\n")
			}
		}
	}
	return b.String()
}

func choiceMarkdown(open, close string, on bool, o Option) string {
	mark := " "
	if on {
		mark = "x"
	}
	s := "  - " + open + mark + close + " `" + o.Value + "`"
	if o.Label != "" {
		s += " " + o.Label
	}
	return s + "\n"
}
