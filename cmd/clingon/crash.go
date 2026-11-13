package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
)

// A crash in the menu ends the session and closes the window, so nobody gets
// to read it. While the menu runs, what a crash would print goes to a file of
// its own, crash-<pid>.tmp; one that's more than its first line is a crash,
// and is kept as crash.log. Only the latest is kept.

const crashHeader = "version: "

func crashPath() string     { return filepath.Join(config.StateDir(), "crash.log") }
func crashSeenPath() string { return filepath.Join(config.StateDir(), "crash-seen") }

// catchCrashes sends crashes to this process's file until stop is called
// with what the menu ended with. Bubble Tea catches panics and prints them to
// os.Stderr; the rest (other goroutines, fatal errors) the runtime prints to
// its crash output.
func catchCrashes() (stop func(error)) {
	_ = os.MkdirAll(config.StateDir(), 0o755)
	f, err := os.OpenFile(filepath.Join(config.StateDir(), fmt.Sprintf("crash-%d.tmp", os.Getpid())), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return func(error) {}
	}
	_, _ = f.WriteString(crashHeader + getVersion() + "\n")
	old := os.Stderr
	os.Stderr = f
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	return func(err error) {
		if errors.Is(err, tea.ErrProgramPanic) {
			time.Sleep(100 * time.Millisecond) // a panic off the main loop is still being written
		}
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		os.Stderr = old
		_ = f.Close()
		collectCrashes()
	}
}

var crashTmp = regexp.MustCompile(`^crash-(\d+)\.tmp$`)

// collectCrashes keeps any crash its process didn't live to keep, and tidies
// up after the rest. Another session's running menu is left alone.
func collectCrashes() {
	entries, _ := os.ReadDir(config.StateDir())
	for _, e := range entries {
		m := crashTmp.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		pid, _ := strconv.Atoi(m[1])
		if pid != os.Getpid() && syscall.Kill(pid, 0) == nil {
			continue
		}
		path := filepath.Join(config.StateDir(), e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if head, rest, _ := strings.Cut(string(b), "\n"); strings.TrimSpace(rest) != "" {
			when := time.Now()
			if fi, err := e.Info(); err == nil {
				when = fi.ModTime()
			}
			saveCrash(when, strings.TrimPrefix(head, crashHeader), rest)
		}
		_ = os.Remove(path)
	}
}

// saveCrash writes crash.log: when, which version, then what it printed.
func saveCrash(when time.Time, version, text string) {
	reason, trace := parseCrash(text)
	log := fmt.Sprintf("time: %s\n%s%s\n\n%s\n\n%s\n", when.Format(time.RFC3339), crashHeader, version, reason, trace)
	if os.WriteFile(crashPath(), []byte(log), 0o600) == nil {
		_ = os.Remove(crashSeenPath())
	}
}

// parseCrash splits what a crash printed into why and the trace, whether
// Bubble Tea printed it or the runtime did.
func parseCrash(text string) (reason, trace string) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if after, ok := strings.CutPrefix(text, "Caught panic:\n\n"); ok {
		reason, trace, _ = strings.Cut(after, "\n\nRestoring terminal...\n\n")
		return reason, strings.TrimSpace(trace)
	}
	reason, trace, _ = strings.Cut(text, "\n\n")
	return strings.TrimPrefix(reason, "panic: "), strings.TrimSpace(trace)
}

// lastCrash is crash.log, read back.
type lastCrash struct {
	when                  time.Time
	version, reason, text string // text is the file as it is
	trace                 string
}

func readCrash() (lastCrash, bool) {
	b, err := os.ReadFile(crashPath())
	if err != nil {
		return lastCrash{}, false
	}
	c := lastCrash{text: string(b)}
	head, body, _ := strings.Cut(string(b), "\n\n")
	for _, l := range strings.Split(head, "\n") {
		if v, ok := strings.CutPrefix(l, "time: "); ok {
			c.when, _ = time.Parse(time.RFC3339, v)
		}
		if v, ok := strings.CutPrefix(l, crashHeader); ok {
			c.version = v
		}
	}
	c.reason, c.trace, _ = strings.Cut(body, "\n\n")
	c.trace = strings.TrimSpace(c.trace)
	return c, true
}

// crashNews is the welcome's and the status bar's row for a crash nobody has
// looked at yet.
func crashNews() (row, bool) {
	c, found := readCrash()
	if !found {
		return row{}, false
	}
	if _, err := os.Stat(crashSeenPath()); err == nil {
		return row{}, false
	}
	return row{mark: fail, glyph: "✗", label: "Crash", detail: "clingon crashed " + ago(c.when), note: "Clingon › General › Show last crash"}, true
}

