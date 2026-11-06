// Package tmux runs clingon's private tmux server: one server per launch,
// on its own socket and config, so it never touches the user's tmux.
package tmux

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

//go:embed tmux.conf
var conf []byte

// Client talks to one clingon tmux server.
type Client struct {
	bin    string
	Socket string
	env    []string

	mu      sync.Mutex
	started bool      // the shell has drawn its first prompt, or had its chance to
	path    string    // the shell's folder, as State last saw it, under mu
	pathAt  time.Time // when

	barMu   sync.Mutex // one draw of the bar at a time
	bar     string     // the bar's newest line, under mu
	drawn   string     // the line last drawn, under barMu
	barPane string     // the bar's pane, once known, under barMu
}

func New(socket string) (*Client, error) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return nil, errors.New("clingon needs tmux: install it with `brew install tmux`")
	}
	return &Client{bin: bin, Socket: socket, env: cleanEnv()}, nil
}

// cleanEnv drops TMUX so clingon also starts from inside another tmux.
func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") {
			env = append(env, kv)
		}
	}
	return env
}

// setEnv sets KEY=value in the environment tmux commands run with.
func (c *Client) setEnv(kv string) {
	key := kv[:strings.Index(kv, "=")+1]
	for i, old := range c.env {
		if strings.HasPrefix(old, key) {
			c.env[i] = kv
			return
		}
	}
	c.env = append(c.env, kv)
}

func (c *Client) run(args ...string) (string, error) { return c.runInput(nil, args...) }

// runInput is run with stdin, for `source-file -`.
func (c *Client) runInput(stdin []byte, args ...string) (string, error) {
	cmd := exec.Command(c.bin, append([]string{"-S", c.Socket}, args...)...)
	cmd.Env = c.env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("tmux %s: %s", args[0], msg)
		}
		return "", fmt.Errorf("tmux %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Launch builds the session — menu on the left third, the user's shell on
// the right — and attaches to it until it ends.
//
// tmux only draws borders between panes, so both are ringed by empty
// one-cell spacer panes; that gives each a full frame, and the active
// pane's frame is drawn in the highlight colour.
//
// env is extra environment for both panes (e.g. a PATH that finds this
// binary as `clingon` when it's a local build). The session's socket goes
// in runDir; the tmux config is passed straight to tmux, never written.
// ready runs while the session is built; if it fails, nothing is attached.
func Launch(self string, env []string, runDir string, ready func() error) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("clingon needs a terminal to run in (for a command without one, try `clingon print`)")
	}
	if len(filepath.Join(runDir, "clingon-0000000")) > 100 { // unix socket paths max out around 104 bytes
		var err error
		if runDir, err = privateTempDir(); err != nil {
			return err
		}
	} else if err := os.MkdirAll(runDir, 0o700); err != nil {
		return err
	}
	sock := filepath.Join(runDir, "clingon-"+strconv.Itoa(os.Getpid()))
	c, err := New(sock)
	if err != nil {
		return err
	}
	// Closing the terminal window sends SIGHUP: catch it so this process
	// outlives tmux long enough to clean up. (Caught, not ignored, so the
	// tmux client still gets the default and exits.) Caught, either signal
	// ends the session, so attach returns rather than waiting on it forever.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(sigs)
	defer func() {
		for i := 0; i < 40 && listening(sock); i++ {
			time.Sleep(50 * time.Millisecond) // the server is shutting down
		}
		if !listening(sock) {
			_ = os.Remove(sock) // tmux leaves its socket behind
		}
	}()
	paneEnv := []string{"-e", "CLINGON=1"}
	for _, kv := range env {
		paneEnv = append(paneEnv, "-e", kv)
		c.setEnv(kv) // tmux gives new panes the creating client's PATH, so set it there too
	}
	go func() {
		<-sigs
		_, _ = c.run("kill-server")
	}()
	sweep(runDir)
	cwd, _ := os.Getwd()
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		w, h = 160, 48
	}
	args := []string{"-f", "/dev/null", "start-server", ";", "source-file", "-", ";",
		"new-session", "-d", "-s", "clingon",
		"-x", strconv.Itoa(w), "-y", strconv.Itoa(h), "-c", cwd, "-P", "-F", "#{pane_id}"}
	args = append(args, "--", "cat") // holds the pane: layout starts the shell in it
	cfg := conf
	if ct := os.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		cfg = append([]byte("set -as terminal-features ',*:RGB'\n"), conf...)
	}
	shell, err := c.runInput(cfg, args...)
	if err != nil {
		return err
	}
	if err := c.layout(self, shell, cwd, paneEnv); err != nil {
		_, _ = c.run("kill-server")
		return err
	}
	if err := ready(); err != nil {
		_, _ = c.run("kill-server")
		return err
	}
	err = c.attach()
	_, _ = c.run("kill-server") // usually gone already; never left running unseen
	return err
}

