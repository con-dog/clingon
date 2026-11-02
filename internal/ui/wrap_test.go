package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWrapSpacesKeepsFlagsWhole(t *testing.T) {
	got := wrapSpaces("find . -type f -name '*.tmp' -print -delete", 38)
	if strings.Join(got, "|") != "find . -type f -name '*.tmp' -print|-delete" {
		t.Fatalf("got %q", got)
	}
	long := wrapSpaces("abcdefghij", 4)
	if strings.Join(long, "|") != "abcd|efgh|ij" {
		t.Fatalf("got %q", long)
	}
}

func TestBlockTailKeepsTheEnd(t *testing.T) {
	got := ansi.Strip(block("one two three four five six", 10, 2, true))
	if got != "… four\nfive six" {
		t.Fatalf("got %q", got)
	}
	got = ansi.Strip(block("one two three four five six", 10, 2, false))
	if got != "one two\nthree fou…" {
		t.Fatalf("got %q", got)
	}
}
