package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// A pack is a folder of Markdown files, read with a small, strict part of
// Markdown so they also read as documentation on GitHub:
//
//	---                    front matter, optional: settings for the whole file
//	needs: docker          programs every command needs (and in _pack.md,
//	                       menu:, which it must have, logo: and tint:)
//	when: compose.yaml     files that must be here or in a folder above
//	---
//	# Name                 the pack's or module's name; every file starts with one
//	Description.
//
//	## Command name        a command
//	> [!WARNING]           asks first ([!CAUTION]: dangerous); its text is the description
//	> What it does.
//
//	```sh when=./x         what's typed; settings for this command after the language
//	docker build -t <tag> .
//	```
//
//	- `<tag>` Question     a blank, with choices under it:
//	  - (x) `value` label  select one, or type your own; (x) is picked to begin with
//	  - [ ] `value` label  select many; [x] starts selected
//
// <!-- comments --> are for whoever edits the file.

// Lines a file can be read from, and where a problem is.
type mdLines struct {
	path  string
	lines []string
	at    int // the next line to read, 0-based
}

func (m *mdLines) errf(line int, format string, a ...any) error {
	return fmt.Errorf("%s:%d: %s", m.path, line+1, fmt.Sprintf(format, a...))
}

var (
	choiceRe    = regexp.MustCompile("^([(\\[])([ x])([)\\]]) `([^`]+)`(?: (.*))?$")
	blankRe     = regexp.MustCompile("^`<([A-Za-z_][A-Za-z0-9_-]*)>`(?: (.*))?$")
	nameCharsRe = regexp.MustCompile(`^[A-Za-z0-9 ()]+$`)
	fenceLangs  = map[string]bool{"": true, "sh": true, "bash": true, "shell": true, "zsh": true}
	secretWords = []string{"secret", "token", "password"}
	listItemRe  = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)]) `)
)

// IsSecretName says whether a blank called name is typed hidden: its name
// says it's a secret, token or password.
func IsSecretName(name string) bool {
	n := strings.ToLower(name)
	for _, w := range secretWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// splitList reads a comma-separated list of names.
func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// stripComments blanks out <!-- comments -->, keeping the lines so line
// numbers still point at the right place. Comments inside code blocks are code.
func stripComments(lines []string) []string {
	out := make([]string, len(lines))
	inComment, inFence := false, false
	for i, l := range lines {
		if !inComment && strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
		}
		if inFence {
			out[i] = l
			continue
		}
		var b strings.Builder
		rest := l
		for rest != "" {
			if inComment {
				end := strings.Index(rest, "-->")
				if end < 0 {
					rest = ""
					break
				}
				rest, inComment = rest[end+3:], false
				continue
			}
			start := strings.Index(rest, "<!--")
			if start < 0 {
				b.WriteString(rest)
				break
			}
			b.WriteString(rest[:start])
			rest, inComment = rest[start+4:], true
		}
		out[i] = strings.TrimRight(b.String(), " \t")
	}
	return out
}

// frontMatter reads settings between --- lines at the top, if there are any.
func (m *mdLines) frontMatter(allowed ...string) (map[string]string, error) {
	for m.at < len(m.lines) && m.lines[m.at] == "" {
		m.at++
	}
	if m.at >= len(m.lines) || m.lines[m.at] != "---" {
		return nil, nil
	}
	start := m.at
	out := map[string]string{}
	for m.at++; m.at < len(m.lines); m.at++ {
		l := m.lines[m.at]
		if l == "---" {
			m.at++
			return out, nil
		}
		if strings.TrimSpace(l) == "" {
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		k = strings.TrimSpace(k)
		if !ok || !contains(allowed, k) {
			return nil, m.errf(m.at, "front matter here can only set %s, as key: value", strings.Join(allowed, ", "))
		}
		out[k] = strings.TrimSpace(v)
	}
	return nil, m.errf(start, "front matter has no closing ---")
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// title reads the # Name every file starts with, and the paragraph under it.
func (m *mdLines) title() (name, desc string, err error) {
	for m.at < len(m.lines) && strings.TrimSpace(m.lines[m.at]) == "" {
		m.at++
	}
	if m.at >= len(m.lines) || !strings.HasPrefix(m.lines[m.at], "# ") {
		line := min(m.at, max(len(m.lines)-1, 0))
		return "", "", m.errf(line, "every pack file starts with a # Name, its name in the menu")
	}
	name = strings.TrimSpace(m.lines[m.at][2:])
	if !nameCharsRe.MatchString(name) {
		return "", "", m.errf(m.at, "names use only letters, digits, spaces and ( ) (not %q)", name)
	}
	m.at++
	var para []string
	for ; m.at < len(m.lines); m.at++ {
		l := m.lines[m.at]
		if strings.HasPrefix(l, "#") || listItemRe.MatchString(l) { // the description ends where something else starts
			break
		}
		if strings.TrimSpace(l) == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		if err := m.plainText(m.at, l); err != nil {
			return "", "", err
		}
		para = append(para, strings.TrimSpace(l))
	}
	return name, strings.Join(para, " "), nil
}

// plainText checks a line of description is just text: Markdown that packs
// don't use is a mistake to point out, not something to guess at.
func (m *mdLines) plainText(i int, l string) error {
	t := strings.TrimSpace(l)
	switch {
	case listItemRe.MatchString(l):
		return m.errf(i, "a list here isn't part of a command: blanks go after the command's code block")
	case strings.HasPrefix(t, ">"):
		return m.errf(i, "only > [!WARNING] and > [!CAUTION] callouts are used, right under a command's ## heading")
	case strings.HasPrefix(t, "```"):
		return m.errf(i, "a code block goes under a command's ## heading")
	case strings.Contains(withoutCode(t), "**") || strings.Contains(withoutCode(t), "__"):
		return m.errf(i, "packs don't use bold or italics")
	}
	return nil
}

