// llm-harness: watch an LLM use clingon the way a person would. It opens
// clingon in a sandbox (temp HOME, a scratch folder), shows the model the
// screen as text, and the model presses keys. Attach to watch:
//
//	tmux -L clingon-llm attach -r
//
// The status line under clingon says what the model is doing and why.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/con-dog/clingon/internal/llm"
	"github.com/con-dog/clingon/internal/palette"
)

const (
	server  = "clingon-llm"
	session = "llm"
)

const defaultTask = `You've just installed clingon and opened it for the first time. Use it like a curious new user would, only through its interface. The task has six phases; when each one ends, call rate_phase for it (with its name exactly as below) before going on.

1. First look: learn the keys (the status bar, and ? for the full list) and get a feel for the menu.
2. Add a pack: add the git pack from the menu, Clingon › Packs › Add packs.
3. Run commands: run two or three harmless commands from the menu, at least one with a blank to fill in. For example, list the files in this folder, and git status.
4. Recipe: build a recipe of two or three harmless, read-only commands, add it with the name "Morning check", then run it from Clingon › Recipes.
5. Create a pack: make a pack of your own with Clingon › Packs › Create a pack, and save it. Give it two or three harmless commands you'd find handy, at least one with a blank to fill in (echo, date, ls, pwd, cal, wc and the like). This is the part we most want your honest opinion on: was it clear how to write a command, a blank, a description? How did saving go?
6. Use your pack: find your pack in the menu and run each of its commands, filling in the blanks.

Then call finish with an overall summary, your UX notes, and an overall rating.`

func systemPrompt(allowRisky bool) string {
	risky := `- Never go through with a risky command. If you reach "This command is risky. Run it?" or a ☠ screen asking you to type a code, choose "No, go back" (or press Escape). The harness refuses to choose Yes for you anyway.`
	if allowRisky {
		risky = `- You may confirm risky (⚠) commands only when they act on clingon's own packs or recipes. Never type the code on a ☠ screen.`
	}
	return `You are testing clingon, a terminal app, by using it exactly as a person at the keyboard would. A person is watching your screen live, and reads a short line about each thing you do.

clingon fills one terminal window. On the left is a menu of commands, on the right a real zsh shell. Picking a command in the menu, filling in its blanks and running it types the command into the shell, which runs it. The bottom line of clingon's window is its status bar: it lists the keys that work right now, so read it on every screen. The top line is the recipe's bar: the recipe you're building, step by step.

How you see and act:
- Every tool returns the screen afterwards, as text.
- In the menu, the row the cursor is on starts with ▶ (it's live: keys act on it) or ▷ (greyed: focus is in the filter's box or in the shell). ◆ is a logo from clingon's own font.
- press sends keys, one after another: Enter, Escape, Up, Down, Left, Right, Tab, BTab (shift-tab), Space, BSpace, a control key such as ctrl+u, or any single character such as / ? 1 r x. Pressing several keys in one call is fine when you're sure where they lead; when you're not, press one and look.
- type sends text, for typing in a text box or a filter.
- wait lets a running command finish, then shows the screen.
- rate_phase rates a phase as it ends: what happened, how it felt, and notes. It isn't an action on the screen.
- finish ends the session with your summary, notes and overall rating.
- Every call takes "why": one short sentence, present tense, for the person watching (say "Opening the Packs section to add git", not "I will now press Enter").

Rules:
- Only use clingon's interface. Don't type commands into the shell by hand unless the task asks you to. (Creating a pack opens clingon's pack form in the right-hand pane: that's clingon's interface too.)
- This is a sandbox with a scratch home folder, but the commands run on a real Mac: pick only harmless, read-only commands.
- Never git push, by any route: not from a pack command, a recipe or the shell.
` + risky + `
- If something doesn't do what you expected, look at the screen, try to work it out like a person would (? shows the keys), and note it for your report.
- Commands you write into a pack of your own must be harmless too: no deleting, moving, killing, network or sudo. The harness refuses to type those.
- Ratings are out of 10. Objective: how well clingon let you do it, judged on what happened (done or not, wrong turns, actions spent, errors). Subjective: how it felt as a new user (clarity, confidence, pleasure). Be honest and specific: they're used to find what to improve, so a 10 should mean nothing could be better.
- Be efficient: don't wander through every section.`
}

