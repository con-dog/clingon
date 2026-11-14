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

// A closed watch holds nothing, however it's used: closed with no Wait ever
// made, closed after a Wait has come back with a change, or closed under a
// Wait that's asleep. (The menu waits for each command's prompt on one, so a
// leak here is four descriptors a command.)
func TestWatchClosedHoldsNothing(t *testing.T) {
	open := func() int {
		fds, err := os.ReadDir("/dev/fd")
		if err != nil {
			t.Skip(err)
		}
		return len(fds)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "ready")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	mark := func() {
		f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte{'.'})
		_ = f.Close()
	}
	before := open()
	for range 50 {
		w, err := WatchPath(file) // never waited on
		if err != nil {
			t.Fatal(err)
		}
		w.Close()
		w.Close()
		if w.Wait() {
			t.Fatal("closed: Wait says false")
		}

		w, _ = WatchPath(file) // a change heard, then closed with nothing asleep
		done := make(chan bool)
		go func() { done <- w.Wait() }()
		mark()
		if !<-done {
			t.Fatal("a write should wake it")
		}
		w.Close()

		w, _ = WatchPath(file) // closed under a Wait (asleep, or about to be)
		go func() { done <- w.Wait() }()
		w.Close()
		if <-done {
			t.Fatal("closed: Wait says false")
		}
	}
	if after := open(); after > before+2 {
		t.Errorf("%d descriptors open before, %d after: closed watches still hold some", before, after)
	}
}
