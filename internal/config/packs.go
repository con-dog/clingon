package config

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

// Built-in packs: a folder each, as they're installed. (all: because embed
// otherwise leaves out files starting with _.)
//
//go:embed all:packs
var packFS embed.FS

// Pack is a folder of submenus: one menu item, whose _pack.yaml says how it
// looks and whose other files are a submenu each.
type Pack struct {
	Name        string
	Where       string // "remote" (from clingon, installed or not) or "mine" (yours)
	Description string
	Requires    []string
	AnyOf       []string // for a pack with no tools of its own whose groups each need one: any will do
	Default     bool
	Sections    []string
	Commands    int
	Modules     []Module
}

// Module is a submenu's file in a pack: aws/ecs is the module ecs of aws.
type Module struct {
	Slug        string // its file's name, as it's installed by
	Name        string // what the menu calls it
	Description string
	Commands    int
}

// Packs lists every built-in pack, by name: the catalog (catalog.go),
// worked out from the packs when they change, so nothing's parsed here.
func Packs() ([]Pack, error) {
	out := slices.Clone(catalog)
	for i := range out {
		out[i].Where = "remote"
	}
	return out, nil
}

// Ref is a pack's full name, for scripts and completion: remote/NAME or mine/NAME.
func (p Pack) Ref() string { return p.Where + "/" + p.Name }

// Shown is what a pack is called to people: see RemoteRef. Your own stay mine/NAME.
func (p Pack) Shown() string {
	if p.Where == "remote" {
		return RemoteRef(p.Name)
	}
	return p.Ref()
}

// RemoteRef is what an installed pack is called to people: its name, or
// remote/NAME if you have a mine/NAME too, when the name alone wouldn't do.
func RemoteRef(name string) string {
	if pack, _ := SplitModule(name); IsLocal(pack) {
		return "remote/" + name
	}
	return name
}

// SplitModule splits aws/ecs into the pack and the module; a pack alone has no module.
func SplitModule(name string) (pack, module string) {
	pack, module, _ = strings.Cut(name, "/")
	return pack, module
}

