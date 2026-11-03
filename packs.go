package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/micro"
	"github.com/con-dog/clingon/internal/ui"
)

const packUsage = `Usage:
  clingon pack list              your packs, installed and local (--remote, --local: just those)
  clingon pack available         packs you can install
  clingon pack add NAME...       install packs
  clingon pack remove NAME...    remove packs (installed or your own)
  clingon pack update [NAME...]  install newer versions of installed packs
  clingon pack create NAME       start a pack of your own (from an example) in micro
  clingon pack edit NAME         edit your pack in micro
  clingon pack sections NAME     list the sections in your pack
  clingon pack inspect NAME      print a pack's file, whether you have it or not

Every pack is remote/NAME (from clingon, in packs/remote once installed) or
local/NAME (your own, in packs/local). NAME alone will do unless you have both.
`

func packCmd(args []string) error {
	if len(args) == 0 {
		outf("%s", styleUsage(packUsage))
		return nil
	}
	var rest []string
	namesOnly, remoteOnly, localOnly := false, false, false
	for _, a := range args[1:] {
		switch a {
		case "--names":
			namesOnly = true
		case "--remote":
			remoteOnly = true
		case "--local":
			localOnly = true
		default:
			rest = append(rest, a)
		}
	}
	one := func(verb string) (string, error) {
		if len(rest) != 1 {
			return "", fmt.Errorf("name one pack, e.g. `clingon pack %s mine`", verb)
		}
		return rest[0], nil
	}

	switch args[0] {
	case "list", "ls":
		var list []config.Pack
		if !localOnly {
			remote, err := config.InstalledPacks()
			if err != nil {
				return err
			}
			list = append(list, remote...)
		}
		if !remoteOnly {
			local, err := config.YourPacks()
			if err != nil {
				return err
			}
			list = append(list, local...)
		}
		printPacks("Your packs", list, namesOnly, "no packs yet: see `clingon pack available`")
		return nil

	case "available":
		all, err := config.Packs()
		if err != nil {
			return err
		}
		var list []config.Pack
		for _, p := range all {
			if !config.IsInstalled(p.Name) {
				list = append(list, p)
			}
		}
		printPacks("Packs you can add", list, namesOnly, "every pack is installed")
		return nil

	case "add", "install":
		if len(rest) == 0 {
			return errors.New("name a pack, e.g. `clingon pack add docker`")
		}
		for _, ref := range rest {
			where, name := config.SplitRef(ref)
			switch {
			case where == "local":
				return fmt.Errorf("%s: only remote packs are installed (yours are already there)", ref)
			case !config.InCatalog(name):
				return fmt.Errorf("no pack called %s to install (see `clingon pack available`); to start your own: `clingon pack create %s`", config.RemoteRef(name), name)
			case config.IsInstalled(name):
				say(info, "%s is already installed", config.RemoteRef(name))
			default:
				if err := config.Install(name); err != nil {
					return err
				}
				say(ok, "installed %s", config.RemoteRef(name))
			}
		}
		return nil

	case "create", "new":
		ref, err := one("create")
		if err != nil {
			return err
		}
		_, name := config.SplitRef(ref)
		if err := config.NewPack(name); err != nil {
			return err
		}
		say(ok, "created %s", config.PackPath(name))
		return editPack(name)

	case "remove", "rm", "uninstall":
		if len(rest) == 0 {
			return errors.New("name a pack, e.g. `clingon pack remove docker`")
		}
		for _, ref := range rest {
			where, name := config.SplitRef(ref)
			remote, local := config.IsInstalled(name), config.IsLocal(name)
			switch {
			case where == "" && remote && local:
				return fmt.Errorf("both packs/remote and packs/local have %s: say remote/%s or local/%s", name, name, name)
			case where == "remote" || where == "" && remote:
				if err := config.Uninstall(name); err != nil {
					return err
				}
				say(ok, "removed %s", config.RemoteRef(name))
			case where == "local" || where == "" && local:
				if err := config.DeletePack(name); err != nil {
					return err
				}
				say(ok, "removed local/%s", name)
			default:
				return fmt.Errorf("you don't have a pack called %s", ref)
			}
		}
		return nil

	case "update", "upgrade":
		ups, err := config.PackUpdates()
		if err != nil {
			return err
		}
		want := map[string]bool{}
		for _, ref := range rest {
			where, name := config.SplitRef(ref)
			if where == "local" {
				return fmt.Errorf("%s: only remote packs are updated (yours are yours to change)", ref)
			}
			if !config.IsInstalled(name) {
				return fmt.Errorf("%s isn't installed", config.RemoteRef(name))
			}
			want[name] = true
		}
		done := 0
		for _, u := range ups {
			if len(want) > 0 && !want[u.Name] {
				continue
			}
			if u.Retired {
				if err := config.Uninstall(u.Name); err != nil {
					return err
				}
				say(ok, "removed %s %s", config.RemoteRef(u.Name), greyStyle.Render("(clingon no longer ships it)"))
				done++
				continue
			}
			if err := config.Install(u.Name); err != nil {
				return err
			}
			say(ok, "updated %s %s", config.RemoteRef(u.Name), greyStyle.Render(u.From+" → "+u.To))
			done++
		}
		if done == 0 {
			say(info, "all set: your installed packs are up to date")
		}
		return nil

	case "edit":
		ref, err := one("edit")
		if err != nil {
			return err
		}
		return editPack(ref)

	case "inspect", "cat":
		ref, err := one("inspect")
		if err != nil {
			return err
		}
		data, err := config.PackSource(ref)
		if err != nil {
			return err
		}
		if isTerm() { // in micro's colours; to a file or a pipe it's the file as it is
			outf("%s", micro.YAML(string(data)))
			return nil
		}
		_, err = os.Stdout.Write(data)
		return err

	case "sections":
		ref, err := one("sections")
		if err != nil {
			return err
		}
		_, name := config.SplitRef(ref)
		for _, s := range config.SectionNames(name) {
			fmt.Println(s)
		}
		return nil
	}
	errf("%s", styleUsage(packUsage))
	os.Exit(2)
	return nil
}

