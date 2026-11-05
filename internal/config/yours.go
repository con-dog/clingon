package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Your packs are folders in packs/local, in the same format as the built-in
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

// LoosePackFiles lists YAML files sitting directly in packs/local or packs/remote,
// which aren't read: a pack is a folder.
func LoosePackFiles() []string {
	return append(yamlFiles(localDir()), yamlFiles(remoteDir())...)
}

// skippedName is a file or folder clingon leaves alone among packs: its own,
// and hidden ones like an editor's.
func skippedName(name string) bool {
	return strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")
}

// packDirs lists the pack folders in root. Your own may be links, say to your
// dotfiles.
func packDirs(root string) []string {
	entries, _ := os.ReadDir(root)
	var out []string
	for _, e := range entries {
		if skippedName(e.Name()) {
			continue
		}
		path := filepath.Join(root, e.Name())
		if fi, err := os.Stat(path); err == nil && fi.IsDir() {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
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
// _pack.yaml or a submenu.
func isPackFile(path string) bool {
	base := filepath.Base(path)
	return base == PackInfoFile || !skippedName(base)
}

// moduleFiles lists the submenu files in a pack's folder.
func moduleFiles(dir string) []string {
	var out []string
	for _, f := range yamlFiles(dir) {
		if !skippedName(filepath.Base(f)) {
			out = append(out, f)
		}
	}
	return out
}

// ModuleFiles lists the submenu files in a pack's folder.
func ModuleFiles(dir string) []string { return moduleFiles(dir) }

func moduleSlug(file string) string {
	return strings.TrimSuffix(strings.TrimSuffix(filepath.Base(file), ".yaml"), ".yml")
}

// readPackDir reads the files of a pack's folder.
func readPackDir(dir string) ([]parsedFile, error) {
	var parts []parsedFile
	for _, f := range yamlFiles(dir) {
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

// YourPacks describes each of your packs.
func YourPacks() ([]Pack, error) { return describePacks(LocalPacks(), "local") }

// describePacks describes each pack folder, leaving out any with no submenus.
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
	for _, f := range yamlFiles(dir) {
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
			p.Description, p.Requires, p.Default = part.info.Description, part.info.Requires, part.info.Default
			if part.info.Name != "" {
				name = part.info.Name
			}
			continue
		}
		p.Commands += len(part.group.Commands)
		p.Modules = append(p.Modules, Module{Slug: moduleSlug(f), Name: part.group.Name, Description: part.group.Description, Commands: len(part.group.Commands)})
	}
	p.Sections = []string{name}
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
// submenu, and a _pack.yaml called after the pack, with a guide to the format in
// comments.
func NewPack(name string) error {
	if err := checkPackName(name); err != nil {
		return err
	}
	dir := PackPath(name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("you already have a pack called local/%s", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	title := sectionTitle(name)
	if pad := len("My commands") - len(title); pad > 0 {
		title += strings.Repeat(" ", pad) // keep the comments lined up
	}
	info := strings.Replace(string(StarterInfo), "name: My commands ", "name: "+title+" ", 1)
	if err := os.WriteFile(filepath.Join(dir, PackInfoFile), []byte(info), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "examples.yaml"), Starter, 0o644)
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
		return fmt.Errorf("you don't have a pack called local/%s", name)
	}
	return removeRef(localDir(), name)
}

// SectionNames lists the submenus in your pack called name.
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

// AddCommand adds a command to a submenu of your pack, creating either if
// needed. Each <placeholder> in cmd becomes a typed-in arg. Comments and
// layout in the file are kept.
func AddCommand(pack, section, name, cmd, desc string) error {
	if err := checkPackName(pack); err != nil {
		return err
	}
	slug := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(section), "-"), "-")
	if slug == "" {
		return fmt.Errorf("a section's name needs a letter or digit in it (not %q)", section)
	}
	dir, path := PackPath(pack), filepath.Join(PackPath(pack), slug+".yaml")
	packIsNew, fileIsNew := false, false
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		packIsNew = true
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		fileIsNew = true
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		guide, _, ok := strings.Cut(string(Starter), "\nname:")
		if !ok {
			return errors.New("the starter pack has no name: line")
		}
		doc := guide + "\nname: " + yamlString(section) + "\ncommands: []\n"
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			return err
		}
	}
	err := func() error {
		doc, err := readDoc(path)
		if err != nil {
			return err
		}
		if err := addCommand(doc.Content[0], pack, section, name, cmd, desc); err != nil {
			return err
		}
		return writeDoc(path, doc)
	}()
	if err != nil {
		// Don't leave an empty pack, or an empty submenu, behind.
		if packIsNew {
			_ = os.RemoveAll(dir)
		} else if fileIsNew {
			_ = os.Remove(path)
		}
	}
	return err
}

// yamlString is s as a YAML value.
func yamlString(s string) string {
	b, err := yaml.Marshal(s)
	if err != nil {
		return `""`
	}
	return strings.TrimSpace(string(b))
}

// addCommand appends a command to section node sec. Each <placeholder> in
// cmd becomes a typed-in arg.
func addCommand(sec *yaml.Node, pack, section, name, cmd, desc string) error {
	if name == "" || cmd == "" {
		return errors.New("a command needs a name and the command to run")
	}
	cmds := seqFor(sec, "commands")
	for _, c := range cmds.Content {
		if v := mapGet(c, "name"); v != nil && v.Value == name {
			return fmt.Errorf("local/%s › %s already has a command called %q", pack, section, name)
		}
	}
	c := &yaml.Node{Kind: yaml.MappingNode}
	mapSet(c, "name", str(name))
	mapSet(c, "id", str(newCommandID(section, "", name)))
	mapSet(c, "cmd", str(cmd))
	if desc != "" {
		mapSet(c, "desc", str(desc))
	}
	var args []*yaml.Node
	seen := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(cmd, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			a := &yaml.Node{Kind: yaml.MappingNode}
			mapSet(a, "name", str(m[1]))
			args = append(args, a)
		}
	}
	if len(args) > 0 {
		mapSet(c, "args", &yaml.Node{Kind: yaml.SequenceNode, Content: args})
	}
	cmds.Style, cmds.Content = 0, append(cmds.Content, c)
	return nil
}

// ---- yaml.Node helpers: editing in place keeps the user's comments ----

func readDoc(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected a mapping at the top", path)
	}
	return &doc, nil
}

// writeDoc saves doc, but only if the result still loads and validates.
func writeDoc(path string, doc *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	part, err := parsePackFile(path, buf.Bytes())
	if err != nil {
		return err
	}
	c := assemble(filepath.Base(filepath.Dir(path)), []parsedFile{part})
	if errs := c.Validate(); len(errs) > 0 {
		return errors.Join(errs...)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// seqFor is the list under key in mapping m, made (or put in place of an
// empty `key:`) if there isn't one.
func seqFor(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if v := m.Content[i+1]; v.Kind == yaml.SequenceNode {
				return v
			}
			m.Content[i+1] = &yaml.Node{Kind: yaml.SequenceNode} // `key:` with nothing after it
			return m.Content[i+1]
		}
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	mapSet(m, key, seq)
	return seq
}

func mapSet(m *yaml.Node, key string, v *yaml.Node) {
	m.Content = append(m.Content, str(key), v)
}

func str(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
