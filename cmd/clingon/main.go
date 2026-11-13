// clingon: a menu of the commands you can never remember, next to a real
// shell that runs them.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/shell"
	"github.com/con-dog/clingon/internal/tmux"
	"github.com/con-dog/clingon/internal/ui"
	"github.com/con-dog/clingon/internal/window"
)

var version = "dev" // set by goreleaser

const usage = `clingon — a menu of the commands you can never remember, next to a real shell.

Usage:
  clingon          open clingon's window: the menu (left) and your shell (right)
  clingon print    pick a command and print it instead, e.g. eval "$(clingon print)"
  clingon pack     list, add and remove packs; make and edit your own
  clingon command  add and list commands in your own packs
  clingon recipe   add, remove and list recipes (sequences of commands)
  clingon check    check your packs for mistakes
  clingon ai       set up the AI: ai key (your OpenRouter key), ai model <model>, ai log (its last run)
  clingon doctor   check tmux, your packs and each pack's tools
  clingon crash    show why clingon last crashed, if it has
  clingon update   upgrade clingon the way it was installed
  clingon welcome  show the banner and anything worth doing
  clingon completion zsh|bash        tab completion (Homebrew sets it up for you)
  clingon version

Packs: %[1]s/packs/remote (added) and %[1]s/packs/mine (your own).
Override the folder with $CLINGON_CONFIG.
`

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	if len(os.Args) > 1 {
		saidTitle = commandTitle(os.Args[1:])
	}
	if cmd == "" || cmd == "menu" || cmd == "print" || cmd == "welcome" {
		// Reading the built-in packs takes a few ms: do it while the
		// installed ones load.
		go func() { _, _ = config.Packs() }()
	}
	if cmd != "version" && cmd != "help" && !strings.HasPrefix(cmd, "-") {
		if err := config.EnsureDefaults(); err != nil {
			sayErr(err)
		}
	}
	switch cmd {
	case "": // the launcher lives as long as the session, so this finishes
		go func() { _ = config.WriteAvailable() }()
	case "pack", "packs":
		_ = config.WriteAvailable()
	}
	switch cmd {
	case "":
		err = launch()
	case "menu":
		err = menu(os.Args[2:])
	case "print":
		err = printMode()
	case "pack", "packs":
		err = packCmd(os.Args[2:])
	case "command", "commands", "cmd":
		err = commandCmd(os.Args[2:])
	case "recipe", "recipes":
		err = recipeCmd(os.Args[2:])
	case "init":
		err = config.NewPack(defaultPack())
		if err == nil {
			say(ok, "created %s", config.PackPath(defaultPack()))
		}
	case "check":
		err = check()
	case "heard": // tmux's, not yours: marks each piece of the shell's output (see shellStill)
		if len(os.Args) > 2 {
			heard(os.Args[2])
		}
		return
	case "welcome":
		// --then-shell is how clingon's shell pane starts; --again is that
		// pane's shell asking for its card once more, the pane's width having
		// changed before anything was run in it (internal/shell's rc files).
		then := len(os.Args) > 2 && os.Args[2] == "--then-shell"
		welcome(then || len(os.Args) > 2 && os.Args[2] == "--again", then)
	case "_check-release":
		checkRelease()
	case "_shell": // the shell inside clingon, started again (System › Reload shell)
		becomeShell()
	case "doctor":
		err = doctor(os.Args[2:])
	case "ai":
		err = aiCmd(os.Args[2:])
	case "crash":
		err = crashCmd()
	case "update", "upgrade":
		err = update()
	case "completion":
		err = completion(os.Args[2:])
	case "version", "--version", "-v":
		sayBoxed("Clingon", getVersion())
	case "help", "--help", "-h":
		sayBoxed("Clingon", styleUsage(fmt.Sprintf(usage, config.Dir())))
	default:
		errUsage(styleUsage(fmt.Sprintf(usage, config.Dir())))
		os.Exit(2)
	}
	flushSaid()
	if err == errQuiet {
		os.Exit(1)
	}
	if err != nil {
		sayErr(err)
		os.Exit(1)
	}
}

