package micro

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigDirWritesOnlyWhatChanged(t *testing.T) {
	base := t.TempDir()
	dir, err := ConfigDir(base)
	if err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.json")
	if _, err := os.Stat(settings); err != nil {
		t.Fatalf("settings.json should be written: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(settings, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigDir(base); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(settings); !fi.ModTime().Equal(old) {
		t.Error("an unchanged file shouldn't be rewritten")
	}
	if err := os.WriteFile(settings, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigDir(base); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(settings); string(b) == "{}" {
		t.Error("a changed file should be put back")
	}
}
