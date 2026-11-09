package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// sectionSlugs are the menu's sections clingon suggests, as a pack's _pack.md
// names one with menu: (infrastructure is Infrastructure). Every _pack.md
// names one: one of these, or one of your own, like work-tools.
var sectionSlugs = map[string]string{
	"other":          Other,
	"ai":             "AI",
	"code":           "Code",
	"command-line":   CommandLine,
	"databases":      "Databases",
	"infrastructure": "Infrastructure",
	"system":         "System",
}

// sectionRe is what a section can be called: lowercase words joined by -.
var sectionRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reservedSections are headings clingon has for itself.
var reservedSections = map[string]bool{"clingon": true, "this-project": true}

// SectionName is what the menu calls a section: code is Code, and one of your
// own, work-tools, is Work Tools.
func SectionName(slug string) string {
	if name, ok := sectionSlugs[slug]; ok {
		return name
	}
	return TitleWords(slug)
}

// TitleWords is a lowercase name as the form shows it: each word, split at -
// and spaces, starting with a capital: select many is Select Many.
func TitleWords(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == ' ' })
	for i, w := range words {
		words[i] = sectionTitle(w)
	}
	return strings.Join(words, " ")
}

// SectionSlugs lists the sections clingon suggests, in the menu's order.
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

// SectionSlug makes what's typed a section's name: Work Tools is work-tools.
// It says what's wrong if it can't be one.
func SectionSlug(typed string) (string, error) {
	s := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(typed)), "-"), "-")
	return s, checkSection(s)
}

// checkSection says what's wrong with a section's name, if anything.
func checkSection(s string) error {
	switch {
	case s == "":
		return fmt.Errorf("menu: needs a name: one of %s, or one of your own", strings.Join(SectionSlugs(), ", "))
	case !sectionRe.MatchString(s):
		return fmt.Errorf("a menu name is lowercase letters, digits and -, like work-tools (not %s)", s)
	case reservedSections[s]:
		return fmt.Errorf("%s is clingon's own heading: give the menu another name", SectionName(s))
	}
	return nil
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

// PackColors are the colours a pack's logo can be given with tint: in its
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

// packDirs lists the pack folders in root. Your own may be links, say to
// your dotfiles.
func packDirs(root string) []string {
	out := subdirs(root)
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

// packDirIn is where the pack called name is under root.
func packDirIn(root, pack string) string { return filepath.Join(root, pack) }

// isBuiltin says whether clingon ships a pack called pack.
func isBuiltin(pack string) bool {
	if pack == "" || strings.ContainsAny(pack, "/.") {
		return false
	}
	fi, err := fs.Stat(packFS, builtinDir(pack))
	return err == nil && fi.IsDir()
}

// builtinPackNames lists the built-in packs.
func builtinPackNames() []string {
	entries, _ := fs.ReadDir(packFS, ".")
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func builtinDir(pack string) string { return pack }
