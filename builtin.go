package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/shell"
	"github.com/con-dog/clingon/internal/ui"
)

// clingon's own sections share its mark (the C of its banner) in its pink.
var clingonMark, clingonPink = config.Logos["clingon"], ui.PinkHex

// packOptions offers packs to pick from, qualified (remote/x, local/x) for
// packs you have.
func packOptions(ps []config.Pack) []config.Option {
	out := make([]config.Option, len(ps))
	for i, p := range ps {
		out[i] = config.Option{Value: p.Name, Label: p.Description}
		if p.Where != "" {
			out[i].Value = p.Where + "/" + p.Name
		}
	}
	return out
}

// builtinSections sit under the Clingon heading at the foot of the main
// menu. Their commands run clingon itself in the shell, like any other command.
func builtinSections() []*config.Section {
	self := selfCommand()
	local, _ := config.YourPacks()
	installed, _ := config.InstalledPacks()
	all, _ := config.Packs()
	var available []config.Pack
	for _, p := range all {
		if !config.IsInstalled(p.Name) {
			available = append(available, p)
		}
	}
	ups, _ := config.PackUpdates()
	unless := func(ok bool, reason string) string {
		if ok {
			return ""
		}
		return reason
	}
	noLocal := unless(len(local) > 0, "You don't have any local packs yet: create one first.")
	noneInstalled := unless(len(installed) > 0, "No packs are installed.")
	sections := []*config.Section{
		{Name: "Packs", Icon: clingonMark, Color: clingonPink, Category: "Clingon", Groups: []config.Group{
			{Name: "Your packs", Commands: []config.Command{
				{Name: "Create a pack", Desc: "Makes a pack of your own in packs/local from a working example, and opens it in micro to make it yours: Ctrl-S saves, Ctrl-Q quits.", Cmd: self + " pack create <name>", Args: []config.Arg{
					{Name: "name", Prompt: "pack name", Desc: "Called <name>.", Placeholder: "letters, digits and dashes, e.g. work-tools"},
				}},
				{Name: "Edit a pack", Disabled: noLocal, Desc: "Opens one of your packs in micro: Ctrl-S saves, Ctrl-Q quits.", Cmd: self + " pack edit <pack>", Args: []config.Arg{
					{Name: "pack", Options: packOptions(local), Desc: "Editing <pack>."},
				}},
				{Name: "List your packs", Disabled: noLocal, Desc: "Lists your own packs.", Cmd: self + " pack list --local"},
				{Name: "Remove a pack", Disabled: noLocal, Desc: "Deletes one of your packs and every command in it, for good.", Risk: config.RiskDanger, Cmd: self + " pack remove <pack>", Args: []config.Arg{
					{Name: "pack", Options: packOptions(local), Desc: "Removing <pack>."},
				}},
			}},
			{Name: "Installed packs", Commands: []config.Command{
				{Name: "Add a pack", Disabled: unless(len(available) > 0, "Every pack is installed already."), Desc: "Installs a pack of ready-made commands into packs/remote.", Cmd: self + " pack add <pack>", Args: []config.Arg{
					{Name: "pack", Options: packOptions(available), Desc: "Adding <pack>."},
				}},
				{Name: "Available packs", Desc: "Lists the packs you can install and what each one covers.", Cmd: self + " pack available"},
				{Name: "List installed packs", Disabled: noneInstalled, Desc: "Lists the packs you've installed, with their versions.", Cmd: self + " pack list --remote"},
				{Name: "Remove a pack", Disabled: noneInstalled, Desc: "Uninstalls a pack, taking its commands out of the menu.", Risk: config.RiskWarn, Cmd: self + " pack remove <pack>", Args: []config.Arg{
					{Name: "pack", Options: packOptions(installed), Desc: "Removing <pack>."},
				}},
				{Name: "Update packs", Disabled: unless(len(ups) > 0, "Every installed pack is up to date."), Desc: "Installs newer versions of your installed packs, replacing their files in packs/remote, and removes any clingon no longer ships: changes you made to those files are lost.", Risk: config.RiskWarn, Cmd: self + " pack update"},
			}},
		}},
		{Name: "Health check", Icon: clingonMark, Color: clingonPink, Category: "Clingon", Groups: []config.Group{
			{Name: "Everything", Commands: []config.Command{
				{Name: "Run all checks", Desc: "Checks tmux, your packs and the tools every installed pack needs.", Cmd: self + " doctor"},
			}},
			{Name: "Crashes", Commands: []config.Command{
				{Name: "Show last crash", Desc: "Shows why clingon last crashed, with the trace to put in a bug report.", Cmd: self + " crash"},
			}},
			{Name: "Packs", Commands: []config.Command{
				{Name: "Check a pack's tools", Disabled: noneInstalled, Desc: "Checks that the tools an installed pack needs are on your PATH.", Cmd: self + " doctor <pack>", Args: []config.Arg{
					{Name: "pack", Options: packOptions(installed), Desc: "Checking <pack>."},
				}},
				{Name: "Check all packs", Desc: "Checks every pack, yours and installed, for mistakes.", Cmd: self + " check"},
			}},
		}},
		{Name: "Update", Icon: clingonMark, Color: clingonPink, Category: "Clingon", Groups: []config.Group{
			{Name: "clingon", Commands: []config.Command{
				{Name: "Show version", Desc: "Shows which version of clingon you're running.", Cmd: self + " version"},
				{Name: "Update clingon", Desc: "Upgrades clingon the same way you installed it.", Cmd: self + " update"},
			}},
			{Name: "Welcome", Commands: []config.Command{
				{Name: "Worth doing", Desc: "Shows the welcome card: updates, packs for tools you have, and anything else worth doing.", Cmd: self + " welcome"},
			}},
		}},
	}
	sort.SliceStable(sections, func(i, j int) bool { return config.NaturalLess(sections[i].Name, sections[j].Name) })
	for _, s := range sections {
		s.Sort()
	}
	return sections
}

