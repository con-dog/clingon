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
	if out := run("add", "git"); !strings.Contains(out, "added git") {
		t.Fatalf("add: %q", out)
	}
	if out := run("add", "git"); !strings.Contains(out, "already added") {
		t.Fatalf("adding twice: %q", out)
	}
	if out := run("list", "--names"); !strings.Contains(out, "remote/git") {
		t.Fatalf("list: %q", out)
	}
	if out := run("available", "--names"); strings.Contains(out, "/git\t") || !strings.Contains(out, "remote/docker\t") {
		t.Fatalf("available lists what isn't installed: %q", out)
	}
	if out := run("update"); !strings.Contains(out, "up to date") {
		t.Fatalf("update with nothing newer: %q", out)
	}
	for _, verb := range []string{"add", "update"} {
		if out, err := output(t, func() error { return packCmd([]string{verb, "mine/docker"}) }); err == nil || !strings.Contains(err.Error(), "mine/docker") {
			t.Fatalf("%s mine/docker should be refused, got %q %v", verb, out, err)
		}
	}
	if config.IsInstalled("docker") {
		t.Fatal("add mine/docker installed remote/docker")
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
}

func TestCheckIncludesTheProjectPack(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	t.Chdir(repo)
	module := filepath.Join(repo, config.ProjectDir, "deploy", "prod.md")
	if err := os.MkdirAll(filepath.Dir(module), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(module, []byte("# Prod\n\n## Ship\n\n```sh\n./ship.sh\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(module), config.PackInfoFile), []byte("---\nmenu: other\n---\n# Deploy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := output(t, check)
	if err != nil || !strings.Contains(out, "project pack") || !strings.Contains(out, "asks before using it") {
		t.Fatalf("got %q %v", out, err)
	}
	if err := os.WriteFile(module, []byte("# Prod\n\n## Ship\n"), 0o644); err != nil {
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
	for _, want := range []string{"packs    valid"} {
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
	remote := config.RemotePath("git/everyday")
	data, _ := os.ReadFile(remote)
	if err := os.WriteFile(remote, append(data, []byte("<!-- old -->\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if h := headline(); !strings.Contains(h.Text, "Update 1 pack: git · Clingon › Packs › Update packs") {
		t.Fatalf("a pack update beats all's well, got %+v", h)
	}
	if err := os.MkdirAll(config.StateDir(), 0o755); err != nil {
		t.Fatal(err)
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
	if err := os.MkdirAll(config.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
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
	if err := os.MkdirAll(config.RemotePath("homemade"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.RemotePath("homemade"), "h.md"), []byte("# H\n\n## x\n\n```sh\nls\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := output(t, func() error { return doctor(nil) })
	if !strings.Contains(out, "homemade") || !strings.Contains(out, "git") {
		t.Fatalf("both packs should be reported:\n%s", out)
	}
}

func TestPackUpdateRemovesRetiredPacks(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := os.MkdirAll(config.RemotePath("old-db"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.RemotePath("old-db"), "g.md"), []byte("# G\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := output(t, func() error { return packCmd([]string{"update"}) })
	if err != nil || !strings.Contains(out, "removed old-db") {
		t.Fatalf("update should remove a retired pack, got %q, %v", out, err)
	}
	if config.IsInstalled("old-db") {
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
		return filepath.Join(config.StateDir(), "crash-"+strconv.Itoa(pid)+".tmp")
	}
	if err := os.MkdirAll(config.StateDir(), 0o755); err != nil {
		t.Fatal(err)
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
	for i, l := range strings.Split(got, "\n") {
		w := ansi.StringWidth(l)
		switch {
		case i == 0 && w != 60: // its title's rule: the card's whole width
			t.Errorf("the title's rule is %d cells wide, want 60: %q", w, l)
		case w > 60:
			t.Errorf("line %d cells wide, over the card's 60: %q", w, l)
		}
	}
	if strings.ContainsAny(got, "╭╮╰╯│├┤") {
		t.Errorf("a card has no box round it:\n%s", got)
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
		return output(t, func() error { return packCmd([]string{"describe", ref}) })
	}
	// One you don't have yet: exactly what installing it would add.
	if out, err := run("docker"); err != nil || !strings.Contains(out, "# Docker") {
		t.Fatalf("not installed: %.100q %v", out, err)
	}
	if err := config.Install("docker"); err != nil {
		t.Fatal(err)
	}
	if out, err := run("remote/docker"); err != nil || !strings.Contains(out, "<!-- Added by clingon.") {
		t.Errorf("installed: %.100q %v", out, err)
	}
	if err := config.AddCommand("docker", "Mine", "Hi", "echo hi", ""); err != nil {
		t.Fatal(err)
	}
	if out, err := run("mine/docker"); err != nil || !strings.Contains(out, "echo hi") {
		t.Errorf("your own: %.100q %v", out, err)
	}
	if _, err := run("docker"); err == nil || !strings.Contains(err.Error(), "say remote/docker or mine/docker") {
		t.Errorf("both: should ask which, got %v", err)
	}
	// Not installed, and asked for as the menu does.
	if out, err := run("remote/git"); err != nil || !strings.Contains(out, "# Git") {
		t.Errorf("remote, not installed: %.100q %v", out, err)
	}
	for _, bad := range []string{"nope", "../recipes", "mine/nope", "mine/git", "remote/nope"} {
		if _, err := run(bad); err == nil {
			t.Errorf("%s: should be refused", bad)
		}
	}
}

func TestEveryPackPickerSaysWhereItsFrom(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.Install("git"); err != nil {
		t.Fatal(err)
	}
	if err := config.AddCommand("mine", "S", "x", "echo x", ""); err != nil {
		t.Fatal(err)
	}
	var inspect *config.Command
	pickers := 0
	var mineInspect *config.Command
	for _, s := range builtinSections() {
		for g := range s.Groups {
			for i := range s.Groups[g].Commands {
				if c := &s.Groups[g].Commands[i]; c.Name == "Describe a pack" || c.Name == "Describe your pack" {
					if s.Groups[g].Name == "My packs" {
						mineInspect = c
					} else {
						inspect = c
					}
				}
			}
		}
		for _, c := range s.All() {
			for _, a := range c.Args {
				if a.Name != "pack" {
					continue
				}
				pickers++
				for _, o := range a.Options {
					if strings.HasPrefix(o.Value, "remote/") {
						t.Errorf("%s: %s needn't say remote/: nothing else has that name here", c.Name, o.Value)
					}
				}
			}
		}
	}
	if inspect == nil || pickers < 5 {
		t.Fatalf("inspect %v, %d pickers", inspect, pickers)
	}
	seen := map[string]int{}
	for _, o := range inspect.Args[0].Options {
		seen[o.Value]++
		if seen[o.Value] > 1 {
			t.Errorf("%s is listed twice", o.Value)
		}
	}
	for _, want := range []string{"mine/mine", "git", "docker"} {
		if seen[want] != 1 {
			t.Errorf("%s should be listed once: %v", want, seen)
		}
	}
	// My packs' own Describe lists only yours.
	if mineInspect == nil || len(mineInspect.Args[0].Options) != 1 {
		t.Fatalf("My packs › Describe your pack should list just your pack: %+v", mineInspect)
	}
}

func TestRecipeCommands(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	run := func(args ...string) (string, error) { return output(t, func() error { return recipeCmd(args) }) }
	if out, err := run("add", "My AWS ECS flow", "aws-1a2b3c4d", "docker-5e6f7a8b"); err != nil || !strings.Contains(out, "My AWS ECS flow") {
		t.Fatalf("add: %q %v", out, err)
	}
	if out, _ := run("list"); !strings.Contains(out, "My AWS ECS flow\taws-1a2b3c4d docker-5e6f7a8b") {
		t.Errorf("list: %q", out)
	}
	if _, err := run("add", "My AWS ECS flow", "aws-1a2b3c4d"); err == nil {
		t.Error("the same name twice should be refused")
	}
	if _, err := run("add", "No steps"); err == nil {
		t.Error("no steps should be refused")
	}
	if err := config.AddCommand("mine", "Deploy", "Ship", "ship <where>", "Ships it."); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	id := cfg.Sections[0].CommandID(cfg.Sections[0].All()[0])
	if _, err := run("add", "Ship it", id, "gone-00000000"); err != nil {
		t.Fatal(err)
	}
	out, err := run("describe", "ship IT")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Ship it", "2 steps", " 1  Ship · Mine", "ship <where>", "Ships it.", "Asks for: Where", " 2  Not available here", "gone-00000000"} {
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
	// One not saved, the menu's current recipe, is described by its steps.
	if out, err := run("describe", "--steps", id); err != nil || !strings.Contains(ansi.Strip(out), "This recipe") || !strings.Contains(ansi.Strip(out), " 1  Ship · Mine") {
		t.Errorf("describe --steps: %v\n%s", err, out)
	}
	// --replace saves over one of that name, and only one that's there.
	if out, err := run("add", "--replace", "Ship it", id); err != nil || !strings.Contains(out, "replaced") {
		t.Errorf("replace: %q %v", out, err)
	}
	if out, _ := run("list"); !strings.Contains(out, "Ship it\t"+id+"\n") {
		t.Errorf("replaced, one step: %q", out)
	}
	if _, err := run("add", "--replace", "Nothing here", id); err == nil {
		t.Error("replacing what isn't there should say so")
	}
	if _, err := run("remove", "My AWS ECS flow"); err != nil {
		t.Error(err)
	}
	if _, err := run("remove", "My AWS ECS flow"); err == nil {
		t.Error("removing it twice should say so")
	}
}

// In a card, everything keeps inside its width, however long; none of it's cut.
// A card is its title's rule and its lines: no sides and no foot to box it.
func TestBoxedWrapsWithinItsEdges(t *testing.T) {
	long := "✓ " + strings.Repeat("remote/aws also has compute-logs ", 8)
	out := ansi.Strip(boxed("Title", []string{long, "short"}, 50))
	lines := strings.Split(out, "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "── Title ─") || ansi.StringWidth(lines[0]) != 50 {
		t.Fatalf("card:\n%s", out)
	}
	if strings.ContainsAny(out, "╭╮╰╯│") {
		t.Errorf("a card has no box round it:\n%s", out)
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 50-2 && l != lines[0] { // two cells clear of each side
			t.Errorf("%d wide: %q", w, l)
		}
		if l != lines[0] && !strings.HasPrefix(l, "  ") {
			t.Errorf("not set in under the title: %q", l)
		}
	}
	if strings.Count(strings.ReplaceAll(out, "\n", " "), "compute-logs") != 8 {
		t.Errorf("nothing should be cut:\n%s", out)
	}
}

func TestCommandTitle(t *testing.T) {
	for want, args := range map[string][]string{
		"Clingon": nil, "Pack add": {"pack", "add", "git"}, "Pack": {"pack"}, "Recipe add": {"recipe", "add", "x", "y"}, "Doctor": {"doctor", "git"},
	} {
		if got := commandTitle(args); got != want {
			t.Errorf("%v: %q, want %q", args, got, want)
		}
	}
}

// Piped, a command's output is what scripts want: no boxes, and nothing held back.
func TestPipedOutputHasNoBoxes(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	out, err := output(t, func() error { return packCmd([]string{"add", "git"}) })
	if err != nil || strings.Contains(out, "╭") || !strings.Contains(out, "added git") {
		t.Fatalf("add: %q %v", out, err)
	}
	if len(said) != 0 {
		t.Errorf("nothing should be held: %q", said)
	}
	out, err = output(t, func() error { return packCmd([]string{"describe", "remote/git/stash"}) })
	if err != nil || strings.Contains(out, "╭") || !strings.Contains(out, "\n## ") {
		t.Fatalf("inspect: %.200q %v", out, err)
	}
}

// Both Remove packs rows pick like Add packs does, for yours and for what's installed.
func TestRemovePacksIsAChecklist(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.Install("git"); err != nil {
		t.Fatal(err)
	}
	if err := config.NewPack("mine"); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, s := range builtinSections() {
		for _, c := range s.All() {
			switch c.Name {
			case "Remove a pack", "Uninstall packs", "Install packs":
				t.Errorf("%s has been renamed", c.Name)
			case "Remove packs", "Remove your packs":
				found++
				if a := c.Args[0]; !a.Multi() || !a.AtLeastOne || a.CountAs != "pack" {
					t.Errorf("%+v", a)
				}
			}
		}
	}
	if found != 2 {
		t.Errorf("found %d", found)
	}
	out, err := output(t, func() error { return packCmd([]string{"remove", "remote/git", "mine/mine"}) })
	if err != nil || !strings.Contains(out, "removed git") || !strings.Contains(out, "removed mine/mine") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestSomeNamesNeverCutsOneInHalf(t *testing.T) {
	if got := someNames([]string{"a", "b", "c", "d"}); got != "a, b, c, d" {
		t.Error(got)
	}
	if got := someNames([]string{"a", "b", "c", "d", "e", "f"}); got != "a, b, c, d +2 more" {
		t.Error(got)
	}
}

func TestCreateGlyphIsTheHammerInClingonsWindow(t *testing.T) {
	t.Setenv("CLINGON", "")
	if got := createGlyph(); got != "⊕" {
		t.Errorf("outside clingon's window: %q", got)
	}
	t.Setenv("CLINGON", "1")
	if got := createGlyph(); got != config.Logos["hammer"] {
		t.Errorf("in clingon's window: %q", got)
	}
}

// remote/ is only said when the name alone wouldn't do.
func TestRemoteIsOnlySaidWhenNeeded(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if got := config.RemoteRef("git"); got != "git" {
		t.Errorf("no pack of your own called git: %q", got)
	}
	if err := config.NewPack("git"); err != nil {
		t.Fatal(err)
	}
	if got := config.RemoteRef("git"); got != "remote/git" {
		t.Errorf("you have your own git too: %q", got)
	}
	if got := config.RemoteRef("git/stash"); got != "remote/git/stash" {
		t.Errorf("part of it, with your own git too: %q", got)
	}
	if got := (config.Pack{Where: "mine", Name: "git"}).Shown(); got != "mine/git" {
		t.Errorf("your own: %q", got)
	}
}

// One word for taking a pack away, whichever kind it is.
func TestRemoveTakesEitherKindOfPack(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.Install("git"); err != nil {
		t.Fatal(err)
	}
	if err := config.NewPack("poop"); err != nil {
		t.Fatal(err)
	}
	pack := func(args ...string) (string, error) {
		return output(t, func() error { return packCmd(args) })
	}
	if out, err := pack("remove", "poop"); err != nil || !strings.Contains(out, "removed mine/poop") || config.IsLocal("poop") {
		t.Errorf("your own: %q %v", out, err)
	}
	// With one of each, it has to be told which.
	if err := config.NewPack("git"); err != nil {
		t.Fatal(err)
	}
	if _, err := pack("remove", "git"); err == nil || !strings.Contains(err.Error(), "say remote/git or mine/git") {
		t.Errorf("one of each: %v", err)
	}
	if out, err := pack("remove", "mine/git"); err != nil || !strings.Contains(out, "removed mine/git") || !config.IsInstalled("git") {
		t.Errorf("mine/git: %q %v", out, err)
	}
	if out, err := pack("remove", "git"); err != nil || !strings.Contains(out, "removed git") || config.IsInstalled("git") {
		t.Errorf("curated: %q %v", out, err)
	}
}

// Tab completion offers the least that works: the name, unless you have one of each.
func TestShortNamesForCompletion(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.Install("git"); err != nil {
		t.Fatal(err)
	}
	if err := config.NewPack("poop"); err != nil {
		t.Fatal(err)
	}
	list := func() string {
		out, err := output(t, func() error { return packCmd([]string{"list", "--short"}) })
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := list(); !strings.Contains(out, "\ngit\t") && !strings.HasPrefix(out, "git\t") || !strings.Contains(out, "\npoop\t") {
		t.Errorf("plain names: %q", out)
	}
	if err := config.NewPack("git"); err != nil {
		t.Fatal(err)
	}
	if out := list(); !strings.Contains(out, "remote/git\t") || !strings.Contains(out, "mine/git\t") || !strings.Contains(out, "\npoop\t") {
		t.Errorf("one of each keeps its prefix: %q", out)
	}
}

// The guide's example is a real pack: it loads, and it's harmless.
func TestPackGuide(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	out, err := output(t, func() error { return packCmd([]string{"guide"}) })
	if err != nil || !strings.Contains(out, "## Say hello") || !strings.Contains(out, "front matter") {
		t.Fatalf("got %q %v", out, err)
	}
	dir := filepath.Join(config.Dir(), "packs", "mine", "work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deploy.md"), []byte(guideExample), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_pack.md"), []byte(guidePackInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	if shown := "  " + strings.ReplaceAll(strings.TrimSuffix(guidePackInfo, "\n"), "\n", "\n  "); !strings.Contains(guideRest, shown) {
		t.Errorf("the guide shows its _pack.md as it's tested:\n%s", shown)
	}
	cfg, _, err := config.Load()
	if err != nil {
		t.Fatalf("the guide's example should load: %v", err)
	}
	for _, c := range cfg.Sections[0].All() {
		if c.Risk != "" || strings.Contains(c.Cmd, "rm ") {
			t.Errorf("the guide's example should be harmless: %s", c.Cmd)
		}
	}
	for _, l := range strings.Split(guideTop+guideRest, "\n") {
		if n := len([]rune(l)); n > 78 {
			t.Errorf("a line of the guide is %d wide, too wide for its box: %q", n, l)
		}
	}
}

// Create a pack goes straight to the form, which asks for everything: the
// menu asks nothing first, and the form leaves its draft for the menu.
func TestAddAPackOpensTheForm(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	for _, s := range builtinSections() {
		for _, c := range s.All() {
			if c.Name == "Create a pack" {
				if !c.Editor || len(c.Args) != 0 || !strings.HasSuffix(c.Cmd, " pack create --menu") {
					t.Fatalf("got %s %+v", c.Cmd, c.Args)
				}
				return
			}
		}
	}
	t.Fatal("no Create a pack")
}

// Only your own packs open in the form: a curated one says how to read it or
// start your own, and one you don't have says how to start it.
func TestEditOnlyYourOwn(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.Install("git"); err != nil {
		t.Fatal(err)
	}
	for ref, want := range map[string]string{
		"git":        "pack describe git",
		"remote/git": "not yours to edit",
		"nope":       "pack create nope",
	} {
		if err := formEdit(ref, "saved", false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", ref, err, want)
		}
	}
}
