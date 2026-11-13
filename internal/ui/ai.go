package ui

// The AI: given a task in the menu (CLINGON › AI › Give a task), a model
// on OpenRouter does it by using the menu as a person would. It's shown the
// menu's screen as text, and it answers with UI-sized moves (keys, typing,
// a search); clingon presses each key itself, a beat apart, so you can follow
// along. A panel under the shell shows the task, the steps and what it's
// thinking. Risky commands stop for you to answer, and esc stops it.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/llm"
)

// AITaskID is clingon's own "Give a task" command: answered, it starts the
// AI rather than running anything in the shell.
const AITaskID = "aitask-00000000"

// Panel is the AI's panel, under the shell: opened rows tall, it says how
// wide it is; drawn a screen at a time; closed when the AI's done.
type Panel interface {
	Open(rows int) (width int)
	Draw(lines []string)
	Close()
	Focused() bool // keys pressed there come to the menu: they're the AI's
}

// aiKeyGap is the beat between the AI's keys: quick, but slow enough to watch.
var aiKeyGap = 160 * time.Millisecond

// aiMaxTurns stops a run that's going nowhere.
const aiMaxTurns = 60

type aiStep struct {
	text string
	done bool
}

// aiRun is a task the AI is doing.
type aiRun struct {
	task   string
	client *llm.Client
	msgs   []llm.Message
	ctx    context.Context
	cancel context.CancelFunc
	seq    int // which reply is awaited; a stopped run's replies are dropped

	notes    []string // what it's chosen to remember, move by move: its memory once old screens are left out
	steps    []aiStep
	thinking string // what it last said it was thinking, or doing
	calls    []llm.ToolCall
	call     *llm.ToolCall // the move being carried out
	keys     []tea.KeyPressMsg
	running  bool          // a move's command is running in the shell: the move's result waits for it to finish
	inForm   bool          // a key of the move being carried out was pressed in the pack form: its keys are the form's, and nothing else's
	inShell  bool          // a command it ran is still running, and it's been shown so: its keys go to that program until it ends
	waits    int           // how many times it's waited on a command, so an old wait's end isn't taken for the latest's
	waitOver chan struct{} // closed when the wait's over (the command finished): whoever's listening for the shell to go still stops
	shellOK  bool          // you've said this move may type into the running program
	askShell bool          // the question up is that one
	turns    int           // replies so far
	nudged   int           // times told to use its tools
	waiting  bool          // for a reply
	yourTurn bool          // stopped at a risky command's question, for you to answer

	question string // a question for you, with its answers
	answers  []string
	answer   int

	done     bool
	summary  string
	headline string // the panel's line on how it ended: the summary's in the log
	err      string
	typing   bool   // its own key is being pressed: not one of yours
	looking  bool   // the screen's being described to it: the status bar says the menu's keys, not the AI's
	note     string // said with the move's result once it's settled: what the user answered, say

	log     *os.File  // the run's transcript, for looking into how it went
	logPath string    //
	asked   time.Time // when the reply awaited was asked for
	used    llm.Usage // tokens, all replies
	title   string    // a short name for the task, from the model
	started time.Time
	frame   int     // the spinner's
	cost    float64 // dollars, all replies (with aiDev)
	cached  int     // prompt tokens read from the cache (with aiDev)
	elided  int     // how many of the oldest screens are left out
	dev     bool    // what each reply cost goes in the log (see aiDevOn)

	// A run move fills in its command's blanks itself, a step at a time.
	fill    map[string]string // blanks not yet given, by name
	fillCmd *config.Command   // the command being filled in
	fillAt  int               // the blank last given, -1 for none yet
}

// aiDevOn turns on what's for working on clingon's AI, not for its users:
// what each reply cost (cache reads included), in the log. CLINGON_AI_DEV=1,
// or `clingon ai dev on`. It's read once, as a run starts.
func aiDevOn(s llm.Settings) bool {
	return os.Getenv("CLINGON_AI_DEV") != "" || s.Dev
}

// aiKeepScreens is how many of the latest screens the conversation keeps
// whole; the rest are left out every aiTrimEvery turns, together, so what's
// cached stays the same in between.
const (
	aiKeepScreens = 3
	aiTrimEvery   = 8
)

// AILogDir is where each run's transcript is kept: the newest aiKeepLogs.
func AILogDir() string { return filepath.Join(config.StateDir(), "ai-runs") }

const aiKeepLogs = 20

// logf adds to the run's transcript.
func (a *aiRun) logf(format string, args ...any) {
	if a.log != nil {
		fmt.Fprintf(a.log, format, args...)
	}
}

// openLog starts the run's transcript, and clears out old ones.
func (a *aiRun) openLog() {
	dir := AILogDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	a.logPath = filepath.Join(dir, time.Now().Format("2006-01-02T15-04-05")+".md")
	if f, err := os.OpenFile(a.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
		a.log = f
	}
	old, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	sort.Strings(old)
	for len(old) > aiKeepLogs {
		_ = os.Remove(old[0])
		old = old[1:]
	}
}

// trimmed is the conversation with the oldest screens left out: recounted
// every aiTrimEvery turns, so the conversation's start (what's cached) only
// changes then.
func (a *aiRun) trimmed(msgs []llm.Message) []llm.Message {
	screens := 0
	for _, m := range msgs {
		if m.Role == "tool" {
			screens++
		}
	}
	if a.turns%aiTrimEvery == 0 {
		a.elided = max(screens-aiKeepScreens, 0)
	}
	left := a.elided
	for i := range msgs {
		if left == 0 {
			break
		}
		if msgs[i].Role == "tool" {
			msgs[i].Content = screenLeftOut(msgs[i].Content)
			left--
		}
	}
	return msgs
}

// aiScreenSep comes between what a move's reply says (the user's answer, or
// why it didn't work) and the screen after it.
const aiScreenSep = "\n\nThe screen now:\n"

// screenLeftOut is a move's reply with its screen left out and what it says
// kept: the user's answers and what went wrong stay in the conversation,
// or the model asks again.
func screenLeftOut(reply string) string {
	const gone = "(earlier screen left out, to keep this short: what you chose to remember from it is in your moves)"
	if note, _, ok := strings.Cut(reply, aiScreenSep); ok {
		return note + "\n\n" + gone
	}
	return gone
}

// closeLog ends the transcript with how it went.
func (a *aiRun) closeLog() {
	if a.log == nil {
		return
	}
	switch {
	case a.err != "":
		a.logf("\n## Ended: %s\n", a.err)
	default:
		a.logf("\n## Done\n\n%s\n", a.summary)
	}
	a.logf("\nTurns: %d · tokens: %d in, %d out\n", a.turns, a.used.Prompt, a.used.Completion)
	if a.dev {
		a.logf("Cached: %d of %d in · cost: $%.4f\n", a.cached, a.used.Prompt, a.cost)
	}
	_ = a.log.Close()
	a.log = nil
}

type aiReplyMsg struct {
	seq   int
	reply llm.Reply
	err   error
}

type aiTickMsg struct{ seq int }

// aiStillMsg says the shell's screen has gone still while a move waits on
// the command it ran: what's there is all there'll be until someone presses
// something, or it finishes.
type aiStillMsg struct {
	run  *aiRun
	wait int // which wait: a later one has its own
}

// aiSpinMsg turns the panel's spinner.
type aiSpinMsg struct{ run *aiRun }

// aiSpinFrames turn while it's working.
var aiSpinFrames = []string{"▘", "▝", "▗", "▖"} // a dot going round: in clingon's font