// errQuiet fails the command after its own output has said why.
var errQuiet = errors.New("")

// doctor checks the install (tmux, PATH, packs) and that each installed
// pack's tools exist; with names, just those packs. Each is a card.
func doctor(names []string) error {
	failed := false
	add := func(rows *[]row, r row) {
		failed = failed || r.mark == fail
		*rows = append(*rows, r)
	}

	var packErr error
	if len(names) == 0 {
		var setup []row
		if out, err := exec.Command("tmux", "-V").Output(); err != nil {
			add(&setup, row{mark: fail, label: "tmux", detail: "not found: install it with `brew install tmux`"})
		} else if v := strings.TrimSpace(string(out)); tmuxTooOld(v) {
			add(&setup, row{mark: fail, label: "tmux", detail: v + " is too old: clingon needs tmux 3.2 or newer"})
		} else {
			add(&setup, row{mark: ok, label: "tmux", detail: v})
		}
		if err := shell.Check(os.Getenv("SHELL")); err != nil {
			add(&setup, row{mark: fail, label: "shell", detail: err.Error()})
		} else {
			add(&setup, row{mark: ok, label: "shell", detail: filepath.Base(os.Getenv("SHELL"))})
		}
		exe, _ := os.Executable()
		local := ""
		if sourceDir(exe) != "" || strings.Contains(exe, "go-build") {
			local = " · local build at " + exe
		}
		if isSelf("clingon") {
			add(&setup, row{mark: ok, label: "clingon", detail: getVersion() + ", on your PATH" + local})
		} else {
			where := "not on your PATH: only clingon's own panes find it"
			if other, err := exec.LookPath("clingon"); err == nil {
				where = "another clingon is first on your PATH, at " + other
			}
			add(&setup, row{mark: warn, label: "clingon", detail: getVersion() + ", " + where + local})
		}
		if _, err := exec.LookPath("micro"); err != nil {
			add(&setup, row{mark: warn, label: "micro", detail: "not installed: it's the editor for your packs (`brew install micro`)"})
		} else {
			add(&setup, row{mark: ok, label: "micro", detail: "installed, for editing your packs"})
		}
		if cfg, _, err := config.Load(); err != nil {
			packErr = err
			add(&setup, row{mark: fail, label: "packs", detail: "have problems: see below"})
		} else {
			n := 0
			for i := range cfg.Sections {
				n += len(cfg.Sections[i].All())
			}
			add(&setup, row{mark: ok, label: "packs", detail: fmt.Sprintf("valid: %d sections, %d commands", len(cfg.Sections), n)})
		}
		outln(card("clingon", setup, cardWidth()))
	}

	all, err := config.Packs()
	if err != nil {
		return err
	}
	byName := map[string]config.Pack{}
	for _, p := range all {
		byName[p.Name] = p
	}
	if len(names) == 0 {
		if names, err = config.Installed(); err != nil {
			return err
		}
	}
	var tools []row
	for _, ref := range names {
		_, n := config.SplitRef(ref)
		p, found := byName[n]
		if !found {
			add(&tools, row{mark: warn, label: n, detail: "clingon no longer ships it: `clingon pack update` removes it"})
			continue
		}
		missing := config.MissingTools(p.Requires)
		var have []string
		for _, tool := range p.AnyOf {
			if _, err := exec.LookPath(tool); err == nil {
				have = append(have, tool)
			}
		}
		switch {
		case len(missing) > 0:
			add(&tools, row{mark: fail, label: n, detail: config.Needs(missing)})
		case len(p.AnyOf) > 0 && len(have) == 0:
			add(&tools, row{mark: warn, label: n, detail: "needs one of " + strings.Join(p.AnyOf, ", ") + " (none installed)"})
		default:
			add(&tools, row{mark: ok, label: n, detail: strings.Join(append(p.Requires, have...), ", ")})
		}
	}
	if len(tools) > 0 {
		outln(card("Pack tools", tools, cardWidth()))
	}
	if packErr != nil {
		outln(failStyle.Render("Your packs have problems:"))
		outln("  " + strings.ReplaceAll(packErr.Error(), "\n", "\n  "))
	}
	if failed {
		return errQuiet
	}
	return nil
}

