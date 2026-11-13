package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/llm"
	"github.com/con-dog/clingon/internal/tmpl"
)

// fakeModel is OpenRouter answering with a script of moves, one reply per
// request; it keeps what it was sent.
type fakeModel struct {
	mu    sync.Mutex
	moves [][2]string // tool name, arguments
	got   []string    // each request's last message
}

func (f *fakeModel) serve(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var req struct {
			Messages []llm.Message `json:"messages"`
			Tools    []any         `json:"tools"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		f.got = append(f.got, req.Messages[len(req.Messages)-1].Content)
		if len(f.moves) == 0 {
			t.Errorf("asked for more moves than scripted")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"?"}}]}`)
			return
		}
		mv := f.moves[0]
		f.moves = f.moves[1:]
		args, _ := json.Marshal(mv[1])
		fmt.Fprintf(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"","reasoning":"thinking about it","tool_calls":[{"id":"c%d","type":"function","function":{"name":%q,"arguments":%s}}]}}]}`, len(f.got), mv[0], args)
	}))
}

type fakePanel struct{ lines []string }

func (p *fakePanel) Open(rows int) int   { return 80 }
func (p *fakePanel) Draw(lines []string) { p.lines = lines }
func (p *fakePanel) Close()              { p.lines = nil }
func (p *fakePanel) Focused() bool       { return false }

// run carries out cmd and the commands it leads to, feeding their messages
// to the menu, until done says so (or it's taking too long).
func run(t *testing.T, m *Model, cmd tea.Cmd, done func() bool) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	deadline := time.Now().Add(20 * time.Second)
	for len(queue) > 0 && !done() {
		if time.Now().After(deadline) {
			t.Fatal("the AI took too long")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case nil:
		case aiReplyMsg, aiTickMsg, aiStillMsg:
			_, next := m.Update(msg)
			queue = append(queue, next)
		}
	}
}

func aiMenu(t *testing.T, f *fakeModel, cmds ...config.Command) (*Model, *fakePanel, *[]string) {
	t.Setenv("CLINGON_CONFIG", t.TempDir()) // its transcripts, not in yours
	gap := aiKeyGap
	aiKeyGap = time.Millisecond // quick: there's no one watching
	t.Cleanup(func() { aiKeyGap = gap })
	srv := f.serve(t)
	t.Cleanup(srv.Close)
	var sent []string
	panel := &fakePanel{}
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: cmds}}},
		Send:  func(c string) error { sent = append(sent, c); return nil },
		Panel: panel, HasAIKey: func() bool { return true }, // not the Keychain: tests never read it
		AIClient: func() (*llm.Client, error) {
			return &llm.Client{Key: "test", Model: "anthropic/claude-sonnet-5.5", Endpoint: srv.URL}, nil
		}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	return m, panel, &sent
}

// Given a task, the AI uses the menu: it searches, runs what it found, and
// ends; it's shown the menu as text, the cursor's row marked; the panel
// shows the model, the task and its steps.
func TestAIUsesTheMenu(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"search", `{"why":"Finding hello","title":"Say Hello Test","text":"hello"}`},
		{"press", `{"why":"Running it","keys":["Enter"]}`},
		{"done", `{"why":"Done","headline":"Said hello","summary":"Said hello. First I searched for it, then I ran it, and the shell printed hello."}`},
	}}
	m, panel, sent := aiMenu(t, f, config.Command{Name: "Say hello", Cmd: "echo hello"}, config.Command{Name: "Other", Cmd: "true"})
	run(t, m, m.startAI("say hello"), func() bool { return m.ai != nil && m.ai.done })
	if len(*sent) != 1 || (*sent)[0] != "echo hello" {
		t.Fatalf("sent %v", *sent)
	}
	if len(f.got) < 2 || !strings.Contains(f.got[0], "The task: say hello") || !strings.Contains(f.got[1], "Say hello") || !strings.Contains(f.got[1], "Status bar keys") {
		t.Errorf("what it was shown: %q", f.got)
	}
	if !strings.HasPrefix(m.ai.summary, "Said hello. First") || m.ai.headline != "Said hello" || m.ai.err != "" {
		t.Errorf("summary %q, err %q", m.ai.summary, m.ai.err)
	}
	m.aiDraw()
	text := strings.Join(panel.lines, "\n")
	if !strings.Contains(text, "sonnet-5.5") || !strings.Contains(text, "Say Hello Test") || !strings.Contains(text, "Finding hello") || !strings.Contains(text, "DONE") || !strings.Contains(text, "Said hello") || strings.Contains(text, "First I searched") {
		t.Errorf("panel:\n%s", text)
	}
	if !strings.Contains(text, "Log: ") {
		t.Errorf("the panel says where the log is:\n%s", text)
	}
	// The run's transcript: what it was shown, thought and did.
	logs, _ := filepath.Glob(filepath.Join(AILogDir(), "*.md"))
	if len(logs) != 1 {
		t.Fatalf("logs %v", logs)
	}
	b, _ := os.ReadFile(logs[0])
	log := string(b)
	for _, want := range []string{"Task: say hello", "Model: anthropic/claude-sonnet-5.5", "thinking about it", "Move: `search", "### After `search`", "Say hello", "## Done", "First I searched for it", "Headline: Said hello", "Turns: 3"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log should have %q:\n%s", want, log)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if text := ansi.Strip(strings.Join(panel.lines, "\n")); m.ai != nil || !strings.Contains(text, "Last run: ✓ Said hello") {
		t.Errorf("esc puts the run away: the panel's a strip again, saying how it went:\n%s", text)
	}
}