// printPacks shows a card titled title (a table when it isn't printing to a
// terminal), or with namesOnly "ref<TAB>description" lines for tab
// completion, where ref is qualified (remote/x, local/x) for packs you
// have.
func printPacks(title string, ps []config.Pack, namesOnly bool, empty string) {
	if namesOnly {
		for _, p := range ps {
			fmt.Printf("%s\t%s\n", p.Ref(), p.Description)
		}
		return
	}
	if len(ps) == 0 {
		say(info, "%s", empty)
		return
	}
	if isTerm() {
		// The note's parts are columns, each as wide as its widest, so they
		// line up down the card: room for "XX commands" at least.
		versionW, countW := 0, 2
		for _, p := range ps {
			versionW = max(versionW, len(p.Version))
			countW = max(countW, len(strconv.Itoa(p.Commands)))
		}
		var rows []row
		for _, p := range ps {
			note := fmt.Sprintf("%*d commands", countW, p.Commands)
			if versionW > 0 {
				note = fmt.Sprintf("%-*s · ", versionW, p.Version) + note
			}
			rows = append(rows, row{mark: info, label: p.Ref(), detail: p.Description, note: note})
		}
		outln(card(title, rows, cardWidth()))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, p := range ps {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d commands\n", p.Ref(), p.Version, p.Description, p.Commands)
	}
	_ = w.Flush()
}

// selfCommand is how the shell should call this binary: plain `clingon`
// when that's what's on PATH, else its full path (e.g. a local build).
func selfCommand() string {
	exe, err := os.Executable()
	if err != nil || isSelf("clingon") {
		return "clingon"
	}
	return "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
}

// isSelf reports whether name, looked up on PATH, is this very binary.
func isSelf(name string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return false
	}
	a, errA := os.Stat(p)
	b, errB := os.Stat(exe)
	return errA == nil && errB == nil && os.SameFile(a, b)
}

const commandUsage = `Usage:
  clingon command add PACK SECTION NAME COMMAND [DESCRIPTION]
  clingon command list

Commands go in your own packs; add creates the pack and section if needed.
Each <placeholder> in COMMAND is asked for when you run it, e.g.
  clingon command add mine Deploy "Tail logs" 'kubectl logs -f deploy/<app>'
`

func commandCmd(args []string) error {
	if len(args) == 0 {
		outf("%s", styleUsage(commandUsage))
		return nil
	}
	switch args[0] {
	case "add":
		a := args[1:]
		if len(a) < 4 || len(a) > 5 {
			return fmt.Errorf("usage: clingon command add PACK SECTION NAME COMMAND [DESCRIPTION]")
		}
		desc := ""
		if len(a) == 5 {
			desc = a[4]
		}
		where, pack := config.SplitRef(a[0])
		if where == "remote" {
			return errors.New("installed packs are overwritten by `clingon pack update`: add commands to a local pack")
		}
		if err := config.AddCommand(pack, a[1], a[2], a[3], desc); err != nil {
			return err
		}
		say(ok, "added %q to local/%s › %s", a[2], pack, a[1])
		return nil
	case "list", "ls":
		cfgs, err := config.YourCommands()
		if err != nil {
			return err
		}
		if len(cfgs) == 0 {
			say(info, "no commands of your own yet: add one with `clingon command add`")
		}
		if !isTerm() || len(cfgs) == 0 {
			for _, c := range cfgs {
				fmt.Printf("local/%s › %s › %s\t%s\n", c.Pack, c.Section, c.Name, c.Cmd)
			}
			return nil
		}
		var rows []row
		for _, c := range cfgs {
			rows = append(rows, row{mark: info, label: "local/" + c.Pack + " › " + c.Section + " › " + c.Name, detail: c.Cmd})
		}
		outln(card("Your commands", rows, cardWidth()))
		return nil
	}
	errf("%s", styleUsage(commandUsage))
	os.Exit(2)
	return nil
}

