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
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/micro"
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
  clingon command  add, remove and list commands in your own packs
  clingon edit     open your pack in micro
  clingon check    check your packs for mistakes
  clingon doctor   check tmux, your packs and each pack's tools
  clingon crash    show why clingon last crashed, if it has
  clingon update   upgrade clingon the way it was installed
  clingon welcome  show the banner and anything worth doing
  clingon completion zsh|bash        tab completion (Homebrew sets it up for you)
  clingon version

Packs: %[1]s/packs/remote (installed) and %[1]s/packs/local (your own).
Override the folder with $CLINGON_CONFIG.
`

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
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
	case "init":
		err = config.NewPack(defaultPack())
		if err == nil {
			say(ok, "created %s", config.PackPath(defaultPack()))
		}
	case "edit":
		err = editPack(defaultPack())
	case "check":
		err = check()
	case "welcome":
		welcome(len(os.Args) > 2 && os.Args[2] == "--then-shell")
	case "_check-release":
		checkRelease()
	case "_shell": // the shell inside clingon, started again (System › Reload shell)
		becomeShell()
	case "doctor":
		err = doctor(os.Args[2:])
	case "crash":
		err = crashCmd()
	case "update", "upgrade":
		err = update()
	case "completion":
		err = completion(os.Args[2:])
	case "version", "--version", "-v":
		outln(nameStyle.Render("clingon") + " " + greyStyle.Render(getVersion()))
	case "help", "--help", "-h":
		outf("%s", styleUsage(fmt.Sprintf(usage, config.Dir())))
	default:
		errf("%s", styleUsage(fmt.Sprintf(usage, config.Dir())))
		os.Exit(2)
	}
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
// v0.0.0-20261030044109-241d3f66c82c+dirty: shown as "dev · 241d3f6".
var pseudoVersion = regexp.MustCompile(`^v0\.0\.0-\d{14}-[0-9a-f]{12}(\+dirty)?$`)

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
			return fmt.Errorf("%w\n\nif these are packs you installed, `clingon pack update` should fix them; if they're your own, fix them and run `clingon check`", err)
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
	err = tmux.Launch(self, env, filepath.Join(config.Dir(), "run"), ready)
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
	go c.WaitForPrompt(*pane, 15*time.Second)
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	m := ui.New(ui.Options{
		Config:  cfg,
		Builtin: builtinSections,
		Reload:  reloader(),
		Send:    func(cmd string) error { return c.Send(*pane, cmd) },
		Dir:     func() string { return c.Path(*pane) },
		// After a command from the menu finishes, focus comes back here,
		// unless the user has moved elsewhere themselves.
		WaitIdle:  func() bool { return c.WaitIdle(*pane) },
		FocusMenu: func() { c.FocusIfActive(*pane, os.Getenv("TMUX_PANE")) },
		Shell: func() ui.ShellState {
			st := c.State(*pane)
			return ui.ShellState{Busy: st.Busy, Scrolling: st.Scrolling, Typed: st.Typed, Starting: st.Starting}
		},
		ClearShell: func() error { return c.Clear(*pane) },
		FocusShell: func() { c.Focus(*pane) },
		Jobs:       func() []string { return c.Jobs(*pane) },
		Bar:        c.Bar,
		BarWidth:   c.BarWidth,
		Headline:   headline,
		Project:    config.FindProject,
		SecretDir:  secretDir(),
		Version:    getVersion(),
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
			step("macOS may ask once to let clingon control your terminal app: it's only to open and style this window")
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
	dir := filepath.Join(config.Dir(), "run")
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

// editPack opens a pack in micro (your local one, unless ref says
// remote/NAME), then says whether it still loads.
func editPack(ref string) error {
	where, name := config.SplitRef(ref)
	if _, err := exec.LookPath("micro"); err != nil {
		return errors.New("clingon edits packs with micro, a simple terminal editor (Ctrl-S saves, Ctrl-Q quits).\n" +
			"Install it with `brew install micro`")
	}
	path := config.PackPath(name)
	switch {
	case where == "" && !config.IsLocal(name) && config.IsInstalled(name):
		return fmt.Errorf("%s is an installed pack: `clingon pack edit remote/%s` edits it (but `clingon pack update` overwrites it), or start your own with `clingon pack create my-%s`", name, name, name)
	case where == "remote":
		if !config.IsInstalled(name) {
			return fmt.Errorf("%s isn't installed", name)
		}
		path = config.RemotePath(name)
		say(warn, "`clingon pack update` overwrites installed packs")
	case !config.IsLocal(name):
		if err := config.NewPack(name); err != nil {
			return err
		}
	}
	dir, err := micro.ConfigDir(config.Dir())
	if err != nil {
		return err
	}
	cmd := exec.Command("micro", "-config-dir", dir, path, "+/^sections:")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if _, _, err := config.Load(); err != nil {
		return fmt.Errorf("saved, but it has problems:\n%w", err)
	}
	say(ok, "saved %s", path)
	return nil
}

// defaultPack is the pack `clingon edit` and `init` mean: your only one,
// or "mine".
func defaultPack() string {
	if yours, _ := config.YourPacks(); len(yours) == 1 {
		return yours[0].Name
	}
	return "mine"
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
	rows := []row{{mark: ok, label: "packs", detail: fmt.Sprintf("%d sections, %d commands from %d installed packs and %d of your own", len(cfg.Sections), n, len(pkgs), len(files))}}
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
