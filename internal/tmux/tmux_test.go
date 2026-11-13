package tmux

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/con-dog/clingon/internal/config"
)

// server starts a private tmux server whose one pane runs sh, and returns
// a client for it and that pane's id. It's killed when the test ends.
func server(t *testing.T) (*Client, string) { return serverWith(t, "sh") }

// serverWith is server with another shell, e.g. "zsh -f".
func serverWith(t *testing.T, shell string) (*Client, string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux isn't installed")
	}
	dir, err := os.MkdirTemp("", "cltmux") // short: socket paths max out near 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	c, err := New(filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	c.setEnv("ENV=") // no shell startup files
	// Each prompt marked, as clingon's own rc files have their shells do: how
	// WaitIdle hears a command's finished.
	c.ReadyFile = filepath.Join(dir, "ready")
	c.setEnv("CLINGON_READY=" + c.ReadyFile)
	c.setEnv(`PROMPT_COMMAND=printf . >> "$CLINGON_READY"`) // sh and bash
	pane, err := c.run("-f", "/dev/null", "new-session", "-d", "-s", "clingon", "-x", "120", "-y", "40",
		// Set here, not in the environment: tmux may start this through
		// bash ($SHELL), which unsets PS1 when it isn't interactive.
		"-c", dir, "-P", "-F", "#{pane_id}", "--", "PS1='❯ ' exec "+shell)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Kill)
	// Started means its prompt is drawn: only then is its line editor up,
	// taking pastes as pastes.
	waitFor(t, "the shell to start", func() bool {
		left, _, ok := c.cursorLine(pane)
		return ok && strings.TrimSpace(left) != "" && c.State(pane).Busy == "" && c.Path(pane) != ""
	})
	if strings.Contains(shell, "zsh") { // zsh marks its prompts from the line editor, as clingon's .zshrc does
		before := c.prompts()
		if _, err := c.run("send-keys", "-t", pane, "-l", `_r() { zle -R; print -n . >> $CLINGON_READY }; zle -N zle-line-init _r`); err != nil {
			t.Fatal(err)
		}
		if _, err := c.run("send-keys", "-t", pane, "Enter"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "zsh to mark its prompts", func() bool { return c.prompts() > before })
	}
	return c, pane
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	// Generous: a cold CI runner under -race can take seconds to start a
	// shell, and this only polls, so a pass is no slower for it.
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func exists(path string) func() bool {
	return func() bool { _, err := os.Stat(path); return err == nil }
}