const aiSpinGap = 180 * time.Millisecond

const AISystem = `You're clingon's built-in AI. Clingon is a terminal app: a menu of commands on the left, a real zsh shell on the right. Picking a command in the menu, filling in its blanks and running it types it into the shell, which runs it. You do the user's task by using clingon exactly as a person would, while they watch.

How you see and act:
- After each move you're shown the menu as text, its status bar (the keys that work right now) and the shell's screen, as the user sees it. The row the cursor's on starts with ▶; ▷ is the cursor greyed (focus is in a filter's box: ↓ or enter goes to the results).
- press sends keys: Enter, Escape, Up, Down, Left, Right, Space, BSpace, Home, End, PageUp, PageDown, ctrl+u, or one character (/ ? 1 r x [ ] …).
- type types text into a filter or a text box.
- search goes to the top of the menu and searches every command for what you give it, landing on the best match: the cursor's on it, in the results, not in the filter's box, so a key like 1 or Enter acts on it straight away. It's usually the quickest way to a command.
- run runs a command by its name, as the menu shows it, with its blanks filled in from what you give: clingon goes to it and fills them in itself, as you'd watch a person do. It's the quickest way to run anything whose name you know (from a search, say). A blank you leave out keeps its default, or stops for you to fill in.
- ask asks the user something, with answers to choose from.
- done ends the task.
- Your first move also takes "title": the task named in two to five words, in Title Case, like a chat's title.
- You don't keep every screen. To keep what you're sent short, screens from more than a few moves back are left out; your own moves stay. So when a screen tells you something you'll need later (a command's exact name and its pack, what an output said, how something felt to use if you've been asked to judge it), say so in that next move's "remember", in a line or two. When you sum up at the end, go by what you remembered, and never say you weren't shown something: you were, and it's since been left out.
- Every move takes "why": the step it's part of, shown to the user as a list. Write it as an intent, not a keystroke: a short phrase in sentence case starting with an -ing verb and naming what, like "Opening the pack form", "Adding option: Binary", "Running Convert base". Moves that serve one step repeat its text ("Adding option: Binary" for typing the value, moving to the label and typing it), so the list reads as the steps of the task.

Doing the task:
- Do all of it. Every part the user asked for, for real: if they ask for a pack with three commands, make the pack, give it three commands, and check they're there. Reading a description isn't trying something; neither is pointing at old output in the shell.
- Don't ask permission for what the task already asks. Ask only when you're truly stuck, or when a choice is genuinely the user's (which of two things they meant, say). When the task leaves the details to you, choose them yourself.
- When something doesn't work (a search finds nothing, a command's missing, a key does nothing), look at the screen, work out why, and try another way: a different search, adding a pack (Clingon › Packs › Add packs), making your own command in a pack, the ? screen for the keys. Give up on a part only after you've really tried, and then say what stopped you.
- Before done, check on the screen that each part is done. Then say what you did, part by part, and anything you couldn't do, and why.

Recipes:
- A recipe lines up commands that are already in the menu: search for each, press 1 on the first, 2 on the next and so on, then r for the recipe, and Add as… to name it. It needs no pack of its own: make a command only if none in the menu does the job.

The pack form:
- Clingon › Packs › Create a pack (or Edit a pack) opens the pack form in the shell's pane. While it's open your keys go to it and you're shown it, not the menu.
- After each move you're told where your keys go now ("Your keys now"): read it before your next keys. While you're typing into a field, every key goes into it, / included, so finish it with enter before you filter or move. A filter stays on after its box closes, until esc clears it.
- Typing on a field starts editing it; enter keeps what's typed and moves to the next field the pack still needs (so a new pack is four values, each followed by enter: its name, its module's, its command's, what it runs); esc keeps it. Picking a field found with the filter clears the filter. ctrl+u clears what's typed before the cursor. Esc on the form leaves it; every change is kept as it's made, and a new pack is added once it has its name, a module name, a command name and a command.
- Don't count Down presses to get around: / filters the form's rows: type a field's name (every one that matters has its own: "pack name", "module name", "command name", "runs" for what a command types into the shell), and the cursor goes to that field, nearest where you were; enter edits it, and [ ] jump between the pack, its modules and commands. The rows "+ ADD COMMAND" and "+ ADD MODULE" add one.
- A <name> in a command is a blank, asked for when it runs.

A command that keeps running:
- You see the shell as the user does. Most commands finish and you're shown their output. One that's still running once its screen has stopped changing is shown to you as it is: it may be working quietly, sitting in a pager (git log, man and less show ":" or "(END)"), or asking for something. While it runs, press and type go to it, not the menu: you drive it. Keys that only move around or leave (q, Enter, Space, Escape, the arrows, PageUp, PageDown, ctrl+c) are pressed straight away; anything else you type into it is put to the user first, who says yes or no. press with no keys waits until its screen changes again or it finishes.

Rules:
- Use clingon's menu. Typing your own line (Command Line › Type a command) always asks the user first.
- A risky command (⚠ or ☠) stops at a question for the user: it's theirs to answer, not yours. You'll be told what they chose.
- Be efficient (few, purposeful moves), but finishing the task matters more than speed.`

// aiSpin turns the spinner a frame on, while the run's going.
func (m *Model) aiSpin() tea.Cmd {
	a := m.ai
	return tea.Tick(aiSpinGap, func(time.Time) tea.Msg { return aiSpinMsg{run: a} })
}

