package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// Each window's ready and changed files are its own, named for its
// launcher: two windows never share one. Those of a window that's gone are
// swept; a running one's are left.
func TestSessionFilesAreEachWindowsOwn(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if SessionFile("ready", 1) == SessionFile("ready", 2) || SessionFile("ready", 1) == SessionFile("changed", 1) {
		t.Fatal("two windows, or a window's two files, share a name")
	}
	gone := exec.Command("/usr/bin/true")
	if err := gone.Run(); err != nil {
		t.Skip(err)
	}
	dead, live := gone.Process.Pid, os.Getpid()
	var files []string
	for _, pid := range []int{dead, live} {
		files = append(files, SessionFile("ready", pid), SessionFile("ready", pid)+"-heard", SessionFile("changed", pid))
	}
	other := filepath.Join(filepath.Dir(files[0]), "clingon-"+strconv.Itoa(dead)) // a socket: not this sweep's
	for _, f := range append(files, other) {
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	SweepSessionFiles()
	for i, f := range files {
		if _, err := os.Stat(f); (err == nil) != (i >= 3) {
			t.Errorf("%s: there=%v, want the gone window's swept and the running one's kept", filepath.Base(f), err == nil)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("only ready and changed files are swept")
	}
	w, err := WatchChanged(SessionFile("changed", live))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
}