// withoutCode is a line with its `code spans` taken out.
func withoutCode(l string) string {
	parts := strings.Split(l, "`")
	var b strings.Builder
	for i, p := range parts {
		if i%2 == 0 {
			b.WriteString(p)
		}
	}
	return b.String()
}

// parsePack reads a _pack.md.
func parsePack(path string, data []byte) (*packInfo, error) {
	m := &mdLines{path: path, lines: stripComments(strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"))}
	fm, err := m.frontMatter("menu", "logo", "tint", "needs")
	if err != nil {
		return nil, err
	}
	keyAt := func(key string) int {
		return max(slices.IndexFunc(m.lines, func(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), key+":") }), 0)
	}
	s := fm["menu"]
	if s == "" {
		return nil, m.errf(0, "_pack.md says where in the menu the pack goes, in its front matter: menu: and one of %s, or one of your own", strings.Join(SectionSlugs(), ", "))
	}
	s, err = SectionSlug(s) // in any case: Command Line is command-line
	if err != nil {
		return nil, m.errf(keyAt("menu"), "%s", err)
	}
	hex := ""
	if c := fm["tint"]; c != "" {
		if hex = ColorHex(strings.ToLower(c)); hex == "" {
			return nil, m.errf(keyAt("tint"), "there's no tint called %s: use one of %s", c, colorNames())
		}
	}
	name, desc, err := m.title()
	if err != nil {
		return nil, err
	}
	for ; m.at < len(m.lines); m.at++ {
		if strings.TrimSpace(m.lines[m.at]) != "" {
			return nil, m.errf(m.at, "_pack.md has a # Name and a description; commands go in a module file next to it")
		}
	}
	return &packInfo{Name: name, Description: desc, Logo: strings.ToLower(fm["logo"]), Color: hex, Section: s, Category: SectionName(s), Requires: splitList(fm["needs"])}, nil
}

