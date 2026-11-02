package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Missing is a tool a section needs that isn't on PATH. Elsewhere is
// where it was found instead, if it's installed in a usual place that
// PATH leaves out; "" means it isn't installed at all, as far as we know.
type Missing struct {
	Tool, Elsewhere string
}

// Needs says what tools are missing and why each can't be run, the same
// everywhere: "needs cargo (not installed), gopls (not on PATH: it's in
// ~/go/bin)".
func Needs(miss []Missing) string {
	var parts []string
	for _, x := range miss {
		why := "not installed"
		if x.Elsewhere != "" {
			where := x.Elsewhere
			if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(where, home+"/") {
				where = "~" + where[len(home):]
			}
			why = "not on PATH: it's in " + where
		}
		parts = append(parts, x.Tool+" ("+why+")")
	}
	return "needs " + strings.Join(parts, ", ")
}

// usualBins are where installers commonly put commands, for telling
// "not installed" from "not on your PATH".
func usualBins() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/local/go/bin"}
	if home != "" {
		for _, d := range []string{".local/bin", "bin", "go/bin", ".cargo/bin", ".bun/bin", ".deno/bin", ".volta/bin", ".npm-global/bin", ".claude/local"} {
			dirs = append(dirs, filepath.Join(home, d))
		}
	}
	return dirs
}

// MissingTools checks which of tools can't be run from PATH.
func MissingTools(tools []string) []Missing {
	var out []Missing
	for _, t := range tools {
		if _, err := exec.LookPath(t); err == nil {
			continue
		}
		m := Missing{Tool: t}
		for _, d := range usualBins() {
			if fi, err := os.Stat(filepath.Join(d, t)); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				m.Elsewhere = d
				break
			}
		}
		out = append(out, m)
	}
	return out
}
