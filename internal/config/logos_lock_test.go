package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// logos.lock records the codepoint each logo and centred symbol has ever
// had. A codepoint must never change meaning: a terminal still using an
// older Clingon Mono would draw the wrong logo (it happened: removing two
// logos shifted the rest, and Claude Code showed the AWS logo).
//
// After adding a logo or symbol: UPDATE_LOGOS_LOCK=1 go test ./internal/config/
const logosLock = "logos.lock"

func TestLogoCodepointsNeverMove(t *testing.T) {
	now := map[string]string{}
	for name, s := range Logos {
		now["logo "+name] = fmt.Sprintf("U+%04X", []rune(s)[0])
	}
	for sym, s := range Centred {
		now["centred "+sym] = fmt.Sprintf("U+%04X", []rune(s)[0])
	}
	locked := map[string]string{}
	if b, err := os.ReadFile(logosLock); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if f := strings.Fields(line); len(f) == 3 {
				locked[f[0]+" "+f[1]] = f[2]
			}
		}
	}
	taken := map[string]string{} // codepoint → what had it first
	for k, cp := range locked {
		taken[cp] = k
	}
	for k, cp := range now {
		switch was, ok := locked[k]; {
		case ok && was != cp:
			t.Errorf("%s moved from %s to %s: append new logos to LOGOS/SYMBOLS in build.py, never reorder or remove", k, was, cp)
		case !ok && taken[cp] != "":
			t.Errorf("%s reuses %s, which was %s's", k, cp, taken[cp])
		case !ok && os.Getenv("UPDATE_LOGOS_LOCK") == "":
			t.Errorf("%s isn't in %s: run UPDATE_LOGOS_LOCK=1 go test ./internal/config/", k, logosLock)
		}
	}
	if os.Getenv("UPDATE_LOGOS_LOCK") != "" && !t.Failed() {
		for k, cp := range now { // dropped logos stay: their codepoints stay taken
			locked[k] = cp
		}
		var lines []string
		for k, cp := range locked {
			lines = append(lines, k+" "+cp)
		}
		sort.Strings(lines)
		if err := os.WriteFile(logosLock, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