// startAI starts the AI on task, opening its panel.
func (m *Model) startAI(task string) tea.Cmd {
	client, err := m.aiClient()
	if err != nil {
		return m.showNotice("The AI can't start", err.Error())
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.ai = &aiRun{task: task, client: client, ctx: ctx, cancel: cancel, dev: aiDevOn(llm.LoadSettings(config.StateDir())), fillAt: -1}
	m.ai.msgs = []llm.Message{
		{Role: "system", Content: AISystem},
		{Role: "user", Content: "The task: " + task + "\n\nThe menu now:\n\n" + m.aiScreen()},
	}
	m.ai.openLog()
	m.ai.logf("# AI run\n\nTask: %s\nModel: %s\nStarted: %s\n\n## Shown at the start\n\n```\n%s\n```\n",
		task, client.Model, time.Now().Format(time.RFC1123), m.ai.msgs[1].Content)
	m.openPanel()
	m.ai.started = time.Now()
	return tea.Batch(m.aiAsk(), m.aiSpin())
}

func (m *Model) aiClient() (*llm.Client, error) {
	if m.opt.AIClient != nil {
		return m.opt.AIClient()
	}
	key := llm.Key()
	if key == "" {
		return nil, fmt.Errorf("there's no OpenRouter key yet: add one with Clingon › AI › OpenRouter key")
	}
	s := llm.LoadSettings(config.StateDir())
	return &llm.Client{Key: key, Model: s.Model, Effort: "medium"}, nil // low gave up early, taking the shortest way it could justify
}

// aiAsk sends the conversation so far, off the UI thread.
func (m *Model) aiAsk() tea.Cmd {
	a := m.ai
	a.seq++
	a.turns++
	a.waiting = true
	if a.turns > aiMaxTurns {
		return m.stopAI("stopped after " + fmt.Sprint(aiMaxTurns) + " turns without finishing")
	}
	a.asked = time.Now()
	seq, client, ctx := a.seq, a.client, a.ctx
	msgs := a.trimmed(append([]llm.Message(nil), a.msgs...))
	return func() tea.Msg {
		reply, err := client.Chat(ctx, msgs, aiTools())
		return aiReplyMsg{seq: seq, reply: reply, err: err}
	}
}

// aiState is what the AI's doing, for the status bar: "driving" while it
// works, "your turn" at a risky command's question, "" otherwise.
func (m *Model) aiState() string {
	switch a := m.ai; {
	case a == nil || a.done:
		return ""
	case a.yourTurn:
		return "your turn"
	}
	return "driving"
}

// stepText is a move's step as the panel lists it: trimmed, its first letter
// capitalised, no full stop.
func stepText(why string) string {
	why = strings.TrimSuffix(strings.TrimSpace(why), ".")
	if why == "" {
		return ""
	}
	return upperFirst(why)
}

// stopAI ends the run: why (if it didn't finish) stays in the panel until
// esc closes it.
func (m *Model) stopAI(why string) tea.Cmd {
	a := m.ai
	if a == nil {
		return nil
	}
	a.cancel()
	a.waiting, a.keys, a.calls, a.call, a.yourTurn, a.question, a.fill = false, nil, nil, nil, false, "", nil
	if why != "" {
		a.err = why
	}
	a.done = true
	a.closeLog()
	return nil
}

// closeAI puts the run away, seen: the panel stays, as it always is, ready
// for the next, saying how this one went.
func (m *Model) closeAI() {
	if m.ai == nil {
		return
	}
	m.ai.cancel()
	m.ai.closeLog() // closed before it finished: said so
	m.lastRun, m.lastRunOK = m.ai.headline, true
	if m.ai.err != "" {
		m.lastRun, m.lastRunOK = "Stopped: "+m.ai.err, false
	}
	m.ai = nil
}

// panelRows is how tall the AI's panel is: one row at rest, a strip under
// the shell; the full panel while it works, as tall as the menu's info pane.
func (m *Model) panelRows() int {
	if m.ai == nil {
		return 1
	}
	return max(infoTop(m.h)-1, 4)
}

// openPanel opens the AI's panel, or makes it the height it should be now. It's
// there only once the AI has a key: someone who never uses it never sees it.
func (m *Model) openPanel() {
	if m.opt.Panel == nil || m.h == 0 {
		return
	}
	if !m.aiKeyed && m.ai == nil {
		if m.panelOpen {
			m.opt.Panel.Close()
			m.panelOpen, m.panelDrawn, m.panelHeight = false, "", 0
		}
		return
	}
	if rows := m.panelRows(); !m.panelOpen || rows != m.panelHeight {
		m.panelW, m.panelOpen, m.panelHeight, m.panelDrawn = m.opt.Panel.Open(rows), true, rows, ""
	}
}

// idleStrip is the panel at rest, one row: the model, how to give it a task,
// and how the last run went, at the right.
func (m *Model) idleStrip(w int) []string {
	w = max(w, 30)
	model := llm.ShortName(m.aiModel)
	chip := lipgloss.NewStyle().Background(soft).Foreground(ink).Bold(true).Padding(0, 1).Render(model)
	line := " " + chip + "  " + valueStyle.Render("Ready")
	if m.lastRun != "" {
		mark := valueStyle.Render("✓ ") // how it went, at a glance: done, or stopped
		if !m.lastRunOK {
			mark = errStyle.Render("✗ ")
		}
		last := faintStyle.Render("Last run: ") + mark + dimStyle.Render(m.lastRun)
		if room := w - ansi.StringWidth(line) - 3; room > 12 {
			last = ansi.Truncate(last, room, "…")
			line += strings.Repeat(" ", max(w-ansi.StringWidth(line)-ansi.StringWidth(last)-1, 1)) + last
		}
	}
	return []string{ansi.Truncate(line, w, "…")}
}

// idlePanelKey is a key pressed in the panel with no task: enter (or →)
// goes to Give a task's box, back in the menu.
func (m *Model) idlePanelKey(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "enter", "right":
	default:
		return nil
	}
	sec := m.sectionNamed("AI")
	if sec == nil {
		return nil
	}
	for _, c := range sec.All() {
		if c.ID != AITaskID {
			continue
		}
		if why := m.unavailable(c); why != "" {
			m.hint = why
			return nil
		}
		m.showSection(sec)
		for i, it := range m.list.view {
			if r, ok := it.ref.(cmdRef); ok && r.cmd.ID == AITaskID {
				m.list.cursor = i
			}
		}
		if m.opt.FocusSelf != nil {
			m.opt.FocusSelf()
		}
		return m.startCommand(cmdRef{sec.Name, sec, c})
	}
	return nil
}

// aiMsg handles the AI's own messages; it says whether msg was one.
func (m *Model) aiMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case aiReplyMsg:
		a := m.ai
		if a == nil || msg.seq != a.seq || a.done {
			return nil, true // a stopped run's
		}
		a.waiting = false
		if msg.err != nil {
			return m.stopAI(msg.err.Error()), true
		}
		r := msg.reply.Message
		a.msgs = append(a.msgs, r)
		u := msg.reply.Usage
		a.used.Prompt, a.used.Completion = a.used.Prompt+u.Prompt, a.used.Completion+u.Completion
		a.logf("\n## Reply %d (%.1fs, %d tokens in, %d out, finish %q)\n", a.turns, time.Since(a.asked).Seconds(), u.Prompt, u.Completion, msg.reply.Finish)
		if a.dev {
			a.cost, a.cached = a.cost+u.Cost, a.cached+u.Details.Cached
			a.logf("\nCached %d of %d in · $%.4f (run so far $%.4f) · %d screens left out\n", u.Details.Cached, u.Prompt, u.Cost, a.cost, a.elided)
		}
		if t := strings.TrimSpace(r.Reasoning); t != "" {
			a.logf("\nReasoning:\n\n%s\n", t)
		}
		if t := strings.TrimSpace(r.Content); t != "" {
			a.logf("\nSaid:\n\n%s\n", t)
		}
		for _, c := range r.ToolCalls {
			a.logf("\nMove: `%s %s`\n", c.Function.Name, c.Function.Arguments)
		}
		if t := strings.TrimSpace(firstNonEmpty(r.Reasoning, r.Content)); t != "" {
			a.thinking = t
		}
		if len(r.ToolCalls) == 0 {
			if a.nudged++; a.nudged > 2 {
				return m.stopAI("the model stopped without finishing"), true
			}
			a.msgs = append(a.msgs, llm.Message{Role: "user", Content: "Carry on through your tools: press, type, search, ask or done."})
			return m.aiAsk(), true
		}
		a.calls = r.ToolCalls
		cmd := m.aiNext()
		if m.ai != nil && m.ai.title == "" { // its first move didn't name it: the task's first words
			words := strings.Fields(m.ai.task)
			m.ai.title = strings.Join(words[:min(len(words), 5)], " ")
			if len(words) > 5 {
				m.ai.title += "…"
			}
		}
		return cmd, true
	case aiSpinMsg:
		if msg.run != m.ai || m.ai.done {
			return nil, true
		}
		m.ai.frame++
		return m.aiSpin(), true
	case aiTickMsg:
		if m.ai == nil || msg.seq != m.ai.seq || m.ai.done {
			return nil, true
		}
		return m.aiTick(), true
	case aiStillMsg:
		return m.aiStill(msg), true
	}
	return nil, false
}

type aiArgs struct {
	Why      string            `json:"why"`
	Keys     keyList           `json:"keys"`
	Text     string            `json:"text"`
	Question string            `json:"question"`
	Answers  []string          `json:"answers"`
	Summary  string            `json:"summary"`
	Headline string            `json:"headline"`
	Title    string            `json:"title"`
	Remember string            `json:"remember"`
	Command  string            `json:"command"`
	Blanks   map[string]string `json:"blanks"`
}

