// Package micro holds clingon's own settings for the micro editor, which
// edits users' packs. They live in clingon's config folder, so the user's
// ~/.config/micro is never read or changed.
package micro

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed config
var files embed.FS

// ConfigDir writes clingon's micro config into base/micro (when changed)
// and returns that folder, to pass to `micro -config-dir`.
func ConfigDir(base string) (string, error) {
	dir := filepath.Join(base, "micro")
	err := fs.WalkDir(files, "config", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, _ := files.ReadFile(path)
		dst := filepath.Join(dir, filepath.FromSlash(path[len("config/"):]))
		if have, err := os.ReadFile(dst); err == nil && bytes.Equal(have, want) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, want, 0o644)
	})
	return dir, err
}
