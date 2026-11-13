package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/con-dog/clingon/internal/palette"
)

// term is the outer tmux session clingon runs in: the harness reads its
// screen and types into it the way a person at the keyboard would.
type term struct {
	server  string // tmux -L name
	session string
	env     []string

	ctlIn   io.Writer // the harness's own tmux client, in control mode
	ctlDone chan error

	// The control client hears every change to the screen (%output): when
	// the last one came, and a nudge for each.
	lastOut atomic.Int64 // unix nanoseconds
	changed chan struct{}
}

func (t *term) tmux(args ...string) (string, error) {
	cmd := exec.Command("tmux", append([]string{"-L", t.server, "-f", "/dev/null"}, args...)...)
	cmd.Env = t.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// start opens clingon at w×h, and keeps it that size whoever watches, so
// every run's model sees the same screen.
func (t *term) start(dir, command string, w, h int) error {
	_, _ = t.tmux("kill-server") // a previous run's session, never anyone else's: the server name is ours
	if _, err := t.tmux("new-session", "-d", "-s", t.session, "-x", strconv.Itoa(w), "-y", strconv.Itoa(h+1), "-c", dir, command); err != nil {
		return err
	}
	for _, kv := range [][]string{
		{"window-size", "manual"},
		{"remain-on-exit", "on"},
		{"status", "on"},
		{"status-position", "bottom"},
		{"status-style", "bg=" + palette.Ink.Tmux() + ",fg=" + palette.Text.Tmux()},
		{"status-left-length", "300"},
		{"status-right", ""},
		{"status-left", " LLM  starting…"},
		{"escape-time", "10"},
	} {
		if _, err := t.tmux("set", "-g", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return t.attachControl()
}

// attachControl attaches the harness's own client, in control mode, to
// type with. send-keys aims at a client: run from the command line, that's
// the newest one, a watcher's read-only client, which tmux refuses to type
// as. From the harness's own client, keys always go. It doesn't size the
// window. It hears the screen's output, which is how settle knows when the
// screen has stopped changing.
func (t *term) attachControl() error {
	cmd := exec.Command("tmux", "-L", t.server, "-f", "/dev/null", "-C", "attach-session", "-f", "ignore-size", "-t", t.session)
	cmd.Env = t.env
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	t.ctlIn, t.ctlDone, t.changed = in, make(chan error, 1), make(chan struct{}, 1)
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		var msg []string
		inBlock := false
		for sc.Scan() {
			l := sc.Text()
			switch {
			case strings.HasPrefix(l, "%output "):
				t.lastOut.Store(time.Now().UnixNano())
				select {
				case t.changed <- struct{}{}:
				default:
				}
			case strings.HasPrefix(l, "%begin"):
				inBlock, msg = true, nil
			case strings.HasPrefix(l, "%end"):
				inBlock = false
				t.ctlDone <- nil
			case strings.HasPrefix(l, "%error"):
				inBlock = false
				t.ctlDone <- errors.New(strings.Join(msg, " "))
			case inBlock:
				msg = append(msg, l)
			}
		}
		t.ctlDone <- errors.New("tmux control client closed")
	}()
	return t.ctlWait() // the attach's own reply
}

func (t *term) ctlWait() error {
	select {
	case err := <-t.ctlDone:
		return err
	case <-time.After(5 * time.Second):
		return errors.New("tmux control client: no reply")
	}
}

// control runs one tmux command as the harness's own client.
func (t *term) control(line string) error {
	if _, err := io.WriteString(t.ctlIn, line+"\n"); err != nil {
		return err
	}
	if err := t.ctlWait(); err != nil {
		return fmt.Errorf("tmux %s: %w", strings.Fields(line)[0], err)
	}
	return nil
}

// narrate puts what the model is doing on the status line, under clingon,
// with its detail (the keys, say) greyed.
func (t *term) narrate(s, detail string) {
	esc := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "#", "##") }
	line := " #[bold]LLM#[nobold]  " + esc(s)
	if detail != "" {
		line += "  #[fg=" + palette.Grey.Tmux() + "]" + esc(detail)
	}
	_, _ = t.tmux("set", "-g", "status-left", line)
}

// viewers counts the people watching: attached clients other than the
// harness's own.
func (t *term) viewers() int {
	out, err := t.tmux("list-clients", "-F", "#{client_control_mode}")
	if err != nil {
		return 0
	}
	return strings.Count(out, "0\n")
}

// sendKey presses a named key (Enter, Up, …) or types one character.
// Characters go as hex bytes, which need no quoting.
func (t *term) sendKey(k string) error {
	if len([]rune(k)) == 1 {
		return t.sendText(k)
	}
	return t.control("send-keys -t " + t.session + " " + k)
}

func (t *term) sendText(s string) error {
	if s == "" {
		return nil
	}
	hex := make([]string, 0, len(s))
	for _, b := range []byte(s) {
		hex = append(hex, fmt.Sprintf("%02x", b))
	}
	return t.control("send-keys -t " + t.session + " -H " + strings.Join(hex, " "))
}