var tmuxVersion = regexp.MustCompile(`(\d+)\.(\d+)`)

func tmuxTooOld(v string) bool {
	m := tmuxVersion.FindStringSubmatch(v)
	if m == nil {
		return false // a dev build like "tmux master": assume it's new
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major < 3 || major == 3 && minor < 2
}

// update upgrades clingon the same way it was installed.
func update() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	var cmd []string
	dir := ""
	switch {
	case strings.Contains(exe, "/Cellar/"):
		cmd = []string{"brew", "upgrade", "con-dog/tap/clingon"}
	case sourceDir(exe) != "":
		dir = sourceDir(exe)
		cmd = []string{"go", "build", "-o", exe, "."}
	case strings.Contains(exe, "go-build"):
		say(info, "this is a `go run` build: every run already builds from your source.")
		return nil
	default:
		say(warn, "this clingon wasn't installed with Homebrew, the only way it's distributed. Install it with: brew install con-dog/tap/clingon")
		return nil
	}
	if dir != "" {
		step(strings.Join(cmd, " ") + greyStyle.Render(" (in "+dir+")"))
	} else {
		step(strings.Join(cmd, " "))
	}
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return err
	}
	say(ok, "updated: restart clingon to use the new version")
	return nil
}

// sourceDir is the clingon source checkout a local build sits in (built
// with `go build -o clingon .`), or "".
func sourceDir(exe string) string {
	dir := filepath.Dir(exe)
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil || !strings.HasPrefix(string(mod), "module github.com/con-dog/clingon\n") {
		return ""
	}
	return dir
}