// keyList is the keys of a press: a list, as asked for, or one string of
// them ("Down Down Enter"), which models sometimes send instead.
type keyList []string

func (k *keyList) UnmarshalJSON(b []byte) error {
	var list []string
	if err := json.Unmarshal(b, &list); err == nil {
		*k = list
		return nil
	}
	var one string
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	if _, isKey := aiKey(one); isKey {
		*k = []string{one} // a single key, maybe a space or a comma
		return nil
	}
	*k = strings.FieldsFunc(one, func(r rune) bool { return r == ' ' || r == ',' })
	return nil
}

// aiNext carries out the next move of the reply, or, with none left, asks
// the model for its next ones.
func (m *Model) aiNext() tea.Cmd {
	a := m.ai
	if len(a.calls) == 0 {
		return m.aiAsk()
	}
	call := a.calls[0]
	a.calls, a.call = a.calls[1:], &call
	a.inForm, a.shellOK = false, false
	var in aiArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &in); err != nil {
		return m.aiFail("Those arguments weren't valid JSON: " + err.Error())
	}
	if t := strings.Trim(strings.TrimSpace(in.Title), `"'.`); t != "" && a.title == "" {
		a.title = t
		a.logf("\nTitle: %s\n", t)
	}
	if note := strings.TrimSpace(in.Remember); note != "" { // its own memory: kept in its moves when old screens are dropped
		a.notes = append(a.notes, note)
		a.logf("\nRemembered: %s\n", note)
	}
	if step := stepText(in.Why); step != "" {
		n := len(a.steps)
		if n == 0 || !strings.EqualFold(a.steps[n-1].text, step) { // the same intent again: still one step
			if n > 0 {
				a.steps[n-1].done = true
			}
			a.steps = append(a.steps, aiStep{text: step})
		}
	}
	switch call.Function.Name {
	case "press":
		for _, k := range in.Keys {
			key, ok := aiKey(k)
			if !ok {
				return m.aiFail(fmt.Sprintf("%q isn't a key it can press: nothing was pressed", k))
			}
			a.keys = append(a.keys, key)
		}
	case "type":
		for _, r := range in.Text {
			a.keys = append(a.keys, typed(string(r)))
		}
	case "search":
		if m.mode != modeBrowse {
			return m.aiFail("search works from the menu's lists: finish or leave what's open first (Escape or Left)")
		}
		if m.section != nil {
			m.showRoot(m.section.Name)
		}
		m.list.clearFilter()
		a.keys = append(a.keys, typed("/"))
		for _, r := range in.Text {
			a.keys = append(a.keys, typed(string(r)))
		}
		a.keys = append(a.keys, tea.KeyPressMsg{Code: tea.KeyDown})
		a.note = "The cursor's on the best match (▶), in the results, not the filter's box: a key like 1 or Enter acts on it now."
	case "run":
		if why := m.aiRunCommand(in.Command, in.Blanks); why != "" {
			return m.aiFail(why)
		}
	case "ask":
		if len(in.Answers) == 0 {
			in.Answers = []string{"Yes", "No"}
		}
		a.question, a.answers, a.answer = in.Question, in.Answers, 0
		a.logf("\nAsked the user: %s (%s)\n", in.Question, strings.Join(in.Answers, " / "))
		return nil // the user's answer carries on
	case "done":
		if n := len(a.steps); n > 0 {
			a.steps[n-1].done = true
		}
		a.summary, a.headline = in.Summary, strings.TrimSpace(in.Headline)
		if a.headline == "" { // its first sentence, then
			a.headline, _, _ = strings.Cut(in.Summary, ". ")
		}
		a.logf("\nHeadline: %s\n", a.headline)
		a.call = nil
		return m.stopAI("")
	default:
		return m.aiFail("there's no move called " + call.Function.Name)
	}
	return m.aiTickCmd()
}

func (m *Model) aiTickCmd() tea.Cmd {
	seq := m.ai.seq
	return tea.Tick(aiKeyGap, func(time.Time) tea.Msg { return aiTickMsg{seq: seq} })
}

// aiTickWhenStill presses the AI's next key once the shell's screen has
// stopped changing: after a key that may have closed what it was pressed in
// (esc in the pack form, q in a pager), so the next isn't typed into
// whatever's behind it. Heard from the shell's own output, not timed.
func (m *Model) aiTickWhenStill() tea.Cmd {
	still := m.opt.ShellStill
	if still == nil {
		return m.aiTickCmd()
	}
	seq := m.ai.seq
	return func() tea.Msg {
		still(false, nil)
		return aiTickMsg{seq: seq}
	}
}

// formClosedNote is said with a move's result when the pack form closed
// under it, part-way through its keys.
const formClosedNote = "The pack form closed part-way through that move, so the rest of its keys weren't pressed: they were for the form."

// formGone says whether the pack form a move's keys were going to has
// closed: the menu's heard so already, or, asked now, the shell's back at its
// prompt. Then the menu's brought up to date, as it would be on hearing.
func (m *Model) formGone() bool {
	if !m.formOpen() {
		return true
	}
	if m.opt.Shell == nil || m.opt.Shell().Busy != "" {
		return false
	}
	m.readShell()
	return true
}

// aiTick presses the AI's next key, or, with none left, waits for the shell
// to be done and says how the move went.
func (m *Model) aiTick() tea.Cmd {
	a := m.ai
	if a.yourTurn {
		return nil
	}
	// A move whose keys were going to the pack form, and the form's closed
	// (its esc left it): what's left of them was meant for the form. Pressed
	// now they'd go to the menu, or, the menu not having heard yet, into the
	// shell, where a spare Escape and the next command's first letter make
	// one key of zsh's and the letter's lost.
	if a.inForm && len(a.keys) > 0 && m.formGone() {
		a.keys = nil
		a.note = strings.TrimSpace(a.note + " " + formClosedNote)
	}
	if a.inShell && (m.busy == "" || m.formOpen()) { // the program it was pressing keys in has ended
		a.inShell = false
		if len(a.keys) > 0 {
			a.keys = nil
			a.note = strings.TrimSpace(a.note + " The program ended, so the rest of that move's keys weren't pressed: they were for it.")
		}
	}
	if len(a.keys) == 0 {
		if a.fill != nil {
			if cmd, filling := m.aiFillStep(); filling {
				return cmd
			}
		}
		if m.busy != "" && !m.formOpen() { // a command's running in the shell: its output's what comes next, once it's finished (aiIdle)
			a.running = true
			a.waits++
			still := m.opt.ShellStill
			if still == nil {
				return nil
			}
			// Or, its screen gone still with it not finished, the shell as it
			// is (aiStill). Asked to wait on one it's already been shown, that's
			// once the screen has changed again.
			run, wait, again, over := a, a.waits, a.inShell, make(chan struct{})
			a.waitOver = over
			return func() tea.Msg {
				if !still(again, over) {
					return nil // finished first
				}
				return aiStillMsg{run: run, wait: wait}
			}
		}
		note := a.note
		a.note = ""
		return m.aiResult(note)
	}
	k := a.keys[0]
	if why := m.aiRefuses(k); why != "" && !m.formOpen() && !a.inShell { // the form's keys aren't the menu's (esc there leaves it), nor are a running program's
		return m.aiFail(why)
	}
	if a.inShell && !m.formOpen() && m.opt.ShellKey != nil { // a program still running in the shell: its keys, a safe few
		if !aiShellKeys[keyName(k)] && !a.shellOK { // typing into a running program: yours to allow
			var text strings.Builder
			for _, q := range a.keys {
				if name, plain := tmuxKeyName(q); plain {
					text.WriteString(name)
				} else {
					text.WriteString(" ‹" + name + "› ")
				}
			}
			a.question = fmt.Sprintf("%s is waiting in the shell. Let the AI type this into it?  %s", m.busy, strings.TrimSpace(text.String()))
			a.answers, a.answer, a.askShell = []string{"No", "Yes"}, 0, true
			a.logf("\nAsked the user to allow typing into %s: %s\n", m.busy, strings.TrimSpace(text.String()))
			return nil // your answer carries on (aiUserKey)
		}
		a.keys = a.keys[1:]
		name, text := tmuxKeyName(k)
		if err := m.opt.ShellKey(name, text); err != nil {
			return m.aiFail("couldn't press that in the shell: " + err.Error())
		}
		return m.aiTickWhenStill() // it may have ended the program: nothing more until its screen's settled
	}
	a.keys = a.keys[1:]
	if m.formOpen() { // the pack form has the keys: pressed there, in the shell's pane
		name, text := tmuxKeyName(k)
		if err := m.opt.ShellKey(name, text); err != nil {
			return m.aiFail("couldn't press that in the form: " + err.Error())
		}
		a.inForm = true
		if k.Code == tea.KeyEscape { // it may have closed the form: nothing more until its screen's settled
			return m.aiTickWhenStill()
		}
		return m.aiTickCmd()
	}
	a.typing = true
	_, cmd := m.Update(k)
	a = m.ai
	if a == nil {
		return cmd
	}
	a.typing = false
	if m.mode == modeConfirm { // a risky command: the user answers
		a.keys, a.fill, a.yourTurn = nil, nil, true
		return cmd
	}
	return tea.Batch(cmd, m.aiTickCmd())
}