// A risky command stops at its question for you; your answer goes back to it.
func TestAIStopsForRiskyCommands(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"press", `{"why":"Running the risky one","keys":["Enter"]}`},
		{"done", `{"why":"Done","summary":"You chose not to."}`},
	}}
	m, _, sent := aiMenu(t, f, config.Command{Name: "Wipe", Cmd: "wipe", Risk: config.RiskWarn})
	m.showSection(&m.opt.Config.Sections[0])
	run(t, m, m.startAI("wipe"), func() bool { return m.ai != nil && m.ai.yourTurn })
	if m.mode != modeConfirm || len(*sent) != 0 {
		t.Fatalf("it should stop at the question: mode %v, sent %v", m.mode, *sent)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // No, first
	run(t, m, cmd, func() bool { return m.ai != nil && m.ai.done })
	if len(*sent) != 0 || !strings.Contains(f.got[len(f.got)-1], "went back") {
		t.Errorf("sent %v; told %q", *sent, f.got[len(f.got)-1])
	}
}

// It can't quit clingon: esc at the top isn't pressed for it.
func TestAICantQuit(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"press", `{"why":"Leaving","keys":["Escape"]}`},
		{"done", `{"why":"Done","summary":"ok"}`},
	}}
	m, _, _ := aiMenu(t, f, config.Command{Name: "A", Cmd: "a"})
	run(t, m, m.startAI("quit"), func() bool { return m.ai != nil && m.ai.done })
	if m.mode == modeQuit || !strings.Contains(f.got[len(f.got)-1], "quits clingon: not pressed") {
		t.Errorf("mode %v; told %q", m.mode, f.got[len(f.got)-1])
	}
}

// While it works, your keys wait, and esc stops it.
func TestAIStopsOnEsc(t *testing.T) {
	f := &fakeModel{moves: [][2]string{{"press", `{"why":"Down","keys":["Down"]}`}}}
	m, _, _ := aiMenu(t, f, config.Command{Name: "A", Cmd: "a"}, config.Command{Name: "B", Cmd: "b"})
	m.startAI("anything")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if !strings.Contains(m.hint, "esc stops it") {
		t.Errorf("hint %q", m.hint)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.ai == nil || !m.ai.done || m.ai.err != "stopped" {
		t.Fatalf("esc stops it: %+v", m.ai)
	}
	if keys := m.barKeys(); len(keys) != 1 || keys[0] != "esc close AI" {
		t.Errorf("keys %q", keys)
	}
}

// A key added while clingon's open (`clingon ai key`) brings Give a task
// back: clingon's own sections are built again when the menu catches up.
func TestAIKeyEnablesGiveATask(t *testing.T) {
	keyed := false
	build := func() []*config.Section {
		why := ""
		if !keyed {
			why = "Add your OpenRouter key first"
		}
		return []*config.Section{{Name: "AI", Icon: "•", Category: "Clingon", Commands: []config.Command{{ID: AITaskID, Name: "Give a task", Cmd: "<task>", Disabled: why, Args: []config.Arg{{Name: "task"}}}}}}
	}
	m := New(Options{Config: &config.Config{}, Builtin: build, Panel: &fakePanel{}, HasAIKey: func() bool { return keyed }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m.showSection(m.sectionNamed("AI"))
	if it := m.list.selected(); it == nil || !it.disabled {
		t.Fatalf("no key: greyed out, on %+v", it)
	}
	keyed = true
	m.catchUp()
	if it := m.list.selected(); it == nil || it.disabled || it.title != "Give a task" {
		t.Errorf("with a key: on %+v", it)
	}
}

// Done, the panel shows the short headline, not the summary: that's in the log.
func TestAIPanelShowsTheHeadline(t *testing.T) {
	m, _, _ := aiMenu(t, &fakeModel{}, config.Command{Name: "A", Cmd: "a"})
	m.ai = &aiRun{task: "t", client: &llm.Client{Model: "x/y"}, done: true, headline: "Ran all three · rated 9 and 8", summary: strings.Repeat("A long summary. ", 60), logPath: "/tmp/x.md"}
	text := strings.Join(m.aiPanel(80, 10), "\n")
	if strings.Contains(text, "A long summary") || !strings.Contains(text, "Ran all three") || !strings.Contains(text, "Log: /tmp/x.md") || strings.Contains(text, "clingon ai log") { // one line points to the log: its path
		t.Errorf("panel:\n%s", text)
	}
}

// With the pack form open, the AI's keys go to it (in the shell's pane),
// esc included: the menu's "esc at the top quits" doesn't apply there.
func TestAIEscLeavesTheForm(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"press", `{"why":"Leave the form","keys":"Escape"}`},
		{"done", `{"why":"x","summary":"x"}`},
	}}
	m, _, _ := aiMenu(t, f, config.Command{Name: "A", Cmd: "a"})
	var pressed []string
	m.opt.ShellKey = func(k string, text bool) error { pressed = append(pressed, k); return nil }
	m.opt.ShellScreen = func() string { return "PACK\n▶ Name  Demo" }
	m.busy, m.editing = "Create a pack", "a new pack · esc when done"
	run(t, m, m.startAI("leave the form"), func() bool { return m.ai != nil && m.ai.done })
	if len(pressed) != 1 || pressed[0] != "Escape" {
		t.Errorf("pressed in the form: %v", pressed)
	}
	if strings.Contains(strings.Join(f.got, "\n"), "quits clingon") {
		t.Error("esc in the form shouldn't be refused")
	}
}

