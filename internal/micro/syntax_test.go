package micro

import (
	"regexp"
	"strings"
	"testing"
)

// micro draws packs with clingon's own syntax for them, and every colour group
// it uses has a colour in clingon's scheme (an unknown one would be plain).
func TestPackSyntaxColoursAreAllDefined(t *testing.T) {
	syntax, err := files.ReadFile("config/syntax/markdown.yaml")
	if err != nil {
		t.Fatal(err)
	}
	scheme, err := files.ReadFile("config/colorschemes/clingon.micro")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(syntax), "filetype: markdown") || !strings.Contains(string(syntax), `filename: "\\.md$"`) {
		t.Fatal("the syntax should replace micro's own Markdown, for .md files")
	}
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^color-link (\S+)`).FindAllStringSubmatch(string(scheme), -1) {
		defined[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`(?m)^\s*- ([a-z.]+):`).FindAllStringSubmatch(string(syntax), -1) {
		if !defined[m[1]] {
			t.Errorf("%s is used in syntax/markdown.yaml but has no colour in clingon.micro", m[1])
		}
	}
}

// micro gets clingon's plugin, which checks a pack as you type with the
// clingon that opened it.
func TestCheckingPlugin(t *testing.T) {
	src, err := files.ReadFile("config/plug/clingon/clingon.lua")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`os.Getenv("CLINGON_CHECK")`, `"_check-file"`, "function onAnyEvent", `"clingon-gutter"`, `"ISSUE: "`, "AddMessage"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("the plugin should have %s", want)
		}
	}
}

// The chip and the gutter's marks have clingon's colours: an issue on orange,
// like the status bar, and the mark that keeps the gutter's room unseen.
func TestIssueColours(t *testing.T) {
	scheme, _ := files.ReadFile("config/colorschemes/clingon.micro")
	for _, want := range []string{`color-link error-message "bold 233,215"`, `color-link gutter-error "bold 215"`, `color-link gutter-info "233"`} {
		if !strings.Contains(string(scheme), want) {
			t.Errorf("missing %s", want)
		}
	}
}
