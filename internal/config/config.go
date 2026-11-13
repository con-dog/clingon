// Package config loads and validates clingon packs.
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/tmpl"
	"go.yaml.in/yaml/v3"
)

// Starter and StarterInfo are the example pack `clingon pack create` writes: a
// module, and the pack's _pack.md.
var (
	//go:embed starter.md
	Starter []byte
	//go:embed starter_pack.md
	StarterInfo []byte
)

// Config is packs put together: what the menu shows.
type Config struct {
	Sections []Section `yaml:"sections"`
}

// PackInfoFile says what a pack is called, where the menu shows it and how it
// looks. Every pack has one.
const PackInfoFile = "_pack.md"

// packInfo is what's in a _pack.md (and for built-in packs, what clingon says of them).
type packInfo struct {
	Name        string   `yaml:"name"`        // what the menu calls the pack; from its folder if unset
	Description string   `yaml:"description"` // shown by `clingon pack`
	Default     bool     `yaml:"default"`     // installed until the user says otherwise
	Requires    []string `yaml:"requires"`    // tools `clingon doctor` looks for
	Icon        string   `yaml:"icon"`
	Logo        string   `yaml:"logo"`
	Color       string   `yaml:"color"`
	Category    string   `yaml:"category"`
	Section     string   `yaml:"-"` // as menu: names it: work-tools, for Work tools
}

// Categories are the main menu's headings clingon suggests, in order
// (alphabetical, Other too); a pack can have one of its own, and those sit
// among them alphabetically (see MenuCategories).
var Categories = []string{"AI", "Code", CommandLine, "Databases", "Infrastructure", Other, "System"}

const (
	Other       = "Other"
	CommandLine = "Command Line"
)

const DefaultIcon = "•"

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type Section struct {
	Name     string    `yaml:"name"`
	Icon     string    `yaml:"icon"`  // one glyph shown before the name; DefaultIcon if unset
	Logo     string    `yaml:"logo"`  // a brand logo in clingon's font, by name (see Logos); wins over Icon
	Color    string    `yaml:"color"` // the icon's colour, #rrggbb; grey if unset
	Category string    `yaml:"category"`
	Desc     string    `yaml:"description"` // what the pack's for, under it in the menu's info pane
	Groups   []Group   `yaml:"groups"`
	Commands []Command `yaml:"commands"` // ungrouped commands, shown first
	Needs    []string  `yaml:"-"`        // tools its pack requires, checked when the menu shows it
}

type Group struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"` // what's in it, shown when you pick it as part of a pack
	Requires    []string  `yaml:"requires"`    // hidden unless all of these are on PATH
	When        []string  `yaml:"when"`        // its commands' when, unless they have their own
	Commands    []Command `yaml:"commands"`
}

// Risk levels. Warn: asks No/Yes before it runs (it changes or stops
// things, but can be put right). Danger: you type a random code, then it
// asks No/Yes (it destroys data or shared state for good).
const (
	RiskWarn   = "warn"
	RiskDanger = "danger"
)

type Command struct {
	Name     string   `yaml:"name"`
	ID       string   `yaml:"id"` // what recipes call it, for good: see CommandID
	Cmd      string   `yaml:"cmd"`
	Desc     string   `yaml:"desc"`
	Risk     string   `yaml:"risk"`     // "", RiskWarn or RiskDanger: how hard it is to run by mistake
	Requires []string `yaml:"requires"` // hidden unless all of these are on PATH
	When     []string `yaml:"when"`     // greyed out unless one of these files is here or in a folder above (./x: here only)
	Args     []Arg    `yaml:"args"`
	Disabled string   `yaml:"-"` // why it can't be used right now: shown greyed out
	Editor   bool     `yaml:"-"` // opens an editor in the shell: focus goes there while it's open
	Source   string   `yaml:"-"` // the pack file it's from: with its heading, what a recipe links to
}