// A move's keys that close the form end there: what's left of them was meant
// for the form, and is pressed nowhere. (A spare Escape once went on into the
// shell behind it, where zsh took it and the next command's first letter as
// one key: "df -h" ran as "f -h".) The AI's told, and carries on.
func TestAIKeysStopWhenTheFormCloses(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"press", `{"why":"Saving and leaving the form","keys":["Enter","Escape","Escape","Escape"]}`},
		{"done", `{"why":"x","summary":"x"}`},
	}}
	m, _, sent := aiMenu(t, f, config.Command{Name: "A", Cmd: "a"})
	var pressed []string
	open := true
	m.opt.ShellKey = func(k string, text bool) error {
		pressed = append(pressed, k)
		if len(pressed) == 3 { // the second Escape leaves the form
			open = false
		}
		return nil
	}
	m.opt.ShellScreen = func() string { return "PACK\n▶ Name  Demo" }
	m.opt.Shell = func() ShellState { // asked fresh: the menu hasn't heard the form's gone
		if open {
			return ShellState{Busy: "clingon"}
		}
		return ShellState{}
	}
	m.busy, m.editing = "Create a pack", "a new pack · esc when done"
	run(t, m, m.startAI("make a pack"), func() bool { return m.ai != nil && m.ai.done })
	if got := strings.Join(pressed, " "); got != "Enter Escape Escape" {
		t.Errorf("pressed in the form: %q, want the keys up to the one that closed it and no more", got)
	}
	if len(*sent) != 0 {
		t.Errorf("nothing was to be sent to the shell: %v", *sent)
	}
	if m.section != nil || m.mode != modeBrowse || m.busy != "" {
		t.Errorf("the spare Escape mustn't be pressed in the menu either: section %v, mode %v, busy %q", m.section, m.mode, m.busy)
	}
	if last := f.got[len(f.got)-1]; !strings.Contains(last, formClosedNote) {
		t.Errorf("the AI should be told the rest weren't pressed:\n%s", last)
	}
}

// With the form open, the AI's told where its keys are going, and the
// form's keys: its screen, as text, can't show the text cursor.
func TestAISeesWhereFormKeysGo(t *testing.T) {
	m, _, _ := aiMenu(t, &fakeModel{}, config.Command{Name: "A", Cmd: "a"})
	m.opt.ShellKey = func(string, bool) error { return nil }
	m.opt.ShellScreen = func() string { return "PACK\n▶ Name  demo" }
	m.busy, m.editing = "Create a pack", "a new pack · esc when done"
	if err := config.WriteFormKeys([]string{config.FormIssuePrefix + "names use only letters", "enter next", "esc keep", config.FormStatePrefix + "typing into the pack's Name"}); err != nil {
		t.Fatal(err)
	}
	s := m.aiScreen()
	for _, want := range []string{"Your keys now: typing into the pack's Name", "Refused: names use only letters", "Status bar keys: enter next · esc keep"} {
		if !strings.Contains(s, want) {
			t.Errorf("want %q in:\n%s", want, s)
		}
	}
}

func TestKeyListTakesAString(t *testing.T) {
	var a aiArgs
	if err := json.Unmarshal([]byte(`{"keys":"Down Down, Enter"}`), &a); err != nil || len(a.Keys) != 3 || a.Keys[2] != "Enter" {
		t.Errorf("%v %q", err, a.Keys)
	}
	if err := json.Unmarshal([]byte(`{"keys":["Up"]}`), &a); err != nil || len(a.Keys) != 1 {
		t.Errorf("%v %q", err, a.Keys)
	}
}

// The oldest screens are left out, all but the last
// three, counted again only every few turns so the cached start holds.
func TestAITrimsOldScreens(t *testing.T) {
	a := &aiRun{turns: aiTrimEvery}
	var msgs []llm.Message
	for i := range 6 {
		msgs = append(msgs, llm.Message{Role: "assistant"}, llm.Message{Role: "tool", Content: fmt.Sprintf("screen %d", i)})
	}
	msgs[3].Content = "The user answered: Hex" + aiScreenSep + "screen 1" // what you said stays
	got := a.trimmed(append([]llm.Message(nil), msgs...))
	var kept []string
	for _, m := range got {
		if m.Role == "tool" {
			kept = append(kept, m.Content)
		}
	}
	gone := screenLeftOut("")
	if strings.Join(kept, ",") != gone+",The user answered: Hex\n\n"+gone+","+gone+",screen 3,screen 4,screen 5" {
		t.Errorf("kept %q", kept)
	}
	if msgs[1].Content != "screen 0" {
		t.Error("the conversation itself keeps every screen: only what's sent is trimmed")
	}
	a.turns++ // not a turn to count again: the same three left out
	more := append(msgs, llm.Message{Role: "tool", Content: "screen 6"})
	if got := a.trimmed(append([]llm.Message(nil), more...)); got[7].Content != "screen 3" {
		t.Errorf("between counts, the same ones stay: %q", got[7].Content)
	}
}

