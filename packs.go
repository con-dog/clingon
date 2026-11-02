package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/con-dog/clingon/internal/config"
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

Installed packs live in packs/remote, your own in packs/local. If both have
a pack of the same name, say which: remote/NAME or local/NAME.
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
			_, name := config.SplitRef(ref)
			switch {
			case !config.InCatalog(name):
				return fmt.Errorf("no pack called %q to install (see `clingon pack available`); to start your own: `clingon pack create %s`", name, name)
			case config.IsInstalled(name):
				say(info, "%s is already installed", name)
			default:
				if err := config.Install(name); err != nil {
					return err
				}
				say(ok, "installed %s into packs/remote", name)
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
				say(ok, "removed %s from packs/remote", name)
			case where == "local" || where == "" && local:
				if err := config.DeletePack(name); err != nil {
					return err
				}
				say(ok, "removed %s from packs/local", name)
			default:
				return fmt.Errorf("you don't have a pack called %s", name)
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
			_, name := config.SplitRef(ref)
			if !config.IsInstalled(name) {
				return fmt.Errorf("%s isn't installed", name)
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
				say(ok, "removed %s %s", u.Name, greyStyle.Render("(clingon no longer ships it)"))
				done++
				continue
			}
			if err := config.Install(u.Name); err != nil {
				return err
			}
			say(ok, "updated %s %s", u.Name, greyStyle.Render(u.From+" → "+u.To))
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
			ref := p.Name
			if p.Where != "" {
				ref = p.Where + "/" + p.Name
			}
			fmt.Printf("%s\t%s\n", ref, p.Description)
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
		whereW, versionW, countW := 0, 0, 2
		for _, p := range ps {
			whereW, versionW = max(whereW, len(p.Where)), max(versionW, len(p.Version))
			countW = max(countW, len(strconv.Itoa(p.Commands)))
		}
		var rows []row
		for _, p := range ps {
			note := fmt.Sprintf("%*d commands", countW, p.Commands)
			if versionW > 0 {
				note = fmt.Sprintf("%-*s · ", versionW, p.Version) + note
			}
			if whereW > 0 {
				note = fmt.Sprintf("%-*s · ", whereW, p.Where) + note
			}
			rows = append(rows, row{mark: info, label: p.Name, detail: p.Description, note: note})
		}
		outln(card(title, rows, cardWidth()))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, p := range ps {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d commands\n", p.Name, p.Where, p.Version, p.Description, p.Commands)
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
  clingon command remove PACK/SECTION/NAME
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
		say(ok, "added %q to %s › %s", a[2], pack, a[1])
		return nil
	case "remove", "rm":
		if len(args) != 2 || strings.Count(args[1], "/") < 2 {
			return fmt.Errorf("usage: clingon command remove PACK/SECTION/NAME")
		}
		parts := strings.SplitN(args[1], "/", 3)
		if err := config.RemoveCommand(parts[0], parts[1], parts[2]); err != nil {
			return err
		}
		say(ok, "removed %q from %s › %s", parts[2], parts[0], parts[1])
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
				fmt.Printf("%s/%s/%s\t%s\n", c.Pack, c.Section, c.Name, c.Cmd)
			}
			return nil
		}
		var rows []row
		for _, c := range cfgs {
			rows = append(rows, row{mark: info, label: c.Pack + " › " + c.Section + " › " + c.Name, detail: c.Cmd})
		}
		outln(card("Your commands", rows, cardWidth()))
		return nil
	}
	errf("%s", styleUsage(commandUsage))
	os.Exit(2)
	return nil
}