const recipeUsage = `Usage:
  clingon recipe save NAME ID...
  clingon recipe remove NAME
  clingon recipe list
  clingon recipe describe NAME

A recipe runs commands in order. The menu saves one for you when you run it, from
Clingon › Recipes, and lists it there; each ID is one of its commands.
`

func recipeCmd(args []string) error {
	if len(args) == 0 {
		outf("%s", styleUsage(recipeUsage))
		return nil
	}
	switch args[0] {
	case "save":
		if len(args) < 3 {
			return errors.New("usage: clingon recipe save NAME ID...")
		}
		if err := config.SaveRecipe(args[1], args[2:]); err != nil {
			return err
		}
		say(ok, "saved recipe %q", args[1])
		return nil
	case "remove", "rm":
		if len(args) != 2 {
			return errors.New("usage: clingon recipe remove NAME")
		}
		if err := config.RemoveRecipe(args[1]); err != nil {
			return err
		}
		say(ok, "removed recipe %q", args[1])
		return nil
	case "list", "ls":
		rs, err := config.Recipes()
		if err != nil {
			return err
		}
		if len(rs) == 0 {
			say(info, "no saved recipes yet: run one from Clingon › Recipes, and save it")
			return nil
		}
		for _, r := range rs {
			fmt.Printf("%s\t%s\n", r.Name, strings.Join(r.Steps, " "))
		}
		return nil
	case "describe":
		if len(args) != 2 {
			return errors.New("usage: clingon recipe describe NAME")
		}
		return describeRecipe(args[1])
	}
	errf("%s", styleUsage(recipeUsage))
	os.Exit(2)
	return nil
}

// describeRecipe says what a saved recipe does, step by step.
func describeRecipe(name string) error {
	rs, err := config.Recipes()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(rs, func(r config.Recipe) bool { return strings.EqualFold(r.Name, name) })
	if i < 0 {
		return fmt.Errorf("you don't have a recipe called %q (see `clingon recipe list`)", name)
	}
	cmds, err := commandsByID()
	if err != nil {
		return err
	}
	r := rs[i]
	const indent = "    " // under the name, past the step's badge
	w := cardWidth()
	inner := w - 4 - len(indent)
	var lines []string
	add := func(st lipgloss.Style, text string) {
		for _, l := range strings.Split(ansi.Wrap(text, inner, ""), "\n") {
			lines = append(lines, indent+st.Render(l))
		}
	}
	for n, id := range r.Steps {
		if n > 0 {
			lines = append(lines, "")
		}
		c, ok := cmds[id]
		if !ok {
			lines = append(lines, ui.StepBadge(n+1)+" "+failStyle.Render("Not available here")+greyStyle.Render(" · "+id))
			add(greyStyle, "Its pack is gone, or needs a tool you don't have. The menu won't run this recipe until it's back.")
			continue
		}
		lines = append(lines, ui.StepBadge(n+1)+" "+okStyle.Render(c.cmd.Name)+greyStyle.Render(" · "+c.section))
		for _, l := range ui.CommandBlock(c.cmd, inner) {
			lines = append(lines, indent+l)
		}
		if c.cmd.Desc != "" {
			add(lipgloss.NewStyle(), c.cmd.Desc)
		}
		if len(c.cmd.Args) > 0 {
			var asks []string
			for _, a := range c.cmd.Args {
				asks = append(asks, a.Label())
			}
			add(greyStyle, "Asks for: "+strings.Join(asks, ", "))
		}
		switch c.cmd.Risk {
		case config.RiskWarn:
			add(warnStyle, "⚠ Asks before it runs")
		case config.RiskDanger:
			add(failStyle, "☠ Can't be undone: you type a code before it runs")
		}
	}
	title := headStyle.Render(r.Name) + greyStyle.Render(" · "+plural(len(r.Steps), "step")+", stops if one fails")
	outln(titled(title, lines, w))
	return nil
}

// foundCommand is a command, and the section it's listed under.
type foundCommand struct {
	section string
	cmd     *config.Command
}

// commandsByID is what the menu offers from this folder, by the ID recipes use.
func commandsByID() (map[string]foundCommand, error) {
	cfg, _, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := map[string]foundCommand{}
	add := func(s *config.Section) {
		for _, c := range s.All() {
			out[s.CommandID(c)] = foundCommand{s.Name, c}
		}
	}
	for _, s := range builtinSections() {
		add(s)
	}
	for i := range cfg.Sections {
		add(&cfg.Sections[i])
	}
	if dir, err := os.Getwd(); err == nil {
		if p := config.FindProject(dir); p != nil && p.Config != nil {
			for i := range p.Config.Sections {
				add(&p.Config.Sections[i])
			}
		}
	}
	return out, nil
}
