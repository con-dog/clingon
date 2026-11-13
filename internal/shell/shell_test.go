package shell

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// run starts shell the way clingon does, in a private tmux server, with a
// home folder holding rc, types each line, and returns the screen.
func run(t *testing.T, shell, rcName, rc string, lines ...string) string {
	t.Helper()
	for _, bin := range []string{"tmux", shell} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " isn't installed")
		}
	}
	home, err := os.MkdirTemp("", "clsh") // short: tmux socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	if err := os.WriteFile(filepath.Join(home, rcName), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	path, _ := exec.LookPath(shell)
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TERM=xterm-256color", "LANG=en_US.UTF-8", "CLINGON_READY=" + filepath.Join(home, "ready")}
	prog, argv, env, err := Command(filepath.Join(home, "cfg"), path, env)
	if err != nil {
		t.Fatal(err)
	}
	// Never let a test start a shell that reads the real user's dotfiles:
	// their plugin managers rewrite files (antidote's .zsh_plugins.zsh did).
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); (k == "HOME" || strings.HasSuffix(k, "ZDOTDIR")) && !strings.HasPrefix(v, home) {
			t.Fatalf("%s=%s is outside the test's home %s: refusing to start the shell", k, v, home)
		}
	}
	sock := filepath.Join(home, "s")
	args := []string{"-S", sock, "-f", "/dev/null", "new-session", "-d", "-x", "100", "-y", "20", "-c", home}
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	// argv[0] like "-zsh" makes a login shell; tmux runs prog, so pass it
	// through exec -a.
	args = append(args, "--", "/bin/sh", "-c", `exec -a "$0" "$@"`, argv[0], prog)
	args = append(args, argv[1:]...)
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		t.Fatalf("tmux: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", sock, "kill-server").Run() })
	capture := func() string {
		out, _ := exec.Command("tmux", "-S", sock, "capture-pane", "-p").Output()
		return string(out)
	}
	// Generous: a cold CI runner under -race can take seconds to start the
	// first shell, and this only polls, so a pass is no slower for it.
	wait := func(what string, ok func(string) bool) string {
		for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			if s := capture(); ok(s) {
				return s
			}
		}
		t.Fatalf("%s: timed out waiting for %s; screen:\n%s", shell, what, capture())
		return ""
	}
	wait("the prompt", func(s string) bool { return strings.Contains(s, "❯") })
	for _, l := range lines {
		_ = exec.Command("tmux", "-S", sock, "send-keys", "-l", l).Run()
		_ = exec.Command("tmux", "-S", sock, "send-keys", "Enter").Run()
	}
	return wait("the output", func(s string) bool { return strings.Contains(s, "done-"+shell) })
}