// aiShellKeys are the keys the AI presses in a program running in the shell
// without asking: they move around in it or leave it. Anything else is typing
// into it, which is put to you first.
var aiShellKeys = map[string]bool{"q": true, "enter": true, "space": true, "esc": true, "up": true, "down": true,
	"left": true, "right": true, "pgup": true, "pgdown": true, "ctrl+c": true}

// aiStill shows the AI the shell as it is when the command a move is waiting
// on hasn't finished and its screen has stopped changing: what a person
// watching would see, and take as "it's waiting, or it's busy". A pager or a
// prompt waits for a key for ever, and the move would with it. From then the
// AI's keys go to that program, until it ends.
func (m *Model) aiStill(msg aiStillMsg) tea.Cmd {
	a := m.ai
	if a == nil || msg.run != a || msg.wait != a.waits || a.done || !a.running {
		return nil // that wait's over
	}
	if m.busy == "" || m.formOpen() {
		return nil // finished: the menu's hearing so (aiIdle)
	}
	a.running, a.inShell = false, true
	note := a.note
	a.note = ""
	return m.aiResult(strings.TrimSpace(note + " " + m.busy + " is still running in the shell, and its screen has stopped changing: it's below, as the user sees it." +
		" Your keys go to it now, not the menu. If it's a pager or waiting for a key, press what it wants (q leaves a pager, ctrl+c stops most things); press with no keys waits for it to change or finish."))
}

// aiIdle carries on with the move whose command was running, now it's
// finished: the menu's told so (idleMsg), and the AI hears it then.
func (m *Model) aiIdle() tea.Cmd {
	a := m.ai
	if a == nil || a.done || !a.running {
		return nil
	}
	a.running, a.inShell = false, false
	if a.waitOver != nil { // no one need listen for the shell going still now
		close(a.waitOver)
		a.waitOver = nil
	}
	return m.aiTick()
}

// aiRefuses says why a key mustn't be pressed for the AI: esc at the top
// would quit clingon.
func (m *Model) aiRefuses(k tea.KeyPressMsg) string {
	if keyName(k) == "esc" && m.mode == modeBrowse && m.section == nil && !m.list.filtered() && !m.help {
		return "Escape at the top of the menu quits clingon: not pressed. You're already at the top."
	}
	return ""
}

// aiResult answers the move being carried out with how it went, and the
// screen now, and goes on to the next.
func (m *Model) aiResult(note string) tea.Cmd {
	m.aiAnswer(note)
	return m.aiNext()
}

// aiFail answers the move being carried out with why it didn't work, and
// the screen now, and drops the reply's moves after it: they were planned
// on it working. Each is answered, as the API needs, saying so.
func (m *Model) aiFail(why string) tea.Cmd {
	a := m.ai
	a.keys, a.fill = nil, nil
	m.aiAnswer(why)
	for _, c := range a.calls {
		a.msgs = append(a.msgs, llm.Message{Role: "tool", ToolCallID: c.ID, Content: "Not done: a move before it in your reply didn't work."})
		a.logf("\nSkipped: `%s %s`\n", c.Function.Name, c.Function.Arguments)
	}
	a.calls = nil
	return m.aiAsk()
}

// aiAnswer answers the move being carried out with how it went, and the
// screen now.
func (m *Model) aiAnswer(note string) {
	a := m.ai
	if a.call == nil {
		return
	}
	out := m.aiScreen()
	if note != "" {
		out = note + aiScreenSep + out
	}
	a.msgs = append(a.msgs, llm.Message{Role: "tool", ToolCallID: a.call.ID, Content: out})
	a.logf("\n### After `%s`\n\n```\n%s\n```\n", a.call.Function.Name, out)
	a.call = nil
}

// aiUserKey is one of your keys while the AI's on: esc stops it (or, done,
// closes its panel); a question of its takes ↑↓ and enter; at a risky
// command's question, the keys are yours. Others wait. It says whether the
// key was the AI's to handle.
func (m *Model) aiUserKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	a := m.ai
	k := keyName(msg)
	switch {
	case a.done:
		if k == "esc" {
			m.closeAI()
			return nil, true
		}
		m.closeAI() // seen: on with what was pressed
		return nil, false
	case a.yourTurn:
		return nil, false // yours to answer; aiAfterKey carries on once it's answered
	case a.question != "":
		switch k {
		case "up", "down":
			a.answer = round(a.answer, k, len(a.answers))
		case "enter", "right":
			choice := a.answers[a.answer]
			a.question, a.answers = "", nil
			if a.askShell { // whether it may type into the running program
				a.askShell = false
				a.logf("\nThe user said: %s\n", choice)
				if choice == "Yes" {
					a.shellOK = true
					return m.aiTickCmd(), true
				}
				a.keys = nil
				return m.aiResult("The user said no: nothing was typed into the running program."), true
			}
			return m.aiResult("The user answered: " + choice), true
		case "esc":
			return m.stopAI("stopped"), true
		}
		return nil, true
	case k == "esc":
		return m.stopAI("stopped"), true
	}
	m.hintWhile("The AI is working · esc stops it", func() bool { return m.aiState() == "driving" })
	return nil, true
}

// aiPanelKey is a key pressed in the AI's panel (tab there and back moves
// focus, through tmux): ↑↓ and enter answer its question, esc stops it, or
// closes the panel once it's done.
func (m *Model) aiPanelKey(msg tea.KeyPressMsg) tea.Cmd {
	a := m.ai
	switch {
	case a.done && keyName(msg) != "esc":
		return nil
	case a.yourTurn:
		m.hintWhile("Answer the risky command's question in the menu: tab to it", func() bool { return m.ai != nil && m.ai.yourTurn })
		return nil
	}
	cmd, _ := m.aiUserKey(msg)
	if m.ai != nil && strings.HasPrefix(m.hint, "The AI is working") {
		m.hint = "" // the panel's own keys are on the status bar
	}
	return cmd
}