// attach runs `tmux attach` and then hands the terminal back clean: the
// settings it had before, blocking reads, and no leftover input. Keys typed
// as the session closes, or a moment after (a double-tapped Ctrl-D, say),
// are dropped: in the user's own shell a stray Ctrl-D logs them out.
func (c *Client) attach() error {
	fd := int(os.Stdin.Fd())
	saved, _ := term.GetState(fd)
	cmd := exec.Command(c.bin, "-S", c.Socket, "attach-session", "-t", "clingon")
	cmd.Env = c.env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if saved != nil {
		_ = term.Restore(fd, saved)
	}
	_ = syscall.SetNonblock(fd, false)
	time.Sleep(300 * time.Millisecond)
	_ = flushInput(fd)
	if _, ok := err.(*exec.ExitError); ok {
		return nil // tmux has already said why, e.g. "[server exited]"
	}
	return err
}

func (c *Client) layout(self, shell, cwd string, paneEnv []string) error {
	// Every split in one tmux call (each costs a few ms): the spacers, then
	// the menu, each printing its new pane's id on a line of its own.
	var splits []string
	split := func(args ...string) {
		splits = append(append(append(splits, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", shell), args...), ";")
	}
	for _, dir := range [][]string{{"-v", "-b"}, {"-v"}, {"-h", "-b"}, {"-h"}} { // top, bottom, left, right
		split(append(dir, "-l", "1", "")...)
	}
	split(append(append([]string{"-h", "-b", "-l", "33%", "-c", cwd}, paneEnv...), "--", self, "menu", "--socket", c.Socket, "--pane", shell)...)
	out, err := c.run(splits[:len(splits)-1]...)
	if err != nil {
		return err
	}
	ids := strings.Fields(out)
	if len(ids) != 5 {
		return fmt.Errorf("tmux split-window: want 5 panes, got %q", out)
	}
	top, bottom, left, right, menu := ids[0], ids[1], ids[2], ids[3], ids[4]
	spacers := ids[:4]

	// tmux spreads a window resize unevenly, so this runs now and after each.
	fix := [][]string{
		{"resize-pane", "-t", top, "-y", "1"}, {"resize-pane", "-t", bottom, "-y", "1"},
		{"resize-pane", "-t", left, "-x", "1"}, {"resize-pane", "-t", right, "-x", "1"},
		{"resize-pane", "-t", menu, "-x", "33%"},
	}
	var hook []string
	var args []string
	for _, f := range fix {
		hook = append(hook, strings.Join(f, " "))
		args = append(append(args, f...), ";")
	}
	for _, s := range spacers {
		args = append(args, "set", "-p", "-t", s, "@spacer", "1", ";")
	}
	// The bottom spacer spans the window under both panes: the menu draws
	// its status bar there.
	args = append(args, "set", "-g", "@clingon-bar", bottom, ";")
	isShellPrompt := "#{&&:#{==:#{pane_id}," + shell + "},#{&&:#{m:*sh,#{pane_current_command}},#{!=:#{pane_current_command},ssh}}}"
	// Selecting with the mouse copies to the system clipboard (tmux's own
	// mouse handling would otherwise keep it in a tmux-only buffer).
	args = append(args, "set", "-g", "copy-command", "pbcopy", ";")
	args = append(args,
		"set-hook", "-g", "window-resized", strings.Join(hook, " ; "), ";",
		// Tab in the menu hops right to the shell; in the shell it is
		// completion as usual.
		"bind", "-n", "Tab", "if", "-F", "#{==:#{pane_id},"+menu+"}", "select-pane -t "+shell, "send-keys Tab", ";",
		// Shift-Tab at the shell prompt hops left to the menu. Inside vim,
		// ssh or any other program it passes through.
		"bind", "-n", "BTab", "if", "-F", isShellPrompt, "select-pane -t "+menu, "send-keys BTab", ";",
		"select-pane", "-t", menu, ";",
	)
	// Only now that the pane is its final width: the welcome card measures
	// it, and a zsh started wider leaves its end-of-line % on the screen.
	args = append(append(append(args, "respawn-pane", "-k", "-t", shell, "-c", cwd), paneEnv...), "--", self, "welcome", "--then-shell")
	_, err = c.run(args...)
	return err
}

// privateTempDir is a folder in the temp dir for the socket when the usual
// one's path is too long: only ours, so no one else can plant a socket.
func privateTempDir() (string, error) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("clingon-%d", os.Getuid()))
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() || fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s isn't a private folder of yours: remove it and try again", dir)
	}
	return dir, nil
}