// parseModule reads a module's file: its commands.
func parseModule(path string, data []byte) (*Group, error) {
	m := &mdLines{path: path, lines: stripComments(strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"))}
	fm, err := m.frontMatter("needs", "when")
	if err != nil {
		return nil, err
	}
	name, desc, err := m.title()
	if err != nil {
		return nil, err
	}
	g := &Group{Name: name, Description: desc, Requires: splitList(fm["needs"]), When: splitList(fm["when"])}
	for m.at < len(m.lines) {
		l := m.lines[m.at]
		switch {
		case strings.TrimSpace(l) == "":
			m.at++
		case strings.HasPrefix(l, "## "):
			at := m.at
			c, err := m.command()
			if err != nil {
				return nil, err
			}
			for _, o := range g.Commands {
				if Anchor(o.Name) == Anchor(c.Name) { // a recipe's link to either would be the same
					return nil, m.errf(at, "there's already a command called %s in this file", o.Name)
				}
			}
			c.Source = path
			g.Commands = append(g.Commands, *c)
		case strings.HasPrefix(l, "# "):
			return nil, m.errf(m.at, "a file has one # Name; commands are ## headings")
		default:
			return nil, m.errf(m.at, "this belongs under a command's ## heading")
		}
	}
	return g, nil
}

// command reads one ## command, up to the next heading.
func (m *mdLines) command() (*Command, error) {
	head := m.at
	c := &Command{Name: strings.TrimSpace(m.lines[m.at][3:])}
	if t := strings.TrimSpace(strings.TrimRight(c.Name, "?!")); t != c.Name {
		return nil, m.errf(m.at, "a command's risk is a > [!WARNING] or > [!CAUTION] callout under its heading, not a ? or ! in its name")
	}
	if !nameCharsRe.MatchString(c.Name) {
		return nil, m.errf(m.at, "command names use only letters, digits, spaces and ( ) (not %q)", c.Name)
	}
	m.at++
	var desc []string
	fenced, listed := false, false
	var blanks []Arg
	for m.at < len(m.lines) {
		l := m.lines[m.at]
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "#"):
			if !strings.HasPrefix(l, "# ") && !strings.HasPrefix(l, "## ") {
				return nil, m.errf(m.at, "packs use # for the file's name and ## for commands, nothing smaller")
			}
			goto done
		case t == "":
			m.at++
		case strings.HasPrefix(t, ">"):
			if fenced {
				return nil, m.errf(m.at, "a > [!WARNING] or > [!CAUTION] callout goes under the ## heading, before the code block")
			}
			if c.Risk != "" {
				return nil, m.errf(m.at, "a command has one callout: > [!WARNING] (asks first) or > [!CAUTION] (dangerous)")
			}
			switch strings.TrimSpace(strings.TrimPrefix(t, ">")) {
			case "[!WARNING]":
				c.Risk = RiskWarn
			case "[!CAUTION]":
				c.Risk = RiskDanger
			default:
				return nil, m.errf(m.at, "the only callouts are > [!WARNING] (asks first) and > [!CAUTION] (dangerous)")
			}
			for m.at++; m.at < len(m.lines) && strings.HasPrefix(strings.TrimSpace(m.lines[m.at]), ">"); m.at++ {
				if s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m.lines[m.at]), ">")); s != "" {
					desc = append(desc, s)
				}
			}
		case strings.HasPrefix(t, "```"):
			if fenced {
				return nil, m.errf(m.at, "a command has one code block: join steps with && in it")
			}
			if listed {
				return nil, m.errf(m.at, "the code block comes before the list of blanks")
			}
			fenced = true
			if err := m.fence(c, t); err != nil {
				return nil, err
			}
		case listItemRe.MatchString(l):
			if !fenced {
				return nil, m.errf(m.at, "the list of blanks comes after the command's code block")
			}
			listed = true
			a, err := m.blank(c)
			if err != nil {
				return nil, err
			}
			blanks = append(blanks, *a)
		default:
			if fenced {
				return nil, m.errf(m.at, "the description goes before the code block")
			}
			if err := m.plainText(m.at, l); err != nil {
				return nil, err
			}
			desc = append(desc, t)
			m.at++
		}
	}
done:
	if !fenced {
		return nil, m.errf(head, "%s has no code block: put what it runs in ```sh … ```", c.Name)
	}
	c.Desc = strings.Join(desc, " ")
	c.Args = blanks
	for _, pm := range placeholderRe.FindAllStringSubmatch(c.Cmd, -1) {
		if !c.Known(pm[1]) {
			return nil, m.errf(head, "%s uses <%s>: list it after the code block, like - `<%s>` Question", c.Name, pm[1], pm[1])
		}
	}
	return c, nil
}

