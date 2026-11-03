package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
)

// output runs f with stdout captured.
func output(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	ferr := f()
	_ = w.Close()
	os.Stdout = old
	return <-done, ferr
}

func TestPackCommands(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	run := func(args ...string) string {
		t.Helper()
		out, err := output(t, func() error { return packCmd(args) })
		if err != nil {
			t.Fatalf("pack %v: %v", args, err)
		}
		return out
	}
	if out := run("add", "git"); !strings.Contains(out, "installed git") {
		t.Fatalf("add: %q", out)
	}
	if out := run("add", "git"); !strings.Contains(out, "already installed") {
		t.Fatalf("adding twice: %q", out)
	}
	if out := run("list", "--names"); !strings.Contains(out, "remote/git") {
		t.Fatalf("list: %q", out)
	}
	if out := run("available", "--names"); strings.Contains(out, "git\t") || !strings.Contains(out, "docker") {
		t.Fatalf("available lists what isn't installed: %q", out)
	}
	if out := run("update"); !strings.Contains(out, "up to date") {
		t.Fatalf("update with nothing newer: %q", out)
	}
	if _, err := output(t, func() error { return packCmd([]string{"add", "no-such-pack"}) }); err == nil {
		t.Fatal("adding an unknown pack should fail")
	}
	run("remove", "git")
	if out := run("list", "--names"); strings.Contains(out, "git") {
		t.Fatalf("removed, but still listed: %q", out)
	}
}

func TestCommandCommands(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	add := []string{"add", "mine", "Deploy", "Tail logs", "kubectl logs -f deploy/<app>", "Streams an app's logs."}
	if out, err := output(t, func() error { return commandCmd(add) }); err != nil || !strings.Contains(out, `added "Tail logs"`) {
		t.Fatalf("add: %q %v", out, err)
	}
	if out, _ := output(t, func() error { return commandCmd([]string{"list"}) }); !strings.Contains(out, "Tail logs") {
		t.Fatalf("list: %q", out)
	}
	if _, err := output(t, func() error { return commandCmd([]string{"add", "mine", "x"}) }); err == nil {
		t.Fatal("too few arguments should say how to use it")
	}
	if _, err := output(t, func() error { return commandCmd([]string{"add", "remote/git", "S", "N", "ls"}) }); err == nil {
		t.Fatal("installed packs are overwritten on update: adding to one should be refused")
	}
	if out, err := output(t, func() error { return commandCmd([]string{"remove", "mine/Deploy/Tail logs"}) }); err != nil || !strings.Contains(out, "removed") {
		t.Fatalf("remove: %q %v", out, err)
	}
}