// Arg fills one <name> placeholder in a command.
type Arg struct {
	AtLeastOne  bool     `yaml:"-"` // a checklist that needs something ticked before it can go on
	CountAs     string   `yaml:"-"` // a checklist's description counts these once there are too many to name, like "pack"
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"` // text | choose | flags | secret; inferred when empty
	Prompt      string   `yaml:"prompt"`
	Desc        string   `yaml:"desc"` // a sentence added to the description once filled; <name> is the value
	Default     string   `yaml:"default"`
	Placeholder string   `yaml:"placeholder"`
	Optional    bool     `yaml:"optional"`
	Options     []Option `yaml:"options"`
	Join        *string  `yaml:"join"`    // separator for flags, default " "
	Quote       bool     `yaml:"quote"`   // shell-quote the value, for free text that may hold quotes or spaces
	Pattern     string   `yaml:"pattern"` // a regexp typed values must match
	IconFrom    string   `yaml:"-"`       // its choices show the logo named by this earlier blank's answer
}

// Option is a choice: in a pack, a ( ) or [ ] line; in a test's YAML, a plain
// string or {value, label}.
type Option struct {
	Value  string `yaml:"value"`
	Label  string `yaml:"label"`
	Indent bool   `yaml:"-"` // drawn under the option above it, as part of it
	Icon   string `yaml:"-"` // a glyph shown before it, like a logo
	Color  string `yaml:"-"` // its icon's colour, #rrggbb
}

func (o *Option) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		o.Value = n.Value
		return nil
	}
	type plain Option
	return n.Decode((*plain)(o))
}

// Kind resolves the arg type, inferring it from options when unset.
func (a Arg) Kind() string {
	if a.Type != "" {
		return a.Type
	}
	if len(a.Options) > 0 {
		return "choose"
	}
	return "text"
}

func (a Arg) Multi() bool { return a.Kind() == "flags" }

// HasOption reports whether v is one of the arg's fixed options.
func (a Arg) HasOption(v string) bool {
	for _, o := range a.Options {
		if o.Value == v {
			return true
		}
	}
	return false
}

// Matches reports whether a typed value fits the arg's pattern (the
// whole value, not just part of it). Validate has checked it compiles.
func (a Arg) Matches(v string) bool {
	if a.Pattern == "" {
		return true
	}
	re, err := regexp.Compile(`^(?:` + a.Pattern + `)$`)
	return err == nil && re.MatchString(v)
}

func (a Arg) Separator() string {
	if a.Join == nil {
		return " "
	}
	return *a.Join
}

// Ticked splits s, a tick-any blank's answer or default (its choices joined by
// Separator), into the choices ticked. A choice can have the separator in it,
// like -n 5, so each is matched against the choices, longest first; anything
// else goes up to the next separator.
func (a Arg) Ticked(s string) []string {
	sep := a.Separator()
	var out []string
	for s != "" {
		best := ""
		for _, o := range a.Options {
			if (s == o.Value || strings.HasPrefix(s, o.Value+sep)) && len(o.Value) > len(best) {
				best = o.Value
			}
		}
		if best == "" {
			best, _, _ = strings.Cut(s, sep)
		}
		if best != "" {
			out = append(out, best)
		}
		s = strings.TrimPrefix(s[len(best):], sep)
	}
	return out
}

func (a Arg) Label() string {
	if a.Prompt != "" {
		return a.Prompt
	}
	return sectionTitle(a.Name) // asked by its name, as words: <file-name> asks "File name"
}

// Known reports whether name is a declared arg, i.e. whether <name> is a placeholder.
func (c *Command) Known(name string) bool {
	for _, a := range c.Args {
		if a.Name == name {
			return true
		}
	}
	return false
}

// All returns every command in a section, ungrouped first.
func (s *Section) All() []*Command {
	var out []*Command
	for i := range s.Commands {
		out = append(out, &s.Commands[i])
	}
	for g := range s.Groups {
		for i := range s.Groups[g].Commands {
			out = append(out, &s.Groups[g].Commands[i])
		}
	}
	return out
}

