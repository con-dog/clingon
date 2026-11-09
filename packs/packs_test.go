package packs

import (
	"io/fs"
	"testing"
)

// Every pack is built in whole: its _pack.md (a file embedding a folder
// would leave out) and at least one module.
func TestEveryPackIsBuiltIn(t *testing.T) {
	dirs, err := fs.ReadDir(FS, ".")
	if err != nil || len(dirs) < 10 {
		t.Fatalf("packs: %d, %v", len(dirs), err)
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if _, err := fs.Stat(FS, d.Name()+"/_pack.md"); err != nil {
			t.Errorf("%s: no _pack.md built in", d.Name())
		}
		if mods, _ := fs.Glob(FS, d.Name()+"/[^_]*.md"); len(mods) == 0 {
			t.Errorf("%s: no modules built in", d.Name())
		}
	}
}