type options struct {
	model, effort, task, clingonBin, repo string
	runsDir                               string
	width, height, maxSteps               int
	delay                                 time.Duration
	wait, allowRisky, showThinking, keep  bool
	screenOnly, peek, open, history       bool
}

func main() {
	var o options
	flag.StringVar(&o.model, "model", "anthropic/claude-opus-5.5", "any OpenRouter model, e.g. anthropic/claude-sonnet-5.5 or deepseek/deepseek-v4-pro")
	flag.StringVar(&o.effort, "effort", "medium", "effort: low, medium, high, xhigh, max")
	flag.StringVar(&o.task, "task", "", "what the model should do (default: the full first-run tour)")
	flag.StringVar(&o.clingonBin, "clingon", "", "clingon binary to test (default: build one from -repo into the sandbox)")
	flag.StringVar(&o.repo, "repo", "", "clingon checkout to build (default: the git root above this folder)")
	flag.IntVar(&o.width, "width", 150, "screen width: the same every run, whatever window watches")
	flag.IntVar(&o.height, "height", 42, "screen height (the status line is extra)")
	flag.IntVar(&o.maxSteps, "max-steps", 250, "stop after this many actions")
	flag.StringVar(&o.runsDir, "runs", "runs", "folder each run's ratings are saved in")
	flag.BoolVar(&o.history, "history", false, "print the ratings of every saved run, and stop")
	flag.DurationVar(&o.delay, "delay", 900*time.Millisecond, "pause after narrating, before each action, so a watcher can follow")
	flag.BoolVar(&o.wait, "wait", true, "wait for someone to attach and watch before starting")
	flag.BoolVar(&o.open, "open", true, "open a Terminal window to watch in, with clingon's profile")
	flag.BoolVar(&o.allowRisky, "allow-risky", false, "let the model confirm ⚠ commands")
	flag.BoolVar(&o.showThinking, "thinking", true, "print the model's thinking summaries")
	flag.BoolVar(&o.keep, "keep", true, "leave the session open when done, to look around")
	flag.BoolVar(&o.peek, "peek", false, "print the screen of the session that's open now, as the model sees it")
	flag.BoolVar(&o.screenOnly, "screen", false, "open the sandbox, print the screen the model would see, and stop (no API calls)")
	flag.Parse()
	if o.task == "" {
		o.task = defaultTask
	}

	if o.history {
		if err := history(o.runsDir); err != nil {
			fmt.Fprintln(os.Stderr, red("error: ")+err.Error())
			os.Exit(1)
		}
		return
	}
	if o.peek {
		t := &term{server: server, session: session, env: os.Environ()}
		raw, err := t.raw()
		if err != nil {
			fmt.Fprintln(os.Stderr, red("error: ")+err.Error())
			os.Exit(1)
		}
		fmt.Println(render(raw))
		return
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, red("error: ")+err.Error())
		os.Exit(1)
	}
}