// Dir is clingon's folder: $CLINGON_CONFIG or ~/.clingon. Packs and recipes are
// at the top, to look at and change; the rest is in StateDir.
func Dir() string {
	if d := os.Getenv("CLINGON_CONFIG"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clingon")
}

// StateDir is what clingon keeps for itself: its shell and editor setup, its
// socket, crash logs, and which projects you've trusted.
func StateDir() string { return filepath.Join(Dir(), "config") }

// Load reads the installed packs (packs/remote) and then the user's own
// (packs/mine), merges sections and groups by name, and validates the
// result. It returns the user's pack folders too.
func Load() (*Config, []string, error) {
	local := packDirs(localDir())
	cfg := &Config{}
	for _, dir := range append(packDirs(remoteDir()), local...) {
		parts, err := readPackDir(dir)
		if err != nil {
			return nil, nil, err
		}
		if err := needsInfo(dir, parts); err != nil {
			return nil, nil, err
		}
		cfg.merge(assemble(filepath.Base(dir), parts))
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	cfg.keepAvailable()
	cfg.sort()
	return cfg, local, nil
}

// needsInfo says what's wrong with a pack folder that has modules but no
// _pack.md: every pack has exactly one.
func needsInfo(dir string, parts []parsedFile) error {
	if len(parts) == 0 || slices.ContainsFunc(parts, func(p parsedFile) bool { return p.info != nil }) {
		return nil
	}
	return fmt.Errorf("%s has no %s: every pack has one, with its # Name and menu: (one of %s, or one of your own)", dir, PackInfoFile, strings.Join(SectionSlugs(), ", "))
}

// newPackInfo is the _pack.md a pack gets when clingon starts it.
func newPackInfo(title string) []byte {
	return []byte("---\nmenu: other\nlogo: " + DefaultPackLogo + "\n---\n# " + title + "\n")
}

// parsedFile is one file of a pack: its _pack.md, or a module.
type parsedFile struct {
	info  *packInfo
	group *Group
}

// decodeStrict reads YAML into v, refusing fields it doesn't know. (Tests
// write whole configs down this way.)
func decodeStrict(name string, data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && err != io.EOF {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// assemble puts a pack's files together as the one menu item they make, called
// after its folder unless its _pack.md says otherwise. A pack with no modules
// isn't in the menu at all.
func assemble(folder string, parts []parsedFile) *Config {
	s := Section{Name: sectionTitle(folder), Category: Other}
	for _, p := range parts {
		switch {
		case p.info != nil:
			if p.info.Name != "" {
				s.Name = p.info.Name
			}
			s.Icon, s.Logo, s.Color, s.Category = p.info.Icon, p.info.Logo, p.info.Color, p.info.Category
			if s.Color == "" {
				s.Color = logoColors[s.Logo]
			}
			s.Needs, s.Desc = p.info.Requires, p.info.Description
		case p.group != nil:
			s.Groups = append(s.Groups, *p.group)
		}
	}
	if len(s.Groups) == 0 {
		return &Config{}
	}
	s.pushDown()
	return &Config{Sections: []Section{s}}
}

func (c *Config) merge(o *Config) {
	for _, s := range o.Sections {
		dst := c.section(s.Name)
		if dst == nil {
			c.Sections = append(c.Sections, s)
			continue
		}
		if dst.Category == "" {
			dst.Category = s.Category
		}
		if dst.Icon == "" {
			dst.Icon = s.Icon
		}
		if dst.Logo == "" {
			dst.Logo = s.Logo
		}
		if dst.Color == "" {
			dst.Color = s.Color
		}
		if dst.Desc == "" {
			dst.Desc = s.Desc
		}
		// dst keeps its own Needs: the status bar checks the pack that
		// made the section, not every pack that adds to it.
		dst.Commands = append(dst.Commands, s.Commands...)
		for _, g := range s.Groups {
			if dg := dst.group(g.Name); dg != nil {
				dg.Commands = append(dg.Commands, g.Commands...)
				if len(dg.Requires) == 0 {
					dg.Requires = g.Requires
				}
				if dg.Description == "" {
					dg.Description = g.Description
				}
			} else {
				dst.Groups = append(dst.Groups, g)
			}
		}
	}
}

// pushDown gives each command its group's when and requires, so they stay
// with that file's commands when another file's group of the same name is
// merged in.
func (s *Section) pushDown() {
	for g := range s.Groups {
		grp := &s.Groups[g]
		for i := range grp.Commands {
			c := &grp.Commands[i]
			if len(c.When) == 0 {
				c.When = grp.When
			}
			c.Requires = append(slices.Clone(grp.Requires), c.Requires...)
		}
		grp.When, grp.Requires = nil, nil
	}
}

func (c *Config) section(name string) *Section {
	for i := range c.Sections {
		if c.Sections[i].Name == name {
			return &c.Sections[i]
		}
	}
	return nil
}

func (s *Section) group(name string) *Group {
	for i := range s.Groups {
		if s.Groups[i].Name == name {
			return &s.Groups[i]
		}
	}
	return nil
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// UnknownLogos lists sections whose logo clingon doesn't have; they show
// their icon instead.
func (c *Config) UnknownLogos() []string {
	var out []string
	for _, s := range c.Sections {
		if _, ok := Logos[s.Logo]; s.Logo != "" && !ok {
			out = append(out, fmt.Sprintf("%s: there's no logo %q (the logos are %s)", s.Name, s.Logo, strings.Join(slices.Sorted(maps.Keys(Logos)), ", ")))
		}
	}
	return out
}

// Validate returns every problem found, each prefixed with where it is.
// It also normalises category names and fills in default icons.
// quotedArgs names the args whose values clingon shell-quotes but which sit
// inside quotes in cmd. Quoting a value twice undoes it: in `echo '<x>'`,
// a value x;rm closes the pack's quotes and runs.
func quotedArgs(c *Command) []string {
	var out []string
	for _, a := range c.Args {
		if a.Quote && inQuotes(c.Cmd, a.Name, c.Known) {
			out = append(out, a.Name)
		}
	}
	return out
}

// inQuotes reports whether the placeholder for name is inside quotes in script.
func inQuotes(script, name string, known func(string) bool) bool {
	for _, q := range QuotesAt(script, name, known) {
		if q != 0 {
			return true
		}
	}
	return false
}

// QuotesAt says which quote each placeholder for name sits inside in script,
// in order: a single quote, a double quote, or 0 for none.
func QuotesAt(script, name string, known func(string) bool) []byte {
	var quote byte
	var out []byte
	for _, seg := range tmpl.Parse(script, known) {
		if seg.Arg != "" {
			if seg.Arg == name {
				out = append(out, quote)
			}
			continue
		}
		for i := 0; i < len(seg.Text); i++ {
			switch c := seg.Text[i]; {
			case quote == '\'':
				if c == '\'' {
					quote = 0
				}
			case c == '\\':
				i++
			case c == quote:
				quote = 0
			case quote == 0 && (c == '\'' || c == '"'):
				quote = c
			}
		}
	}
	return out
}

// commandText is everything in a command that's run or shown as text.
func commandText(c *Command) []string {
	out := []string{c.Name, c.Cmd, c.Desc}
	for _, a := range c.Args {
		out = append(out, a.Name, a.Prompt, a.Desc, a.Default, a.Placeholder)
		if a.Join != nil {
			out = append(out, *a.Join)
		}
		for _, o := range a.Options {
			out = append(out, o.Value, o.Label)
		}
	}
	return out
}

// Hidden finds a character that can hide or change what's on screen: control
// characters (a terminal acts on escape codes), and invisible and text
// direction ones. Tabs and line breaks are fine.
func Hidden(s string) (rune, bool) {
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n':
		case r < ' ' || r >= 0x7f && r < 0xa0,
			r == 0xad || r == 0x61c || r == 0xfeff || r == 0xfffd, // 0xfffd: a stray byte, maybe an escape code
			r >= 0x200b && r <= 0x200f,
			r >= 0x202a && r <= 0x202e,
			r >= 0x2060 && r <= 0x2069,
			r >= 0xe0000 && r <= 0xe007f:
			return r, true
		}
	}
	return 0, false
}

func (c *Config) Validate() []error {
	var errs []error
	bad := func(where, format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", where, fmt.Sprintf(format, a...)))
	}
	for i := range c.Sections {
		s := &c.Sections[i]
		if strings.TrimSpace(s.Name) == "" {
			bad("section", "missing name")
		}
		// Shown as they are, so they get the same check as a command's text.
		// A joiner is part of many emoji, and an icon can be one.
		for _, text := range []string{s.Name, strings.ReplaceAll(s.Icon, "\u200d", "")} {
			if r, ok := Hidden(text); ok {
				bad(s.Name, "%q has a character that can hide or change what's on screen (%U)", text, r)
			}
		}
		for g := range s.Groups {
			for _, text := range []string{s.Groups[g].Name, s.Groups[g].Description} {
				if r, ok := Hidden(text); ok {
					bad(s.Name, "group %q has a character that can hide or change what's on screen (%U)", text, r)
				}
			}
		}
		for _, cmd := range s.All() {
			if cmd.ID != "" && !commandIDRe.MatchString(cmd.ID) {
				bad(s.Name, "%s: id %q should be a name and 8 hex digits, like aws-1a2b3c4d", cmd.Name, cmd.ID)
			}
		}
		// A logo clingon doesn't have (a typo, or one an update dropped from
		// a pack you have installed) leaves the icon: `clingon check` says so,
		// but it's no reason not to start.
		if g, ok := Logos[s.Logo]; ok {
			s.Icon = g
		}
		if s.Color != "" && !hexColor.MatchString(s.Color) {
			bad(s.Name, "color %q should be a hex colour like #ff87d7", s.Color)
		}
		if w := ansi.StringWidth(s.Icon); w > 2 {
			bad(s.Name, "icon %q should be a single glyph", s.Icon)
		}
		if s.Icon == "" {
			s.Icon = DefaultIcon
		}
		s.Category = canonical(s.Category)
		if len(s.All()) == 0 {
			bad(s.Name, "section has no commands")
		}
		for g := range s.Groups {
			for i := range s.Groups[g].Commands {
				if c := &s.Groups[g].Commands[i]; len(c.When) == 0 {
					c.When = s.Groups[g].When
				}
			}
		}
		for _, cmd := range s.All() {
			where := s.Name + " › " + cmd.Name
			if cmd.Name == "" {
				where = s.Name + " › (unnamed)"
				bad(where, "command is missing name")
			}
			if strings.TrimSpace(cmd.Cmd) == "" {
				bad(where, "command is missing cmd")
			}
			for _, when := range cmd.When {
				pat := strings.TrimPrefix(when, "./")
				if _, err := filepath.Match(pat, ""); err != nil || strings.ContainsAny(pat, "*?[") && strings.Contains(pat, "/") {
					bad(where, "when: %q isn't a file name or pattern like Dockerfile or *.tf", when)
				}
			}
			if bang := historyBang(cmd.Cmd); bang != "" {
				bad(where, "the shell reads %q as history expansion when it's typed in, and garbles the command: put it in single quotes or leave the ! out", bang)
			}
			switch cmd.Risk {
			case "", RiskWarn, RiskDanger:
			default:
				bad(where, "unknown risk %q (use warn or danger)", cmd.Risk)
			}
			for _, name := range quotedArgs(cmd) {
				bad(where, "<%s> is inside quotes, where clingon's quoting of its value doesn't protect it: close the quotes around it, like 'some text '<%s>", name, name)
			}
			for _, s := range commandText(cmd) {
				if r, ok := Hidden(s); ok {
					bad(where, "%q has a character that can hide or change what's on screen (%U)", s, r)
				}
			}
			seen := map[string]bool{}
			used := map[string]bool{}
			for _, seg := range tmpl.Parse(cmd.Cmd, cmd.Known) {
				used[seg.Arg] = true
			}
			for _, a := range cmd.Args {
				aw := where + " › " + a.Name
				switch {
				case !identRe.MatchString(a.Name):
					bad(aw, "arg name must be letters, digits, _ or -")
				case seen[a.Name]:
					bad(aw, "duplicate arg name")
				case !used[a.Name]:
					bad(aw, "arg is never used; add <%s> to cmd", a.Name)
				}
				seen[a.Name] = true
				switch a.Kind() {
				case "text":
				case "choose":
					if len(a.Options) == 0 {
						bad(aw, "choose needs options")
					}
				case "flags":
					if len(a.Options) == 0 {
						bad(aw, "flags needs options")
					}
				case "secret":
				default:
					bad(aw, "unknown type %q (use text, choose, flags or secret)", a.Type)
				}
				if a.Pattern != "" {
					if _, err := regexp.Compile(a.Pattern); err != nil {
						bad(aw, "pattern doesn't compile: %v", err)
					}
				}
			}
		}
	}
	return errs
}

// historyBang finds a ! that an interactive shell would treat as history
// expansion (bash and zsh both do, on what's typed at the prompt): one
// outside single quotes and not escaped, $!, or followed by a space, =, (
// or the end. zsh even deletes a !" outright, leaving the quote open.
func historyBang(cmd string) string {
	single, double := false, false
	for i := 0; i < len(cmd); i++ {
		switch ch := cmd[i]; {
		case ch == '\\' && !single:
			i++ // the next character is escaped
		case ch == '\'' && !double:
			single = !single
		case ch == '"' && !single:
			double = !double
		case ch == '!' && !single:
			if i > 0 && cmd[i-1] == '$' || i > 1 && cmd[i-2:i] == "${" { // $! and ${!var}
				continue
			}
			if i+1 == len(cmd) || strings.IndexByte(" \t\n=(", cmd[i+1]) >= 0 {
				continue
			}
			return cmd[i:min(i+2, len(cmd))]
		}
	}
	return ""
}

// canonical maps a category name to its spelling: one clingon suggests, in
// any case, is spelt its way, and none at all is Other.
func canonical(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return Other
	}
	for _, c := range Categories {
		if strings.EqualFold(name, c) {
			return c
		}
	}
	return name
}

// MenuCategories are the main menu's headings for sections, in order: the
// ones clingon suggests and any of your own, alphabetically, Other among
// them. (Clingon's own sections go after them all: see the ui's showRoot.)
func MenuCategories(sections []Section) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range Categories {
		seen[c], out = true, append(out, c)
	}
	for _, s := range sections {
		if c := s.Category; !seen[c] {
			seen[c], out = true, append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// sort puts sections, groups, commands and fixed options in alphabetical
// order.
func (c *Config) sort() {
	sort.SliceStable(c.Sections, func(i, j int) bool { return NaturalLess(c.Sections[i].Name, c.Sections[j].Name) })
	for i := range c.Sections {
		c.Sections[i].Sort()
	}
}

// Sort puts a section's groups, commands and fixed options in natural
// alphabetical order.
func (s *Section) Sort() {
	sortCmds := func(cmds []Command) {
		sort.SliceStable(cmds, func(i, j int) bool { return NaturalLess(cmds[i].Name, cmds[j].Name) })
		for _, c := range cmds {
			for _, a := range c.Args {
				sort.SliceStable(a.Options, func(i, j int) bool { return NaturalLess(a.Options[i].Value, a.Options[j].Value) })
			}
		}
	}
	sortCmds(s.Commands)
	sort.SliceStable(s.Groups, func(i, j int) bool { return NaturalLess(s.Groups[i].Name, s.Groups[j].Name) })
	for g := range s.Groups {
		sortCmds(s.Groups[g].Commands)
	}
}

// NaturalLess orders strings case-insensitively, with runs of digits
// compared as numbers: "2" < "16" < "100".
func NaturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		da, db := digits(a), digits(b)
		if da != "" && db != "" {
			na, nb := strings.TrimLeft(da, "0"), strings.TrimLeft(db, "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = a[len(da):], b[len(db):]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func digits(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i]
}

// keepAvailable hides commands and groups whose required tools aren't on
// PATH, then any section left with no commands.
func (c *Config) keepAvailable() {
	have := map[string]bool{}
	found := func(tool string) bool {
		if v, ok := have[tool]; ok {
			return v
		}
		_, err := exec.LookPath(tool)
		have[tool] = err == nil
		return have[tool]
	}
	allFound := func(tools []string) bool {
		for _, tool := range tools {
			if !found(tool) {
				return false
			}
		}
		return true
	}
	commands := func(cmds []Command) []Command {
		var out []Command
		for _, c := range cmds {
			if allFound(c.Requires) {
				out = append(out, c)
			}
		}
		return out
	}
	var sections []Section
	for _, s := range c.Sections {
		var groups []Group
		for _, g := range s.Groups {
			if g.Commands = commands(g.Commands); allFound(g.Requires) && len(g.Commands) > 0 {
				groups = append(groups, g)
			}
		}
		s.Commands = commands(s.Commands)
		s.Groups = groups
		if len(s.All()) > 0 {
			sections = append(sections, s)
		}
	}
	c.Sections = sections
}
