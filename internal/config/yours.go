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

	"go.yaml.in/yaml/v3"
)

// Your packs are files in packs/local, in the same format as the built-in
// ones. They're always loaded: adding or removing one is creating or
// deleting its file.

// PackPath is where your pack called name lives.
func PackPath(name string) string { return filepath.Join(localDir(), name+".yaml") }

// Pack names are letters, digits and dashes, not starting with a dash.
var packNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

func checkPackName(name string) error {
	if !packNameRe.MatchString(name) {
		return fmt.Errorf("pack names are letters, digits and dashes, not starting with a dash, like work-tools (not %q)", name)
	}
	return nil
}

// Files lists your pack files, which Load reads after the installed packs.
func Files() ([]string, error) { return packFiles(localDir()) }

// packFiles lists the pack files in dir, skipping clingon's own "_" files.
func packFiles(dir string) ([]string, error) {
	var files []string
	for _, f := range yamlFiles(dir) {
		if !strings.HasPrefix(filepath.Base(f), "_") {
			files = append(files, f)
		}
	}
	return files, nil
}

// yamlFiles lists dir's .yaml and .yml files, sorted. (Not a glob: a
// folder with [ or * in its name would be read as part of the pattern.)
func yamlFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && (strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml")) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out
}

// YourPacks describes each of your packs.
func YourPacks() ([]Pack, error) {
	files, err := Files()
	if err != nil {
		return nil, err
	}
	return describePacks(files, "local")
}

// describePacks reads each pack file's name, version, description and
// contents.
func describePacks(files []string, where string) ([]Pack, error) {
	var out []Pack
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			return nil, err
		}
		key := described{f, fi.ModTime(), fi.Size()}
		describedMu.Lock()
		p, ok := describedCache[key]
		describedMu.Unlock()
		if !ok {
			if p, err = describePack(f); err != nil {
				return nil, err
			}
			describedMu.Lock()
			describedCache[key] = p
			describedMu.Unlock()
		}
		p.Where = where
		out = append(out, p)
	}
	return out, nil
}

// describedCache keeps what describePack read from each file, until the
// file changes: the menu asks about the same packs several times over.
var (
	describedMu    sync.Mutex
	describedCache = map[described]Pack{}
)

type described struct {
	path string
	mod  time.Time
	size int64
}

func describePack(f string) (Pack, error) {
	data, err := os.ReadFile(f)
	if err != nil {
		return Pack{}, err
	}
	c, err := parse(f, data)
	if err != nil {
		return Pack{Name: packName(f), Description: "has errors: run `clingon check`"}, nil
	}
	return describe(f, c), nil
}

func packName(f string) string {
	return strings.TrimSuffix(strings.TrimSuffix(filepath.Base(f), ".yaml"), ".yml")
}

func describe(f string, c *Config) Pack {
	p := Pack{Name: packName(f), Description: c.Description, Version: c.Version}
	for i := range c.Sections {
		p.Sections = append(p.Sections, c.Sections[i].Name)
		p.Commands += len(c.Sections[i].All())
	}
	return p
}

// remember notes what a pack file Load has just parsed says about itself.
func remember(fi os.FileInfo, f string, c *Config) {
	describedMu.Lock()
	describedCache[described{f, fi.ModTime(), fi.Size()}] = describe(f, c)
	describedMu.Unlock()
}

// NewPack creates a pack of your own from the starter: a working example
// section, named after the pack, with a guide to the format in comments.
func NewPack(name string) error {
	if err := checkPackName(name); err != nil {
		return err
	}
	title := sectionTitle(name)
	if pad := len("My commands") - len(title); pad > 0 {
		title += strings.Repeat(" ", pad) // keep the comments lined up
	}
	return writeNewPack(name, strings.Replace(string(Starter), "  - name: My commands ", "  - name: "+title+" ", 1))
}

// newEmptyPack creates a pack with the starter's guide but no sections.
func newEmptyPack(name string) error {
	if err := checkPackName(name); err != nil {
		return err
	}
	guide, _, ok := strings.Cut(string(Starter), "\nsections:")
	if !ok {
		return errors.New("the starter pack has no sections: line")
	}
	return writeNewPack(name, guide+"\nsections: []\n")
}

// sectionTitle turns a pack name into a section name: work-tools → Work tools.
func sectionTitle(name string) string {
	t := strings.ReplaceAll(name, "-", " ")
	return strings.ToUpper(t[:1]) + t[1:]
}

