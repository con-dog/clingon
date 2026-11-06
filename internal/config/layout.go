package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Packs sit in section folders: packs/<section>/<pack>/, where the section is
// where the menu shows them. A pack straight in packs/mine/ goes under Other.
var sectionSlugs = map[string]string{
	"ai":             "AI",
	"code":           "Code",
	"command-line":   CommandLine,
	"databases":      "Databases",
	"infrastructure": "Infrastructure",
	"system":         "System",
}

// SectionName is what the menu calls a section folder: code is Code.
func SectionName(slug string) string { return sectionSlugs[slug] }

// SectionSlugs lists the section folders, in the menu's order.
func SectionSlugs() []string {
	var out []string
	for _, c := range Categories {
		for slug, name := range sectionSlugs {
			if name == c {
				out = append(out, slug)
			}
		}
	}
	return out
}

// logoColors is each logo's colour in the menu: a pack's logo brings its own.
var logoColors = map[string]string{
	"amazon": "#ff8700", "claude": "#d7875f", "openai": "#eeeeee", "docker": "#0087ff",
	"hash": "#875fd7", "git": "#ff5f5f", "github": "#eeeeee", "go": "#00afd7",
	"apple": "#eeeeee", "node": "#5faf5f", "window": "#5fd787", "postgres": "#5f87d7",
	"python": "#5f87af", "redis": "#ff5f5f",
	// Generic ones the built-in packs use, so the System section isn't grey.
	"folder": "#d7af5f", "globe": "#5fafff", "box": "#d7875f", "terminal": "#87d787",
	"cpu": "#af87ff", "shield": "#ffd75f",
}

// PackColors are the colours a pack's logo can be given with colour: in its
// _pack.md, by name, in the order `clingon pack create` offers them.
var PackColors = []struct{ Name, Hex string }{
	{"grey", "#8787af"}, {"pink", "#ff87d7"}, {"purple", "#af87ff"}, {"blue", "#5f87ff"},
	{"cyan", "#5fd7d7"}, {"green", "#87d787"}, {"yellow", "#ffd75f"}, {"orange", "#ff8700"},
	{"red", "#ff5f5f"}, {"white", "#eeeeee"},
}

// LogoColor is a logo's own colour, or "" for the menu's grey.
func LogoColor(logo string) string { return logoColors[logo] }

// colorHex is a colour name's hex, or "" if there's no such colour.
func colorHex(name string) string {
	for _, c := range PackColors {
		if c.Name == name {
			return c.Hex
		}
	}
	return ""
}

// colorNames lists the colours, for an error that says what would do.
func colorNames() string {
	var out []string
	for _, c := range PackColors {
		out = append(out, c.Name)
	}
	return strings.Join(out, ", ")
}

// PackLogos are the logos for anyone's own pack: generic ones, not brands.
// `clingon pack create` offers them, and any logo in Logos works in _pack.md.
var PackLogos = []string{
	"book", "box", "briefcase", "bug", "chart", "checklist", "cloud", "code", "cpu", "database",
	"flag", "folder", "gear", "globe", "hammer", "hash", "house", "key", "lightning", "lock",
	"mail", "play", "puzzle", "rocket", "server", "shield", "star", "terminal", "window", "wrench",
}

// DefaultPackLogo is the logo a new pack gets unless another is chosen.
const DefaultPackLogo = "terminal"

// defaultPacks are added the first time clingon runs.
var defaultPacks = map[string]bool{"files": true, "network": true, "package-managers": true, "ssh": true, "system": true}

// categoryOf is the menu section of a pack folder: the section folder it's
// in, or Other.
func categoryOf(dir string) string {
	if name, ok := sectionSlugs[filepath.Base(filepath.Dir(dir))]; ok {
		return name
	}
	return Other
}

// packDirs lists the pack folders in root: those in section folders, and
// any straight in it. Your own may be links, say to your dotfiles.
func packDirs(root string) []string {
	var out []string
	for _, dir := range subdirs(root) {
		if _, ok := sectionSlugs[filepath.Base(dir)]; ok {
			out = append(out, subdirs(dir)...)
			continue
		}
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

func subdirs(root string) []string {
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
	return out
}

// packDirIn is where the pack called name is under root: where it is, or
// where it would go (a built-in pack in its section, your own straight in mine/).
func packDirIn(root, pack string) string {
	for _, dir := range packDirs(root) {
		if filepath.Base(dir) == pack {
			return dir
		}
	}
	if sec := builtinSection(pack); sec != "" && root == remoteDir() {
		return filepath.Join(root, sec, pack)
	}
	return filepath.Join(root, pack)
}

var (
	builtinOnce  sync.Once
	builtinIndex map[string]string // pack → its section folder
)

// builtinSection is the section folder a built-in pack ships in, or "".
func builtinSection(pack string) string {
	builtinOnce.Do(func() {
		builtinIndex = map[string]string{}
		secs, _ := fs.ReadDir(packFS, "packs")
		for _, s := range secs {
			packs, _ := fs.ReadDir(packFS, "packs/"+s.Name())
			for _, p := range packs {
				if p.IsDir() {
					builtinIndex[p.Name()] = s.Name()
				}
			}
		}
	})
	return builtinIndex[pack]
}

// builtinPackNames lists the built-in packs.
func builtinPackNames() []string {
	builtinSection("")
	var out []string
	for p := range builtinIndex {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func builtinDir(pack string) string { return "packs/" + builtinSection(pack) + "/" + pack }
