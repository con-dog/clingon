package shell

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TERM=xterm-256color", "LANG=en_US.UTF-8"}
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
