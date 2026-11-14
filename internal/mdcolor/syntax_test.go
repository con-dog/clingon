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
	used := append([]rule{}, packRules...)
	for _, r := range packRegions {
		used = append(append(used, rule{group: r.group}), r.rules...)
	}
	if len(used) < 20 {
		t.Fatalf("only %d rules", len(used))
	}
	for _, r := range used {
		g := r.group
		for !defined[g] && strings.Contains(g, ".") { // special.blank falls back to special
			g = g[:strings.LastIndex(g, ".")]
		}
		if !defined[g] {
			t.Errorf("%s is used in rules.go but has no colour in colours.txt", r.group)
		}
	}
}