// sweep removes sockets left by finished launches: the launching pid (in
// the name) is gone and no server answers on the socket. A socket whose
// server still runs is never removed, or that server could never be
// reached again.
func sweep(runDir string) {
	socks, _ := filepath.Glob(filepath.Join(runDir, "clingon-*"))
	for _, s := range socks {
		pid, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(s), "clingon-"))
		if err == nil && syscall.Kill(pid, 0) == syscall.ESRCH && !listening(s) {
			_ = os.Remove(s)
		}
	}
}

// listening reports whether a server accepts connections on unix socket path.
func listening(path string) bool {
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return true // can't tell: leave it be
	}
	defer func() { _ = syscall.Close(fd) }()
	return syscall.Connect(fd, &syscall.SockaddrUnix{Name: path}) == nil
}

// escape stops tmux treating a trailing ';' in an argument as a command
// separator (it would eat the `\;` in `find -exec ... \;`).
func escape(s string) string {
	if strings.HasSuffix(s, ";") {
		return s[:len(s)-1] + `\;`
	}
	return s
}

// Send types text into pane as if the user had and presses Enter, leaving focus
// where it is. Text with several lines, tabs or other control characters,
// or too long for one tmux message, is bracketed-pasted instead: the shell
// takes it as it is, rather than running it line by line or completing at
// each tab.
func (c *Client) Send(pane, text string) error {
	// Focus stays where it is: only the user moves it into the shell (tab), so a key
	// meant for the menu can never end up typed into the shell by surprise.
	then := []string{";", "send-keys", "-t", pane, "Enter"}
	if len(text) > 4000 || strings.ContainsFunc(text, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		_, err := c.runInput([]byte(text), append([]string{"load-buffer", "-b", "clingon", "-", ";",
			"paste-buffer", "-p", "-d", "-b", "clingon", "-t", pane}, then...)...)
		return err
	}
	_, err := c.run(append([]string{"send-keys", "-t", pane, "-l", "--", escape(text)}, then...)...)
	return err
}

// Path is the pane's current directory, so `when:` follows the user's cd.
// State reads it too: just after that, it's taken from there instead of
// asking tmux again.
func (c *Client) Path(pane string) string {
	c.mu.Lock()
	p, fresh := c.path, time.Since(c.pathAt) < 250*time.Millisecond
	c.mu.Unlock()
	if fresh && p != "" {
		return p
	}
	p, err := c.run("display-message", "-p", "-t", pane, "#{pane_current_path}")
	if err != nil || p == "" {
		p, _ = os.Getwd()
	}
	return p
}

func (c *Client) Kill() { _, _ = c.run("kill-server") }

// WaitIdle blocks until the shell in pane is back at its prompt, i.e. the
// command just sent to it has finished. It sleeps on the kernel's process
// exit event instead of polling. It returns false if that can't be known.
func (c *Client) WaitIdle(pane string) bool {
	out, err := c.run("display-message", "-p", "-t", pane, "#{pane_pid}")
	if err != nil {
		return false
	}
	shell, err := strconv.Atoi(out)
	if err != nil {
		return false
	}
	time.Sleep(200 * time.Millisecond) // give the shell time to start the command
	for {
		fg, err := foregroundGroup(shell)
		if err != nil || fg <= 0 {
			return false
		}
		if fg == shell {
			// Between the parts of `a; b` the shell has the terminal for a
			// moment: only count it as done if it still has it a little later.
			time.Sleep(60 * time.Millisecond)
			if fg, err := foregroundGroup(shell); err == nil && fg != shell {
				continue
			}
			// It has the terminal before it has drawn its prompt (precmd
			// hooks run first), and that blank line says nothing yet.
			if c.waitPromptDrawn(pane, shell) {
				return true
			}
			continue
		}
		// Wait on any live process in the foreground group: in a pipeline
		// the group's leader can exit before the rest.
		pid, err := groupMember(fg)
		if err == nil {
			err = waitExit(pid)
		}
		if err != nil {
			time.Sleep(100 * time.Millisecond) // the group is just ending; look again
		}
	}
}

// promptGrace is how long a shell with the terminal back gets to draw its
// prompt. One that never does is waiting for the rest of a command.
const promptGrace = 2 * time.Second

