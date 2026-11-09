package mdcolor

import (
	"regexp"
	"strings"
	"testing"
)

// Every colour group the rules use has a colour (an unknown one would be
// plain).
func TestRulesColoursAreAllDefined(t *testing.T) {
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^color-link (\S+)`).FindAllStringSubmatch(coloursFile, -1) {
		defined[m[1]] = true
	}
	used := regexp.MustCompile(`(?m)^\s*- ([a-z.]+):`).FindAllStringSubmatch(rulesFile, -1)
	if len(used) == 0 {
		t.Fatal("no rules read")
	}
	for _, m := range used {
		g := m[1]
		for !defined[g] && strings.Contains(g, ".") { // special.blank falls back to special
			g = g[:strings.LastIndex(g, ".")]
		}
		if !defined[g] {
			t.Errorf("%s is used in markdown.yaml but has no colour in colours.txt", m[1])
		}
	}
}
