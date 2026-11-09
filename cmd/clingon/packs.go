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
	"github.com/con-dog/clingon/internal/mdcolor"
	"github.com/con-dog/clingon/internal/ui"
)

const packUsage = `Usage:
  clingon pack list              your packs, added and your own (--remote, --mine: just those)
  clingon pack available         packs you can add
  clingon pack add NAME...       add curated packs, or single modules, like aws/ecs
  clingon pack remove NAME...    remove packs or single modules (curated or your own)
  clingon pack update [NAME...]  bring added packs up to date
  clingon pack create [NAME]     start your own pack, as a form (or NAME LOGO TINT)
  clingon pack edit NAME         edit your pack, as a form
  clingon pack modules NAME      list the modules in your pack
  clingon pack describe NAME     print a pack's files, whether you have it or not
  clingon pack guide             how to write a pack: the folders and the format

A pack comes from clingon (in packs/remote once added) or is your own (in
packs/mine). If you have one of each with the same name, say remote/NAME or
mine/NAME. A pack is a folder, with a file for each module: aws is all of AWS,
and aws/ecs is just its ECS commands.
`

func packCmd(args []string) error {
	if len(args) == 0 {
		sayBoxed("Clingon pack", styleUsage(packUsage))
		return nil
	}
	var rest []string
	var names func(config.Pack) string // how to print a list as names, not as a card
	remoteOnly, localOnly, menu := false, false, false
	for _, a := range args[1:] {
		switch a {
		case "--names":
			names = config.Pack.Ref
		case "--short":
			names = config.Pack.Short
		case "--remote":
			remoteOnly = true
		case "--mine":
			localOnly = true
		case "--menu": // the form was opened from the menu, which asks whether to save
			menu = true
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
		// Two cards, so a pack's name is enough in each: you can have both a
		// remote/docker and a mine/docker.
		var remote, local []config.Pack
		var err error
		if !localOnly {
			if remote, err = config.InstalledPacks(); err != nil {
				return err
			}
		}
		if !remoteOnly {
			if local, err = config.YourPacks(); err != nil {
				return err
			}
		}
		switch {
		case remoteOnly:
			printPacks("Added packs", remote, names, "no packs added: see `clingon pack available`")
		case localOnly:
			printPacks("My packs", local, names, "no packs of your own yet: `clingon pack create` starts one")
		case len(remote)+len(local) == 0:
			printPacks("", nil, names, "no packs yet: see `clingon pack available`")
		default:
			if len(remote) > 0 {
				printPacks("Added packs", remote, names, "")
			}
			if len(local) > 0 {
				printPacks("My packs", local, names, "")
			}
		}
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
			} else if missing := config.MissingModules(p.Name); len(missing) > 0 {
				p.Description = "you have some of its modules; also: " + moduleSlugs(missing)
				p.Modules = missing
				list = append(list, p)
			}
		}
		printPacks("Packs you can add", list, names, "every pack is added")
		return nil

	case "add":
		if len(rest) == 0 {
			return errors.New("name a pack, e.g. `clingon pack add docker`")
		}
		for _, ref := range rest {
			where, name := config.SplitRef(ref)
			switch {
			case where == "mine":
				return fmt.Errorf("%s: only curated packs are added (yours are already there)", ref)
			case !config.InCatalog(name):
				return fmt.Errorf("no pack called %s to add (see `clingon pack available`); to start your own: `clingon pack create %s`", config.RemoteRef(name), name)
			case config.IsInstalled(name) && (strings.Contains(name, "/") || len(config.MissingModules(name)) == 0):
				say(info, "%s is already added", config.RemoteRef(name))
			default:
				if err := config.Install(name); err != nil {
					return err
				}
				say(ok, "added %s", config.RemoteRef(name))
			}
		}
		return nil

	case "create", "new":
		if len(rest) == 0 { // a new pack, made in the form
			return newPackForm(menu)
		}
		logo, color := "", ""
		if len(rest) == 3 { // a pack, its logo and the logo's colour
			color, rest = rest[2], rest[:2]
		}
		if len(rest) == 2 {
			logo, rest = rest[1], rest[:1]
		}
		ref, err := one("create")
		if err != nil {
			return err
		}
		_, name := config.SplitRef(ref)
		if err := config.NewPackWithLogo(name, logo, color); err != nil {
			return err
		}
		return formEdit(name, "created", menu)

	case "remove", "rm":
		if len(rest) == 0 {
			return errors.New("name a pack, e.g. `clingon pack remove docker`")
		}
		for _, ref := range rest {
			where, name := config.SplitRef(ref)
			remote, local := config.IsInstalled(name), config.IsLocal(name)
			switch {
			case where == "" && remote && local:
				return fmt.Errorf("both packs/remote and packs/mine have %s: say remote/%s or mine/%s", name, name, name)
			case where == "remote" || where == "" && remote:
				shown := config.RemoteRef(name)
				if err := config.Uninstall(name); err != nil {
					return err
				}
				say(ok, "removed %s", shown)
			case where == "mine" || where == "" && local:
				if err := config.DeletePack(name); err != nil {
					return err
				}
				say(ok, "removed mine/%s", name)
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
			if where == "mine" {
				return fmt.Errorf("%s: only remote packs are updated (yours are yours to change)", ref)
			}
			pack, _ := config.SplitModule(name) // the whole of what you have of it
			if !config.IsInstalled(pack) {
				return fmt.Errorf("%s isn't added", config.RemoteRef(pack))
			}
			want[pack] = true
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
			if err := config.UpdatePack(u.Name); err != nil {
				return err
			}
			say(ok, "updated %s", config.RemoteRef(u.Name))
			done++
		}
		if done == 0 {
			say(info, "all set: your added packs are up to date")
		}
		// An update never adds to what you chose: this is how you hear what's there.
		installed, _ := config.Installed()
		for _, name := range installed {
			if missing := config.MissingModules(name); len(missing) > 0 && (len(want) == 0 || want[name]) {
				say(info, "%s also has %s: `clingon pack add %s/%s`", config.RemoteRef(name), moduleSlugs(missing), config.RemoteRef(name), missing[0].Slug)
			}
		}
		return nil

	case "edit":
		ref, err := one("edit")
		if err != nil {
			return err
		}
		return formEdit(ref, "saved", menu)

	case "guide":
		return guide()

	case "describe", "cat":
		ref, err := one("describe")
		if err != nil {
			return err
		}
		data, err := config.PackSource(ref)
		if err != nil {
			return err
		}
		if isTerm() { // in the menu's colours, in a box; to a file or a pipe it's the file as it is
			sayBoxed(ref, mdcolor.Markdown(string(data)))
			return nil
		}
		_, err = os.Stdout.Write(data)
		return err

	case "modules":
		ref, err := one("modules")
		if err != nil {
			return err
		}
		_, name := config.SplitRef(ref)
		names := config.SectionNames(name)
		if isTerm() && len(names) > 0 {
			var rows []row
			for _, s := range names {
				rows = append(rows, row{mark: info, label: s})
			}
			outln(card("Modules of "+ref, rows, cardWidth()))
			return nil
		}
		for _, s := range names {
			fmt.Println(s)
		}
		return nil
	}
	errUsage(styleUsage(packUsage))
	os.Exit(2)
	return nil
}