// waitPromptDrawn watches pane for up to promptGrace, until its prompt shows.
// It returns false if the shell gives the terminal away again meanwhile.
func (c *Client) waitPromptDrawn(pane string, shell int) bool {
	for end := time.Now().Add(promptGrace); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if left, _, ok := c.cursorLine(pane); !ok || strings.Contains(left, promptMark) {
			return true
		}
		if fg, err := foregroundGroup(shell); err == nil && fg != shell {
			return false
		}
	}
	return true
}

// State is what the shell in a pane is doing, as far as whether the menu
// can type a command into it.
type State struct {
	Busy      string // the program running in it, or "" at the prompt
	Scrolling bool   // scrolled back (tmux copy mode): keys would go to that
	Typed     string // what's typed at the prompt but not run yet
	Starting  bool   // Busy is the shell's rc file at work: no first prompt yet
}

// State looks at pane: what's running, whether it's scrolled back, and
// what's typed after the prompt.
func (c *Client) State(pane string) State {
	left, f, ok := c.cursorLine(pane)
	if !ok {
		return State{}
	}
	var st State
	if shell, err := strconv.Atoi(f[0]); err == nil {
		if fg, err := foregroundGroup(shell); err == nil && fg > 0 && fg != shell {
			st.Busy = f[1]
			c.mu.Lock()
			st.Starting = !c.started
			c.mu.Unlock()
			return st
		}
	}
	st.Scrolling = f[2] == "1"
	st.Typed = c.typedOn(left)
	return st
}

// Jobs names what else is on the pane's terminal besides its shell: jobs in
// the background and stopped ones, which end with the session.
func (c *Client) Jobs(pane string) []string {
	out, err := c.run("display-message", "-p", "-t", pane, "#{pane_pid}|#{pane_tty}")
	shell, tty, ok := strings.Cut(out, "|")
	if err != nil || !ok {
		return nil
	}
	ps, err := exec.Command("ps", "-t", strings.TrimPrefix(tty, "/dev/"), "-o", "pid=,stat=,comm=").Output()
	if err != nil {
		return nil
	}
	return jobNames(string(ps), shell)
}

// jobNames reads `ps -o pid=,stat=,comm=`: each program once, without the
// shell itself and without zombies, which have ended already.
func jobNames(ps, shell string) []string {
	var names []string
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] == shell || strings.HasPrefix(f[1], "Z") {
			continue
		}
		if name := filepath.Base(strings.Join(f[2:], " ")); !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// cursorLine reads, in one tmux call, the pane's pid, current command,
// whether it's in copy mode, and its cursor line up to the cursor.
func (c *Client) cursorLine(pane string) (left string, fields []string, ok bool) {
	// tmux turns tabs in the output into "_", so fields are split on "|".
	// The folder comes last, so a | in it can't shift the other fields.
	out, err := c.run("display-message", "-p", "-t", pane, "#{pane_pid}|#{pane_current_command}|#{pane_in_mode}|#{cursor_x}|#{cursor_y}|#{pane_current_path}", ";",
		"capture-pane", "-p", "-t", pane)
	if err != nil {
		return "", nil, false
	}
	head, screen, _ := strings.Cut(out, "\n")
	f := strings.SplitN(head, "|", 6)
	if len(f) != 6 {
		return "", nil, false
	}
	c.mu.Lock()
	c.path, c.pathAt = f[5], time.Now()
	c.mu.Unlock()
	x, _ := strconv.Atoi(f[3])
	y, _ := strconv.Atoi(f[4])
	if lines := strings.Split(screen, "\n"); y < len(lines) {
		left = ansi.Truncate(lines[y], x, "")
	}
	return left, f, true
}

// promptMark ends clingon's prompt, in each of the shells it runs in.
const promptMark = "❯"

// WaitForPrompt watches pane until the shell's first prompt shows, or within
// has passed. Until then the shell is starting up, and its cursor line says
// nothing about what it's doing.
func (c *Client) WaitForPrompt(pane string, within time.Duration) {
	for end := time.Now().Add(within); time.Now().Before(end); time.Sleep(40 * time.Millisecond) {
		if left, _, ok := c.cursorLine(pane); !ok || strings.Contains(left, promptMark) {
			break
		}
	}
	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
}