func writeNewPack(name, content string) error {
	if err := checkPackName(name); err != nil {
		return err
	}
	if _, err := os.Stat(PackPath(name)); err == nil {
		return fmt.Errorf("you already have a pack called %s", name)
	}
	if err := os.MkdirAll(localDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(PackPath(name), []byte(content), 0o644)
}

// DeletePack removes your pack called name.
func DeletePack(name string) error {
	if err := checkPackName(name); err != nil {
		return err
	}
	if _, err := os.Stat(PackPath(name)); err != nil {
		return fmt.Errorf("you don't have a pack called %s", name)
	}
	return os.Remove(PackPath(name))
}

// SectionNames lists the sections in your pack called name.
func SectionNames(name string) []string {
	data, err := os.ReadFile(PackPath(name))
	if err != nil {
		return nil
	}
	c, err := parse(name, data)
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range c.Sections {
		out = append(out, s.Name)
	}
	return out
}

// YourCommand is a command in one of your packs.
type YourCommand struct {
	Pack, Section, Name, Cmd string
}

// YourCommands lists every command in your packs.
func YourCommands() ([]YourCommand, error) {
	files, err := Files()
	if err != nil {
		return nil, err
	}
	var out []YourCommand
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		c, err := parse(f, data)
		if err != nil {
			continue
		}
		pack := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(f), ".yaml"), ".yml")
		for i := range c.Sections {
			for _, cmd := range c.Sections[i].All() {
				out = append(out, YourCommand{pack, c.Sections[i].Name, cmd.Name, cmd.Cmd})
			}
		}
	}
	return out, nil
}

var placeholderRe = regexp.MustCompile(`<([A-Za-z_][A-Za-z0-9_-]*)>`)

// AddCommand adds a command to a section of your pack, creating either if
// needed. Each <placeholder> in cmd becomes a typed-in arg. Comments and
// layout in the file are kept.
func AddCommand(pack, section, name, cmd, desc string) error {
	if err := checkPackName(pack); err != nil {
		return err
	}
	created := false
	if _, err := os.Stat(PackPath(pack)); errors.Is(err, os.ErrNotExist) {
		if err := newEmptyPack(pack); err != nil {
			return err
		}
		created = true
	}
	err := func() error {
		doc, err := readDoc(PackPath(pack))
		if err != nil {
			return err
		}
		if err := addCommand(findSection(doc, section, true), pack, section, name, cmd, desc); err != nil {
			return err
		}
		return writeDoc(PackPath(pack), doc)
	}()
	if err != nil && created {
		_ = os.Remove(PackPath(pack)) // don't leave an empty pack behind
	}
	return err
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
			return fmt.Errorf("%s › %s already has a command called %q", pack, section, name)
		}
	}
	c := &yaml.Node{Kind: yaml.MappingNode}
	mapSet(c, "name", str(name))
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

// RemoveCommand removes a command from your pack, and its section if that
// leaves it empty.
func RemoveCommand(pack, section, name string) error {
	if err := checkPackName(pack); err != nil {
		return err
	}
	doc, err := readDoc(PackPath(pack))
	if err != nil {
		return fmt.Errorf("you don't have a pack called %s", pack)
	}
	sec := findSection(doc, section, false)
	if sec == nil {
		return fmt.Errorf("%s has no section called %q", pack, section)
	}
	removed := removeNamed(mapGet(sec, "commands"), name)
	if groups := mapGet(sec, "groups"); groups != nil {
		for _, g := range groups.Content {
			removed = removeNamed(mapGet(g, "commands"), name) || removed
		}
	}
	if !removed {
		return fmt.Errorf("%s › %s has no command called %q", pack, section, name)
	}
	if groups := mapGet(sec, "groups"); groups != nil { // a group with nothing left goes
		kept := groups.Content[:0]
		for _, g := range groups.Content {
			if !empty(mapGet(g, "commands")) {
				kept = append(kept, g)
			}
		}
		groups.Content = kept
	}
	if empty(mapGet(sec, "commands")) && empty(mapGet(sec, "groups")) {
		removeNamed(mapGet(doc.Content[0], "sections"), section)
	}
	return writeDoc(PackPath(pack), doc)
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
	c, err := parse(path, buf.Bytes())
	if err != nil {
		return err
	}
	if errs := c.Validate(); len(errs) > 0 {
		return errors.Join(errs...)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func findSection(doc *yaml.Node, name string, create bool) *yaml.Node {
	root := doc.Content[0]
	secs := mapGet(root, "sections")
	if secs == nil || secs.Kind != yaml.SequenceNode {
		if !create {
			return nil
		}
		secs = seqFor(root, "sections")
	}
	for _, s := range secs.Content {
		if v := mapGet(s, "name"); v != nil && v.Value == name {
			return s
		}
	}
	if !create {
		return nil
	}
	s := &yaml.Node{Kind: yaml.MappingNode}
	mapSet(s, "name", str(name))
	secs.Style, secs.Content = 0, append(secs.Content, s)
	return s
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

func removeNamed(seq *yaml.Node, name string) bool {
	if seq == nil {
		return false
	}
	for i, n := range seq.Content {
		if v := mapGet(n, "name"); v != nil && v.Value == name {
			seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
			return true
		}
	}
	return false
}

func empty(seq *yaml.Node) bool { return seq == nil || len(seq.Content) == 0 }