func TestFormOnlyDropsThePreview(t *testing.T) {
	screen := "  PACK                          _pack.md\n  Name   Demo                   ---\n\n\n  Command  echo hi              menu: other"
	got := formOnly(screen)
	if strings.Contains(got, "_pack.md") || strings.Contains(got, "menu: other") || !strings.Contains(got, "Name   Demo") || strings.Contains(got, "\n\n\n") {
		t.Errorf("%q", got)
	}
}

// Before its first move names the task, the panel's title is a spinner, not
// the task in full; a first move that doesn't name it leaves the task's first
// words.
func TestAIPanelTitleSpinsTillNamed(t *testing.T) {
	f := &fakeModel{moves: [][2]string{{"done", `{"why":"Done","headline":"ok","summary":"ok"}`}}}
	m, _, _ := aiMenu(t, f, config.Command{Name: "A", Cmd: "a"})
	task := "Without browsing sections, use search to find three commands and run each one"
	cmd := m.startAI(task)
	if head := ansi.Strip(m.aiPanel(80, 6)[0]); strings.Contains(head, "Without browsing") || !strings.ContainsAny(head, strings.Join(aiSpinFrames, "")) {
		t.Errorf("unnamed: %q", head)
	}
	run(t, m, cmd, func() bool { return m.ai != nil && m.ai.done })
	if head := ansi.Strip(m.aiPanel(80, 6)[0]); !strings.Contains(head, "Without browsing sections, use search…") {
		t.Errorf("not named: %q", head)
	}
}

// In a blank's pick list, the AI sees which option the cursor's on: the
// highlighted row's band comes before its bar, and the bar's still found.
func TestAISeesThePickCursor(t *testing.T) {
	m, _, _ := aiMenu(t, &fakeModel{}, config.Command{Name: "Conv", Cmd: "x <to>", Args: []config.Arg{{Name: "to", Type: "choose", Default: "2", Options: []config.Option{{Value: "2", Label: "Binary"}, {Value: "16", Label: "Hex"}}}}})
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if s := m.aiScreen(); !strings.Contains(s, "▶ 2\n") || strings.Contains(s, "▶ 16") || !strings.Contains(s, "Binary") { // what it means is under the list, not beside it
		t.Errorf("the cursor's option should be marked:\n%s", s)
	}
}

// Steps read as intents: the moves of one step, said the same way, are one
// step; each is tidied (a capital, no full stop).
func TestAIStepsGroupByIntent(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"press", `{"why":"adding option: Binary.","keys":["Down"]}`},
		{"press", `{"why":"Adding option: Binary","keys":["Up"]}`},
		{"press", `{"why":"Adding option: Hex","keys":["Down"]}`},
		{"done", `{"why":"Done","headline":"ok","summary":"ok"}`},
	}}
	m, _, _ := aiMenu(t, f, config.Command{Name: "A", Cmd: "a"}, config.Command{Name: "B", Cmd: "b"})
	run(t, m, m.startAI("t"), func() bool { return m.ai != nil && m.ai.done })
	var got []string
	for _, s := range m.ai.steps {
		got = append(got, s.text)
	}
	if strings.Join(got, " | ") != "Adding option: Binary | Adding option: Hex | Done" {
		t.Errorf("steps %q", got)
	}
}

// While it drives, the status bar says so (a chip of its own) and gives its
// keys, not the form's or the menu's; at a risky command it's your turn. And
// opening the pack form doesn't take your keys there: the menu keeps them, so
// esc stops it.
func TestAIDrivingIsSaid(t *testing.T) {
	focused := false
	m, _, _ := aiMenu(t, &fakeModel{}, config.Command{Name: "A", Cmd: "a"})
	m.opt.FocusMenu = func() {}
	m.opt.FocusShell = func() { focused = true }
	m.ai = &aiRun{task: "t", client: &llm.Client{Model: "x/y"}, ctx: context.Background(), cancel: func() {}}
	if m.where() != "AI DRIVING" || !strings.Contains(ansi.Strip(m.statusBar(120)), "AI DRIVING") {
		t.Errorf("chip %q", m.where())
	}
	m.busy, m.editing, m.blurred = "Create a pack", "a new pack · esc when done", true
	// Focus has gone to the form with it: esc still stops it from there (the
	// window sends it to the menu), so that's the one key said.
	if bar := ansi.Strip(m.statusBar(120)); !strings.Contains(bar, "esc stop AI") || strings.Contains(bar, "shift-tab menu") {
		t.Errorf("the form open, focus there with the AI: %q", bar)
	}
	m.ai.yourTurn = true
	if m.where() != "YOUR TURN" {
		t.Errorf("chip %q", m.where())
	}
	m.ai.yourTurn, m.busy, m.editing, m.blurred = false, "", "", false
	m.finish(pending{editor: true, name: "Create a pack", cmd: "clingon pack create --menu", editing: "a new pack · esc when done"})
	if focused {
		t.Error("the AI opening the form shouldn't move your keys there")
	}
	m.ai.done = true
	if m.where() == "AI DRIVING" {
		t.Error("done: back to where you are")
	}
}