// moduleSlugs names modules, as you'd add them.
func moduleSlugs(ms []config.Module) string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Slug
	}
	return strings.Join(out, ", ")
}

// printPacks shows a card titled title (a table when it isn't printing to a
// terminal), or with names "ref<TAB>description" lines for tab
// completion, where ref is qualified (remote/x, mine/x) for packs you
// have.
func printPacks(title string, ps []config.Pack, names func(config.Pack) string, empty string) {
	if names != nil {
		for _, p := range ps {
			fmt.Printf("%s\t%s\n", names(p), p.Description)
			if len(p.Modules) > 1 {
				for _, m := range p.Modules {
					fmt.Printf("%s/%s\t%s\n", names(p), m.Slug, m.Name)
				}
			}
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
		countW := 2
		for _, p := range ps {
			countW = max(countW, len(strconv.Itoa(p.Commands)))
		}
		var rows []row
		for _, p := range ps {
			note := fmt.Sprintf("%*d command", countW, p.Commands)
			if p.Commands != 1 {
				note += "s"
			} else {
				note += " "
			}
			if len(p.Modules) > 0 && p.Where == "remote" && config.IsInstalled(p.Name) {
				if missing := len(config.MissingModules(p.Name)); missing > 0 {
					note += fmt.Sprintf(" · %s not added", plural(missing, "module"))
				}
			}
			rows = append(rows, row{mark: info, label: p.Name, detail: p.Description, note: note})
		}
		outln(card(title, rows, cardWidth()))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, p := range ps {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", p.Ref(), p.Description, plural(p.Commands, "command"))
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
  clingon command add PACK MODULE NAME COMMAND [DESCRIPTION]
  clingon command list

Commands go in your own packs; add creates the pack and module if needed.
Each <placeholder> in COMMAND is asked for when you run it, e.g.
  clingon command add mine Deploy "Tail logs" 'kubectl logs -f deploy/<app>'
`

func commandCmd(args []string) error {
	if len(args) == 0 {
		sayBoxed("Clingon command", styleUsage(commandUsage))
		return nil
	}
	switch args[0] {
	case "add":
		a := args[1:]
		if len(a) < 4 || len(a) > 5 {
			return fmt.Errorf("usage: clingon command add PACK MODULE NAME COMMAND [DESCRIPTION]")
		}
		desc := ""
		if len(a) == 5 {
			desc = a[4]
		}
		where, pack := config.SplitRef(a[0])
		if where == "remote" {
			return errors.New("curated packs are overwritten by `clingon pack update`: add commands to a pack of your own")
		}
		if err := config.AddCommand(pack, a[1], a[2], a[3], desc); err != nil {
			return err
		}
		say(ok, "added %q to mine/%s › %s", a[2], pack, a[1])
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
				fmt.Printf("mine/%s › %s › %s\t%s\n", c.Pack, c.Section, c.Name, c.Cmd)
			}
			return nil
		}
		var rows []row
		for _, c := range cfgs {
			rows = append(rows, row{mark: info, label: "mine/" + c.Pack + " › " + c.Section + " › " + c.Name, detail: c.Cmd})
		}
		outln(card("Your commands", rows, cardWidth()))
		return nil
	}
	errUsage(styleUsage(commandUsage))
	os.Exit(2)
	return nil
}

const recipeUsage = `Usage:
  clingon recipe add [--replace] NAME ID...
  clingon recipe remove NAME
  clingon recipe list
  clingon recipe describe NAME
  clingon recipe describe --steps ID...

A recipe runs commands in order. Build one in the menu with 1-9, then Add as…
from Clingon › Recipes, where it's listed; each ID is one of its commands.
--replace replaces the recipe of that name, keeping its description. Each is
a Markdown file in recipes/, next to packs/, that you can edit too.
`

func recipeCmd(args []string) error {
	if len(args) == 0 {
		sayBoxed("Clingon recipe", styleUsage(recipeUsage))
		return nil
	}
	switch args[0] {
	case "add":
		replace := len(args) > 1 && args[1] == "--replace"
		if replace {
			args = append(args[:1:1], args[2:]...)
		}
		if len(args) < 3 {
			return errors.New("usage: clingon recipe add [--replace] NAME ID...")
		}
		var labels []string // each link says its command's name, where that's known
		if found, err := commandsByID(); err == nil {
			for _, id := range args[2:] {
				labels = append(labels, found[id].name())
			}
		}
		save, did := config.SaveRecipe, "added"
		if replace {
			save, did = config.ReplaceRecipe, "replaced"
		}
		if err := save(args[1], args[2:], labels); err != nil {
			return err
		}
		say(ok, "%s recipe %q", did, args[1])
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
			say(info, "no recipes yet: run one from Clingon › Recipes, and add it")
			return nil
		}
		if isTerm() {
			var rows []row
			for _, r := range rs {
				rows = append(rows, row{mark: info, label: r.Name, detail: plural(len(r.Steps), "step")})
			}
			outln(card("Your recipes", rows, cardWidth()))
			return nil
		}
		for _, r := range rs {
			fmt.Printf("%s\t%s\n", r.Name, strings.Join(r.Steps, " "))
		}
		return nil
	case "describe":
		if len(args) > 2 && args[1] == "--steps" { // a recipe not saved yet: the menu's current one
			return describeSteps("This recipe", args[2:])
		}
		if len(args) != 2 {
			return errors.New("usage: clingon recipe describe NAME, or --steps ID...")
		}
		return describeRecipe(args[1])
	}
	errUsage(styleUsage(recipeUsage))
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
	return describeSteps(rs[i].Name, rs[i].Steps)
}

// describeSteps says what a recipe called name, of these steps, does, step
// by step.
func describeSteps(name string, steps []string) error {
	cmds, err := commandsByID()
	if err != nil {
		return err
	}
	r := config.Recipe{Name: name, Steps: steps}
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

// name is the command's name, or "" if there's no such command.
func (f foundCommand) name() string {
	if f.cmd == nil {
		return ""
	}
	return f.cmd.Name
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