// formOpen says whether the pack form is open in the shell: the AI's keys
// go there, and it's shown that, not the menu.
func (m *Model) formOpen() bool {
	return m.busy != "" && m.editing != "" && m.opt.ShellKey != nil && m.opt.ShellScreen != nil
}

// aiAfterKey carries on once you've answered a risky command's question.
func (m *Model) aiAfterKey() tea.Cmd {
	a := m.ai
	if a == nil || !a.yourTurn || m.mode == modeConfirm {
		return nil
	}
	a.yourTurn = false
	if m.busy == "" {
		return m.aiResult("The user went back without running it.")
	}
	a.note = "The user said Yes, and it ran: its output is in the shell's last lines." // once it's finished
	return m.aiTickCmd()
}

// aiScreen is the menu as the AI sees it: what's on the menu's screen, as
// text with the cursor's row marked, the keys that work, and the shell's
// last lines.
func (m *Model) aiScreen() string {
	if m.formOpen() { // the pack form, open in the shell's pane: that's where its keys go
		out := "The pack form is open, in the shell's pane; your keys go to it (esc leaves it, its changes kept as they're made). Its screen:\n\n" + formOnly(m.opt.ShellScreen())
		if state := config.FormState(); state != "" { // where your keys are going: the screen's text can't show its text cursor
			out += "\n\nYour keys now: " + state
		}
		if keys := config.FormKeys(); len(keys) > 0 {
			if why, ok := strings.CutPrefix(keys[0], config.FormIssuePrefix); ok {
				out += "\nRefused: " + why
				keys = keys[1:]
			}
			out += "\nStatus bar keys: " + strings.Join(keys, " · ")
		}
		return out
	}
	if m.ai != nil {
		m.ai.looking = true
		defer func() { m.ai.looking = false }()
	}
	// The cursor's bar starts its row; a pink cell anywhere else (a recipe
	// step's number, a chip) is something else.
	pink, grey := cursorBar(cursorStyle), cursorBar(blurredStyle)
	var lines []string
	blank := false
	for _, l := range strings.Split(m.view(), "\n") {
		// At the row's start, after nothing but colour (a highlighted row's
		// band comes first).
		for bar, mark := range map[string]string{pink: "▶", grey: "▷"} {
			if i := strings.Index(l, bar); bar != " " && i >= 0 && ansi.Strip(l[:i]) == "" {
				l = l[:i] + mark + l[i+len(bar):]
				break
			}
		}
		l = ansi.Strip(l)
		if strings.ContainsAny(l, "█▀▄") { // the logo: nothing to read
			continue
		}
		l = strings.TrimRight(l, " ")
		if l == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		lines = append(lines, l)
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(strings.Join(lines, "\n")))
	keys := m.barKeys()
	if m.mode != modeBrowse {
		keys = m.promptKeys()
	}
	fmt.Fprintf(&b, "\n\nStatus bar keys: %s", strings.Join(keys, " · "))
	if m.hint != "" && !strings.HasPrefix(m.hint, "The AI is working") {
		fmt.Fprintf(&b, "\nStatus bar says: %s", m.hint)
	}
	if m.errMsg != "" {
		fmt.Fprintf(&b, "\nStatus bar says: %s", m.errMsg)
	}
	if len(m.recipe) > 0 {
		var steps []string
		for i, s := range m.recipe {
			steps = append(steps, fmt.Sprintf("%d %s", i+1, s.ref.cmd.Name))
		}
		fmt.Fprintf(&b, "\nRecipe: %s", strings.Join(steps, " → "))
	}
	if m.busy != "" {
		fmt.Fprintf(&b, "\nRunning in the shell: %s", m.busy)
	}
	if m.opt.ShellTail != nil {
		if tail := strings.TrimSpace(m.opt.ShellTail()); tail != "" {
			fmt.Fprintf(&b, "\n\nThe shell, as the user sees it:\n%s", tail)
		}
	}
	return b.String()
}