// The frames go gold while the AI drives and back when it isn't (your
// turn, or done), told to tmux only when that changes.
func TestAIDrivingFrames(t *testing.T) {
	m, _, _ := aiMenu(t, &fakeModel{}, config.Command{Name: "A", Cmd: "a"})
	var told []string
	m.opt.AIDriving = func(where string) { told = append(told, where) }
	m.opt.Bar, m.opt.BarWidth = func(string) {}, func() int { return 120 }
	draw := func() {
		if cmd := m.drawBar(nil); cmd != nil {
			cmd()
		}
	}
	draw()
	m.ai = &aiRun{task: "t", client: &llm.Client{Model: "x/y"}, ctx: context.Background(), cancel: func() {}}
	draw()
	draw()
	// Its keys go to the pack form, in the shell's pane: the gold frame goes
	// there with it, and back to the menu when the form's closed.
	m.opt.ShellKey, m.opt.ShellScreen = func(string, bool) error { return nil }, func() string { return "" }
	m.busy, m.editing = "Create a pack", "a new pack · esc when done"
	draw()
	draw()
	m.busy, m.editing = "", ""
	draw()
	m.ai.yourTurn = true
	draw()
	m.ai.yourTurn, m.ai.done = false, true
	draw()
	if got := strings.Join(told, ","); got != "menu,shell,menu," {
		t.Errorf("frames told %q: want gold round the pane it's working in, each change once, and off when it isn't driving", got)
	}
}

// The status bar never says what's stopped being true. "The AI is working"
// goes the moment it isn't, however it ended and wherever focus is: no key
// in the menu has to come along and clear it.
func TestStatusHintsDontOutliveTheirState(t *testing.T) {
	m, _, _ := aiMenu(t, &fakeModel{}, config.Command{Name: "A", Cmd: "a"})
	m.opt.Bar, m.opt.BarWidth = func(string) {}, func() int { return 120 }
	m.ai = &aiRun{task: "t", client: &llm.Client{Model: "x/y"}, ctx: context.Background(), cancel: func() {}}
	m.Update(typed("z")) // one of your keys while it works: it's told why nothing happened
	if !strings.Contains(ansi.Strip(m.statusBar(120)), "The AI is working") {
		t.Fatalf("a key while the AI works should say so: %q", ansi.Strip(m.statusBar(120)))
	}
	m.blurred, m.focus = true, "shell" // you've gone to the shell: no key of the menu's will clear it
	m.stopAI("stopped")
	m.Update(busyGraceMsg{}) // anything at all arriving: the bar's worked out afresh
	if bar := ansi.Strip(m.statusBar(120)); strings.Contains(bar, "The AI is working") {
		t.Errorf("stopped, and the bar still says it's working: %q", bar)
	}

	// A hint that answers a key in the menu goes when you leave the menu.
	m.closeAI()
	m.blurred, m.focus = false, "menu"
	m.hint = "Press / to filter"
	m.Update(changeMsg{where: "shell"})
	if m.hint != "" {
		t.Errorf("gone to the shell, the menu's hint should go: %q", m.hint)
	}
	// One about a recipe running goes when it's no longer running.
	m.run = &recipeRun{}
	m.hintWhile("The recipe is running: x clears it", func() bool { return m.run != nil })
	m.freshen()
	if m.hint == "" {
		t.Error("still running: still said")
	}
	m.run = nil
	m.freshen()
	if m.hint != "" {
		t.Errorf("not running any more: %q", m.hint)
	}
}

