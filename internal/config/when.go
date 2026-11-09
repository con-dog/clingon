package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Here answers `when:` questions about one folder: is one of these files
// in it, or in a folder above it (`./x`: in it, only)? Answers are kept, so
// asking for every command in the menu stays cheap.
type Here struct {
	Dir  string
	seen map[string]bool
}

func NewHere(dir string) *Here { return &Here{Dir: dir, seen: map[string]bool{}} }

// Unmet says why c can't be used in this folder, or "" if it can.
func (h *Here) Unmet(c *Command) string {
	if len(c.When) == 0 || h == nil || h.Dir == "" {
		return ""
	}
	for _, pat := range c.When {
		if h.found(pat) {
			return ""
		}
	}
	where, names := " in this folder.", make([]string, len(c.When))
	for i, pat := range c.When {
		names[i] = strings.TrimPrefix(pat, "./")
		if names[i] == pat {
			where = " in this folder or one above it."
		}
	}
	return "Needs " + strings.Join(names, " or ") + where
}

func (h *Here) found(pat string) bool {
	if v, ok := h.seen[pat]; ok {
		return v
	}
	var ok bool
	if name, here := strings.CutPrefix(pat, "./"); here {
		ok = matchIn(filepath.Clean(h.Dir), name)
	} else {
		for dir := filepath.Clean(h.Dir); ; dir = filepath.Dir(dir) {
			if ok = matchIn(dir, pat); ok || dir == filepath.Dir(dir) {
				break
			}
		}
	}
	h.seen[pat] = ok
	return ok
}

// matchIn reports whether dir holds a file matching pat. Only pat is a
// pattern: a folder called proj[1] is just a name.
func matchIn(dir, pat string) bool {
	if !strings.ContainsAny(pat, "*?[") {
		_, err := os.Stat(filepath.Join(dir, pat))
		return err == nil
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if m, _ := filepath.Match(pat, e.Name()); m {
			return true
		}
	}
	return false
}