// getVersion is the release version, the module version for `go install`,
// or for a local build "dev" plus the git commit it was built from.
func getVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" && !pseudoVersion.MatchString(v) {
		return v // a tagged release, installed with go install
	}
	v, dirty := "dev", false
	for _, s := range bi.Settings {
		switch {
		case s.Key == "vcs.revision" && len(s.Value) >= 7:
			v += " · " + s.Value[:7]
		case s.Key == "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if dirty {
		v += " · modified"
	}
	return v
}

// pseudoVersion is what Go stamps on a build of an untagged commit, e.g.
// v0.0.0-20261030044109-241d3f66c82c+dirty, or once the repo has a tag,
// v0.5.4-0.20261110055126-60cea1199d5e+dirty: shown as "dev · 241d3f6".
var pseudoVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+-(?:[0-9A-Za-z.-]*\.)?\d{14}-[0-9a-f]{12}(\+dirty)?$`)

// errInside stops a second menu opening inside clingon's own shell.
var errInside = errors.New("already inside clingon (shift-tab switches to the menu)")

func inside() bool { return os.Getenv("CLINGON") != "" }

func launch() error {
	if inside() {
		return errInside
	}
	if err := shell.Check(os.Getenv("SHELL")); err != nil {
		return err
	}
	// clingon always runs in a window of its own, in its own font (the
	// packs' logos are in it): from anywhere else, it opens that window.
	own := os.Getenv(window.OwnEnv)
	if own == "" {
		return openWindow()
	}
	_ = os.Unsetenv(window.OwnEnv) // not for the panes
	collectCrashes()               // one that took its process with it: the welcome says so
	var closeWindow func()
	if own == "terminal" {
		closeWindow = window.TerminalCloser()
	}
	// Check the packs while tmux starts, but before attaching: problems are
	// shown here, in the user's terminal, rather than inside the pane.
	checked := make(chan error, 1)
	go func() {
		_, _, err := config.Load()
		checked <- err
	}()
	ready := func() error {
		if err := <-checked; err != nil {
			return fmt.Errorf("%w\n\nif these are packs you added, `clingon pack update` should fix them; if they're your own, fix them and run `clingon check`", err)
		}
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	// A local build that isn't the `clingon` on PATH goes first on the
	// session's PATH, so `clingon` in either pane means this build. CLINGON_BIN
	// puts it back there after the user's own setup (brew shellenv, say).
	var env []string
	if !isSelf("clingon") {
		env = append(env, "PATH="+filepath.Dir(self)+string(os.PathListSeparator)+os.Getenv("PATH"), "CLINGON_BIN="+filepath.Dir(self))
	}
	env = append(env, "CLINGON_CHANGED="+config.ChangedFile()) // where tmux and the shell say what's changed, for the menu
	_ = os.MkdirAll(config.Dir(), 0o755)
	_ = os.WriteFile(config.ReadyFile(), nil, 0o600)       // no prompts yet, this session
	env = append(env, "CLINGON_READY="+config.ReadyFile()) // where the shell says its prompt is up
	flushSaid()                                            // tmux takes over the terminal
	err = tmux.Launch(self, env, filepath.Join(config.StateDir(), "run"), ready)
	if err != nil {
		// Only now, so starting up never waits on it: an old tmux explains
		// most failures to build the session.
		if out, verr := exec.Command("tmux", "-V").Output(); verr == nil && tmuxTooOld(strings.TrimSpace(string(out))) {
			return fmt.Errorf("%s is too old: clingon needs tmux 3.2 or newer (`brew upgrade tmux`)\n(%w)", strings.TrimSpace(string(out)), err)
		}
	}
	if err == nil && closeWindow != nil { // after an error, the window stays so it can be read
		closeWindow()
	}
	return err
}

// stillFor is how long the shell's screen has to go without changing to be
// taken as still: the one thing here that's a length of time, because nothing
// in a terminal says "I'm waiting for you". A screen that's stopped changing
// is what a person goes by too. It's counted from the shell's last output,
// heard as it's written: a program still printing is never cut in on.
const stillFor = time.Second

// shellStill is ui.Options.ShellStill for the shell in pane: it sleeps until
// that shell's screen has stopped changing. tmux pipes the pane's output to
// `clingon heard`, which marks each piece in a file; the menu watches the
// file (a kqueue event per mark, no polling). Nothing of the output is kept.
// It starts listening the first time it's asked.
func shellStill(c *tmux.Client, pane string) func(afterNext bool, over <-chan struct{}) bool {
	var (
		once   sync.Once
		output chan struct{} // a piece of output, since last looked
	)
	listen := func() {
		self, err := os.Executable()
		if err != nil {
			return
		}
		file := filepath.Join(config.Dir(), "heard")
		if err := os.MkdirAll(config.Dir(), 0o755); err != nil {
			return
		}
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			return
		}
		w, err := config.WatchPath(file)
		if err != nil {
			return
		}
		if err := c.Hear(pane, self, file); err != nil {
			w.Close()
			return
		}
		ch := make(chan struct{}, 1)
		go func() {
			for w.Wait() {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}()
		output = ch
	}
	return func(afterNext bool, over <-chan struct{}) bool {
		once.Do(listen)
		if afterNext {
			select { // what's already been heard isn't what's waited for
			case <-output:
			default:
			}
			select {
			case <-output:
			case <-over:
				return false
			}
		}
		for {
			select {
			case <-output: // still changing: start counting again
			case <-over:
				return false
			case <-time.After(stillFor):
				return true
			}
		}
	}
}

// heard reads the shell's output from tmux and adds a mark to file for each
// piece, so the menu, watching the file, knows the screen's changing. It
// keeps none of what it reads.
func heard(file string) {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 64<<10)
	for {
		if _, err := os.Stdin.Read(buf); err != nil {
			return
		}
		if st, err := f.Stat(); err == nil && st.Size() > 1<<16 {
			_ = f.Truncate(0)
		}
		_, _ = f.Write([]byte{'.'})
	}
}

func menu(args []string) error {
	fs := flag.NewFlagSet("menu", flag.ContinueOnError)
	socket := fs.String("socket", "", "tmux socket name")
	pane := fs.String("pane", "", "shell pane id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := tmux.New(*socket)
	if err != nil {
		return err
	}
	defer c.Kill()
	c.ReadyFile = config.ReadyFile()
	go c.WaitForPrompt(*pane, 15*time.Second)
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	// The menu hears tmux and the shell say what's changed (focus, scrolling,
	// the prompt drawn again) as they say it: no looking on a timer.
	var changed func() bool
	if w, err := config.WatchChanged(); err == nil {
		defer w.Close()
		changed = w.Wait
	}
	m := ui.New(ui.Options{
		Config:  cfg,
		Builtin: builtinSections,
		Reload:  reloader(),
		Send:    func(cmd string) error { return c.Send(*pane, cmd) },
		Dir:     func() string { return c.Path(*pane) },
		// After a command from the menu finishes, focus comes back here,
		// unless the user has moved elsewhere themselves.
		WaitIdle:   func() bool { return c.WaitIdle(*pane) },
		LastFailed: func() (bool, bool) { return c.LastFailed(*pane) },
		Recipes: func() []config.Recipe {
			rs, _ := config.Recipes() // a broken file: `clingon recipe list` says what's wrong with it
			return rs
		},
		Self:      selfCommand(),
		FocusMenu: func() { c.FocusIfActive(*pane, os.Getenv("TMUX_PANE")) },
		Shell: func() ui.ShellState {
			st := c.State(*pane)
			return ui.ShellState{Busy: st.Busy, Scrolling: st.Scrolling, Typed: st.Typed, Starting: st.Starting}
		},
		ClearShell:  func() error { return c.Clear(*pane) },
		FocusShell:  func() { c.Focus(*pane) },
		Jobs:        func() []string { return c.Jobs(*pane) },
		Panel:       c.NewPanel(*pane, os.Getenv("TMUX_PANE")),
		ShellKey:    func(key string, text bool) error { return c.SendKey(*pane, key, text) },
		ShellScreen: func() string { return c.Screen(*pane) },
		FocusSelf:   func() { c.Focus(os.Getenv("TMUX_PANE")) },
		Focused:     func() string { return c.Where(os.Getenv("TMUX_PANE"), *pane) },
		Changed:     changed,
		ShellTail:   func() string { return c.Tail(*pane, 500) }, // all of its screen: what you see, the AI sees
		ShellStill:  shellStill(c, *pane),
		Bar:         c.Bar,
		BarWidth:    c.BarWidth,
		TopBar:      c.TopBar,
		AIDriving: func(where string) { // focus and the gold frame go to the pane it's working in
			menu := os.Getenv("TMUX_PANE")
			switch where {
			case "menu":
				c.AIDriving(menu, menu)
			case "shell":
				c.AIDriving(*pane, menu)
			default:
				c.AIDriving("", menu)
			}
		},
		Headline:  headline,
		Project:   config.FindProject,
		SecretDir: secretDir(),
		Version:   getVersion(),
	})
	stop := catchCrashes()
	_, err = tea.NewProgram(m).Run()
	stop(err)
	for _, f := range m.Secrets {
		_ = os.Remove(f) // normally gone already: the command deletes it as it reads it
	}
	return err
}

// openWindow opens clingon in a new window of the user's terminal app, in
// clingon's own font (installed for the user the first time) and colours.
func openWindow() error {
	if os.Getenv("SSH_CONNECTION") != "" {
		return errors.New("clingon opens in a window of its own, and there's none to open over SSH: `clingon print` works here")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dir, _ := os.Getwd()
	term := window.Terminal(os.Getenv)
	plan, err := window.PlanFor(term, self, dir, os.Getenv("SHELL"), home)
	if err != nil {
		return err
	}
	// Only once there's a window to use it in.
	installed, err := window.InstallFont(home)
	if err != nil {
		return fmt.Errorf("couldn't install clingon's font: %w", err)
	}
	if installed {
		say(ok, "installed clingon's font, Clingon Mono, for your user")
		if plan.Path == "osascript" {
			step("MacOs may ask once to let clingon control your terminal app: it's only to open and style this window")
		}
	}
	if err := window.Run(plan); err != nil {
		return err
	}
	say(ok, "opened clingon in a new %s window", map[string]string{
		"terminal": "Terminal", "iterm": "iTerm2", "ghostty": "Ghostty", "kitty": "kitty", "alacritty": "Alacritty",
	}[term])
	return nil
}

// secretDir is where secret values wait for the command that reads them.
// Ones still there after ten minutes weren't run (a printed command never
// eval'd, say): they're deleted.
func secretDir() string {
	dir := filepath.Join(config.StateDir(), "run")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && strings.HasPrefix(e.Name(), "secret-") && time.Since(fi.ModTime()) > 10*time.Minute {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return dir
}

func printMode() error {
	if inside() {
		return errInside
	}
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	m := ui.New(ui.Options{
		Config:    cfg,
		Dir:       func() string { d, _ := os.Getwd(); return d },
		Headline:  headline,
		Project:   config.FindProject,
		SecretDir: secretDir(),
		Version:   getVersion(),
	})
	if _, err := tea.NewProgram(m, tea.WithOutput(os.Stderr)).Run(); err != nil {
		return err
	}
	if m.Result == "" {
		os.Exit(1)
	}
	fmt.Println(m.Result)
	return nil
}

// formEdit opens your pack in clingon's pack form, then says it's done. Only
// your own: one you've added is clingon's, to read with `pack describe`.
func formEdit(ref, done string, menu bool) error {
	where, name := config.SplitRef(ref)
	switch {
	case where == "remote" || where == "" && !config.IsLocal(name) && config.IsInstalled(name):
		return fmt.Errorf("%s is a curated pack, not yours to edit: `clingon pack describe %s` shows it, or start your own with `clingon pack create my-%s`", name, name, name)
	case !config.IsLocal(name):
		return fmt.Errorf("you don't have a pack called %s: `clingon pack create %s` starts one", name, name)
	}
	form, err := ui.NewPackForm(name, menu)
	if err != nil {
		return err
	}
	return runForm(form, done)
}

// newPackForm opens the pack form on a new pack.
func newPackForm(menu bool) error {
	form, err := ui.NewPackFormForNew(menu)
	if err != nil {
		return err
	}
	return runForm(form, "created")
}

// runForm runs the pack form, then says what came of it: written (as it was
// changed), where it is and whether it loads; or not added yet, and why.
func runForm(form *ui.PackForm, done string) error {
	flushSaid()   // the form takes over the terminal
	if inside() { // tab and shift-tab move on from it, as from any panel
		form.MenuHeight = tmux.MenuHeight // its foot lines up with the menu's
		form.AIWorking = tmux.AIWorking   // and goes while the AI drives it: it's for people
		form.Focus = func(next bool) bool {
			if next {
				return tmux.FocusPanel()
			}
			tmux.FocusMenu()
			return true
		}
	}
	_, err := tea.NewProgram(form).Run()
	config.RemoveFormName() // closed: the menu stops saying it's open
	if err != nil {
		return err
	}
	p := form.Pack()
	switch form.Left {
	case "draft": // kept as it was left, to carry on from
		if why := p.CheckFilled(); why != nil {
			say(info, "not added yet: %s · open the form again to carry on", why)
		}
		return nil
	case "saved": // written as it was changed
	default:
		return nil
	}
	path := config.PackPath(p.Folder)
	if _, _, err := config.Load(); err != nil {
		return fmt.Errorf("%s %s, but it has problems:\n%w", done, path, err)
	}
	say(ok, "%s %s · in the menu under %s › %s", done, path, config.SectionName(p.Section), p.Title)
	return nil
}

// defaultPack is the pack `clingon init` means: your only one,
// or "my-commands".
func defaultPack() string {
	if yours, _ := config.YourPacks(); len(yours) == 1 {
		return yours[0].Name
	}
	return "my-commands"
}

func check() error {
	cfg, files, err := config.Load()
	if err != nil {
		return err
	}
	n := 0
	for i := range cfg.Sections {
		n += len(cfg.Sections[i].All())
	}
	pkgs, err := config.Installed()
	if err != nil {
		return err
	}
	rows := []row{{mark: ok, label: "packs", detail: fmt.Sprintf("%d sections, %d commands from %d added packs and %d of your own", len(cfg.Sections), n, len(pkgs), len(files))}}
	for _, f := range config.LoosePackFiles() {
		rows = append(rows, row{mark: warn, label: "not read", detail: f + ": a pack is a folder, so this belongs in one"})
	}
	for _, w := range cfg.UnknownLogos() {
		rows = append(rows, row{mark: warn, label: "logo", detail: w + ": it shows its icon instead"})
	}
	dir, _ := os.Getwd()
	p := config.FindProject(dir)
	if p != nil && p.Err == nil {
		trust := "the menu asks before using it"
		if p.Trusted {
			trust = "trusted"
		}
		rows = append(rows, row{mark: ok, label: "project pack", detail: p.Path, note: fmt.Sprintf("%d commands · %s", len(p.Cmds), trust)})
	}
	outln(card("Check", rows, cardWidth()))
	if p != nil && p.Err != nil {
		return fmt.Errorf("this folder's project pack has problems:\n%w", p.Err)
	}
	return nil
}

// reloader returns a function that reloads the packs, or returns nil,
// nil when nothing in the config folder has changed since the last call.
func reloader() func() (*config.Config, error) {
	seen := config.Fingerprint()
	return func() (*config.Config, error) {
		now := config.Fingerprint()
		if now == seen {
			return nil, nil
		}
		seen = now
		cfg, _, err := config.Load()
		return cfg, err
	}
}
