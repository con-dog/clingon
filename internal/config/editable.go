package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// EditablePack is one of your packs as clingon's pack form edits it: plain
// data, written back to the pack's Markdown files by Save, in the format a
// pack is read in (see markdown.go). Comments in the files aren't kept.
type EditablePack struct {
	Folder      string // the pack's folder name: what `clingon pack` calls it
	Section     string // its section, as section: names it: ai, code, …, other
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
// one gets a question from its name, one no longer used goes, and the rest
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
		a := Arg{Name: name, Prompt: sectionTitle(name), Optional: true}
		if IsSecretName(name) {
			a.Type, a.Quote = "secret", true
		}
		args = append(args, a)
	}
	c.Args = args
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

// Save writes the pack's files if they check out, removing any module's file
// that's gone.
func (p *EditablePack) Save() error {
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
	fm = append(fm, "section: "+p.Section)
	if p.Logo != "" {
		fm = append(fm, "logo: "+p.Logo)
	}
	if p.Color != "" {
		fm = append(fm, "colour: "+p.Color)
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
		if c.Risk != "" {
			b.WriteString("> ")
		}
		b.WriteString(c.Desc + "\n")
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
			on := strings.Fields(a.Default)
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