// A command that never finishes by itself (git log in its pager, say) isn't
// a dead end: once its screen's gone still the AI's shown the shell as it
// is, and drives it. Keys that move around or leave go straight to it;
// anything it would type into the program is yours to allow first.
func TestAIDrivesAProgramThatKeepsRunning(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"press", `{"why":"Waiting on the log","keys":[]}`},
		{"type", `{"why":"Answering it","text":"yes"}`},
		{"press", `{"why":"Leaving the pager","keys":["q"]}`},
		{"done", `{"why":"x","summary":"x"}`},
	}}
	m, _, _ := aiMenu(t, f, config.Command{Name: "A", Cmd: "a"})
	var pressed []string
	m.opt.ShellKey = func(k string, text bool) error {
		pressed = append(pressed, k)
		if k == "q" {
			m.busy = "" // the pager's gone
		}
		return nil
	}
	m.opt.ShellScreen = func() string { return "" }
	m.opt.ShellTail = func() string { return "abc123 A commit\n(END)" }
	var asked []bool
	m.opt.ShellStill = func(afterNext bool, over <-chan struct{}) bool { // its screen's still, straight away
		asked = append(asked, afterNext)
		return true
	}
	m.busy = "git" // running, and not the pack form
	// It stops at the question about typing "yes" into it: yours to answer.
	run(t, m, m.startAI("show the log"), func() bool { return m.ai != nil && (m.ai.done || m.ai.question != "") })
	if m.ai.question == "" || !strings.Contains(m.ai.question, "yes") || !strings.Contains(m.ai.question, "git") || len(pressed) != 0 {
		t.Fatalf("typing into a running program should be put to the user first: question %q, pressed %v", m.ai.question, pressed)
	}
	if fmt.Sprint(m.ai.answers) != "[No Yes]" || m.ai.answer != 0 {
		t.Errorf("answers %v, on %d: No first", m.ai.answers, m.ai.answer)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // to Yes
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	run(t, m, cmd, func() bool { return m.ai != nil && m.ai.done })
	if got := strings.Join(pressed, " "); got != "y e s q" {
		t.Errorf("pressed in the shell: %q, want what you allowed, then the q, which needs no asking", got)
	}
	all := strings.Join(f.got, "\n---\n")
	for _, want := range []string{"git is still running in the shell", "(END)", "Your keys go to it now"} {
		if !strings.Contains(all, want) {
			t.Errorf("the AI should have been told %q; it was told:\n%s", want, all)
		}
	}
	if len(asked) == 0 || asked[0] {
		t.Errorf("the first wait on a command is for its screen to go still, not to change first: %v", asked)
	}
}

// Said no, nothing's typed into the running program, and the AI's told.
func TestAIDoesntTypeIntoAProgramWhenYouSayNo(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"press", `{"why":"Waiting","keys":[]}`},
		{"type", `{"why":"Answering it","text":"yes"}`},
		{"done", `{"why":"x","summary":"x"}`},
	}}
	m, _, _ := aiMenu(t, f, config.Command{Name: "A", Cmd: "a"})
	var pressed []string
	m.opt.ShellKey = func(k string, text bool) error { pressed = append(pressed, k); return nil }
	m.opt.ShellScreen = func() string { return "" }
	m.opt.ShellStill = func(bool, <-chan struct{}) bool { return true }
	m.busy = "sudo"
	run(t, m, m.startAI("t"), func() bool { return m.ai != nil && (m.ai.done || m.ai.question != "") })
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // No, where the cursor starts
	run(t, m, cmd, func() bool { return m.ai != nil && m.ai.done })
	if len(pressed) != 0 {
		t.Errorf("you said no: nothing should be typed, got %v", pressed)
	}
	if !strings.Contains(strings.Join(f.got, "\n"), "The user said no") {
		t.Error("the AI should be told you said no")
	}
}

