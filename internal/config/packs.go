package config

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Built-in packs, one file each, named after the file.
//
//go:embed packs/*.yaml
var packFS embed.FS

type Pack struct {
	Name        string
	Where       string // "remote" (installed) or "local" (yours); "" for the catalog
	Version     string
	Description string
	Requires    []string
	AnyOf       []string // for a pack with no tools of its own whose groups each need one: any will do
	Default     bool
	Sections    []string
	Commands    int
}

// Packs lists every built-in pack, by name: the catalog (catalog.go),
// worked out from the packs when they change, so nothing's parsed here.
func Packs() ([]Pack, error) {
	return slices.Clone(catalog), nil
}

func loadPack(name string) (*Config, error) {
	data, err := packFS.ReadFile("packs/" + name + ".yaml")
	if err != nil {
		return nil, err
	}
	return parse("pack "+name, data)
}

// The packs folder holds:
//
//	_available.yaml  every pack you can install (written by clingon)
//	remote/          installed packs, one file each: installing copies a pack
//	                 in, removing deletes it (rather like node_modules)
//	local/           your own packs
//
// Files starting with "_" are clingon's own and never packs; pack names
// can't start with "_".
func packsDir() string  { return filepath.Join(Dir(), "packs") }
func remoteDir() string { return filepath.Join(packsDir(), "remote") }
func localDir() string  { return filepath.Join(packsDir(), "local") }

func remotePath(name string) string { return filepath.Join(remoteDir(), name+".yaml") }

// availableFile lists the packs that can be installed.
func availableFile() string { return filepath.Join(packsDir(), "_available.yaml") }

type availableList struct {
	Available []availablePack `yaml:"available"`
}

type availablePack struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Sections    []string `yaml:"sections,flow"`
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
		l.Available = append(l.Available, availablePack{p.Name, p.Version, p.Description, p.Sections, p.Commands})
	}
	var buf bytes.Buffer
	buf.WriteString("# Packs you can install, written by clingon. Don't edit: it's rewritten.\n")
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

const remoteHeader = "# Installed by clingon. `clingon pack update` overwrites this file: put\n# your own commands in packs/local/ instead.\n"

// Install copies a pack from the catalog into remote/, replacing any copy
// already there.
func Install(name string) error {
	data, err := packFS.ReadFile("packs/" + name + ".yaml")
	if err != nil {
		return fmt.Errorf("no pack called %q (see `clingon pack available`)", name)
	}
	if err := os.MkdirAll(remoteDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(remotePath(name), append([]byte(remoteHeader), data...), 0o644)
}

// Uninstall deletes an installed pack from remote/.
func Uninstall(name string) error {
	if err := checkPackName(name); err != nil {
		return err
	}
	if err := os.Remove(remotePath(name)); err != nil {
		return fmt.Errorf("%s isn't installed", name)
	}
	return nil
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

// InstalledPacks describes the installed packs, versions as installed.
func InstalledPacks() ([]Pack, error) {
	files, err := packFiles(remoteDir())
	if err != nil {
		return nil, err
	}
	return describePacks(files, "remote")
}

// Ref is a pack name, optionally qualified as remote/NAME or local/NAME
// for when both folders have one of that name.
func SplitRef(ref string) (where, name string) {
	if w, n, ok := strings.Cut(ref, "/"); ok && (w == "remote" || w == "local") {
		return w, n
	}
	return "", ref
}

// IsInstalled and IsLocal say whether remote/NAME or local/NAME exists.
func IsInstalled(name string) bool { _, err := os.Stat(remotePath(name)); return err == nil }
func IsLocal(name string) bool     { _, err := os.Stat(PackPath(name)); return err == nil }

// InCatalog says whether a pack called name can be installed.
func InCatalog(name string) bool {
	_, err := packFS.ReadFile("packs/" + name + ".yaml")
	return err == nil
}

// RemotePath is where the installed pack called name lives.
func RemotePath(name string) string { return remotePath(name) }

// PackUpdate is an installed pack with a different version available, or
// one clingon no longer ships (Retired: updating removes it).
type PackUpdate struct {
	Name, From, To string
	Retired        bool
}

// PackUpdates lists installed packs whose catalog version differs from
// the installed one, and those no longer in the catalog (a pack split up
// or dropped). `clingon pack update` installs or removes them.
func PackUpdates() ([]PackUpdate, error) {
	installed, err := InstalledPacks()
	if err != nil {
		return nil, err
	}
	all, err := Packs()
	if err != nil {
		return nil, err
	}
	latest := map[string]string{}
	for _, p := range all {
		latest[p.Name] = p.Version
	}
	var ups []PackUpdate
	for _, p := range installed {
		switch v, ok := latest[p.Name]; {
		case !ok:
			ups = append(ups, PackUpdate{Name: p.Name, From: p.Version, Retired: true})
		case v != p.Version:
			ups = append(ups, PackUpdate{Name: p.Name, From: p.Version, To: v})
		}
	}
	return ups, nil
}

// Fingerprint changes whenever any file in the config folder does; the menu
// compares it when it regains focus to decide whether to reload.
func Fingerprint() string {
	var b strings.Builder
	var m []string
	for _, dir := range []string{packsDir(), remoteDir(), localDir()} {
		m = append(m, yamlFiles(dir)...)
	}
	for _, f := range m {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(&b, "%s %d %d\n", f, st.Size(), st.ModTime().UnixNano())
		}
	}
	return b.String()
}