// openViewer opens a Terminal window that watches, read-only, in clingon's
// own Terminal profile when there is one (from running clingon), for its
// font and colours. It only picks that profile for the window: no profile
// is ever added or changed. It says which profile it used.
func openViewer(attach string, cols, rows int) (string, error) {
	out, err := exec.Command("osascript", "-l", "JavaScript", "-e", viewerScript, attach, strconv.Itoa(cols), strconv.Itoa(rows)).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

const viewerScript = `function run(argv) {
	const T = Application("Terminal");
	const name = T.settingsSets.name().filter(n => n.startsWith("clingon")).pop() || "";
	const tab = T.doScript(" " + argv[0]);
	if (name) tab.currentSettings = T.settingsSets.byName(name);
	try { tab.numberOfColumns = +argv[1]; tab.numberOfRows = +argv[2]; } catch (e) {} // the screen's own size: it doesn't follow the window
	T.activate();
	return name;
}`

// raw is the screen with its colours, as tmux has it.
func (t *term) raw() (string, error) {
	return t.tmux("capture-pane", "-p", "-e", "-t", t.session)
}

func (t *term) dead() bool {
	out, _ := t.tmux("display", "-p", "-t", t.session, "#{pane_dead}")
	return strings.TrimSpace(out) == "1"
}

// settle waits until the screen has had no output for `quiet` (or until
// `within` passes), then returns it as text. It goes by tmux's own word that
// the screen changed, not by comparing captures.
func (t *term) settle(quiet, within time.Duration) (string, error) {
	start := time.Now()
	deadline := start.Add(within)
	for {
		last := time.Unix(0, t.lastOut.Load())
		if last.Before(start) {
			last = start
		}
		wake := last.Add(quiet)
		if wake.After(deadline) {
			wake = deadline
		}
		if !time.Now().Before(wake) {
			break
		}
		select {
		case <-t.changed:
		case <-time.After(time.Until(wake)):
		}
	}
	raw, err := t.raw()
	if err != nil {
		return "", err
	}
	return render(raw), nil
}

// The menu marks its cursor's row with one coloured cell rather than a
// character: pink while the menu has focus, grey while the shell has it.
var (
	cursorBg  = fmt.Sprintf("5;%d", palette.Pink.Num)
	blurredBg = fmt.Sprintf("5;%d", palette.Grey.Num)
)

// render turns tmux's coloured capture into plain text a model can read:
// the cursor's cell becomes ▶ (menu focused) or ▷ (shell focused), and
// glyphs from clingon's own font (pack logos) become ◆.
func render(raw string) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		type cell struct {
			r  rune
			bg string
		}
		var cells []cell
		bg := ""
		rs := []rune(line)
		for i := 0; i < len(rs); i++ {
			r := rs[i]
			if r == 0x1b && i+1 < len(rs) && rs[i+1] == '[' {
				j := i + 2
				for j < len(rs) && (rs[j] < 0x40 || rs[j] > 0x7e) {
					j++
				}
				if j < len(rs) && rs[j] == 'm' {
					bg = sgrBg(string(rs[i+2:j]), bg)
				}
				i = j
				continue
			}
			cells = append(cells, cell{r, bg})
		}
		var b strings.Builder
		for i, c := range cells {
			// The cursor is one coloured cell on its own: a run of them is
			// something else, like a key on the status bar.
			alone := (i == 0 || cells[i-1].bg != c.bg) && (i == len(cells)-1 || cells[i+1].bg != c.bg)
			switch r := c.r; {
			case r == ' ' && alone && c.bg == cursorBg:
				b.WriteRune('▶')
			case r == ' ' && alone && c.bg == blurredBg:
				b.WriteRune('▷')
			case r >= 0xE000 && r <= 0xF8FF || r >= 0xF0000:
				b.WriteRune('◆')
			default:
				b.WriteRune(r)
			}
		}
		out.WriteString(strings.TrimRight(b.String(), " "))
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

// sgrBg follows the background colour through one SGR sequence.
func sgrBg(params, bg string) string {
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		switch p := ps[i]; {
		case p == "" || p == "0" || p == "49":
			bg = ""
		case p == "38" || p == "58": // foreground / underline colour: skip its arguments
			if i+1 < len(ps) && ps[i+1] == "5" {
				i += 2
			} else if i+1 < len(ps) && ps[i+1] == "2" {
				i += 4
			}
		case p == "48":
			if i+2 < len(ps) && ps[i+1] == "5" {
				bg = "5;" + ps[i+2]
				i += 2
			} else if i+4 < len(ps) && ps[i+1] == "2" {
				bg = "2;" + strings.Join(ps[i+2:i+5], ";")
				i += 4
			}
		case len(p) == 2 && p[0] == '4' && p[1] >= '0' && p[1] <= '7':
			bg = "5;" + p
		case len(p) == 3 && p[:2] == "10":
			bg = "5;" + p
		}
	}
	return bg
}