// The panel's always there once the AI has a key: one row at rest (how to
// give it a task, how the last run went), the full panel while it works,
// back to one row once the run's put away; enter at rest goes to the task's
// box. With no key, there's no strip.
func TestAIPanelStripAtRest(t *testing.T) {
	keyed := false
	panel := &rowsPanel{}
	build := func() []*config.Section {
		return []*config.Section{{Name: "AI", Icon: "•", Category: "Clingon", Commands: []config.Command{{ID: AITaskID, Name: "Give a task", Cmd: "<task>", Args: []config.Arg{{Name: "task"}}}}}}
	}
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	m := New(Options{Config: &config.Config{}, Builtin: build, Panel: panel, HasAIKey: func() bool { return keyed }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	if m.panelOpen {
		t.Fatal("no key: no strip")
	}
	keyed = true
	m.catchUp()
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	if text := ansi.Strip(strings.Join(panel.lines, "\n")); !m.panelOpen || panel.rows != 1 || len(panel.lines) != 1 || !strings.Contains(text, "Ready") {
		t.Fatalf("a key: one row at rest, %d rows:\n%s", panel.rows, text)
	}
	m.blurred = true
	m.idlePanelKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeFill || m.job == nil || m.job.ref.cmd.ID != AITaskID {
		t.Errorf("enter in the strip goes to the task's box: mode %v", m.mode)
	}
	m.ai = &aiRun{task: "t", client: &llm.Client{Model: "x/y"}, ctx: context.Background(), cancel: func() {}, done: true, headline: "Did it"}
	m.aiDraw()
	if panel.rows <= 1 {
		t.Errorf("at work: the full panel, %d rows", panel.rows)
	}
	m.closeAI()
	m.aiDraw()
	if text := ansi.Strip(strings.Join(panel.lines, "\n")); panel.rows != 1 || !strings.Contains(text, "Last run: ✓ Did it") {
		t.Errorf("put away: one row again, with how it went: %d rows, %q", panel.rows, text)
	}
}

// rowsPanel is a panel that keeps how tall it was last opened.
type rowsPanel struct {
	fakePanel
	rows int
}

func (p *rowsPanel) Open(rows int) int { p.rows = rows; return 80 }

// What the AI types into a blank is text, never shell: quoted for where the
// blank sits, so it can't end the command and start another. Picked options
// are the pack's, and stay as they are.
func TestAIBlanksAreQuoted(t *testing.T) {
	for _, tc := range []struct{ cmd, val, want string }{
		{"echo <x>", "hello", "echo hello"},
		{"echo <x>", "a; rm -rf ~", `echo 'a; rm -rf ~'`},
		{"echo <x>", "~/my dir", `echo ~/'my dir'`},
		{`echo "<x>"`, "$(id) `id` !!", `echo ""'$(id) ` + "`id`" + ` !!'""`},
		{"echo '<x>'", "it's", `echo 'it'\''s'`},
		{`git commit -m "<x>"`, "fix the typo", `git commit -m ""'fix the typo'""`},
		{"x <o>", "a b", "x 'a b'"},   // typed in rather than picked
		{"x <o>", "-l -a", "x -l -a"}, // the pack's own option
	} {
		c := &config.Command{Name: "C", Cmd: tc.cmd, Args: []config.Arg{{Name: "x"}, {Name: "o", Options: []config.Option{{Value: "-l -a"}}}}}
		raw := map[string]string{"x": tc.val, "o": tc.val}
		vals, shown := shellValues(c, raw), shellValues(c, raw)
		if why := aiQuote(c, raw, vals, shown); why != "" {
			t.Fatal(why)
		}
		if got := tmpl.Render(c.Cmd, c.Known, vals); got != tc.want {
			t.Errorf("%s with %q: %q, want %q", tc.cmd, tc.val, got, tc.want)
		}
	}
	// And end to end: what the AI fills in through run.
	f := &fakeModel{moves: [][2]string{
		{"run", `{"why":"Running it","command":"C","blanks":{"x":"a; rm -rf ~"}}`},
		{"done", `{"why":"Done","headline":"ok","summary":"ok"}`},
	}}
	m, _, sent := aiMenu(t, f, config.Command{Name: "C", Cmd: "echo <x>", Args: []config.Arg{{Name: "x"}}})
	run(t, m, m.startAI("t"), func() bool { return m.ai != nil && m.ai.done })
	if len(*sent) != 1 || (*sent)[0] != `echo 'a; rm -rf ~'` {
		t.Errorf("sent %q", *sent)
	}
	if _, ok := quoteFor("a b", []byte{0, '"'}); ok {
		t.Error("a blank both in and out of quotes can't take a value that needs quoting")
	}
	if got, ok := quoteFor("plain", []byte{0, '"'}); !ok || got != "plain" {
		t.Errorf("a plain word goes anywhere: %q", got)
	}
}

// You typing into a blank is as before: the AI's quoting is only for it.
func TestYourBlanksAreAsTyped(t *testing.T) {
	m, _, sent := aiMenu(t, &fakeModel{}, config.Command{Name: "C", Cmd: "echo <x>", Args: []config.Arg{{Name: "x"}}})
	m.showSection(&m.opt.Config.Sections[0])
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(typed("a b"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(*sent) != 1 || (*sent)[0] != "echo a b" {
		t.Errorf("sent %q", *sent)
	}
}

// run goes to a command by name and fills in its blanks: a pick by its
// option, one left out keeps its default; a name that's in two packs needs
// its pack.
func TestAIRunFillsBlanks(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"run", `{"why":"Converting","command":"conv","blanks":{"n":"255","<to>":"16"}}`},
		{"done", `{"why":"Done","headline":"ok","summary":"ok"}`},
	}}
	m, _, sent := aiMenu(t, f,
		config.Command{Name: "Other", Cmd: "true"},
		config.Command{Name: "Conv", Cmd: "conv <n> <to> <sep>", Args: []config.Arg{
			{Name: "n"},
			{Name: "to", Options: []config.Option{{Value: "2", Label: "Binary"}, {Value: "16", Label: "Hex"}}},
			{Name: "sep", Default: "-"},
		}})
	run(t, m, m.startAI("t"), func() bool { return m.ai != nil && m.ai.done })
	if len(*sent) != 1 || (*sent)[0] != "conv 255 16 -" {
		t.Errorf("sent %q", *sent)
	}
}

// A run move that can't be done says why; the moves after it in the same
// reply aren't carried out, and each is answered saying so.
func TestAIFailedMoveDropsTheRest(t *testing.T) {
	f := &fakeModel{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []llm.Message `json:"messages"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.got = append(f.got, string(b))
		if len(f.got) == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[`+
				`{"id":"a","type":"function","function":{"name":"run","arguments":"{\"why\":\"x\",\"command\":\"Nope\"}"}},`+
				`{"id":"b","type":"function","function":{"name":"press","arguments":"{\"why\":\"x\",\"keys\":[\"Enter\"]}"}}]}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c","type":"function","function":{"name":"done","arguments":"{\"why\":\"x\",\"summary\":\"x\"}"}}]}}]}`)
	}))
	t.Cleanup(srv.Close)
	m, _, sent := aiMenu(t, f, config.Command{Name: "Say hello", Cmd: "echo hello"})
	m.opt.AIClient = func() (*llm.Client, error) {
		return &llm.Client{Key: "test", Model: "x/y", Endpoint: srv.URL}, nil
	}
	run(t, m, m.startAI("t"), func() bool { return m.ai != nil && m.ai.done })
	if len(*sent) != 0 {
		t.Errorf("the Enter after the failed move was pressed: sent %q", *sent)
	}
	if len(f.got) != 2 || !strings.Contains(f.got[1], "no command called") || !strings.Contains(f.got[1], `"tool_call_id":"b"`) || !strings.Contains(f.got[1], "Not done") {
		t.Errorf("second request: %s", f.got[len(f.got)-1])
	}
}