func run(o options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	sb, err := newSandbox(o)
	if err != nil {
		return err
	}
	t := &term{server: server, session: session, env: sb.env}
	if err := t.start(sb.work, "CLINGON_WINDOW=harness exec clingon", o.width, o.height); err != nil {
		return err
	}
	if o.screenOnly {
		screen, err := t.settle(startQuiet, 10*time.Second)
		fmt.Println(screen)
		fmt.Println(dim("sandbox " + sb.base + " · watch: tmux -L " + server + " attach -r"))
		return err
	}
	if !o.keep {
		defer t.tmux("kill-server")
	}
	fmt.Println(bold("clingon LLM harness"))
	fmt.Println(dim("sandbox  ") + sb.base)
	tmuxBin, _ := exec.LookPath("tmux")
	attach := tmuxBin + " -L " + server + " attach -r"
	if o.open {
		if profile, err := openViewer(attach, o.width, o.height+1); err != nil {
			fmt.Println(yellow("couldn't open a window to watch in: ") + profile)
			o.open = false
		} else if profile == "" {
			fmt.Println(yellow("opened a window to watch in, but without clingon's Terminal profile (run clingon once to get it): pack logos show as boxes"))
		} else {
			fmt.Println(dim("watch    in the Terminal window just opened (profile \"" + profile + "\")"))
		}
	}
	if !o.open {
		fmt.Println(dim("watch    ") + bold(attach) + dim("   (from a Terminal window with the clingon profile, for the logos)"))
	}
	if o.wait {
		fmt.Println(dim("waiting for someone to attach…"))
		for t.viewers() == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
	screen, err := t.settle(startQuiet, 10*time.Second)
	if err != nil {
		return err
	}
	a := &agent{o: o, t: t, sb: sb}
	return a.loop(ctx, screen)
}

type sandbox struct {
	base, home, work string
	env              []string
}

// newSandbox makes a throwaway HOME and working folder, and a clean
// environment, so neither the model nor clingon touches the user's own
// setup: clingon's config, the shell's config and history all land here.
func newSandbox(o options) (*sandbox, error) {
	base, err := os.MkdirTemp("", "clingon-llm-")
	if err != nil {
		return nil, err
	}
	base, _ = filepath.EvalSymlinks(base)
	sb := &sandbox{base: base, home: filepath.Join(base, "home"), work: filepath.Join(base, "work")}
	bin := filepath.Join(base, "bin")
	for _, d := range []string{sb.home, sb.work, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	realHome, _ := os.UserHomeDir()
	if realHome != "" && (sb.home == realHome || strings.HasPrefix(realHome, sb.home+"/")) {
		return nil, fmt.Errorf("sandbox home %s would be your own home", sb.home)
	}
	sb.env = []string{
		"HOME=" + sb.home,
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"SHELL=/bin/zsh",
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
		"USER=" + os.Getenv("USER"),
		"LOGNAME=" + os.Getenv("USER"),
		"TMPDIR=" + os.Getenv("TMPDIR"),
	}
	if d := os.Getenv("TMUX_TMPDIR"); d != "" { // where `tmux -L` puts the socket you attach to
		sb.env = append(sb.env, "TMUX_TMPDIR="+d)
	}

	exe := filepath.Join(bin, "clingon")
	if o.clingonBin != "" {
		src, err := filepath.Abs(o.clingonBin)
		if err != nil {
			return nil, err
		}
		if err := os.Symlink(src, exe); err != nil {
			return nil, err
		}
	} else {
		repo := o.repo
		if repo == "" {
			out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
			if err != nil {
				return nil, errors.New("not in the clingon checkout: pass -repo or -clingon")
			}
			repo = strings.TrimSpace(string(out))
		}
		fmt.Println(dim("building clingon from " + repo + "…"))
		build := exec.Command("go", "build", "-o", exe, "./cmd/clingon")
		build.Dir = repo
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			return nil, fmt.Errorf("building clingon: %w", err)
		}
	}

	// Something for folder and git commands to look at.
	files := map[string]string{
		"README.md":      "# Demo project\n\nA scratch folder for the clingon LLM harness.\n",
		"notes.txt":      "remember to water the plants\n",
		"src/main.go":    "package main\n\nfunc main() { println(\"hello\") }\n",
		"src/helpers.go": "package main\n",
	}
	for name, body := range files {
		p := filepath.Join(sb.work, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return nil, err
		}
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "."},
		{"-c", "user.name=Demo", "-c", "user.email=demo@example.com", "commit", "-q", "-m", "First commit"},
	} {
		g := exec.Command("git", args...)
		g.Dir, g.Env = sb.work, sb.env
		if out, err := g.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git %s: %v: %s", args[0], err, out)
		}
	}
	_ = os.WriteFile(filepath.Join(sb.work, "notes.txt"), []byte("remember to water the plants\nand the cat\n"), 0o644) // an uncommitted change
	return sb, nil
}

