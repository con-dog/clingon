package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Your packs are folders in packs/mine, in the same format as the built-in
// ones. They're always loaded: adding or removing one is creating or deleting
// its folder.

// PackPath is where your pack called name lives: its folder, or with
// work-tools/deploy, that module's file.
func PackPath(name string) string { return refPath(localDir(), name) }

// Pack names are letters, digits and dashes, not starting with a dash.
var packNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

func checkPackName(name string) error {
	if !packNameRe.MatchString(name) {
		return fmt.Errorf("pack names are letters, digits and dashes, not starting with a dash, like work-tools (not %q)", name)
	}
	return nil
}

// checkRef checks a pack, or a pack and module like aws/ecs.
func checkRef(name string) error {
	pack, module := SplitModule(name)
	if err := checkPackName(pack); err != nil {
		return err
	}
	if strings.Contains(name, "/") {
		return checkPackName(module)
	}
	return nil
}

// LocalPacks lists your pack folders, which Load reads after the installed packs.
func LocalPacks() []string { return packDirs(localDir()) }

// LoosePackFiles lists Markdown files sitting directly in packs/mine or packs/remote,
// which aren't read: a pack is a folder.
func LoosePackFiles() []string {
	return append(mdFiles(localDir()), mdFiles(remoteDir())...)
}

// skippedName is a file or folder clingon leaves alone among packs: its own,
// and hidden ones like an editor's.
func skippedName(name string) bool {
	return strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

func isMarkdown(name string) bool { return strings.HasSuffix(name, ".md") }

// mdFiles lists dir's .md files, sorted: a pack's files.
func mdFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && isMarkdown(n) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out
}

// yamlFiles lists dir's .yaml and .yml files, sorted. (Not a glob: a
// folder with [ or * in its name would be read as part of the pattern.)
func yamlFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && isYAML(n) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out
}

// isPackFile says whether a file in a pack's folder is part of the pack:
// _pack.md or a module.
func isPackFile(path string) bool {
	base := filepath.Base(path)
	return base == PackInfoFile || !skippedName(base)
}

// moduleFiles lists the module files in a pack's folder.
func moduleFiles(dir string) []string {
	var out []string
	for _, f := range mdFiles(dir) {
		if !skippedName(filepath.Base(f)) {
			out = append(out, f)
		}
	}
	return out
}

// ModuleFiles lists the module files in a pack's folder.
func ModuleFiles(dir string) []string { return moduleFiles(dir) }

func moduleSlug(file string) string {
	return strings.TrimSuffix(filepath.Base(file), ".md")
}

// readPackDir reads the files of a pack's folder.
func readPackDir(dir string) ([]parsedFile, error) {
	var parts []parsedFile
	for _, f := range mdFiles(dir) {
		if !isPackFile(f) {
			continue
		}
		fi, err := os.Stat(f)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		part, err := parsePackFile(f, data)
		if err != nil {
			return nil, err
		}
		remember(fi, f, part) // so describing the packs later needn't read them again
		parts = append(parts, part)
	}
	return parts, nil
}