func TestZshGetsClingonsLookOverTheUsersSetup(t *testing.T) {
	screen := run(t, "zsh", ".zshrc", `
alias hi='echo from-users-zshrc'
PROMPT='USERPROMPT> '
starship_precmd() { PROMPT='STARSHIP> ' }
precmd_functions+=(starship_precmd)
`, "hi", `echo "zdotdir=[$ZDOTDIR]"`, "echo done-zsh")
	for _, want := range []string{"from-users-zshrc", "zdotdir=[]", "❯"} {
		if !strings.Contains(screen, want) {
			t.Errorf("want %q on screen:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "USERPROMPT>") || strings.Contains(screen, "STARSHIP>") {
		t.Errorf("clingon's prompt should win over the user's and starship's:\n%s", screen)
	}
}

func TestBashGetsClingonsLookOverTheUsersSetup(t *testing.T) {
	screen := run(t, "bash", ".bash_profile", `
alias hi='echo from-users-profile'
PS1='USERPROMPT$ '
starship_precmd() { PS1='STARSHIP$ '; }
PROMPT_COMMAND=starship_precmd
`, "hi", "false", "echo done-bash")
	if !strings.Contains(screen, "from-users-profile") {
		t.Errorf("the user's setup should load:\n%s", screen)
	}
	if strings.Contains(screen, "USERPROMPT$") || strings.Contains(screen, "STARSHIP$") {
		t.Errorf("clingon's prompt should win:\n%s", screen)
	}
}

// The prompt is two lines, so a long folder never squeezes what's typed: the
// folder, then the mark alone, with what's typed after it.
func TestPromptMarkIsOnALineOfItsOwn(t *testing.T) {
	for shell, rc := range map[string]string{"zsh": ".zshrc", "bash": ".bash_profile"} {
		screen := run(t, shell, rc, "", "echo typed-here", "echo done-"+shell)
		if !regexp.MustCompile(`(?m)^❯ echo typed-here$`).MatchString(screen) {
			t.Errorf("%s: the mark should start its own line:\n%s", shell, screen)
		}
		if !regexp.MustCompile(`(?m)^~\s*\n❯ `).MatchString(screen) {
			t.Errorf("%s: the folder should be on the line above the mark:\n%s", shell, screen)
		}
	}
}

// A local build stays first on PATH, whatever the user's setup puts there.
func TestLocalBuildStaysFirstOnPath(t *testing.T) {
	for shell, rc := range map[string]string{"zsh": ".zshrc", "bash": ".bash_profile"} {
		screen := run(t, shell, rc, "export CLINGON_BIN=/lbin\nexport PATH=/usersfirst:$PATH\n", `echo "first=${PATH%%:*}"`, "echo done-"+shell)
		if !strings.Contains(screen, "first=/lbin") {
			t.Errorf("%s: the local build should be first:\n%s", shell, screen)
		}
	}
}

// The active AWS profile shows in the prompt, as text that never runs, and
// goes away with the profile.
func TestPromptShowsTheAWSProfile(t *testing.T) {
	for shell, rc := range map[string]string{"zsh": ".zshrc", "bash": ".bash_profile"} {
		screen := run(t, shell, rc, "", `export AWS_PROFILE='a%d$(echo b)'`, "unset AWS_PROFILE", "echo done-"+shell)
		if !strings.Contains(screen, "aws:a%d$(echo b)") {
			t.Errorf("%s should show the profile as written:\n%s", shell, screen)
		}
		if n := strings.Count(screen, "aws:"); n != 1 {
			t.Errorf("%s: only the prompt after export should show a profile, got %d:\n%s", shell, n, screen)
		}
	}
}

func TestOtherShellsAreRefused(t *testing.T) {
	for _, sh := range []string{"/bin/tcsh", "/opt/homebrew/bin/nu", "/opt/homebrew/bin/fish", "/bin/sh", ""} {
		if Check(sh) == nil {
			t.Errorf("%q should be refused", sh)
		}
		if _, _, _, err := Command(t.TempDir(), sh, nil); err == nil {
			t.Errorf("%q shouldn't start", sh)
		}
	}
	for _, sh := range []string{"/bin/zsh", "/usr/bin/bash"} {
		if err := Check(sh); err != nil {
			t.Errorf("%q: %v", sh, err)
		}
	}
}

// setup writes every embedded file over the user's copy, so only clingon's
// own shell files may be embedded: never a history or anything else.
func TestOnlyShellFilesAreEmbedded(t *testing.T) {
	want := map[string]bool{
		"files/bash/bashrc": true, "files/zsh/.zshenv": true,
		"files/zsh/.zprofile": true, "files/zsh/.zshrc": true,
	}
	_ = fs.WalkDir(files, "files", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !want[path] {
			t.Errorf("%s is embedded, and setup would write it over the user's copy", path)
		}
		return err
	})
}

// Each prompt tells the menu it's back, by appending to $CLINGON_CHANGED
// (which the menu watches), with a builtin: no process, nothing to wait on.
func TestPromptSaysItsBack(t *testing.T) {
	for shell, rc := range map[string]string{"zsh": ".zshrc", "bash": ".bash_profile"} {
		screen := run(t, shell, rc, "export CLINGON_CHANGED=$HOME/changed\n", "true", "true", `echo "dots=$(wc -c < "$CLINGON_CHANGED" | tr -d ' ')"`, "echo done-"+shell)
		dots := 0
		if m := regexp.MustCompile(`dots=(\d+)`).FindStringSubmatch(screen); m != nil { // the output can share a line with what's typed next
			dots, _ = strconv.Atoi(m[1])
		}
		if dots < 3 { // the first prompt, and one after each true
			t.Errorf("%s: %d prompts said so, want 3 or more:\n%s", shell, dots, screen)
		}
	}
}

// The welcome card over the first prompt is drawn again when the pane's
// width changes before anything's been run there (a window still growing to
// its full size left it cut short), and left alone once something has.
func TestWelcomeCardIsDrawnAgainUntilSomethingRuns(t *testing.T) {
	for _, bin := range []string{"tmux", "zsh"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " isn't installed")
		}
	}
	home, err := os.MkdirTemp("", "clsh") // short: tmux socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A stand-in for clingon: its card says how wide it was drawn.
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\n[ \"$1 $2\" = \"welcome --again\" ] && echo \"card $(stty size </dev/tty | cut -d' ' -f2) wide\"\n"
	if err := os.WriteFile(filepath.Join(bin, "clingon"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	zsh, _ := exec.LookPath("zsh")
	// CLINGON_BIN keeps it first on the path, as a local build is: a login
	// shell's own setup puts the system's folders ahead of anything in PATH.
	env := []string{"HOME=" + home, "PATH=" + bin + ":" + os.Getenv("PATH"), "CLINGON_BIN=" + bin, "TERM=xterm-256color", "LANG=en_US.UTF-8", CardColsEnv + "=100"}
	prog, argv, env, err := Command(filepath.Join(home, "cfg"), zsh, env)
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range env { // never the real user's dotfiles
		if k, v, _ := strings.Cut(kv, "="); (k == "HOME" || strings.HasSuffix(k, "ZDOTDIR")) && !strings.HasPrefix(v, home) {
			t.Fatalf("%s=%s is outside the test's home %s: refusing to start the shell", k, v, home)
		}
	}
	sock := filepath.Join(home, "s")
	tmux := func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-S", sock}, args...)...).CombinedOutput()
		return string(out)
	}
	args := []string{"-f", "/dev/null", "new-session", "-d", "-x", "100", "-y", "20", "-c", home}
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	args = append(args, "--", "/bin/sh", "-c", `exec -a "$0" "$@"`, argv[0], prog)
	if out := tmux(append(args, argv[1:]...)...); strings.TrimSpace(out) != "" {
		t.Fatalf("tmux: %s", out)
	}
	t.Cleanup(func() { tmux("kill-server") })
	wait := func(what string, ok func(string) bool) string {
		for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			if s := tmux("capture-pane", "-p"); ok(s) {
				return s
			}
		}
		t.Fatalf("timed out waiting for %s; screen:\n%s", what, tmux("capture-pane", "-p"))
		return ""
	}
	if s := wait("the prompt", func(s string) bool { return strings.Contains(s, "❯") }); strings.Contains(s, "card ") {
		t.Fatalf("the card is as wide as the pane: nothing to draw again yet:\n%s", s)
	}

	tmux("resize-window", "-x", "120") // the window grows: the card again, at its new width, then the prompt
	s := wait("the card again", func(s string) bool { return strings.Contains(s, "card 120 wide") })
	s = wait("the prompt under it", func(s string) bool {
		return strings.Index(s, "❯") > strings.Index(s, "card 120 wide") && strings.Contains(s, "card 120 wide")
	})
	if strings.Count(s, "❯") != 1 || strings.Count(s, "card ") != 1 {
		t.Errorf("one card and one prompt on a cleared screen, got:\n%s", s)
	}

	tmux("send-keys", "-l", "echo ran-it")
	tmux("send-keys", "Enter")
	wait("the command", func(s string) bool { return strings.Contains(s, "\nran-it") })
	tmux("resize-window", "-x", "90") // something's been run: the screen's yours now
	time.Sleep(600 * time.Millisecond)
	if s := tmux("capture-pane", "-p"); strings.Contains(s, "card 90 wide") || !strings.Contains(s, "ran-it") {
		t.Errorf("after a command the card isn't drawn again, and nothing's cleared:\n%s", s)
	}
}

// Each shell marks every prompt it puts up in $CLINGON_READY: how the menu
// hears that a command it sent has finished, without looking again and again.
func TestShellsMarkEachPrompt(t *testing.T) {
	for shell, rc := range map[string]string{"zsh": ".zshrc", "bash": ".bashrc"} {
		screen := run(t, shell, rc, "",
			`a=$(wc -c < "$CLINGON_READY")`, "true", "true",
			`b=$(wc -c < "$CLINGON_READY")`, `echo "marks: first $((a)), then $((b - a)) more; done"-`+shell) // "done-zsh" only once it's run, not as it's typed
		m := regexp.MustCompile(`marks: first (\d+), then (\d+) more`).FindStringSubmatch(strings.ReplaceAll(screen, "\n", ""))
		if m == nil {
			t.Fatalf("%s: no count on the screen:\n%s", shell, screen)
		}
		first, _ := strconv.Atoi(m[1])
		more, _ := strconv.Atoi(m[2])
		if first < 1 || more != 3 { // the assignment's own prompt, then one for each of the three lines after it
			t.Errorf("%s: %d marks by the first prompt, %d for the next three: want at least 1, then 3\n%s", shell, first, more, screen)
		}
	}
}