func TestCheckIncludesTheProjectPack(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	t.Chdir(repo)
	if err := os.WriteFile(filepath.Join(repo, config.ProjectFile), []byte("sections:\n  - name: Deploy\n    groups:\n      - name: Prod\n        commands:\n          - name: Ship\n            cmd: ./ship.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := output(t, check)
	if err != nil || !strings.Contains(out, "project pack") || !strings.Contains(out, "asks before using it") {
		t.Fatalf("got %q %v", out, err)
	}
	if err := os.WriteFile(filepath.Join(repo, config.ProjectFile), []byte("sections:\n  - name: Deploy\n    commands:\n      - name: Ship\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := output(t, check); err == nil || !strings.Contains(err.Error(), "project pack has problems") {
		t.Fatalf("a broken project pack should fail the check, got %v", err)
	}
}

func TestOtherShellsAreRefusedBeforeAnyWindowOpens(t *testing.T) {
	t.Setenv("CLINGON", "")
	t.Setenv("SHELL", "/bin/tcsh")
	if err := launch(); err == nil || !strings.Contains(err.Error(), "zsh or bash") {
		t.Fatalf("got %v", err)
	}
}

func TestDoctor(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.Install("git"); err != nil {
		t.Fatal(err)
	}
	out, _ := output(t, func() error { return doctor(nil) })
	for _, want := range []string{"packs    valid", "micro"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor should mention %q:\n%s", want, out)
		}
	}
	out, _ = output(t, func() error { return doctor([]string{"git"}) })
	if !strings.Contains(out, "git") {
		t.Errorf("doctor git:\n%s", out)
	}
}

func TestCompletionScriptsParse(t *testing.T) {
	for shell, check := range map[string][]string{"bash": {"bash", "-n"}, "zsh": {"zsh", "-n"}} {
		out, err := output(t, func() error { return completion([]string{shell}) })
		if err != nil || !strings.Contains(out, "clingon") {
			t.Fatalf("%s: %v", shell, err)
		}
		if _, err := exec.LookPath(check[0]); err != nil {
			continue // that shell isn't installed here
		}
		c := exec.Command(check[0], check[1:]...)
		c.Stdin = strings.NewReader(out)
		if msg, err := c.CombinedOutput(); err != nil {
			t.Errorf("%s completion doesn't parse: %v\n%s", shell, err, msg)
		}
	}
	if _, err := output(t, func() error { return completion([]string{"tcsh"}) }); err == nil {
		t.Error("an unknown shell should be an error")
	}
}

func TestHeadlinePicksTheMostUsefulNews(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // no tools, so no pack suggestions
	old := version
	version = "v0.1.0"
	defer func() { version = old }()

	if h := headline(); !strings.HasPrefix(h.Text, "✓ All set: ") {
		t.Fatalf("nothing to do: all's well, got %+v", h)
	}
	if err := config.Install("git"); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(config.Dir(), "packs", "remote", "git.yaml")
	data, _ := os.ReadFile(remote)
	if err := os.WriteFile(remote, []byte(strings.Replace(string(data), "version: ", "version: 0.0.0-old-", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if h := headline(); !strings.Contains(h.Text, "Update 1 pack: git · Clingon › Packs › Update packs") {
		t.Fatalf("a pack update beats all's well, got %+v", h)
	}
	if err := os.WriteFile(releaseCachePath(), []byte(strconv.FormatInt(time.Now().Unix(), 10)+" v0.2.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if h := headline(); !strings.Contains(h.Text, "Update clingon: v0.1.0 → v0.2.0") {
		t.Fatalf("a new release beats everything, got %+v", h)
	}
}

func TestPacksWithAToolPerGroupAreSuggestedOnlyIfOneIsHere(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if suggest, _ := packAdvice(); slices.Contains(suggest, "package-managers") {
		t.Fatal("no brew: package-managers isn't worth suggesting")
	}
	if err := os.WriteFile(filepath.Join(bin, "brew"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if suggest, _ := packAdvice(); !slices.Contains(suggest, "package-managers") {
		t.Fatalf("brew is here: package-managers is worth suggesting, got %v", suggest)
	}
}

func TestAFailedReleaseCheckKeepsWhatItKnew(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // no curl: the check fails
	if err := os.WriteFile(releaseCachePath(), []byte("1 v9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkRelease()
	if c := readReleaseCache(); c.Latest != "v9.9.9" || time.Since(c.Checked) > time.Minute {
		t.Fatalf("offline isn't \"no update\": got %+v", c)
	}
}

func TestDoctorCarriesOnPastAPackItDoesntKnow(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.Install("git"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.Dir(), "packs", "remote", "homemade.yaml"), []byte("sections:\n  - name: H\n    commands:\n      - name: x\n        cmd: ls\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := output(t, func() error { return doctor(nil) })
	if !strings.Contains(out, "homemade") || !strings.Contains(out, "git") {
		t.Fatalf("both packs should be reported:\n%s", out)
	}
}

func TestPackUpdateRemovesRetiredPacks(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(config.RemotePath("databases")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.RemotePath("databases"), []byte("version: 1.0.0\nsections: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := output(t, func() error { return packCmd([]string{"update"}) })
	if err != nil || !strings.Contains(out, "removed databases") {
		t.Fatalf("update should remove a retired pack, got %q, %v", out, err)
	}
	if config.IsInstalled("databases") {
		t.Error("the retired pack is still installed")
	}
}

func TestCrashIsKeptSaidAndShown(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	// A crash whose process died before it could keep it: a dead pid's file.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	tmp := func(pid int) string {
		return filepath.Join(config.Dir(), "crash-"+strconv.Itoa(pid)+".tmp")
	}
	trace := "goroutine 1 [running]:\ngithub.com/con-dog/clingon/internal/ui.(*Model).update(0x1)\n\tgithub.com/con-dog/clingon/internal/ui/model.go:552 +0x10\n"
	if err := os.WriteFile(tmp(dead.Process.Pid), []byte(crashHeader+"0.1.0\npanic: boom\n\n"+trace), 0o600); err != nil {
		t.Fatal(err)
	}
	// A clean exit's file, and a running session's: neither is a crash.
	empty := exec.Command("true")
	if err := empty.Run(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(tmp(empty.Process.Pid), []byte(crashHeader+"0.1.0\n"), 0o600)
	_ = os.WriteFile(tmp(os.Getppid()), []byte(crashHeader+"0.1.0\n"), 0o600)

	collectCrashes()
	if _, err := os.Stat(tmp(dead.Process.Pid)); err == nil {
		t.Error("the crash's temp file should be gone")
	}
	if _, err := os.Stat(tmp(empty.Process.Pid)); err == nil {
		t.Error("a clean exit's temp file should be gone")
	}
	if _, err := os.Stat(tmp(os.Getppid())); err != nil {
		t.Error("a running session's file should be left alone")
	}

	r, found := crashNews()
	if !found || r.label != "Crash" {
		t.Fatalf("an unseen crash should be news, got %+v", r)
	}
	c, _ := readCrash()
	if c.reason != "boom" || c.version != "0.1.0" || crashWhere(c.trace) != "internal/ui/model.go:552" {
		t.Errorf("read back wrong: %+v, where %q", c, crashWhere(c.trace))
	}
	got := ansi.Strip(crashCard(c, 60))
	for _, want := range []string{"Crash", "boom", "model.go:552"} {
		if !strings.Contains(got, want) {
			t.Errorf("card lacks %q:\n%s", want, got)
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(l); w != 60 {
			t.Errorf("line %d cells wide, want 60: %q", w, l)
		}
	}

	out, err := output(t, crashCmd) // not a terminal: the log as it is
	if err != nil || !strings.Contains(out, "boom") || !strings.Contains(out, "model.go:552") {
		t.Errorf("crash printed %q, %v", out, err)
	}
	if _, found := crashNews(); found {
		t.Error("once looked at, it isn't news")
	}
}

// The card starts where it happened, and stays short enough for its title to
// stay on screen, however long the trace.
func TestCrashCardIsShort(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	trace := "goroutine 1 [running]:\nruntime/debug.Stack()\n\t/go/runtime/debug/stack.go:26 +0x64\ncharm.land/bubbletea/v2.(*Program).recoverFromPanic(0x1, {0x2})\n\t/m/tea.go:1323 +0xec\npanic({0x1?, 0x2?})\n\t/go/runtime/panic.go:859 +0x120\nruntime.panicmem(...)\n\t/go/runtime/panic.go:262\n"
	trace += "github.com/con-dog/clingon/internal/ui.(*Model).update(0x1, {0x2})\n\tgithub.com/con-dog/clingon/internal/ui/model.go:552 +0x10\n"
	for range 40 {
		trace += "charm.land/bubbletea/v2.(*Program).Run(0x1)\n\t/m/tea.go:1172 +0x7dc\n"
	}
	got := ansi.Strip(crashCard(lastCrash{reason: "boom", trace: trace}, 80))
	if strings.Contains(got, "recoverFromPanic") || strings.Contains(got, "panicmem") {
		t.Errorf("the card should start where it happened:\n%s", got)
	}
	if !strings.Contains(got, "ui.(*Model).update") || !strings.Contains(got, "more lines in the log") {
		t.Errorf("missing the top frame or the count:\n%s", got)
	}
	if n := strings.Count(got, "\n") + 1; n > 24 {
		t.Errorf("%d lines tall: the title would scroll away", n)
	}
}

func TestParseCrashFromBubbleTea(t *testing.T) {
	reason, trace := parseCrash("Caught panic:\r\n\r\nnil pointer\r\n\r\nRestoring terminal...\r\n\r\ngoroutine 1 [running]:\r\nx()\r\n")
	if reason != "nil pointer" || trace != "goroutine 1 [running]:\nx()" {
		t.Errorf("got %q, %q", reason, trace)
	}
}

func TestPackInspect(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	run := func(ref string) (string, error) {
		return output(t, func() error { return packCmd([]string{"inspect", ref}) })
	}
	// One you don't have yet: exactly what installing it would add.
	if out, err := run("docker"); err != nil || !strings.Contains(out, "name: Docker") {
		t.Fatalf("not installed: %.100q %v", out, err)
	}
	if err := config.Install("docker"); err != nil {
		t.Fatal(err)
	}
	if out, err := run("remote/docker"); err != nil || !strings.HasPrefix(out, "# Installed by clingon.") {
		t.Errorf("installed: %.100q %v", out, err)
	}
	if err := config.AddCommand("docker", "Mine", "Hi", "echo hi", ""); err != nil {
		t.Fatal(err)
	}
	if out, err := run("local/docker"); err != nil || !strings.Contains(out, "echo hi") {
		t.Errorf("your own: %.100q %v", out, err)
	}
	if _, err := run("docker"); err == nil || !strings.Contains(err.Error(), "say remote/docker or local/docker") {
		t.Errorf("both: should ask which, got %v", err)
	}
	for _, bad := range []string{"nope", "../recipes", "local/nope", "remote/git"} {
		if _, err := run(bad); err == nil {
			t.Errorf("%s: should be refused", bad)
		}
	}
}

func TestRecipeCommands(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	run := func(args ...string) (string, error) { return output(t, func() error { return recipeCmd(args) }) }
	if out, err := run("save", "My AWS ECS flow", "aws-1a2b3c4d", "docker-5e6f7a8b"); err != nil || !strings.Contains(out, "My AWS ECS flow") {
		t.Fatalf("save: %q %v", out, err)
	}
	if out, _ := run("list"); !strings.Contains(out, "My AWS ECS flow\taws-1a2b3c4d docker-5e6f7a8b") {
		t.Errorf("list: %q", out)
	}
	if _, err := run("save", "My AWS ECS flow", "aws-1a2b3c4d"); err == nil {
		t.Error("the same name twice should be refused")
	}
	if _, err := run("save", "No steps"); err == nil {
		t.Error("no steps should be refused")
	}
	if err := config.AddCommand("mine", "Deploy", "Ship", "ship <where>", "Ships it."); err != nil {
		t.Fatal(err)
	}
	id := (&config.Section{Name: "Deploy"}).CommandID(&config.Command{Name: "Ship"})
	if _, err := run("save", "Ship it", id, "gone-00000000"); err != nil {
		t.Fatal(err)
	}
	out, err := run("describe", "ship IT")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Ship it", "2 steps", " 1  Ship · Deploy", "ship <where>", "Ships it.", "Asks for: where", " 2  Not available here", "gone-00000000"} {
		if !strings.Contains(ansi.Strip(out), want) {
			t.Errorf("describe should say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "❯") {
		t.Errorf("describe shouldn't look like the shell's prompt:\n%s", out)
	}
	if _, err := run("describe", "Nope"); err == nil {
		t.Error("describing what isn't there should say so")
	}
	if _, err := run("remove", "My AWS ECS flow"); err != nil {
		t.Error(err)
	}
	if _, err := run("remove", "My AWS ECS flow"); err == nil {
		t.Error("removing it twice should say so")
	}
}