type agent struct {
	o         options
	t         *term
	sb        *sandbox
	steps     int
	phaseFrom int // steps when the last phase was rated
	keys      int // keystrokes so far: each key pressed, each character typed
	keysFrom  int // keys when the last phase was rated
	log       *os.File
	run       runRecord
}

func (a *agent) loop(ctx context.Context, screen string) (err error) {
	key := llm.Key()
	if key == "" {
		return errors.New("no OpenRouter key: set OPENROUTER_API_KEY, or add one with `clingon ai key`")
	}
	client := &llm.Client{Key: key, Model: a.o.model, Effort: a.o.effort}
	a.log, _ = os.Create(filepath.Join(a.sb.base, "transcript.md"))
	defer a.log.Close()
	a.run = runRecord{Started: time.Now(), Model: a.o.model, Effort: a.o.effort, Task: a.o.task,
		Transcript: filepath.Join(a.sb.base, "transcript.md")}
	defer func() { // whatever happens: the phases rated so far are worth keeping, and why it stopped
		if err != nil {
			a.run.Error = err.Error()
		}
		a.report()
	}()
	fmt.Fprintf(a.log, "# clingon LLM harness\n\nTask:\n\n%s\n\n## Start\n\n```\n%s\n```\n", a.o.task, screen)
	fmt.Println(dim("model    ") + a.o.model + dim(" via OpenRouter"))

	msgs := []llm.Message{
		{Role: "system", Content: systemPrompt(a.o.allowRisky)},
		{Role: "user", Content: "Your task:\n\n" + a.o.task + "\n\nclingon has just opened. The screen:\n\n```\n" + screen + "\n```"},
	}
	nudges := 0
	for {
		if a.steps >= a.o.maxSteps {
			return errors.New("reached -max-steps")
		}
		trimScreens(msgs)
		reply, err := a.chat(ctx, client, msgs)
		if err != nil {
			return err
		}
		m := reply.Message
		msgs = append(msgs, m)
		if a.o.showThinking && strings.TrimSpace(m.Reasoning) != "" {
			fmt.Println(dim(indent(m.Reasoning)))
		}
		if strings.TrimSpace(m.Content) != "" {
			fmt.Println(m.Content)
		}
		finished := false
		for _, call := range m.ToolCalls {
			out, isErr, done := a.act(ctx, call.Function.Name, []byte(call.Function.Arguments))
			finished = finished || done
			if isErr {
				out = "Error: " + out
			}
			msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: call.ID, Content: out})
		}
		if finished {
			return nil
		}
		if len(m.ToolCalls) == 0 {
			if nudges++; nudges > 2 {
				return errors.New("the model stopped without calling finish")
			}
			msgs = append(msgs, llm.Message{Role: "user", Content: "Keep going with the task through the tools; call finish when you're done."})
		}
	}
}

// chat asks the model for its next turn and counts what it cost. A long run
// shouldn't be lost to a provider's bad minute: the client retries quickly,
// and this waits longer and tries twice more.
func (a *agent) chat(ctx context.Context, client *llm.Client, msgs []llm.Message) (llm.Reply, error) {
	for attempt := 0; ; attempt++ {
		reply, err := client.Chat(ctx, msgs, tools())
		if err == nil {
			u := reply.Usage
			a.run.Usage.Prompt += u.Prompt
			a.run.Usage.Cached += u.Details.Cached
			a.run.Usage.Completion += u.Completion
			a.run.Usage.Cost += u.Cost
			return reply, nil
		}
		if attempt == 2 || ctx.Err() != nil {
			return reply, err
		}
		fmt.Println(yellow("    model error, trying again in 30s: ") + err.Error())
		select {
		case <-ctx.Done():
			return reply, ctx.Err()
		case <-time.After(30 * time.Second):
		}
	}
}