func TestSendTypesIntoTheShell(t *testing.T) {
	c, pane := server(t)
	dir := c.Path(pane)

	if err := c.Send(pane, "printf hi > one"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a plain command to run", exists(filepath.Join(dir, "one")))

	// A trailing ; is part of the command, not a tmux separator.
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(pane, `find . -name x.txt -exec touch {}.hit \;`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "find -exec … \\; to run", exists(filepath.Join(dir, "x.txt.hit")))

	// Several lines go in as one paste, and all run.
	if err := c.Send(pane, "printf a > two\nprintf b > three"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "both lines to run", func() bool {
		return exists(filepath.Join(dir, "two"))() && exists(filepath.Join(dir, "three"))()
	})
}

func TestPathFollowsCd(t *testing.T) {
	c, pane := server(t)
	start := c.Path(pane)
	if err := os.Mkdir(filepath.Join(start, "sub dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(pane, "cd 'sub dir'"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the cd", func() bool { return filepath.Base(c.Path(pane)) == "sub dir" })
}

func TestBusyAndWaitIdle(t *testing.T) {
	c, pane := server(t)
	if b := c.State(pane).Busy; b != "" {
		t.Fatalf("at the prompt it isn't busy, got %q", b)
	}
	// A pipeline whose first part ends first: wait for all of it.
	start := time.Now()
	if err := c.Send(pane, "sleep 0.2 | sleep 2"); err != nil { // long enough to catch it busy, even on a loaded machine
		t.Fatal(err)
	}
	waitFor(t, "it to be busy", func() bool { return c.State(pane).Busy == "sleep" })
	if !c.WaitIdle(pane) {
		t.Fatal("WaitIdle should know when it's finished")
	}
	if took := time.Since(start); took < 2*time.Second {
		t.Fatalf("WaitIdle came back after %v, before the pipeline ended", took)
	}
	if b := c.State(pane).Busy; b != "" {
		t.Fatalf("finished: not busy any more, got %q", b)
	}
}

func TestFocusIfActive(t *testing.T) {
	c, a := server(t)
	b, err := c.run("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", a, "sh")
	if err != nil {
		t.Fatal(err)
	}
	active := func() string { s, _ := c.run("display-message", "-p", "#{pane_id}"); return s }
	if _, err := c.run("select-pane", "-t", a); err != nil {
		t.Fatal(err)
	}
	c.FocusIfActive(a, b)
	if active() != b {
		t.Fatal("from had focus: it should move")
	}
	c.FocusIfActive(a, a)
	if active() != b {
		t.Fatal("from didn't have focus: it should stay put")
	}
}

func TestLayout(t *testing.T) {
	c, shell := server(t)
	// A stand-in for clingon that just stays running as the menu.
	self := filepath.Join(filepath.Dir(c.Socket), "fake-clingon")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.layout(self, shell, filepath.Dir(c.Socket), []string{"-e", "CLINGON=1"}); err != nil {
		t.Fatal(err)
	}
	out, err := c.run("list-panes", "-F", "#{pane_id} #{pane_width} #{pane_active} #{@spacer} #{pane_height} #{window_width} #{window_height}")
	if err != nil {
		t.Fatal(err)
	}
	var spacers, sides, bars, menuW, shellW int
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line + " -")
		w, _ := strconv.Atoi(f[1])
		switch {
		case f[3] == "1":
			spacers++
			h, _ := strconv.Atoi(f[4])
			winW, _ := strconv.Atoi(f[5])
			winH, _ := strconv.Atoi(f[6])
			switch {
			case w == 1 && h == winH: // a side: the window's whole height
				sides++
			case h == 1 && w == winW-4: // a bar: between the sides, a border and a spacer in from each edge
				bars++
			}
		case f[0] == shell:
			shellW = w
		case f[2] == "1": // the menu has focus
			menuW = w
		}
	}
	if spacers != 4 {
		t.Errorf("want 4 spacer panes framing the others, got %d:\n%s", spacers, out)
	}
	// The sides run the window's height and the bars sit between them, so no
	// rule runs on past a corner of the frame.
	if sides != 2 || bars != 2 {
		t.Errorf("want 2 full-height sides and 2 bars between them, got %d and %d:\n%s", sides, bars, out)
	}
	if got := c.BarWidth(); got != 120-4 {
		t.Errorf("BarWidth() = %d, want the bar's own width, %d", got, 120-4)
	}
	if menuW == 0 || shellW == 0 || menuW > shellW {
		t.Errorf("want the focused menu on the narrower left third: menu %d, shell %d:\n%s", menuW, shellW, out)
	}
	// The menu's pane is named, for the pack form to line up with it.
	if menu, err := c.run("show-options", "-gv", "@clingon-menu"); err != nil || menu == "" || menu == shell {
		t.Errorf("@clingon-menu %q, err %v", menu, err)
	}
}