// fence reads a command's code block: its settings, then its lines, a
// trailing \ joining a line to the next.
func (m *mdLines) fence(c *Command, open string) error {
	start := m.at
	info := strings.Fields(strings.TrimPrefix(open, "```"))
	if len(info) > 0 && !strings.Contains(info[0], "=") {
		if !fenceLangs[info[0]] {
			return m.errf(m.at, "a code block's language is sh, bash, shell, zsh or nothing (not %s)", info[0])
		}
		info = info[1:]
	}
	for _, kv := range info {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case k == "needs" && v != "":
			c.Requires = append(c.Requires, splitList(v)...)
		case k == "when" && v != "":
			c.When = append(c.When, splitList(v)...)
		default:
			return m.errf(m.at, "after the language, a code block can only have needs=… and when=… (not %s)", kv)
		}
	}
	var code []string
	for m.at++; ; m.at++ {
		if m.at >= len(m.lines) {
			return m.errf(start, "this code block has no closing ```")
		}
		l := m.lines[m.at]
		if strings.TrimSpace(l) == "```" {
			m.at++
			break
		}
		code = append(code, l)
	}
	var cmd strings.Builder
	for i, l := range code {
		if i > 0 {
			cmd.WriteString(" ")
			l = strings.TrimLeft(l, " \t")
		}
		if i < len(code)-1 {
			if !strings.HasSuffix(l, "\\") {
				return m.errf(start+1+i, "a code block is one command: end a line with \\ to carry on to the next, or join steps with &&")
			}
			l = strings.TrimRight(strings.TrimSuffix(l, "\\"), " \t")
		}
		cmd.WriteString(l)
	}
	c.Cmd = strings.TrimSpace(cmd.String())
	return nil
}

// blank reads a `<name>` Question list item and the choices under it.
func (m *mdLines) blank(c *Command) (*Arg, error) {
	l := m.lines[m.at]
	lm := listItemRe.FindStringSubmatch(l)
	if lm[2] != "-" {
		return nil, m.errf(m.at, "lists use - for bullets")
	}
	if lm[1] != "" {
		return nil, m.errf(m.at, "choices go under a `<blank>` item")
	}
	bm := blankRe.FindStringSubmatch(strings.TrimSpace(l[len(lm[0]):]))
	if bm == nil {
		return nil, m.errf(m.at, "a blank is written - `<name>` Question, with the name in backticks")
	}
	a := &Arg{Name: bm[1], Prompt: strings.TrimSpace(bm[2]), Optional: true}
	kind := ""
	var ticked []string
	for m.at++; m.at < len(m.lines); m.at++ {
		l := m.lines[m.at]
		cm := listItemRe.FindStringSubmatch(l)
		if strings.TrimSpace(l) == "" || cm == nil || cm[1] == "" {
			break
		}
		if cm[2] != "-" {
			return nil, m.errf(m.at, "lists use - for bullets")
		}
		ch := choiceRe.FindStringSubmatch(strings.TrimSpace(l[len(cm[0]):]))
		if ch == nil {
			return nil, m.errf(m.at, "a choice is ( ) or [ ], then what it types in backticks, then a label if you like: ( ) `bash`")
		}
		k := "choose"
		if ch[1] == "[" {
			k = "flags"
		}
		if ch[1] == "(" && ch[3] != ")" || ch[1] == "[" && ch[3] != "]" {
			return nil, m.errf(m.at, "a choice is ( ) to select one or [ ] to select many")
		}
		if kind != "" && k != kind {
			return nil, m.errf(m.at, "a blank's choices are all ( ) (select one) or all [ ] (select many)")
		}
		kind = k
		a.Options = append(a.Options, Option{Value: ch[4], Label: strings.TrimSpace(ch[5])})
		if ch[2] == "x" {
			if k == "choose" && a.Default != "" {
				return nil, m.errf(m.at, "only one ( ) choice can start picked")
			}
			if k == "choose" {
				a.Default = ch[4]
			} else {
				ticked = append(ticked, ch[4])
			}
		}
	}
	switch {
	case kind == "flags":
		a.Type = "flags"
		a.Default = strings.Join(ticked, " ")
	case kind == "choose" && len(a.Options) == 1 && a.Default != "":
		// One choice, picked: a typed blank that starts filled in.
		a.Default, a.Options = a.Options[0].Value, nil
		if IsSecretName(a.Name) {
			a.Type, a.Quote = "secret", true
		}
	case kind == "choose":
		a.Type = "choose"
	case IsSecretName(a.Name):
		a.Type, a.Quote = "secret", true
	}
	return a, nil
}

// parsePackFile reads one file of a pack, going by its name.
func parsePackFile(path string, data []byte) (parsedFile, error) {
	if filepath.Base(path) == PackInfoFile {
		info, err := parsePack(path, data)
		return parsedFile{info: info}, err
	}
	g, err := parseModule(path, data)
	if err != nil {
		return parsedFile{}, err
	}
	return parsedFile{group: g}, nil
}