// Every screen the model has seen stays in the chat, and a screen is a few
// thousand tokens: a long run would outgrow the model's context. So the
// older screens are left out, all but the last few. That's done in batches,
// not every turn, so the chat's start stays the same for long stretches and
// prompt caching keeps working.
const (
	keepScreens = 4  // the latest screens, always kept
	trimEvery   = 16 // screens let pile up before the older ones go
)

func trimScreens(msgs []llm.Message) {
	var full []int
	for i, m := range msgs {
		if m.Role == "tool" && strings.Contains(m.Content, "```") {
			full = append(full, i)
		}
	}
	if len(full) < keepScreens+trimEvery {
		return
	}
	for _, i := range full[:len(full)-keepScreens] {
		head, _, _ := strings.Cut(msgs[i].Content, "```")
		msgs[i].Content = strings.TrimSpace(head) + " (left out to save space: the later screens show what's there now)"
	}
}

func tools() []llm.Tool {
	why := map[string]any{"type": "string", "description": "One short sentence for the person watching: what you're doing and why."}
	tool := func(name, desc string, props map[string]any, required ...string) llm.Tool {
		props["why"] = why
		return llm.Tool{Name: name, Description: desc, Params: props, Required: append([]string{"why"}, required...)}
	}
	score := func(what string) map[string]any {
		return map[string]any{"type": "integer", "minimum": 1, "maximum": 10, "description": what}
	}
	str := func(what string) map[string]any { return map[string]any{"type": "string", "description": what} }
	notes := func(what string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": what}
	}
	return []llm.Tool{
		tool("press", "Press keys, in order, then see the screen.", map[string]any{
			"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "Key names: Enter, Escape, Up, Down, Left, Right, Tab, BTab, Space, BSpace, Home, End, PageUp, PageDown, a control key like ctrl+u, or one character."},
		}, "keys"),
		tool("type", "Type text, as into a text box or filter, then see the screen. It doesn't press Enter.", map[string]any{
			"text": map[string]any{"type": "string"},
		}, "text"),
		tool("wait", "Wait for something to finish, then see the screen.", map[string]any{
			"seconds": map[string]any{"type": "number", "description": "1 to 30"},
		}, "seconds"),
		tool("rate_phase", "Rate a phase of the task as it ends. Not an action on the screen.", map[string]any{
			"phase":             str("The phase's name, exactly as the task gives it."),
			"completed":         map[string]any{"type": "boolean", "description": "Whether you did what the phase asked."},
			"ux_notes":          notes("What, in this phase, confused you, slowed you down, seemed inconsistent or worked well: each one concrete."),
			"objective_score":   score("Objective rating out of 10 for this phase: how well clingon let you do it, judged on what happened (done or not, wrong turns, actions spent, errors), not on taste."),
			"objective_reason":  str("One or two sentences of evidence for the objective score."),
			"subjective_score":  score("Subjective rating out of 10 for this phase: how it felt (clarity, confidence, pleasure), as a new user."),
			"subjective_reason": str("One or two sentences on why it felt that way."),
		}, "phase", "completed", "ux_notes", "objective_score", "objective_reason", "subjective_score", "subjective_reason"),
		tool("finish", "End the session with the overall rating.", map[string]any{
			"summary":           str("What you did, and whether each part of the task worked."),
			"ux_notes":          notes("Anything in the interface that confused you, slowed you down or seemed inconsistent, each as one concrete note."),
			"objective_score":   score("Overall objective rating out of 10 for the whole run: how well clingon let you do the task, judged on what happened (parts done, wrong turns, actions spent, errors), not on taste."),
			"objective_reason":  str("One or two sentences of evidence for the objective score."),
			"subjective_score":  score("Overall subjective rating out of 10 for the whole run: how clingon felt to use (clarity, confidence, pleasure), as a new user."),
			"subjective_reason": str("One or two sentences on why it felt that way."),
		}, "summary", "ux_notes", "objective_score", "objective_reason", "subjective_score", "subjective_reason"),
	}
}

