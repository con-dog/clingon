package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/shell"
	"github.com/con-dog/clingon/internal/ui"
)

// popularity orders pack suggestions: the most widely useful first.
var popularity = []string{"git", "docker", "node", "github", "aws", "postgres", "claude-code", "codex", "redis"}

func rank(name string) int {
	for i, n := range popularity {
		if n == name {
			return i
		}
	}
	return len(popularity)
}

// welcome prints clingon's status card: what's worth doing, or that all's
// well. With execShell it then becomes the user's login shell, which is how
// clingon's shell pane starts.
func welcome(execShell bool) {
	if execShell {
		defer becomeShell()
	}
	if !execShell { // in clingon's shell pane, the menu's banner already says it
		outln() // a gap below the command that ran it
		outln(nameStyle.Render("clingon") + " " + greyStyle.Render(getVersion()))
		outln()
	}
	rows := recommendations()
	title := "Worth doing"
	if len(rows) == 0 {
		rows, title = []row{allSet()}, "clingon"
	}
	outln(card(title, rows, cardWidth()))
	outln()
}

// recommendations are what's worth doing, most useful first: the news,
// then whether the packs load at all.
func recommendations() []row {
	acts := news()
	if _, _, err := config.Load(); err != nil {
		acts = append(acts, row{mark: fail, label: "Fix your packs", detail: "they have mistakes", note: "Clingon › Health check › Check all packs"})
	}
	return acts
}

// news is what's worth doing, as the welcome card's rows and, the first of
// them, the menu's status bar: "label: detail", then where in the menu to
// do it. (Checking the packs load is left to the welcome: the menu is
// running, so they do.)
func news() []row {
	var acts []row
	if r, found := crashNews(); found {
		acts = append(acts, r)
	}
	if latest := latestRelease(); latest != "" {
		acts = append(acts, row{mark: todo, glyph: "↑", label: "Update clingon", detail: getVersion() + " → " + latest, note: "Clingon › Update › Update clingon"})
	}
	if ups, err := config.PackUpdates(); err == nil && len(ups) > 0 {
		var names []string
		for _, u := range ups {
			names = append(names, retiredName(u))
		}
		acts = append(acts, row{mark: todo, glyph: "↑", label: "Update " + plural(len(ups), "pack"), detail: strings.Join(names, ", "), note: "Clingon › Packs › Update packs"})
	}
	suggest, broken := packAdvice()
	if len(suggest) > 0 {
		acts = append(acts, row{mark: todo, glyph: packGlyph(), label: "Add " + plural(len(suggest), "pack"), detail: "you have the tools for " + strings.Join(suggest, ", "), note: "Clingon › Packs › Add a pack"})
	}
	if len(broken) > 0 { // nothing in the menu installs tools: no note; its rows say what's missing
		acts = append(acts, row{mark: todo, glyph: "↓", label: "Install tools", detail: strings.Join(broken, ", "), nudge: true})
	}
	if files, err := config.Files(); err == nil && len(files) == 0 {
		acts = append(acts, row{mark: todo, glyph: "⊕", label: "Create a pack", detail: "the commands you keep looking up, in one place", note: "Clingon › Packs › Create a pack", nudge: true})
	}
	return acts
}

// allSet is the row for when nothing's worth doing.
func allSet() row {
	names, _ := config.Installed()
	return row{mark: ok, label: "All set", detail: plural(len(names), "pack") + ", all up to date"}
}

// packAdvice lists packs worth adding (their tools are on PATH) and, for
// installed packs whose tools are missing, what's missing: "Azure needs az",
// named as the menu names them.
func packAdvice() (suggest, broken []string) {
	all, err := config.Packs()
	if err != nil {
		return nil, nil
	}
	names, _ := config.Installed()
	installed := map[string]bool{}
	for _, n := range names {
		installed[n] = true
	}
	for _, p := range all {
		var missing []string
		for _, tool := range p.Requires {
			if _, err := exec.LookPath(tool); err != nil {
				missing = append(missing, tool)
			}
		}
		// A pack whose groups each need their own tool (package-managers:
		// brew) is worth having if any one of them is here.
		if len(p.AnyOf) > 0 && !slices.ContainsFunc(p.AnyOf, func(tool string) bool { _, err := exec.LookPath(tool); return err == nil }) {
			missing = append(missing, "one of "+strings.Join(p.AnyOf, ", "))
		}
		if len(p.Requires) == 0 && len(p.AnyOf) == 0 {
			continue // nothing to tell from: don't suggest it
		}
		switch {
		case !installed[p.Name] && len(missing) == 0:
			suggest = append(suggest, p.Name)
		case installed[p.Name] && len(missing) > 0:
			name := p.Name
			if len(p.Sections) > 0 {
				name = p.Sections[0]
			}
			broken = append(broken, name+" needs "+strings.Join(missing, ", "))
		}
	}
	sort.SliceStable(suggest, func(i, j int) bool { return rank(suggest[i]) < rank(suggest[j]) })
	return suggest, broken
}