// typedOn is what's typed after the prompt on a cursor line (up to the
// cursor). A right-hand prompt or a grey autosuggestion is past the cursor,
// so it doesn't count. No prompt on the line means a continuation prompt
// ("dquote> "), a typed line wrapped onto a new row, or a program (even read)
// waiting for input.
func (c *Client) typedOn(left string) string {
	if _, rest, ok := strings.Cut(left, promptMark); ok {
		return strings.TrimSpace(rest)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started {
		return ""
	}
	return "an unfinished command"
}

// failMark is how the prompt's mark starts after a failed command: red, 204
// in clingon's palette (see the prompts in internal/shell).
const failMark = "38;5;204"

// LastFailed says whether the command that just finished failed, going by the
// colour of the prompt at the cursor. The shell says so nowhere else that
// wouldn't mean changing what's sent to it, and so what's in its history.
// known is false if there's no prompt there to read.
func (c *Client) LastFailed(pane string) (failed, known bool) {
	out, err := c.run("display-message", "-p", "-t", pane, "#{cursor_y}", ";", "capture-pane", "-p", "-e", "-t", pane)
	if err != nil {
		return false, false
	}
	head, screen, _ := strings.Cut(out, "\n")
	y, err := strconv.Atoi(head)
	lines := strings.Split(screen, "\n")
	if err != nil || y < 0 || y >= len(lines) {
		return false, false
	}
	before, _, ok := strings.Cut(lines[y], promptMark)
	if !ok {
		return false, false
	}
	return strings.Contains(before, failMark), true
}

// Clear throws away what's typed at the shell's prompt (Ctrl-C, which also
// ends an unfinished multi-line command) and waits for the fresh prompt:
// keys sent straight after Ctrl-C can be dropped with the line it cancels.
func (c *Client) Clear(pane string) error {
	if _, err := c.run("send-keys", "-t", pane, "C-c"); err != nil {
		return err
	}
	for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if st := c.State(pane); st.Typed == "" && st.Busy == "" {
			time.Sleep(30 * time.Millisecond) // let the shell finish drawing it
			return nil
		}
	}
	return errors.New("the shell didn't come back to an empty prompt: have a look at it")
}

// Bar draws line, already styled and as wide as the bar, in the bar: the
// empty pane under clingon's frame, which takes output from tmux itself.
// Calls may overlap: whichever draws last draws the newest line.
func (c *Client) Bar(line string) {
	c.mu.Lock()
	c.bar = line
	c.mu.Unlock()
	c.barMu.Lock()
	defer c.barMu.Unlock()
	c.mu.Lock()
	line = c.bar
	c.mu.Unlock()
	pane := c.findBar()
	if pane == "" || line == c.drawn {
		return
	}
	// No wrapping (a line a cell too wide would scroll the bar away), back
	// to the start, clear, draw.
	if _, err := c.runInput([]byte("\x1b[?7l\x1b[H\x1b[2K"+line), "display-message", "-I", "-t", pane); err == nil {
		c.drawn = line
	}
}

// BarWidth is the bar's width in cells, 0 if there's no bar. The bar spans
// the window, so it's the window's width: asked for with the bar's pane,
// in one call.
func (c *Client) BarWidth() int {
	out, err := c.run("display-message", "-p", "#{@clingon-bar}|#{window_width}")
	pane, width, _ := strings.Cut(out, "|")
	if err != nil || pane == "" {
		return 0
	}
	c.barMu.Lock()
	c.barPane = pane
	c.barMu.Unlock()
	w, _ := strconv.Atoi(width)
	return w
}

// findBar is the bar's pane id, looked up once it's been set. Under barMu.
func (c *Client) findBar() string {
	if c.barPane == "" {
		c.barPane, _ = c.run("show-options", "-gv", "@clingon-bar")
	}
	return c.barPane
}

// Focus moves focus to pane.
func (c *Client) Focus(pane string) { _, _ = c.run("select-pane", "-t", pane) }

// FocusMenu moves focus from clingon's shell to its menu, beside it on the
// left: for a program running in the shell (the pack form). It does nothing
// outside clingon's tmux.
func FocusMenu() {
	socket, _, _ := strings.Cut(os.Getenv("TMUX"), ",")
	pane := os.Getenv("TMUX_PANE")
	if socket == "" || pane == "" {
		return
	}
	if c, err := New(socket); err == nil {
		_, _ = c.run("select-pane", "-t", pane, "-L")
	}
}

// FocusIfActive moves focus to pane to, but only if pane from still has it,
// so it never pulls the user away from somewhere they chose to go.
func (c *Client) FocusIfActive(from, to string) {
	_, _ = c.run("if-shell", "-F", "-t", from, "#{pane_active}", "select-pane -t "+to)
}