// A move whose command is running waits for it to finish, told so (idleMsg),
// not by looking again and again: then the move's result goes back. (One
// timer runs beside the wait, for a command that never finishes: aiStill.)
func TestAIWaitsForTheCommandToFinish(t *testing.T) {
	m, _, _ := aiMenu(t, &fakeModel{}, config.Command{Name: "A", Cmd: "a"})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	call := llm.ToolCall{ID: "c1"}
	call.Function.Name = "press"
	m.ai = &aiRun{task: "t", client: &llm.Client{Model: "x/y"}, ctx: ctx, cancel: cancel, call: &call, fillAt: -1}
	m.busy = "sleep"
	still := m.aiTick()
	if !m.ai.running || m.ai.call == nil || len(m.ai.msgs) != 0 {
		t.Fatalf("running: it should wait, told nothing yet (running %v, msgs %d)", m.ai.running, len(m.ai.msgs))
	}
	m.Update(idleMsg{seq: -1}) // the command's finished
	if m.ai.running || m.ai.call != nil || len(m.ai.msgs) == 0 || m.ai.msgs[len(m.ai.msgs)-1].ToolCallID != "c1" {
		t.Errorf("finished: the move's result should go back: running %v, msgs %d", m.ai.running, len(m.ai.msgs))
	}
	n := len(m.ai.msgs)
	if still != nil { // the wait's timer, arriving after it's over: nothing more is said
		m.Update(still())
	}
	if len(m.ai.msgs) != n {
		t.Errorf("a finished wait's timer shouldn't say anything: %d messages, then %d", n, len(m.ai.msgs))
	}
}

// The AI's panel goes straight to its pane, so its colours are sent as the
// palette's 256, never as 24-bit: Terminal.app reads a 24-bit colour tmux
// passes on as "dim" and a stray number, which greyed the whole panel.
func TestAIPanelIsDrawnIn256Colours(t *testing.T) {
	m, panel, _ := aiMenu(t, &fakeModel{})
	m.lastRun, m.lastRunOK = "Explored the menu", true
	m.panelDrawn = ""
	m.aiDraw()
	drawn := strings.Join(panel.lines, "\n")
	if drawn == "" || !strings.Contains(ansi.Strip(drawn), "Ready") {
		t.Fatalf("the strip wasn't drawn: %q", drawn)
	}
	if strings.Contains(drawn, "38;2;") || strings.Contains(drawn, "48;2;") {
		t.Errorf("24-bit colour in the panel: %q", drawn)
	}
	if !strings.Contains(drawn, fmt.Sprintf("48;5;%d", 147)) { // the model's chip: periwinkle, by number
		t.Errorf("the model's chip should be on the palette's periwinkle: %q", drawn)
	}
}

// The AI keeps its own notes: what it says to remember with a move stays in
// the conversation (its moves are never left out), is kept on the run and
// goes in the log, so what it learned early outlives the screens it learned
// it from. A screen left out says so, and where to look.
func TestAIRemembersPastTheScreensItsShown(t *testing.T) {
	f := &fakeModel{moves: [][2]string{
		{"press", `{"why":"Looking around","keys":["Down"],"remember":"Uptime and load is in System › Processes"}`},
		{"done", `{"why":"x","summary":"x"}`},
	}}
	m, _, _ := aiMenu(t, f, config.Command{Name: "A", Cmd: "a"}, config.Command{Name: "B", Cmd: "b"})
	run(t, m, m.startAI("look around"), func() bool { return m.ai != nil && m.ai.done })
	if len(m.ai.notes) != 1 || m.ai.notes[0] != "Uptime and load is in System › Processes" {
		t.Errorf("notes %q", m.ai.notes)
	}
	kept := false
	for _, msg := range m.ai.msgs {
		for _, c := range msg.ToolCalls {
			kept = kept || strings.Contains(c.Function.Arguments, "Uptime and load is in System")
		}
	}
	if !kept {
		t.Error("what it remembered should stay in the conversation, in its own move")
	}
	if log, _ := os.ReadFile(m.ai.logPath); !strings.Contains(string(log), "Remembered: Uptime and load is in System › Processes") {
		t.Errorf("the log should say what it remembered:\n%s", log)
	}
	if got := screenLeftOut("The user answered: Yes" + aiScreenSep + "a screen"); !strings.Contains(got, "The user answered: Yes") || !strings.Contains(got, "what you chose to remember") || strings.Contains(got, "a screen\n") {
		t.Errorf("a screen left out keeps what was said with it and says where its notes are: %q", got)
	}
	for _, tool := range aiTools() {
		if _, ok := tool.Params["remember"]; !ok {
			t.Errorf("%s should take remember, as every move does", tool.Name)
		}
	}
}