// packGlyph marks pack suggestions: in clingon's window, the box that
// Package managers has, from its font; elsewhere a symbol every font has.
func packGlyph() string {
	if os.Getenv("CLINGON") == "" {
		return "⊞"
	}
	return config.Logos["box"]
}

// headline is the menu's status bar on the top level: the most useful
// news, worded as the welcome card words it, or that all's well.
func headline() ui.Headline {
	r := allSet()
	kind, glyph := ui.Good, "✓"
	if acts := news(); len(acts) > 0 && !acts[0].nudge {
		r, kind, glyph = acts[0], ui.Info, acts[0].glyph
		if glyph == "↑" || r.mark == fail {
			kind = ui.Attention
		}
	}
	text := glyph + " " + r.label + ": " + r.detail
	if r.note != "" {
		text += " · " + r.note
	}
	return ui.Headline{Kind: kind, Text: text}
}

// retiredName is how an update is named: a pack clingon no longer ships
// says so, since updating removes it.
func retiredName(u config.PackUpdate) string {
	if u.Retired {
		return u.Name + " (retired)"
	}
	return u.Name
}

func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

// becomeShell replaces this process with the user's login shell.
func becomeShell() {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	err := shell.Exec(config.Dir(), sh) // with clingon's look; only returns if it couldn't
	sayErr(err)
	argv0 := "-" + filepath.Base(sh) // a leading dash makes it a login shell
	_ = syscall.Exec(sh, []string{argv0}, os.Environ())
	_ = syscall.Exec("/bin/sh", []string{"-sh"}, os.Environ())
}

// ---- update check ----
//
// welcome never waits on the network: it reads the last answer from a small
// cache and, at most once a day, refreshes it in the background with curl.

// releasePage redirects to the newest release's page, .../releases/tag/vX.Y.Z,
// so the version is in the redirect: no API, no JSON.
const releasePage = "https://github.com/con-dog/clingon/releases/latest"

// releaseCache is the last check's answer, kept as one line:
// "<unix time checked> <latest version>".
type releaseCache struct {
	Checked time.Time
	Latest  string
}

func readReleaseCache() releaseCache {
	var c releaseCache
	b, err := os.ReadFile(releaseCachePath())
	if err != nil {
		return c
	}
	when, latest, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	if secs, err := strconv.ParseInt(when, 10, 64); err == nil {
		c.Checked, c.Latest = time.Unix(secs, 0), latest
	}
	return c
}

func releaseCachePath() string { return filepath.Join(config.Dir(), "latest-release") }

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// latestRelease returns a newer release's version, or "" if there's none
// known (or this is a dev build, which never checks).
func latestRelease() string {
	if !semverRe.MatchString(getVersion()) {
		return ""
	}
	c := readReleaseCache()
	if time.Since(c.Checked) > 24*time.Hour {
		checkOnce.Do(func() { // the menu asks often: one check per run is plenty
			if self, err := os.Executable(); err == nil {
				cmd := exec.Command(self, "_check-release")
				cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // outlive the pane
				if cmd.Start() == nil {
					go func() { _ = cmd.Wait() }() // reap it, so it doesn't linger as a zombie
				}
			}
		})
	}
	if newer(c.Latest, getVersion()) {
		return c.Latest
	}
	return ""
}

// checkRelease asks GitHub for the latest release and caches the answer.
var checkOnce sync.Once

func checkRelease() {
	latest := readReleaseCache().Latest // kept if this check fails: offline isn't "no update"
	out, err := exec.Command("curl", "-fsS", "-o", "/dev/null", "-w", "%{redirect_url}", "--max-time", "5", releasePage).Output()
	if err == nil {
		latest = "" // no release yet redirects to .../releases, which isn't one
		if tag := path.Base(strings.TrimSpace(string(out))); semverRe.MatchString(tag) {
			latest = tag
		}
	}
	_ = os.MkdirAll(filepath.Dir(releaseCachePath()), 0o755)
	_ = os.WriteFile(releaseCachePath(), []byte(fmt.Sprintf("%d %s\n", time.Now().Unix(), latest)), 0o644)
}

// newer reports whether version a is later than b (both like v1.2.3).
func newer(a, b string) bool {
	ma, mb := semverRe.FindStringSubmatch(a), semverRe.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			return x > y
		}
	}
	return false
}