func TestSweepKeepsLiveSockets(t *testing.T) {
	dir, err := os.MkdirTemp("", "clsweep")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	dead := 1 << 22 // above any real pid
	stale := filepath.Join(dir, "clingon-"+strconv.Itoa(dead))
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ours := filepath.Join(dir, "clingon-"+strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(ours, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A server still answering on a dead launcher's socket.
	answering := filepath.Join(dir, "clingon-"+strconv.Itoa(dead+1))
	l, err := net.Listen("unix", answering)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	sweep(dir)
	if exists(stale)() {
		t.Error("a dead launch's socket should go")
	}
	if !exists(ours)() {
		t.Error("a running launch's socket should stay")
	}
	if !exists(answering)() {
		t.Error("a socket a server still answers on should stay")
	}
}

func TestEscape(t *testing.T) {
	for in, want := range map[string]string{`find . -exec rm {} \;`: `find . -exec rm {} \\;`, "a; b": "a; b", "ls": "ls"} {
		if got := escape(in); got != want {
			t.Errorf("escape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTypedOnReadsClingonsPrompt(t *testing.T) {
	// As tmux captures them: trailing spaces trimmed.
	c := &Client{started: true}
	for left, want := range map[string]string{
		"❯":                         "",
		"~/code/app ❯":              "",
		"~/code/app main ❯ npm run": "npm run",
		"~ ❯ cat x | grep >":        "cat x | grep >",
		"~ ❯ echo ❯ x":              "echo ❯ x",
		"dquote>":                   "an unfinished command",
		"> ":                        "an unfinished command",
		"":                          "an unfinished command", // a typed line wrapped onto a new row, or read waiting
		"git sta":                   "an unfinished command",
	} {
		if got := c.typedOn(left); got != want {
			t.Errorf("typedOn(%q) = %q, want %q", left, got, want)
		}
	}
	// Still starting up: only a prompt says anything.
	starting := &Client{}
	if got := starting.typedOn("Loading plugins..."); got != "" {
		t.Errorf("starting up: got %q", got)
	}
	if got := starting.typedOn("~ ❯ git sta"); got != "git sta" {
		t.Errorf("starting up, with a prompt: got %q", got)
	}
}

func TestSendTabsAndLongCommandsArriveIntact(t *testing.T) {
	// zsh: macOS's sh is bash 3.2, too old for bracketed paste, so a tab
	// pasted into it still completes. zsh and bash 4.4+ take it as typed.
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh isn't installed")
	}
	c, pane := serverWith(t, "zsh -f")
	dir := c.Path(pane)
	if err := c.Send(pane, "printf 'a\tb' > tabbed"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the tab command", func() bool { b, _ := os.ReadFile(filepath.Join(dir, "tabbed")); return len(b) > 0 })
	if b, _ := os.ReadFile(filepath.Join(dir, "tabbed")); string(b) != "a\tb" {
		t.Errorf("the tab should arrive as a tab, got %q", b)
	}
	long := "printf %s " + strings.Repeat("y", 17000) + " > long"
	if err := c.Send(pane, long); err != nil {
		t.Fatalf("a 17 KB command: %v", err)
	}
	waitFor(t, "the long command", func() bool { b, _ := os.ReadFile(filepath.Join(dir, "long")); return len(b) == 17000 })
}

func TestWaitIdleWaitsForEveryPartOfAList(t *testing.T) {
	c, pane := server(t)
	start := time.Now()
	if err := c.Send(pane, "sleep 0.4; sleep 0.4; sleep 0.4"); err != nil {
		t.Fatal(err)
	}
	if !c.WaitIdle(pane) {
		t.Fatal("WaitIdle should know when it's done")
	}
	if took := time.Since(start); took < 1200*time.Millisecond {
		t.Fatalf("came back after %v, before the last part ended", took)
	}
}

func TestStateSeesTypingScrollingAndUnfinished(t *testing.T) {
	c, pane := server(t)
	dir := c.Path(pane)
	c.WaitForPrompt(pane, 2*time.Second)
	if st := c.State(pane); st != (State{}) {
		t.Fatalf("at a fresh prompt: nothing going on, got %+v", st)
	}
	if _, err := c.run("send-keys", "-t", pane, "-l", "echo half"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the typing to show", func() bool { return c.State(pane).Typed == "echo half" })
	if err := c.Clear(pane); err != nil {
		t.Fatal(err)
	}
	if st := c.State(pane); st.Typed != "" {
		t.Fatalf("Clear should leave an empty prompt, got %+v", st)
	}
	if err := c.Send(pane, "printf ok > after-clear"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a command right after Clear to run", exists(filepath.Join(dir, "after-clear")))

	if _, err := c.run("send-keys", "-t", pane, "-l", `echo "open`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.run("send-keys", "-t", pane, "Enter"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the unfinished command to show", func() bool { return c.State(pane).Typed == "an unfinished command" })
	_ = c.Clear(pane)
	if st := c.State(pane); st.Typed != "" {
		t.Fatalf("Clear should end an unfinished command too, got %+v", st)
	}

	// A quote left open by a command the menu sent.
	if err := c.Send(pane, `echo "open`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the open quote to show", func() bool { return c.State(pane).Typed == "an unfinished command" })
	_ = c.Clear(pane)

	if _, err := c.run("copy-mode", "-t", pane); err != nil {
		t.Fatal(err)
	}
	if !c.State(pane).Scrolling {
		t.Fatal("copy mode is scrolling")
	}
	if _, err := c.run("send-keys", "-t", pane, "q"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "scrolling to stop", func() bool { return !c.State(pane).Scrolling })
}

func TestLastFailedReadsThePromptsColour(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh isn't installed")
	}
	c, pane := serverWith(t, "env TERM=xterm-256color zsh -f")
	c.WaitForPrompt(pane, 2*time.Second)
	// As clingon's prompt does it: a pink mark, red after a failure, on a line
	// of its own under one that can have anything in it.
	if err := c.Send(pane, `precmd() { local ok=$?; local m=212; (( ok )) && m=204; PROMPT=$'%F{204}red-text%f\n'"%F{$m}❯%f " }`); err != nil {
		t.Fatal(err)
	}
	c.WaitIdle(pane)
	run := func(cmd string, want bool) {
		t.Helper()
		if err := c.Send(pane, cmd); err != nil {
			t.Fatal(err)
		}
		if !c.WaitIdle(pane) {
			t.Fatal("WaitIdle should know when it's done")
		}
		if got, known := c.LastFailed(pane); got != want || !known {
			t.Errorf("after %q: failed = %v, known = %v, want %v, true", cmd, got, known, want)
		}
	}
	run("true", false)
	run("false", true)
	run("true", false)

	// No prompt at the cursor: that's not a command that worked.
	if err := c.Send(pane, `precmd() { PROMPT='plain> ' }`); err != nil {
		t.Fatal(err)
	}
	c.WaitIdle(pane)
	if _, known := c.LastFailed(pane); known {
		t.Error("with no prompt mark to read, the result can't be known")
	}
}

func TestJobNames(t *testing.T) {
	ps := "  10 Ss+  -zsh\n  11 S    sleep\n  12 T    /opt/homebrew/bin/vim\n  13 S    sleep\n  14 Z    gone\n"
	if got, want := jobNames(ps, "10"), []string{"sleep", "vim"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := jobNames("  10 Ss+  -zsh\n", "10"); len(got) != 0 {
		t.Errorf("a shell alone has no jobs, got %q", got)
	}
}

// A slow precmd (vcs_info in a big repo, say) has the terminal back with no
// prompt drawn yet: that blank line isn't an unfinished command.
func TestWaitIdleWaitsForASlowPrompt(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh isn't installed")
	}
	c, pane := serverWith(t, "zsh -f")
	c.WaitForPrompt(pane, 2*time.Second)
	if err := c.Send(pane, `precmd() { : $(sleep 0.4) }`); err != nil {
		t.Fatal(err)
	}
	if !c.WaitIdle(pane) {
		t.Fatal("WaitIdle should know when it's done")
	}
	if err := c.Send(pane, "true"); err != nil {
		t.Fatal(err)
	}
	if !c.WaitIdle(pane) {
		t.Fatal("WaitIdle should know when it's done")
	}
	if st := c.State(pane); st != (State{}) {
		t.Fatalf("the command has finished: nothing going on, got %+v", st)
	}

	// No prompt is coming for an open quote: it's still reported, just later.
	if err := c.Send(pane, `echo "open`); err != nil {
		t.Fatal(err)
	}
	if !c.WaitIdle(pane) {
		t.Fatal("WaitIdle should know when it's done")
	}
	if st := c.State(pane); st.Typed != "an unfinished command" {
		t.Fatalf("an open quote is an unfinished command, got %+v", st)
	}
}

// Sending a command leaves focus where it is: keys meant for the menu mustn't
// land in the shell.
func TestSendLeavesFocusAlone(t *testing.T) {
	c, pane := serverWith(t, "env TERM=xterm-256color zsh -f")
	c.WaitForPrompt(pane, 2*time.Second)
	menu, err := c.run("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", pane, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.run("select-pane", "-t", menu); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"true", "printf 'a\\tb\\n'", strings.Repeat("true; ", 800) + "true"} {
		if err := c.Send(pane, cmd); err != nil {
			t.Fatal(err)
		}
		if got, _ := c.run("display-message", "-p", "#{pane_id}"); got != menu {
			t.Errorf("after %.20q focus is on %s, want the menu %s", cmd, got, menu)
		}
		c.WaitIdle(pane)
	}
}

// The panel, open as a one-row strip, grows to the full panel when asked,
// and stays that tall: resizing it sets off its own keep-the-height hook,
// which mustn't put it back to the strip.
func TestPanelGrowsWhenOpen(t *testing.T) {
	c, shell := server(t)
	p := c.NewPanel(shell, shell)
	height := func() int {
		out, _ := c.run("display-message", "-p", "-t", p.pane, "#{pane_height}")
		h, _ := strconv.Atoi(strings.TrimSpace(out))
		return h
	}
	if p.Open(1) == 0 || height() != 1 {
		t.Fatalf("the strip: %d rows", height())
	}
	p.Open(12)
	time.Sleep(200 * time.Millisecond) // for any hook to have run
	if h := height(); h != 12 {
		t.Errorf("open already, asked for 12 rows: %d", h)
	}
	if w, _ := c.run("show-options", "-gv", "@clingon-ai-working"); w != "1" {
		t.Errorf("the full panel: the AI's at work, said %q", w)
	}
	p.Open(1)
	time.Sleep(200 * time.Millisecond)
	if h := height(); h != 1 {
		t.Errorf("back to the strip: %d rows", h)
	}
	if w, _ := c.run("show-options", "-gv", "@clingon-ai-working"); w != "0" {
		t.Errorf("the strip: not at work, said %q", w)
	}
	p.Close()
}

// tmux tells the menu when focus moves or a pane scrolls: its hooks append
// to $CLINGON_CHANGED, and a watch on it wakes, with nothing polled.
func TestChangesWakeTheWatch(t *testing.T) {
	c, shell := server(t)
	self := filepath.Join(filepath.Dir(c.Socket), "fake-clingon")
	if err := os.WriteFile(self, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(filepath.Dir(c.Socket), "changed")
	if err := os.WriteFile(changed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.run("set-environment", "-g", "CLINGON_CHANGED", changed); err != nil {
		t.Fatal(err)
	}
	if err := c.layout(self, shell, filepath.Dir(c.Socket), []string{"-e", "CLINGON=1"}); err != nil {
		t.Fatal(err)
	}
	w, err := config.WatchPath(changed)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { w.Close(); w.Wait() }()
	woke := func(what string, change func()) {
		t.Helper()
		before := size(changed)
		done := make(chan bool, 1)
		go func() { done <- w.Wait() }()
		change()
		select {
		case ok := <-done:
			if !ok {
				t.Fatalf("%s: the watch ended", what)
			}
		case <-time.After(5 * time.Second): // only so a missing hook fails rather than hangs
			t.Fatalf("%s didn't wake it", what)
		}
		for end := time.Now().Add(5 * time.Second); size(changed) == before && time.Now().Before(end); {
			if !w.Wait() { // an earlier hook's byte woke it: this one's next
				t.Fatal("the watch ended")
			}
		}
		if size(changed) == before {
			t.Fatalf("%s wrote nothing", what)
		}
	}
	woke("focus moving to the shell", func() { c.Focus(shell) })
	woke("the shell scrolling back", func() { _, _ = c.run("copy-mode", "-t", shell) })
}

func size(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return st.Size()
}

// The cursor's row in a pane's screen is found by its bar: a pink-backed
// blank starting the row, after nothing but colour and the form's guide
// lines. One in another colour (the grey of a pane without focus) isn't it.
func TestCursorCellFindsThePinkBar(t *testing.T) {
	for line, want := range map[string]bool{
		"\x1b[48;5;212m \x1b[0m\x1b[40m Name  Demo": true,
		"\x1b[38;5;60m│\x1b[48;5;212m \x1b[0m Name": true,
		"\x1b[48;2;255;135;215m \x1b[0m Name":       true,
		"\x1b[48;5;103m \x1b[0m Name":               false,
		"Name  \x1b[48;5;212m \x1b[0m":              false,
	} {
		if got := cursorCell.MatchString(line); got != want {
			t.Errorf("%q: the cursor's row %v, want %v", line, got, want)
		}
	}
	if got := cursorCell.ReplaceAllString("\x1b[38;5;60m│\x1b[48;5;212m \x1b[0m Name", "${1}▶"); !strings.Contains(got, "▶") {
		t.Errorf("marked %q", got)
	}
}
