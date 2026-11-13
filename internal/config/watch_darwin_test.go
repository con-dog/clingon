package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A watch wakes when a file in its folder is written as writeWhole writes,
// and Close wakes it to let go. Nothing polls: Wait sleeps until then.
func TestDirWatchWakesOnWrite(t *testing.T) {
	dir := t.TempDir()
	w, err := WatchPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	woke := make(chan bool, 1)
	go func() { woke <- w.Wait() }()
	if err := writeWhole(filepath.Join(dir, "pack-form.name"), []byte("Demo\nOther\n1 2")); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-woke:
		if !ok {
			t.Fatal("woke saying closed, on a write")
		}
	case <-time.After(5 * time.Second): // only so a broken watch fails rather than hangs
		t.Fatal("a write didn't wake it")
	}
	go func() { woke <- w.Wait() }()
	w.Close()
	select {
	case ok := <-woke:
		if ok {
			t.Fatal("Close should say false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close didn't wake it")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