type actInput struct {
	Why     string   `json:"why"`
	Keys    []string `json:"keys"`
	Text    string   `json:"text"`
	Seconds float64  `json:"seconds"`
	Summary string   `json:"summary"`
	Notes   []string `json:"ux_notes"`

	Phase     string `json:"phase"`
	Completed bool   `json:"completed"`

	ObjectiveScore   int    `json:"objective_score"`
	ObjectiveReason  string `json:"objective_reason"`
	SubjectiveScore  int    `json:"subjective_score"`
	SubjectiveReason string `json:"subjective_reason"`
}

var placeholder = regexp.MustCompile(`<[A-Za-z_][A-Za-z0-9_-]*>`)

func unsafe(text string) bool { return unsafeText.MatchString(placeholder.ReplaceAllString(text, "")) }

var pushRow = regexp.MustCompile(`(?i)\bpush\b`)

var keyName = regexp.MustCompile(`^(Enter|Escape|Up|Down|Left|Right|Tab|BTab|Space|BSpace|Home|End|PageUp|PageDown)$`)

// ctrlKey is a control key, as the model may write it ("ctrl+u", "C-u",
// "Ctrl-U"); it's sent as tmux names it. Not c, d or z: in the shell they
// interrupt, log out or suspend.
var ctrlKey = regexp.MustCompile(`(?i)^(?:ctrl[+-]|c-)([abefghijklnopqrstuvwxy])$`)

// tmuxKey is the key as tmux names it, or "" if it isn't one the harness sends.
func tmuxKey(k string) string {
	if keyName.MatchString(k) || len([]rune(k)) == 1 {
		return k
	}
	if m := ctrlKey.FindStringSubmatch(k); m != nil {
		return "C-" + strings.ToLower(m[1])
	}
	return ""
}

// Text the model may never type, even in a sandbox: these reach past it.
// Placeholders like <message> are left out first: only a real > redirects.
var unsafeText = regexp.MustCompile(`(?i)\b(rm|sudo|kill|killall|pkill|mkfs|dd|shutdown|reboot|chmod|chown|mv|launchctl|osascript|defaults|diskutil|curl|wget|ssh|scp)\b|git\s+push|>`)