// builtinFiles lists a built-in pack's files, _pack.yaml first.
func builtinFiles(pack string) []string {
	entries, err := packFS.ReadDir("packs/" + pack)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// builtinFile is a file of a built-in pack, as clingon ships it.
func builtinFile(pack, file string) ([]byte, error) {
	return packFS.ReadFile("packs/" + pack + "/" + file)
}

func loadPack(name string) (*Config, error) {
	files := builtinFiles(name)
	if files == nil {
		return nil, fmt.Errorf("no pack called %s", name)
	}
	var parts []parsedFile
	for _, f := range files {
		data, err := builtinFile(name, f)
		if err != nil {
			return nil, err
		}
		part, err := parsePackFile("pack "+name+"/"+f, data)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return assemble(name, parts), nil
}

// The clingon folder holds:
//
//	packs/_available.yaml  every pack you can install (written by clingon)
//	packs/remote/          installed packs, a folder each: installing copies
//	                       one in, removing deletes it (rather like node_modules)
//	packs/mine/           your own packs, a folder each
//	recipes.yaml           your saved recipes
//	config/                what clingon keeps for itself
//
// Files starting with "_" are clingon's own and never submenus; pack names
// can't start with "_".
func packsDir() string  { return filepath.Join(Dir(), "packs") }
func remoteDir() string { return filepath.Join(packsDir(), "remote") }
func localDir() string  { return filepath.Join(packsDir(), "mine") }

// remotePath is where an installed pack's folder is, or with aws/ecs, the file
// of that module.
func remotePath(name string) string { return refPath(remoteDir(), name) }

func refPath(root, name string) string {
	pack, module := SplitModule(name)
	if module == "" {
		return filepath.Join(root, pack)
	}
	return filepath.Join(root, pack, module+".yaml")
}

// availableFile lists the packs that can be installed.
func availableFile() string { return filepath.Join(packsDir(), "_available.yaml") }

type availableList struct {
	Available []availablePack `yaml:"available"`
}

type availablePack struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Modules     []string `yaml:"modules,flow"`
	Commands    int      `yaml:"commands"`
}

// WriteAvailable writes _available.yaml, the packs you can install, when
// it has changed. For now they're the packs built into clingon; later they
// can come from the published release.
func WriteAvailable() error {
	all, err := Packs()
	if err != nil {
		return err
	}
	var l availableList
	for _, p := range all {
		var modules []string
		for _, m := range p.Modules {
			modules = append(modules, m.Slug)
		}
		l.Available = append(l.Available, availablePack{p.Name, p.Description, modules, p.Commands})
	}
	var buf bytes.Buffer
	buf.WriteString("# Packs you can add, written by clingon. Don't edit: it's rewritten.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(l); err != nil {
		return err
	}
	if have, err := os.ReadFile(availableFile()); err == nil && bytes.Equal(have, buf.Bytes()) {
		return nil
	}
	if err := os.MkdirAll(packsDir(), 0o755); err != nil {
		return err
	}
	return writeWhole(availableFile(), buf.Bytes())
}

// writeWhole writes a file so it's never seen half-written: a temp file,
// then renamed over it.
func writeWhole(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // gone already once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

const remoteHeader = "# Added by clingon. `clingon pack update` overwrites this file: put\n# your own commands in packs/mine/ instead.\n"

// Install copies a pack from the catalog into remote/: all of it, or with
// aws/ecs, that module and the file saying how the pack looks. Anything of
// it already there is replaced.
func Install(name string) error {
	if err := checkRef(name); err != nil {
		return err
	}
	if !InCatalog(name) {
		return fmt.Errorf("no pack called %s (see `clingon pack available`)", RemoteRef(name))
	}
	pack, module := SplitModule(name)
	files := builtinFiles(pack)
	if module != "" {
		files = []string{PackInfoFile, module + ".yaml"}
	}
	for _, f := range files {
		if err := installFile(pack, f); err != nil {
			return err
		}
	}
	return nil
}

func installFile(pack, file string) error {
	data, err := builtinFile(pack, file)
	if err != nil {
		return err
	}
	dir := filepath.Join(remoteDir(), pack)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeWhole(filepath.Join(dir, file), append([]byte(remoteHeader), data...))
}

// Uninstall deletes an installed pack from remote/, or with aws/ecs, that
// module: the pack goes too once it has no modules left.
func Uninstall(name string) error {
	if err := checkRef(name); err != nil {
		return err
	}
	if !IsInstalled(name) {
		return fmt.Errorf("%s isn't added", RemoteRef(name))
	}
	return removeRef(remoteDir(), name)
}

// removeRef removes a pack's folder under root, or a module's file and then the
// folder if that was its last.
func removeRef(root, name string) error {
	pack, module := SplitModule(name)
	if module != "" {
		if err := os.Remove(refPath(root, name)); err != nil {
			return err
		}
		if len(moduleFiles(filepath.Join(root, pack))) > 0 {
			return nil
		}
	}
	return os.RemoveAll(filepath.Join(root, pack))
}

// EnsureDefaults installs the default packs the first time clingon runs,
// when there's no remote/ folder yet. After that the folder is the user's
// to manage, even when empty.
func EnsureDefaults() error {
	if _, err := os.Stat(remoteDir()); err == nil {
		return nil
	}
	all, err := Packs()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(remoteDir(), 0o755); err != nil {
		return err
	}
	for _, p := range all {
		if p.Default {
			if err := Install(p.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Installed lists the installed packs' names.
func Installed() ([]string, error) {
	ps, err := InstalledPacks()
	var names []string
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return names, err
}

// InstalledPacks describes the installed packs.
func InstalledPacks() ([]Pack, error) { return describePacks(packDirs(remoteDir()), "remote") }

// Ref is a pack name, optionally qualified as remote/NAME or mine/NAME
// for when both folders have one of that name.
func SplitRef(ref string) (where, name string) {
	if w, n, ok := strings.Cut(ref, "/"); ok && (w == "remote" || w == "mine") {
		return w, n
	}
	return "", ref
}

// IsInstalled and IsLocal say whether remote/NAME or mine/NAME exists: a pack
// with something in it, or with aws/ecs, that module.
func IsInstalled(name string) bool { return refExists(remoteDir(), name) }
func IsLocal(name string) bool     { return refExists(localDir(), name) }

func refExists(root, name string) bool {
	if checkRef(name) != nil {
		return false
	}
	pack, module := SplitModule(name)
	if module == "" {
		return len(moduleFiles(filepath.Join(root, pack))) > 0
	}
	_, err := os.Stat(refPath(root, name))
	return err == nil
}

// InCatalog says whether a pack called name, or with aws/ecs a module, can be
// installed.
func InCatalog(name string) bool {
	if checkRef(name) != nil {
		return false
	}
	pack, module := SplitModule(name)
	if module == "" {
		return builtinFiles(pack) != nil
	}
	_, err := builtinFile(pack, module+".yaml")
	return err == nil
}

// MissingModules is what the catalog has of a pack that you haven't installed.
func MissingModules(pack string) []Module {
	var out []Module
	for _, p := range catalog {
		if p.Name == pack {
			for _, m := range p.Modules {
				if !IsInstalled(pack + "/" + m.Slug) {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// PackSource is a pack's files as they stand, or one module's: your own, the
// installed copy, or if you have neither, what would be installed (remote/NAME
// says the same). Several files come one after another, each headed by its name.
func PackSource(ref string) ([]byte, error) {
	where, name := SplitRef(ref)
	if err := checkRef(name); err != nil {
		return nil, err
	}
	remote, local := IsInstalled(name), IsLocal(name)
	switch {
	case where == "" && remote && local:
		return nil, fmt.Errorf("both packs/remote and packs/mine have %s: say remote/%s or mine/%s", name, name, name)
	case where == "mine" && local, where == "" && local:
		return diskSource(localDir(), name)
	case where == "remote" && remote, where == "" && remote:
		return diskSource(remoteDir(), name)
	case where != "mine" && InCatalog(name):
		pack, module := SplitModule(name)
		if module != "" {
			return builtinFile(pack, module+".yaml")
		}
		var parts [][2]string
		for _, f := range builtinFiles(pack) {
			data, err := builtinFile(pack, f)
			if err != nil {
				return nil, err
			}
			parts = append(parts, [2]string{pack + "/" + f, string(data)})
		}
		return joinFiles(parts), nil
	}
	return nil, fmt.Errorf("no pack called %s (see `clingon pack list` and `clingon pack available`)", ref)
}

func diskSource(root, name string) ([]byte, error) {
	pack, module := SplitModule(name)
	if module != "" {
		return os.ReadFile(refPath(root, name))
	}
	var parts [][2]string
	for _, f := range yamlFiles(filepath.Join(root, pack)) {
		if isPackFile(f) {
			data, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			parts = append(parts, [2]string{pack + "/" + filepath.Base(f), string(data)})
		}
	}
	return joinFiles(parts), nil
}

func joinFiles(parts [][2]string) []byte {
	if len(parts) == 1 {
		return []byte(parts[0][1])
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("# ---- " + p[0] + " ----\n" + p[1])
	}
	return []byte(b.String())
}

// RemotePath is where the installed pack called name lives.
func RemotePath(name string) string { return remotePath(name) }

// PackUpdate is an installed pack whose files differ from clingon's, or one
// clingon no longer ships (Retired: updating removes it).
type PackUpdate struct {
	Name    string
	Retired bool
}

// sameAsBuiltin remembers whether each installed file matches clingon's, until
// the file changes: the menu asks about the same files several times over.
var (
	sameMu    sync.Mutex
	sameCache = map[described]bool{}
)

// matchesBuiltin says whether an installed file is exactly what clingon ships,
// which is how an update is known: comparing the files needs no version numbers.
func matchesBuiltin(pack, path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	key := described{path, fi.ModTime(), fi.Size()}
	sameMu.Lock()
	same, ok := sameCache[key]
	sameMu.Unlock()
	if ok {
		return same
	}
	have, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	want, err := builtinFile(pack, filepath.Base(path))
	same = err == nil && bytes.Equal(have, append([]byte(remoteHeader), want...))
	sameMu.Lock()
	sameCache[key] = same
	sameMu.Unlock()
	return same
}

// PackUpdates lists installed packs that differ from the catalog's, and those no
// longer in it (a pack split up or dropped). `clingon pack update` brings them
// into line. Only what you have counts: a module you haven't installed isn't an
// update.
func PackUpdates() ([]PackUpdate, error) {
	var ups []PackUpdate
	for _, dir := range packDirs(remoteDir()) {
		pack := filepath.Base(dir)
		if builtinFiles(pack) == nil {
			ups = append(ups, PackUpdate{Name: pack, Retired: true})
			continue
		}
		files := yamlFiles(dir)
		stale := !slices.Contains(files, filepath.Join(dir, PackInfoFile))
		for _, f := range files {
			if isPackFile(f) && !matchesBuiltin(pack, f) {
				stale = true
			}
		}
		if stale {
			ups = append(ups, PackUpdate{Name: pack})
		}
	}
	return ups, nil
}

// UpdatePack brings an installed pack into line with the catalog: what you have
// is replaced and what clingon's dropped goes. It never adds a module.
func UpdatePack(pack string) error {
	if err := checkPackName(pack); err != nil {
		return err
	}
	dir := filepath.Join(remoteDir(), pack)
	for _, f := range moduleFiles(dir) {
		name := filepath.Base(f)
		if _, err := builtinFile(pack, name); err != nil {
			if err := os.Remove(f); err != nil {
				return err
			}
			continue
		}
		if err := installFile(pack, name); err != nil {
			return err
		}
	}
	if len(moduleFiles(dir)) == 0 {
		return os.RemoveAll(dir) // everything it had is gone
	}
	return installFile(pack, PackInfoFile)
}

// Fingerprint changes whenever any file in the packs folder does; the menu
// compares it when it regains focus to decide whether to reload.
func Fingerprint() string {
	var b strings.Builder
	m := yamlFiles(packsDir())
	for _, root := range []string{remoteDir(), localDir()} {
		for _, dir := range packDirs(root) {
			m = append(m, yamlFiles(dir)...)
		}
	}
	for _, f := range m {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(&b, "%s %d %d\n", f, st.Size(), st.ModTime().UnixNano())
		}
	}
	return b.String()
}