func ago(t time.Time) string {
	switch d := time.Since(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	default:
		return plural(int(d.Hours()/24), "day") + " ago"
	}
}

// crashCmd shows the last crash as a card, or the log as it is when it isn't
// printing to a terminal, for pasting into a bug report.
func crashCmd() error {
	c, found := readCrash()
	if !found {
		if isTerm() {
			outln(card(failStyle.Render("Crash"), []row{{mark: ok, label: "No crashes", detail: "clingon hasn't crashed"}}, cardWidth()))
		}
		return nil
	}
	_ = os.WriteFile(crashSeenPath(), nil, 0o644)
	if !isTerm() {
		fmt.Print(c.text)
		return nil
	}
	outln(crashCard(c, cardWidth()))
	return nil
}

var crashFrame = regexp.MustCompile(`([^\s/]+(?:/[^\s/]+)*\.go:\d+)`)

// crashWhere is the deepest line of clingon's own the trace goes through.
func crashWhere(trace string) string {
	lines := strings.Split(trace, "\n")
	for i := 0; i+1 < len(lines); i++ {
		if !strings.Contains(lines[i], "con-dog/clingon/") {
			continue
		}
		if m := crashFrame.FindString(lines[i+1]); m != "" {
			if _, rest, ok := strings.Cut(m, "/clingon/"); ok {
				return rest
			}
			return m
		}
	}
	return ""
}

// crashFrames is how many lines of trace the card shows, so its top stays on
// screen: the log has the rest.
const crashFrames = 12

// frame is a line of the card's trace: which function, and where.
type frame struct {
	fn, at string
	own    bool // clingon's, not a library's or the runtime's
}

// frames reads a trace, starting where the panic happened: what's above that
// is only the panic being caught and printed.
func frames(trace string) []frame {
	var out []frame
	lines := strings.Split(trace, "\n")
	for i := 0; i+1 < len(lines); i++ {
		at := crashFrame.FindString(lines[i+1])
		if at == "" || !strings.HasPrefix(lines[i+1], "\t") {
			continue
		}
		fn := lines[i]
		if j := strings.LastIndex(fn, "("); j > 0 && strings.HasSuffix(fn, ")") {
			fn = fn[:j]
		}
		if fn == "panic" {
			out = nil
			continue
		}
		own := strings.Contains(fn, "con-dog/clingon/") || strings.HasPrefix(fn, "main.")
		if strings.HasPrefix(fn, "runtime.") && len(out) == 0 {
			continue
		}
		fn = fn[strings.LastIndex(fn, "/")+1:]
		parts := strings.Split(at, "/")
		out = append(out, frame{fn, strings.Join(parts[max(len(parts)-2, 0):], "/"), own})
	}
	return out
}

// crashCard is a card titled Crash, in red: what happened and where, one line
// to a function, with clingon's own picked out.
func crashCard(c lastCrash, w int) string {
	rows := []row{{mark: fail, label: "Crashed", detail: c.when.Local().Format("2 Jan 2006 15:04") + " (" + ago(c.when) + ")"}}
	if c.version != "" {
		rows = append(rows, row{mark: info, label: "Version", detail: c.version})
	}
	if where := crashWhere(c.trace); where != "" {
		rows = append(rows, row{mark: info, label: "Where", detail: where})
	}
	rows = append(rows, row{mark: info, label: "Log", detail: crashPath()})
	lines := strings.Split(card(failStyle.Render("Crash"), rows, w), "\n")

	inner := max(w-4, 1)
	add := func(line string, st lipgloss.Style) {
		for _, l := range strings.Split(ansi.Hardwrap(line, inner, true), "\n") {
			lines = append(lines, cardIndent+st.Render(l))
		}
	}
	lines = append(lines, faintStyle.Render(strings.Repeat("─", max(w, 0))))
	for _, l := range strings.Split(c.reason, "\n") {
		add(l, failStyle)
	}
	fs := frames(c.trace)
	if len(fs) > 0 {
		add("", faintStyle)
	}
	fnW := 0
	for _, f := range fs[:min(len(fs), crashFrames)] {
		fnW = max(fnW, ansi.StringWidth(f.fn))
	}
	fnW = min(fnW, inner*2/3)
	for _, f := range fs[:min(len(fs), crashFrames)] {
		st := faintStyle
		if f.own {
			st = greyStyle
		}
		add(ansi.Truncate(fit(f.fn, fnW)+"  "+f.at, inner, "…"), st)
	}
	if n := len(fs) - crashFrames; n > 0 {
		add("… and "+plural(n, "more line")+" in the log", faintStyle)
	}
	return strings.Join(lines, "\n")
}