// act runs one tool call and returns what the model sees next.
func (a *agent) act(ctx context.Context, name string, raw []byte) (out string, isErr, done bool) {
	var in actInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "invalid input: " + err.Error(), true, false
	}
	if name == "rate_phase" { // not an action: no step, no pause
		a.ratePhase(in)
		return "Rated.", false, false
	}
	a.steps++
	label := ""
	switch name {
	case "press":
		label = strings.Join(in.Keys, " ")
	case "type":
		label = fmt.Sprintf("%q", in.Text)
	case "wait":
		label = fmt.Sprintf("%gs", in.Seconds)
	}
	fmt.Printf("%s %s %s  %s\n", dim(fmt.Sprintf("%3d", a.steps)), bold(in.Why), dim("·"), dim(name+" "+label))
	fmt.Fprintf(a.log, "\n## %d. %s\n\n`%s %s`\n", a.steps, in.Why, name, label)
	if name == "finish" {
		a.t.narrate(fmt.Sprintf("done · overall objective %d/10 · subjective %d/10", in.ObjectiveScore, in.SubjectiveScore), firstLine(in.Summary))
		a.steps-- // finishing isn't an action on the screen
		overall := in.rating()
		a.run.Overall, a.run.Summary, a.run.Notes, a.run.Finished = &overall, in.Summary, in.Notes, true
		if a.o.keep {
			fmt.Println(dim("the session is still open; end it with ") + "tmux -L " + server + " kill-server")
		}
		return "Session ended.", false, true
	}

	a.t.narrate(in.Why, name+" "+label)
	select {
	case <-ctx.Done():
		return "interrupted", true, true
	case <-time.After(a.o.delay):
	}

	refused := ""
	switch name {
	case "press":
		for _, k := range in.Keys {
			if tk := tmuxKey(k); tk == "" {
				refused = fmt.Sprintf("unknown key %q: nothing after it was pressed", k)
				break
			} else {
				k = tk
			}
			if why := a.guardKey(k); why != "" {
				refused = why
				break
			}
			if err := a.t.sendKey(k); err != nil {
				return err.Error(), true, false
			}
			a.keys++
			// Let the key land before the next, so the next key's guard
			// sees the screen it will act on.
			if _, err := a.t.settle(keyQuiet, 2*time.Second); err != nil {
				return err.Error(), true, false
			}
		}
	case "type":
		if unsafe(in.Text) {
			refused = "the harness doesn't type that: it could reach outside the sandbox"
		} else if why := a.guardKey("text"); why != "" {
			refused = why
		} else if err := a.t.sendText(in.Text); err != nil {
			return err.Error(), true, false
		} else {
			a.keys += len([]rune(in.Text))
		}
	case "wait":
		s := min(max(in.Seconds, 1), 30)
		time.Sleep(time.Duration(s * float64(time.Second)))
	default:
		return "unknown tool " + name, true, false
	}

	screen, err := a.t.settle(actionQuiet, 6*time.Second)
	if err != nil {
		return err.Error(), true, false
	}
	fmt.Fprintf(a.log, "\n```\n%s\n```\n", screen)
	var b strings.Builder
	if refused != "" {
		fmt.Println(yellow("    refused: " + refused))
		fmt.Fprintf(a.log, "\nRefused: %s\n", refused)
		b.WriteString("Refused by the harness: " + refused + "\n\n")
	}
	if a.t.dead() {
		b.WriteString("clingon has exited.\n\n")
	}
	fmt.Fprintf(&b, "Screen (action %d of at most %d):\n\n```\n%s\n```", a.steps, a.o.maxSteps, screen)
	return b.String(), refused != "", false
}

// guardKey stops the keys that would go through with a risky command.
func (a *agent) guardKey(k string) string {
	raw, err := a.t.raw()
	if err != nil {
		return ""
	}
	s := render(raw)
	if strings.Contains(s, "It can't be undone. Type") && (k == "text" || len([]rune(k)) == 1) {
		return "the harness never types the code for a ☠ command"
	}
	if !a.o.allowRisky && strings.Contains(s, "› Yes, run it") && (k == "Enter" || k == "Right") {
		return `the harness doesn't run risky commands: choose "No, go back"`
	}
	// Never a push: not from the menu (the cursor on a Push command) nor
	// from anything showing the command it's about to run.
	if k == "Enter" || k == "Right" {
		for _, l := range strings.Split(s, "\n") {
			if strings.Contains(l, "▶") && pushRow.MatchString(l) {
				return "the harness never pushes"
			}
		}
		if strings.Contains(s, "git push") {
			return "the harness never pushes"
		}
	}
	return ""
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n    ")
}

func ansi(code string) func(string) string {
	return func(s string) string { return "\x1b[" + code + "m" + s + "\x1b[0m" }
}

var (
	bold   = ansi("1")
	dim    = ansi("2")
	red    = ansi(palette.Red.SGR())
	yellow = ansi(palette.Orange.SGR())
	green  = ansi(palette.Green.SGR())
)

// How long the screen must go without changing to count as settled: when
// clingon starts, after an action, and between the keys of one press.
const (
	startQuiet  = time.Second
	actionQuiet = 400 * time.Millisecond
	keyQuiet    = 150 * time.Millisecond
)
