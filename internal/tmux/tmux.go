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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/palette"
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

	bottom bar // the status bar, under clingon's frame
	top    bar // the recipe's bar, over it

	// ReadyFile is where the shell marks each prompt it puts up
	// (config.ReadyFile): its size is how many there have been.
	ReadyFile string
	sent      int64 // how many prompts there had been when the last command was sent, under mu; -1 once that's been waited on
}

// bar is one of the bars drawn in the empty spacer panes that span the
// window: the status bar below, the recipe's above.
type bar struct {
	option string     // the tmux option that names its pane
	mu     sync.Mutex // one draw at a time
	newest string     // its newest line, under Client.mu
	drawn  string     // the line last drawn, under mu
	pane   string     // its pane, once known, under mu
}

func New(socket string) (*Client, error) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return nil, errors.New("clingon needs tmux: install it with `brew install tmux`")
	}
	return &Client{bin: bin, Socket: socket, env: cleanEnv(), sent: -1, bottom: bar{option: "@clingon-bar"}, top: bar{option: "@clingon-topbar"}}, nil
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
// one-cell spacer panes; that gives each a full frame. The side spacers run
// the window's whole height and the bars sit between them, so every rule
// ends at a side line: none runs on past a corner.
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
	// The sides first, the window's whole height; then the bars, between
	// them. The other way round, the rules under and over the bars would run
	// on a cell past each corner of the frame.
	for _, dir := range [][]string{{"-h", "-b"}, {"-h"}, {"-v", "-b"}, {"-v"}} { // left, right, top, bottom
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
	left, right, top, bottom, menu := ids[0], ids[1], ids[2], ids[3], ids[4]
	spacers := ids[:4]

	// tmux spreads a window resize unevenly, so this runs now and after each.
	fix := [][]string{
		{"resize-pane", "-t", left, "-x", "1"}, {"resize-pane", "-t", right, "-x", "1"},
		{"resize-pane", "-t", top, "-y", "1"}, {"resize-pane", "-t", bottom, "-y", "1"},
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
	// The bottom spacer runs under both panes, inside the frame's sides: the
	// menu draws its status bar there.
	args = append(args, "set", "-g", "@clingon-bar", bottom, ";")
	// The top one spans it over both: the menu draws the recipe there.
	args = append(args, "set", "-g", "@clingon-topbar", top, ";")
	// The menu's, for what lines up with it from the shell (the pack form).
	args = append(args, "set", "-g", "@clingon-menu", menu, ";")
	isShellPrompt := "#{&&:#{==:#{pane_id}," + shell + "},#{&&:#{m:*sh,#{pane_current_command}},#{!=:#{pane_current_command},ssh}}}"
	// Selecting with the mouse copies to the system clipboard (tmux's own
	// mouse handling would otherwise keep it in a tmux-only buffer).
	args = append(args, "set", "-g", "copy-command", "pbcopy", ";")
	// What the menu shows that tmux knows first (where focus is, the shell
	// scrolled back or not), tmux says as it changes: a byte appended to
	// $CLINGON_CHANGED, which the menu watches. No polling, and nothing's lost
	// however many come at once (tmux's wait-for keeps only one).
	for _, h := range []string{"pane-focus-in", "after-select-pane", "pane-mode-changed"} {
		args = append(args, "set-hook", "-g", h+"["+changeHook+"]", `run-shell -b 'printf . >> "$CLINGON_CHANGED" 2>/dev/null'`, ";")
	}
	// Whether focus is in the shell, for the frame round it to take in the
	// AI's panel under it (tmux.conf's pane-border-style): kept as focus
	// moves, by tmux itself, so the frame's never a beat behind.
	// The keys while the AI drives (AIDriving): esc is the menu's, which stops
	// it, from whichever pane it's taken focus to; nothing else of yours goes
	// anywhere, so it can't land in what the AI's typing into.
	args = append(args, "bind", "-T", aiTable, "Escape", "send-keys", "-t", menu, "Escape", ";",
		"bind", "-T", aiTable, "Any", "if", "-F", "0", "", ";")
	args = append(args, "set-hook", "-g", "after-select-pane["+litHook+"]",
		"if -F -t "+shell+" '#{pane_active}' 'set -g @clingon-in-shell 1' 'set -g @clingon-in-shell 0'", ";")
	args = append(args,
		"set-hook", "-g", "window-resized", strings.Join(hook, " ; "), ";",
		// Tab in the menu hops right to the shell; in the shell it is
		// completion as usual.
		// From the AI's panel, when it's open, on to the menu.
		"bind", "-n", "Tab", "if", "-F", "#{==:#{pane_id},"+menu+"}", "select-pane -t "+shell,
		"if -F '#{==:#{pane_id},#{@clingon-panel}}' 'select-pane -t "+menu+"' 'send-keys Tab'", ";",
		// Shift-Tab at the shell prompt hops left to the menu. Inside vim,
		// ssh or any other program it passes through.
		// From the AI's panel, back to the shell.
		"bind", "-n", "BTab", "if", "-F", "#{==:#{pane_id},#{@clingon-panel}}", "select-pane -t "+shell,
		"if -F '"+isShellPrompt+"' 'select-pane -t "+menu+"' 'send-keys BTab'", ";",
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
	c.mu.Lock()
	c.sent = c.prompts() // the next prompt after this is the command finished
	c.mu.Unlock()
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

// prompts is how many prompts the shell has put up: the size of ReadyFile.
func (c *Client) prompts() int64 {
	st, err := os.Stat(c.ReadyFile)
	if err != nil {
		return 0
	}
	return st.Size()
}

// nextPrompt sleeps until the shell has put up more prompts than after, or
// until within has passed (0: however long it takes). Not by looking again
// and again: the shell marks each prompt in ReadyFile, and a kqueue event
// wakes this the moment it does. It says whether the prompt came.
func (c *Client) nextPrompt(after int64, within time.Duration) bool {
	w, err := config.WatchPath(c.ReadyFile)
	if err != nil {
		return false
	}
	defer w.Close()
	if c.prompts() > after { // marked before the watch was up
		return true
	}
	woke := make(chan bool, 1)
	go func() {
		for w.Wait() {
			if c.prompts() > after {
				woke <- true
				return
			}
		}
		woke <- false
	}()
	if within <= 0 {
		return <-woke
	}
	select {
	case ok := <-woke:
		return ok
	case <-time.After(within):
		return c.prompts() > after
	}
}

// WaitIdle blocks until the shell in pane is back at its prompt: the command
// just sent to it has finished (all of it: a pipeline, a list of commands,
// whatever its prompt's own hooks run), or, with none just sent, whatever is
// running there has. The shell says so itself, as its prompt comes up, and
// this sleeps until it does. It returns false if that can't be known.
func (c *Client) WaitIdle(pane string) bool {
	if c.ReadyFile == "" {
		return false
	}
	c.mu.Lock()
	after := c.sent
	c.sent = -1
	c.mu.Unlock()
	if after < 0 { // nothing of the menu's to wait on: whatever's running now
		after = c.prompts()
		if c.State(pane).Busy == "" { // and it's finished already
			return true
		}
	}
	return c.nextPrompt(after, 0)
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
	c.nextPrompt(0, within) // heard, or the shell's had its chance
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

// failMark is how the prompt's mark starts after a failed command: the
// palette's red (see the prompts in internal/shell).
var failMark = palette.Red.SGR()

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
	before := c.prompts()
	if _, err := c.run("send-keys", "-t", pane, "C-c"); err != nil {
		return err
	}
	c.nextPrompt(before, time.Second) // the fresh prompt, as the shell says it's up
	if st := c.State(pane); st.Typed == "" && st.Busy == "" {
		return nil
	}
	return errors.New("the shell didn't come back to an empty prompt: have a look at it")
}

// Bar draws line, already styled and as wide as the bar, in the status bar:
// the empty pane under clingon's frame, which takes output from tmux itself.
// Calls may overlap: whichever draws last draws the newest line.
func (c *Client) Bar(line string) { c.draw(&c.bottom, line) }

// TopBar draws line in the bar over clingon's frame, as Bar does below it.
func (c *Client) TopBar(line string) { c.draw(&c.top, line) }

func (c *Client) draw(b *bar, line string) {
	c.mu.Lock()
	b.newest = line
	c.mu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	c.mu.Lock()
	line = b.newest
	c.mu.Unlock()
	if b.pane == "" {
		b.pane, _ = c.run("show-options", "-gv", b.option)
	}
	if b.pane == "" || line == b.drawn {
		return
	}
	// No wrapping (a line a cell too wide would scroll the bar away), back
	// to the start, clear, draw.
	if _, err := c.runInput([]byte("\x1b[?7l\x1b[H\x1b[2K"+line), "display-message", "-I", "-t", b.pane); err == nil {
		b.drawn = line
	}
}

// BarWidth is the bars' width in cells, 0 if there's no bar. The bars sit
// inside the frame's sides, so it's their pane's own width, not the window's.
func (c *Client) BarWidth() int {
	pane, err := c.run("show-options", "-gv", c.bottom.option)
	if err != nil || pane == "" {
		return 0
	}
	c.bottom.mu.Lock()
	c.bottom.pane = pane
	c.bottom.mu.Unlock()
	width, err := c.run("display-message", "-p", "-t", pane, "#{pane_width}")
	if err != nil {
		return 0
	}
	w, _ := strconv.Atoi(width)
	return w
}

// Panel is a pane under the shell for the menu to draw in (the AI's): an
// empty pane, like the bars, that takes output from tmux itself.
type Panel struct {
	c           *Client
	shell, menu string
	mu          sync.Mutex
	pane        string
}

// NewPanel is a panel under the shell pane shell, not yet open; the keys
// pressed in it go to the menu's pane menu, which draws it.
func (c *Client) NewPanel(shell, menu string) *Panel { return &Panel{c: c, shell: shell, menu: menu} }

// panelKeys are the keys the panel takes: pressed there, they're the menu's
// (an empty pane has nothing to read them), which runs the AI.
var panelKeys = []string{"Up", "Down", "Left", "Right", "Enter", "Escape"}

// Open splits rows off the foot of the shell for the panel, or, open
// already, makes it rows tall; it says how wide it is.
func (p *Panel) Open(rows int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pane == "" {
		out, err := p.c.run("split-window", "-v", "-d", "-l", strconv.Itoa(rows), "-t", p.shell, "-P", "-F", "#{pane_id}", "")
		if err != nil {
			return 0
		}
		p.pane = strings.TrimSpace(out)
		args := []string{"set", "-g", "@clingon-panel", p.pane, ";", // tab finds it
			"set", "-p", "-t", p.pane, "@clingon-with-shell", "1", ";"} // framed with the shell, when you're in it
		for _, k := range panelKeys {
			args = append(args, "bind", "-n", k, "if", "-F", "#{==:#{pane_id},"+p.pane+"}", "send-keys -t "+p.menu+" "+k, "send-keys "+k, ";")
		}
		_, _ = p.c.run(args[:len(args)-1]...)
	}
	// Its height is clingon's to set, not a drag's: after any resize (a
	// border dragged, the window resized), it's put back. Only when it's
	// wrong, so putting it back doesn't set it off again. The hook's set to
	// the new height before it's resized: the resize sets the hook off, and
	// the old one would put it straight back.
	fix := fmt.Sprintf("if -F -t %[1]s '#{!=:#{pane_height},%[2]d}' 'resize-pane -t %[1]s -y %[2]d'", p.pane, rows)
	// Whether the AI's at work (the full panel, not the strip) is said
	// before the resize: a program in the shell (the pack form), resized by
	// it, asks then, and finds it.
	working := "0"
	if rows > 1 {
		working = "1"
	}
	_, _ = p.c.run("set-hook", "-g", "after-resize-pane["+panelHook+"]", fix, ";", "set-hook", "-g", "window-resized["+panelHook+"]", fix,
		";", "set", "-g", "@clingon-ai-working", working,
		";", "resize-pane", "-t", p.pane, "-y", strconv.Itoa(rows)) // open already: as tall as asked (a strip at rest, the full panel at work)
	out, _ := p.c.run("display-message", "-p", "-t", p.pane, "#{pane_width}")
	w, _ := strconv.Atoi(strings.TrimSpace(out))
	return w
}

// panelHook is the panel's place in tmux's hook lists, clear of the layout's.
const panelHook = "42"

// changeHook is the change hooks' place in tmux's hook lists.
const changeHook = "43"

// litHook is the place of the hook that keeps @clingon-in-shell.
const litHook = "44"

// Draw draws lines, already styled, in the panel.
func (p *Panel) Draw(lines []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pane == "" {
		return
	}
	// On the alternate screen, which keeps no scrollback, with the mouse
	// taken: the wheel neither scrolls back through old draws nor starts
	// copy mode. A redraw replaces it all.
	_, _ = p.c.runInput([]byte("\x1b[?1049h\x1b[?1000h\x1b[?7l\x1b[H\x1b[2J"+strings.Join(lines, "\r\n")), "display-message", "-I", "-t", p.pane)
}

// Close takes the panel away, giving its rows back to the shell.
func (p *Panel) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pane != "" {
		args := []string{"kill-pane", "-t", p.pane, ";", "set", "-gu", "@clingon-panel", ";", "set", "-gu", "@clingon-ai-working",
			";", "set-hook", "-gu", "after-resize-pane[" + panelHook + "]", ";", "set-hook", "-gu", "window-resized[" + panelHook + "]"}
		for _, k := range panelKeys {
			args = append(args, ";", "unbind", "-n", k)
		}
		_, _ = p.c.run(args...)
		p.pane = ""
	}
}

// Focused says whether the panel has focus: keys the menu gets then are
// pressed in the panel.
func (p *Panel) Focused() bool {
	p.mu.Lock()
	pane := p.pane
	p.mu.Unlock()
	if pane == "" {
		return false
	}
	out, _ := p.c.run("display-message", "-p", "-t", pane, "#{pane_active}")
	return strings.TrimSpace(out) == "1"
}

// FocusPanel moves focus to the AI's panel, if it's open, from a program in
// clingon's shell (the pack form). It says whether there was one.
func FocusPanel() bool {
	socket, _, _ := strings.Cut(os.Getenv("TMUX"), ",")
	if socket == "" {
		return false
	}
	c, err := New(socket)
	if err != nil {
		return false
	}
	pane, _ := c.run("show-options", "-gv", "@clingon-panel")
	if pane = strings.TrimSpace(pane); pane == "" {
		return false
	}
	_, err = c.run("select-pane", "-t", pane)
	return err == nil
}

// SendKey presses key in pane: a tmux key name (Enter, Up, C-u), or text,
// typed as it is.
func (c *Client) SendKey(pane, key string, text bool) error {
	if text { // as bytes, in hex: a lone ";" would be tmux's own command separator
		args := []string{"send-keys", "-t", pane, "-H"}
		for _, b := range []byte(key) {
			args = append(args, fmt.Sprintf("%02x", b))
		}
		_, err := c.run(args...)
		return err
	}
	_, err := c.run("send-keys", "-t", pane, key)
	return err
}

// Screen is pane's screen as text, the rows its program marks with a cursor
// bar (a pink cell starting the row, as clingon's lists and form do) begun
// with ▶.
func (c *Client) Screen(pane string) string {
	out, err := c.run("capture-pane", "-p", "-e", "-t", pane)
	if err != nil {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = cursorCell.ReplaceAllString(l, "${1}▶")
		lines = append(lines, strings.TrimRight(ansi.Strip(l), " "))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// cursorCell is a pink-backed blank near a row's start: the cursor's bar.
var cursorCell = regexp.MustCompile(`^((?:\x1b\[[0-9;]*m|[ │])*)\x1b\[[0-9;]*48;(?:5;212|2;255;135;215)m `)

// Hear has tmux pass everything pane writes, as it writes it, to
// `self heard file`, which marks each piece in file: how the menu hears the
// shell's screen changing without looking at it over and over.
func (c *Client) Hear(pane, self, file string) error {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	_, err := c.run("pipe-pane", "-O", "-t", pane, "exec "+q(self)+" heard "+q(file))
	return err
}

// Tail is the last n lines of pane's screen, blank lines at the end left out.
func (c *Client) Tail(pane string, n int) string {
	out, err := c.run("capture-pane", "-p", "-t", pane)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(out, "\n "), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Where says which of clingon's panes has focus: "menu", "shell", "form"
// (the pack form, running in the shell's pane) or "panel" (the AI's); ""
// for none of them. tmux always knows; the menu, told only when it gains or
// loses focus itself, doesn't.
func (c *Client) Where(menu, shell string) string {
	out, err := c.run("display-message", "-p", "#{pane_id}|#{pane_current_command}|#{@clingon-panel}")
	if err != nil {
		return ""
	}
	parts := strings.SplitN(out, "|", 3)
	if len(parts) < 3 {
		return ""
	}
	switch id, cmd, panel := parts[0], parts[1], parts[2]; {
	case id == menu:
		return "menu"
	case panel != "" && id == panel:
		return "panel"
	case id == shell && cmd == "clingon":
		return "form"
	case id == shell:
		return "shell"
	}
	return ""
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

// MenuHeight is how tall clingon's menu pane is, for a program in its shell
// (the pack form) to line up with it, or 0 outside clingon's window. Asked
// as the program's size changes, not over and over.
func MenuHeight() int {
	socket, _, _ := strings.Cut(os.Getenv("TMUX"), ",")
	if socket == "" {
		return 0
	}
	c, err := New(socket)
	if err != nil {
		return 0
	}
	menu, err := c.run("show-options", "-gv", "@clingon-menu")
	if err != nil || menu == "" {
		return 0
	}
	out, err := c.run("display-message", "-p", "-t", menu, "#{pane_height}")
	if err != nil {
		return 0
	}
	h, _ := strconv.Atoi(out)
	return h
}

// AIWorking says whether clingon's AI is at work (its full panel open under
// the shell), for a program in the shell (the pack form) to leave out what's
// only for people. Asked as the program's size changes: the panel opening
// or closing resizes it.
func AIWorking() bool {
	socket, _, _ := strings.Cut(os.Getenv("TMUX"), ",")
	if socket == "" {
		return false
	}
	c, err := New(socket)
	if err != nil {
		return false
	}
	out, err := c.run("show-options", "-gv", "@clingon-ai-working")
	return err == nil && out == "1"
}

// aiTable is the key table the window has while the AI drives.
const aiTable = "clingon-ai"

// AIDriving takes focus to pane, the one the AI's working in, frames it in
// the AI's gold (tmux.conf's border styles read @clingon-ai-pane) and has
// the window take only esc from you, which is the menu's and stops it. ""
// gives it all back: your keys, the purple frame, and focus to back.
func (c *Client) AIDriving(pane, back string) {
	if pane == "" {
		_, _ = c.run("set", "-g", "@clingon-ai-pane", "", ";", "set", "-u", "key-table", ";", "select-pane", "-t", back)
		return
	}
	_, _ = c.run("set", "-g", "@clingon-ai-pane", pane, ";", "set", "key-table", aiTable, ";", "select-pane", "-t", pane)
}

// FocusIfActive moves focus to pane to, but only if pane from still has it,
// so it never pulls the user away from somewhere they chose to go.
func (c *Client) FocusIfActive(from, to string) {
	_, _ = c.run("if-shell", "-F", "-t", from, "#{pane_active}", "select-pane -t "+to)
}