// YourSections lists the sections of your own that your packs are in: not
// ones clingon suggests, sorted.
func YourSections() []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range LocalPacks() {
		part, err := describeFile(filepath.Join(dir, PackInfoFile))
		if err != nil || part.info == nil {
			continue
		}
		if s := part.info.Section; !seen[s] && sectionSlugs[s] == "" {
			seen[s], out = true, append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// YourPacks describes each of your packs.
func YourPacks() ([]Pack, error) { return describePacks(LocalPacks(), "mine") }

// describePacks describes each pack folder, leaving out any with no modules.
func describePacks(dirs []string, where string) ([]Pack, error) {
	var out []Pack
	for _, dir := range dirs {
		p := describePack(dir)
		p.Where = where
		if len(p.Modules) > 0 {
			out = append(out, p)
		}
	}
	return out, nil
}

// describedCache keeps what each file of a pack says about itself, until the
// file changes: the menu asks about the same packs several times over.
var (
	describedMu    sync.Mutex
	describedCache = map[described]parsedFile{}
)

type described struct {
	path string
	mod  time.Time
	size int64
}

func describePack(dir string) Pack {
	p := Pack{Name: filepath.Base(dir)}
	name, broken := sectionTitle(p.Name), false
	for _, f := range mdFiles(dir) {
		if !isPackFile(f) {
			continue
		}
		part, err := describeFile(f)
		if err != nil {
			broken = true
			if filepath.Base(f) != PackInfoFile {
				p.Modules = append(p.Modules, Module{Slug: moduleSlug(f), Name: sectionTitle(moduleSlug(f))})
			}
			continue
		}
		if part.info != nil {
			p.Description, p.Requires, p.Default = part.info.Description, part.info.Requires, defaultPacks[p.Name]
			if part.info.Name != "" {
				name = part.info.Name
			}
			continue
		}
		p.Commands += len(part.group.Commands)
		p.Modules = append(p.Modules, Module{Slug: moduleSlug(f), Name: part.group.Name, Description: part.group.Description, Commands: len(part.group.Commands)})
	}
	p.Sections = []string{name}
	if !slices.Contains(mdFiles(dir), filepath.Join(dir, PackInfoFile)) {
		broken = true
	}
	if broken {
		p.Description = "has errors: run `clingon check`"
	}
	return p
}

// describeFile is what a file of a pack says, read once until it changes.
func describeFile(f string) (parsedFile, error) {
	fi, err := os.Stat(f)
	if err != nil {
		return parsedFile{}, err
	}
	key := described{f, fi.ModTime(), fi.Size()}
	describedMu.Lock()
	part, ok := describedCache[key]
	describedMu.Unlock()
	if ok {
		return part, nil
	}
	data, err := os.ReadFile(f)
	if err != nil {
		return parsedFile{}, err
	}
	if part, err = parsePackFile(f, data); err != nil {
		return parsedFile{}, err
	}
	remember(fi, f, part)
	return part, nil
}

// remember notes what a pack file has just been parsed to.
func remember(fi os.FileInfo, f string, part parsedFile) {
	describedMu.Lock()
	describedCache[described{f, fi.ModTime(), fi.Size()}] = part
	describedMu.Unlock()
}

// NewPack creates a pack of your own from the starter: a working example
// module, and a _pack.md called after the pack, with the default logo.
func NewPack(name string) error { return NewPackWithLogo(name, "", "") }

// NewPackWithLogo is NewPack with one of clingon's logos ("" for the default)
// in one of PackColors ("" for the logo's own).
func NewPackWithLogo(name, logo, color string) error {
	if logo == "" {
		logo = DefaultPackLogo
	}
	if _, ok := Logos[logo]; !ok {
		return fmt.Errorf("there's no logo called %s: try one of %s", logo, strings.Join(PackLogos, ", "))
	}
	if color != "" && colorHex(color) == "" {
		return fmt.Errorf("there's no colour called %s: try one of %s", color, colorNames())
	}
	if err := checkPackName(name); err != nil {
		return err
	}
	dir := PackPath(name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("you already have a pack called mine/%s", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	info := strings.Replace(string(StarterInfo), "# My commands", "# "+sectionTitle(name), 1)
	logoLine := "logo: " + logo
	if color != "" {
		logoLine += "\ncolour: " + color
	}
	info = strings.Replace(info, "logo: "+DefaultPackLogo, logoLine, 1)
	if err := os.WriteFile(filepath.Join(dir, PackInfoFile), []byte(info), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "examples.md"), Starter, 0o644)
}

// sectionTitle turns a pack or file name into a menu name: work-tools → Work tools.
func sectionTitle(name string) string {
	t := strings.ReplaceAll(name, "-", " ")
	r, size := utf8.DecodeRuneInString(t)
	if size == 0 {
		return t
	}
	return string(unicode.ToUpper(r)) + t[size:]
}

// DeletePack removes your pack called name, or with work-tools/deploy, that module.
func DeletePack(name string) error {
	if err := checkRef(name); err != nil {
		return err
	}
	if !IsLocal(name) {
		return fmt.Errorf("you don't have a pack called mine/%s", name)
	}
	return removeRef(localDir(), name)
}

// SectionNames lists the modules in your pack called name.
func SectionNames(name string) []string {
	if checkPackName(name) != nil {
		return nil
	}
	var out []string
	for _, m := range describePack(PackPath(name)).Modules {
		out = append(out, m.Name)
	}
	return out
}

// YourCommand is a command in one of your packs.
type YourCommand struct {
	Pack, Section, Name, Cmd string
}

// YourCommands lists every command in your packs.
func YourCommands() ([]YourCommand, error) {
	var out []YourCommand
	for _, dir := range LocalPacks() {
		for _, f := range moduleFiles(dir) {
			part, err := describeFile(f)
			if err != nil || part.group == nil {
				continue
			}
			for _, c := range part.group.Commands {
				out = append(out, YourCommand{filepath.Base(dir), part.group.Name, c.Name, c.Cmd})
			}
		}
	}
	return out, nil
}

var placeholderRe = regexp.MustCompile(`<([A-Za-z_][A-Za-z0-9_-]*)>`)

// AddCommand adds a command to a module of your pack, creating either if
// needed. Each <placeholder> in cmd becomes a typed-in blank. What's in the
// file already is kept as it is: the command goes on the end.
func AddCommand(pack, section, name, cmd, desc string) error {
	if err := checkPackName(pack); err != nil {
		return err
	}
	if name == "" || cmd == "" {
		return errors.New("a command needs a name and the command to run")
	}
	slug := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(section), "-"), "-")
	if slug == "" {
		return fmt.Errorf("a section's name needs a letter or digit in it (not %q)", section)
	}
	dir, path := PackPath(pack), filepath.Join(PackPath(pack), slug+".md")
	have, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		have = []byte("# " + section + "\n")
	case err != nil:
		return err
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(string(have), "\n") + "\n\n## " + name + "\n")
	if desc != "" {
		b.WriteString(desc + "\n")
	}
	b.WriteString("\n```sh\n" + cmd + "\n```\n")
	seen := map[string]bool{}
	var blanks []string
	for _, m := range placeholderRe.FindAllStringSubmatch(cmd, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			blanks = append(blanks, "- `<"+m[1]+">` "+sectionTitle(m[1]))
		}
	}
	if len(blanks) > 0 {
		b.WriteString("\n" + strings.Join(blanks, "\n") + "\n")
	}
	doc := []byte(b.String())
	part, err := parsePackFile(path, doc)
	if err != nil {
		return err
	}
	n := 0
	for _, c := range part.group.Commands {
		if c.Name == name {
			n++
		}
	}
	if n > 1 {
		return fmt.Errorf("mine/%s › %s already has a command called %q", pack, part.group.Name, name)
	}
	if errs := assemble(pack, []parsedFile{part}).Validate(); len(errs) > 0 {
		return errors.Join(errs...)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if info := filepath.Join(dir, PackInfoFile); !exists(info) { // a new pack
		if err := os.WriteFile(info, newPackInfo(sectionTitle(pack)), 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(path, doc, 0o644)
}