// formOnly is the form's screen without its preview of the pack's files,
// the right half beside it, and without runs of blank lines: what the form
// says is all the AI needs.
func formOnly(screen string) string {
	lines := strings.Split(screen, "\n")
	width := 0
	for _, l := range lines {
		width = max(width, utf8.RuneCountInString(l))
	}
	var out []string
	blank := false
	for _, l := range lines {
		if r := []rune(l); len(r) > width/2+1 {
			l = string(r[:width/2+1])
		}
		l = strings.TrimRight(l, " ")
		if l == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// aiDraw draws the panel, if it's changed.
func (m *Model) aiDraw() {
	m.openPanel()
	if !m.panelOpen {
		return
	}
	lines := m.idleStrip(m.panelW)
	if m.ai != nil {
		lines = m.aiPanel(m.panelW, m.panelRows())
	}
	text := strings.Join(lines, "\n")
	if text == m.panelDrawn {
		return
	}
	m.panelDrawn = text
	// In the palette's 256, as the bars are: the panel goes straight to its
	// pane too, past the renderer that does this for the menu. Passed on by
	// tmux as 24-bit, Terminal.app reads each colour as "dim" and a stray
	// number: grey text, no chip, and dimness that leaks into the menu.
	drawn := make([]string, len(lines))
	for i, l := range lines {
		drawn[i] = in256(l)
	}
	m.opt.Panel.Draw(drawn)
}

// aiPanel is the panel: a header with the model and the task's title, and
// its turn and time at the right; under a rule, the steps on the left, and on
// the right whatever needs you (a question, a risky command's answer), how it
// ended, or that nothing does.
func (m *Model) aiPanel(w, rows int) []string {
	a := m.ai
	w, rows = max(w, 40), max(rows, 4)
	rule := faintStyle.Render(strings.Repeat("─", w))

	// The header.
	chip := lipgloss.NewStyle().Background(soft).Foreground(ink).Bold(true).Padding(0, 1).Render(llm.ShortName(a.client.Model)) // as at rest
	title := lipgloss.NewStyle().Bold(true).Foreground(text).Render(ansi.Truncate(a.title, max(w-30, 1), "…"))
	if a.title == "" { // being named: a spinner till it is, not the task in full
		title = cursorStyle.Render(aiSpinFrames[a.frame%len(aiSpinFrames)])
	}
	elapsed := time.Since(a.started)
	if a.started.IsZero() {
		elapsed = 0
	}
	meta := faintStyle.Render(fmt.Sprintf("turn %d · %d:%02d", a.turns, int(elapsed.Minutes()), int(elapsed.Seconds())%60))
	room := w - ansi.StringWidth(chip) - ansi.StringWidth(meta) - 4
	head := " " + chip + "  " + ansi.Truncate(title, max(room, 1), "…")
	head += strings.Repeat(" ", max(w-ansi.StringWidth(head)-ansi.StringWidth(meta)-1, 1)) + meta

	body := rows - 2
	needW := (w - 3) / 2 // what needs you, on the left, as the form's fields are; the steps as wide, on the right
	stepsW := w - needW - 3

	// Left: the steps, the latest last.
	var left []string
	for i, s := range a.steps {
		current := !s.done && i == len(a.steps)-1
		switch {
		case current && !a.done:
			mark := cursorStyle.Render(aiSpinFrames[a.frame%len(aiSpinFrames)])
			if a.question != "" || a.yourTurn {
				mark = warnStyle.Render("●") // waiting on you
			}
			left = append(left, mark+" "+valueStyle.Render(ansi.Truncate(s.text, stepsW-3, "…")))
		default:
			left = append(left, valueStyle.Render("✓")+" "+dimStyle.Render(ansi.Truncate(s.text, stepsW-3, "…")))
		}
	}
	if len(a.steps) == 0 {
		left = []string{cursorStyle.Render(aiSpinFrames[a.frame%len(aiSpinFrames)]) + " " + dimStyle.Render("Reading the menu")}
	}
	if len(left) > body {
		hidden := len(left) - body + 1
		left = append([]string{faintStyle.Render(fmt.Sprintf("… %d earlier", hidden))}, left[hidden:]...)
	}

	// Right: what needs you, or how it ended.
	heading := func(st lipgloss.Style, s string) string { return st.Bold(true).Render(strings.ToUpper(s)) }
	wrap := func(st lipgloss.Style, s string) []string {
		var out []string
		for _, l := range wrapLines(s, needW-2) {
			out = append(out, st.Render(l))
		}
		return out
	}
	var right []string
	var foot []string // pinned to the column's foot
	switch {
	case a.err != "":
		right = append([]string{heading(errStyle, "Stopped")}, wrap(dimStyle, upperFirst(a.err))...)
		if a.headline != "" {
			right = append(right, wrap(dimStyle, a.headline)...)
		}
	case a.done:
		line := wrap(lipgloss.NewStyle().Foreground(text), a.headline)
		if len(line) > 2 {
			line = append(line[:1], ansi.Truncate(line[1], needW-3, "…"))
		}
		right = append([]string{heading(valueStyle, "Done")}, line...) // all it did, and why: in the log, its path at the foot
	case a.question != "":
		right = append([]string{heading(warnStyle, "Needs you")}, wrap(lipgloss.NewStyle().Foreground(text), a.question)...)
		right = append(right, "")
		for i, ans := range a.answers {
			if i == a.answer {
				right = append(right, cursorBar(cursorStyle)+" "+cursorStyle.Render(ansi.Truncate(ans, needW-5, "…")))
			} else {
				right = append(right, "  "+dimStyle.Render(ansi.Truncate(ans, needW-5, "…")))
			}
		}
	case a.yourTurn:
		right = append([]string{heading(warnStyle, "Your turn")}, wrap(dimStyle, "A risky command is waiting for your answer, in the menu.")...)
	default:
		// Nothing needs you: what it's doing, as you asked for it, where a
		// question would be. (An empty half with one faint line read as broken.)
		right = append([]string{heading(faintStyle, "The task")}, wrap(dimStyle, a.task)...)
	}
	if a.done && a.logPath != "" {
		path := a.logPath
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+"/") {
			path = "~" + strings.TrimPrefix(path, home)
		}
		foot = []string{faintStyle.Render(ansi.Truncate("Log: "+path, needW-2, "…"))}
	}
	if limit := body - len(foot); len(right) > limit && limit > 1 { // a long summary: what fits, the rest in the log
		right = append(right[:limit-1], faintStyle.Render("… the rest is in the log"))
	}
	for len(right) < body-len(foot) {
		right = append(right, "")
	}
	right = append(right, foot...)

	out := []string{head, rule}
	bar := faintStyle.Render("│")
	for i := range body {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		need := " " + ansi.Truncate(r, needW-1, "…") // what needs you, left; the steps, right
		out = append(out, need+strings.Repeat(" ", max(needW-ansi.StringWidth(need), 0))+bar+"  "+ansi.Truncate(l, stepsW, "…"))
	}
	return out
}

// wrapLines wraps s to w cells.
func wrapLines(s string, w int) []string {
	return strings.Split(ansi.Wrap(s, max(w, 1), ""), "\n")
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// tmuxKeyName is a key press as tmux names it, for pressing in another
// pane; text says it's typed as it is.
func tmuxKeyName(k tea.KeyPressMsg) (string, bool) {
	switch k.Code {
	case tea.KeyEnter:
		return "Enter", false
	case tea.KeyEscape:
		return "Escape", false
	case tea.KeyUp:
		return "Up", false
	case tea.KeyDown:
		return "Down", false
	case tea.KeyLeft:
		return "Left", false
	case tea.KeyRight:
		return "Right", false
	case tea.KeyBackspace:
		return "BSpace", false
	case tea.KeyHome:
		return "Home", false
	case tea.KeyEnd:
		return "End", false
	case tea.KeyPgUp:
		return "PPage", false
	case tea.KeyPgDown:
		return "NPage", false
	case tea.KeySpace:
		return "Space", false
	}
	if k.Mod == tea.ModCtrl {
		return "C-" + string(k.Code), false
	}
	return k.Text, true
}

// aiKey is a key the AI asked for, as a key press.
func aiKey(k string) (tea.KeyPressMsg, bool) {
	named := map[string]rune{"enter": tea.KeyEnter, "escape": tea.KeyEscape, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "bspace": tea.KeyBackspace, "backspace": tea.KeyBackspace,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pageup": tea.KeyPgUp, "pagedown": tea.KeyPgDown}
	if c, ok := named[strings.ToLower(k)]; ok {
		return tea.KeyPressMsg{Code: c}, true
	}
	switch strings.ToLower(k) {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, true
	case "ctrl+u", "c-u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, true
	case "ctrl+c", "c-c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, true
	}
	if utf8.RuneCountInString(k) == 1 {
		return typed(k), true
	}
	return tea.KeyPressMsg{}, false
}

func aiTools() []llm.Tool {
	why := map[string]any{"type": "string", "description": "The step this move is part of, for the user: a short phrase in sentence case starting with an -ing verb and naming what, like \"Opening the pack form\", \"Adding option: Binary\" or \"Running Convert base\". Give the moves of one step the same text."}
	title := map[string]any{"type": "string", "description": "Your first move only: the task named in two to five words, in Title Case, like a chat's title."}
	remember := map[string]any{"type": "string", "description": "Optional. What the screens you've just been shown told you that you'll need later, in a line or two: a command's exact name and pack, what an output said, a number, what worked or didn't. Screens from more than a few moves ago are dropped from what you're sent; this isn't."}
	tool := func(name, desc string, props map[string]any, required ...string) llm.Tool {
		props["why"], props["title"], props["remember"] = why, title, remember
		return llm.Tool{Name: name, Description: desc, Params: props, Required: append([]string{"why"}, required...)}
	}
	return []llm.Tool{
		tool("press", "Press keys in the menu, in order; then see it.", map[string]any{
			"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "Enter, Escape, Up, Down, Left, Right, Space, BSpace, Home, End, PageUp, PageDown, ctrl+u, or one character."},
		}, "keys"),
		tool("type", "Type text into the filter or text box that's open; then see the menu. It doesn't press Enter.", map[string]any{
			"text": map[string]any{"type": "string"},
		}, "text"),
		tool("search", "Go to the top of the menu and search every command, landing on the best match; then see it.", map[string]any{
			"text": map[string]any{"type": "string", "description": "Words in the command's name, like git status."},
		}, "text"),
		tool("run", "Run a command by its name, its blanks filled in from what you give; clingon goes to it and fills them in, then you see the menu (and the shell, once it's run). A risky one still stops for the user.", map[string]any{
			"command": map[string]any{"type": "string", "description": "The command's name as the menu shows it, like Say hello; if two packs have one by that name, the pack's first: Network › Ping."},
			"blanks": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"},
				"description": "Each blank's value, by the blank's name (the <name> in what it runs). For one with options, the option's value; for one taking several, them all, as the blank joins them."},
		}, "command"),
		tool("ask", "Ask the user something, with answers to choose from; you're told their answer. Only when you're truly stuck, or a choice is genuinely theirs: never to ask leave to do what the task already asks.", map[string]any{
			"question": map[string]any{"type": "string"},
			"answers":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "question", "answers"),
		tool("done", "End the task: only once every part of it is done, checked on the screen, or you've truly tried and can't.", map[string]any{
			"headline": map[string]any{"type": "string", "description": "One short line for the panel, at most 12 words: how it went, with any ratings, like \"Ran all three · rated 9 objective, 8 subjective\"."},
			"summary":  map[string]any{"type": "string", "description": "For the log: what you did, part by part, anything you couldn't do and why, and any ratings with their reasons."},
		}, "headline", "summary"),
	}
}

// aiQuote makes every value the AI typed into a blank (raw) plain text in
// the command, in vals and shown: a blank's value goes into the shell as
// written, so `x; rm -rf ~` in a command that isn't risky would otherwise
// run without asking. Each is quoted for where its blank sits: bare, in
// '…' or in "…" (there it leaves the double quotes for single ones, so $,
// ` and ! stay as they are). Picked options and secrets are the pack's own,
// or never typed into the command, and stay as they are. It says why a
// value can't be made safe, or "".
func aiQuote(c *config.Command, raw, vals, shown map[string]string) string {
	for _, a := range c.Args {
		v := raw[a.Name]
		switch k := a.Kind(); {
		case v == "", k == "secret", k == "flags", k == "choose" && a.HasOption(v):
			continue
		}
		q, ok := quoteFor(v, config.QuotesAt(c.Cmd, a.Name, c.Known))
		if !ok {
			return fmt.Sprintf("%s: %q can't go in this command safely (its blank sits both inside and outside quotes)", a.Label(), v)
		}
		vals[a.Name], shown[a.Name] = q, q
	}
	return ""
}

// quoteFor is v quoted for every place its blank sits (the quote it's in,
// each time: see config.QuotesAt). A safe word needs nothing; otherwise the
// places must agree.
func quoteFor(v string, at []byte) (string, bool) {
	if shellSafe.MatchString(v) {
		return v, true
	}
	if len(at) == 0 {
		return shellValue(v), true
	}
	for _, q := range at[1:] {
		if q != at[0] {
			return "", false
		}
	}
	single := strings.ReplaceAll(v, "'", `'\''`)
	switch at[0] {
	case '\'':
		return single, true
	case '"':
		return `"'` + single + `'"`, true
	}
	return shellValue(v), true
}

// aiRunCommand starts a run move: it finds the command by name, goes to it
// and presses enter, as you would, and leaves its blanks for aiFillStep. It
// says why it can't, or "".
func (m *Model) aiRunCommand(name string, blanks map[string]string) string {
	a := m.ai
	if m.mode != modeBrowse {
		return "run works from the menu's lists: finish or leave what's open first (Escape or Left)"
	}
	name = strings.TrimSpace(name)
	pack, cmdName, inPack := strings.Cut(name, "›")
	var found []cmdRef
	for _, r := range m.commandsByID() {
		switch {
		case r.cmd.ID == AITaskID:
		case inPack && strings.EqualFold(strings.TrimSpace(pack), r.section) && strings.EqualFold(strings.TrimSpace(cmdName), r.cmd.Name),
			!inPack && strings.EqualFold(name, r.cmd.Name):
			found = append(found, r)
		}
	}
	switch len(found) {
	case 0:
		return fmt.Sprintf("there's no command called %q: search for it, or add the pack it's in", name)
	case 1:
	default:
		var where []string
		for _, r := range found {
			where = append(where, r.section+" › "+r.cmd.Name)
		}
		sort.Strings(where)
		return fmt.Sprintf("%q is in more than one pack: say which, like %s", name, strings.Join(where, " or "))
	}
	r := found[0]
	if why := m.unavailable(r.cmd); why != "" {
		return r.cmd.Name + " can't be run just now: " + why
	}
	m.showSection(r.sec)
	at := -1
	for i, it := range m.list.view {
		if c, ok := it.ref.(cmdRef); ok && c.cmd == r.cmd {
			at = i
		}
	}
	if at < 0 {
		return "couldn't find " + r.cmd.Name + " in its pack's list: go to it yourself"
	}
	m.list.cursor = at
	a.fill, a.fillCmd, a.fillAt = map[string]string{}, r.cmd, -1
	for k, v := range blanks {
		a.fill[strings.ToLower(strings.Trim(k, "<> "))] = v
	}
	a.keys = append(a.keys, tea.KeyPressMsg{Code: tea.KeyEnter})
	return ""
}

// aiFillStep gives the blank being asked for its value from the run move,
// pressing the keys you'd press; filling says whether it's still filling in.
// The blanks done (the command's run, or stopped at a risky one's question),
// what wasn't used is said with the move's result.
func (m *Model) aiFillStep() (cmd tea.Cmd, filling bool) {
	a := m.ai
	j := m.job
	if m.mode != modeFill || j == nil || j.ref.cmd != a.fillCmd {
		if len(a.fill) > 0 {
			var left []string
			for k := range a.fill {
				left = append(left, k)
			}
			sort.Strings(left)
			a.note = strings.TrimSpace(a.note + " Not blanks it asked for, so not used: " + strings.Join(left, ", ") + ".")
		}
		a.fill = nil
		return nil, false
	}
	arg := m.arg()
	if a.fillAt == j.step { // enter was pressed on it, and it's still asking
		return m.aiFail(fmt.Sprintf("The value for %s wasn't taken: the screen says why. Fill it in yourself, or Escape.", arg.Name)), true
	}
	a.fillAt = j.step
	v, given := a.fill[strings.ToLower(arg.Name)]
	delete(a.fill, strings.ToLower(arg.Name))
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	switch {
	case !given && (arg.Optional || m.initial(arg) != "" || arg.Multi() && !arg.AtLeastOne):
		// its default, or nothing
	case !given:
		a.fill = nil
		return m.aiFail(fmt.Sprintf("%s needs a value for %s: it's asking for it now. Fill it in yourself, or Escape.", j.ref.cmd.Name, arg.Name)), true
	case !j.usePick:
		j.input.SetValue("")
		for _, r := range v {
			a.keys = append(a.keys, typed(string(r)))
		}
	case j.pick.multi:
		for i := range j.pick.chosen {
			j.pick.chosen[i] = false
		}
		for _, want := range arg.Ticked(v) {
			at := -1
			for _, it := range j.pick.items {
				if it.value == want {
					at = it.idx
				}
			}
			if at < 0 {
				return m.aiFail(fmt.Sprintf("%q isn't one of %s's options: %s", want, arg.Name, optionList(arg))), true
			}
			j.pick.tick(at, true)
		}
	default:
		if !arg.HasOption(v) {
			return m.aiFail(fmt.Sprintf("%q isn't one of %s's options: %s. To give your own, press / and type it.", v, arg.Name, optionList(arg))), true
		}
		j.pick.clearFilter()
		j.pick.selectValue(v)
	}
	a.keys = append(a.keys, enter)
	return m.aiTickCmd(), true
}

// optionList is an arg's options' values, for saying what they are.
func optionList(a config.Arg) string {
	var out []string
	for _, o := range a.Options {
		out = append(out, o.Value)
	}
	return strings.Join(out, ", ")
}
