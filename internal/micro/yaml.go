package micro

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// YAML colours a pack as micro shows it: micro's own rules for YAML (its
// runtime/syntax/yaml.yaml), applied the way micro applies them, in clingon's
// colour scheme. That means its quirks too: the y in x.y is a boolean to it.
func YAML(src string) string {
	var out strings.Builder
	var in *region // a string left open carries on to the next line
	for i, line := range strings.Split(src, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		cells := make([]string, len(line))
		if in != nil {
			in = paintRegion(cells, line, 0, line, in)
		} else {
			in = paintPlain(cells, line, 0, line)
		}
		for at := 0; at < len(line); {
			end := at + 1
			for end < len(line) && cells[end] == cells[at] {
				end++
			}
			if sgr := colours[cells[at]]; sgr != "" {
				out.WriteString("\x1b[" + sgr + "m" + line[at:end] + "\x1b[m")
			} else {
				out.WriteString(line[at:end])
			}
			at = end
		}
	}
	return out.String()
}

type pattern struct {
	group string
	re    *regexp.Regexp
}

type region struct {
	group            string
	start, end, skip *regexp.Regexp
	patterns         []pattern
}

// In micro's order: where patterns overlap, the later one wins.
var (
	yamlPatterns = []pattern{
		{"type", regexp.MustCompile(`(^| )!!(binary|bool|float|int|map|null|omap|seq|set|str) `)},
		{"constant", regexp.MustCompile(`\b(YES|yes|Y|y|ON|on|TRUE|True|true|NO|no|N|n|OFF|off|FALSE|False|false)\b`)},
		{"statement", regexp.MustCompile(`(:[[:space:]]|\[|\]|:[[:space:]]+[|>]|^[[:space:]]*- )`)},
		{"identifier", regexp.MustCompile(`[[:space:]][\*&][A-Za-z0-9]+`)},
		{"type", regexp.MustCompile(`[-.\w]+:`)},
		{"statement", regexp.MustCompile(`:`)},
		{"special", regexp.MustCompile(`(^---|^\.\.\.|^%YAML|^%TAG)`)},
	}
	escapes     = []pattern{{"constant", regexp.MustCompile(`\\.`)}}
	yamlRegions = []*region{
		{"constant.string", regexp.MustCompile(`(^| )"`), regexp.MustCompile(`"`), regexp.MustCompile(`\\.`), escapes},
		{"constant.string", regexp.MustCompile(`(^| )'`), regexp.MustCompile(`'`), regexp.MustCompile(`(\\.)|('')`), escapes},
		{"comment", regexp.MustCompile(`#`), regexp.MustCompile(`$`), nil, []pattern{{"todo", regexp.MustCompile(`(TODO|XXX|FIXME):?`)}}},
	}
)

// paintPlain colours line, a piece of full starting at at, outside any region.
// It says which region is left open at the end of the line, if any.
func paintPlain(cells []string, full string, at int, line string) *region {
	var first *region
	var loc []int
	for _, r := range yamlRegions {
		if l := r.start.FindStringIndex(line); l != nil && (loc == nil || l[0] < loc[0]) {
			first, loc = r, l
		}
	}
	if first != nil {
		fill(cells, at+loc[0], at+loc[1], first.group)
		paintPlain(cells, full, at, line[:loc[0]])
		return paintRegion(cells, full, at+loc[1], line[loc[1]:], first)
	}
	applyPatterns(cells, at, line, yamlPatterns)
	return nil
}

// paintRegion colours line, a piece of full starting at at, inside region r.
// Going by what's left of the line means ^ matches again after a string has
// closed, as it does in micro. So does micro's slip: it compares where the piece
// starts in the line with where the end is in the piece, and if they're the same
// leaves out the escapes and the like, on any line that happens to fall that way.
func paintRegion(cells []string, full string, at int, line string, r *region) *region {
	loc := findEnd(r, line)
	if loc == nil {
		fill(cells, at, at+len(line), r.group)
		applyPatterns(cells, at, line, r.patterns)
		return r
	}
	fill(cells, at, at+loc[1], r.group)
	if utf8.RuneCountInString(full[:at]) != utf8.RuneCountInString(line[:loc[0]]) {
		applyPatterns(cells, at, line[:loc[0]], r.patterns)
	}
	return paintPlain(cells, full, at+loc[1], line[loc[1]:])
}

// findEnd looks for r's end with whatever it should skip blanked out.
func findEnd(r *region, line string) []int {
	if r.skip != nil {
		line = r.skip.ReplaceAllStringFunc(line, func(m string) string { return strings.Repeat("\x00", len(m)) })
	}
	return r.end.FindStringIndex(line)
}

func applyPatterns(cells []string, at int, line string, patterns []pattern) {
	for _, p := range patterns {
		for _, m := range p.re.FindAllStringIndex(line, -1) {
			fill(cells, at+m[0], at+m[1], p.group)
		}
	}
}

func fill(cells []string, from, to int, group string) {
	for i := from; i < to; i++ {
		cells[i] = group
	}
}

var colourLink = regexp.MustCompile(`(?m)^color-link (\S+) "(bold )?(\d+)"$`)

// colours is SGR codes by group, from the colour scheme micro itself is given.
var colours = func() map[string]string {
	scheme, err := files.ReadFile("config/colorschemes/clingon.micro")
	if err != nil {
		panic(err)
	}
	out := map[string]string{}
	for _, m := range colourLink.FindAllStringSubmatch(string(scheme), -1) {
		sgr := "38;5;" + m[3]
		if m[2] != "" {
			sgr = "1;" + sgr
		}
		out[m[1]] = sgr
	}
	return out
}()
