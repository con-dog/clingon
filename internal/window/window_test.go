package window

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTerminalIsTheOneClingonRunsIn(t *testing.T) {
	for env, want := range map[string]string{
		"TERM_PROGRAM=Apple_Terminal": "terminal",
		"TERM_PROGRAM=iTerm.app":      "iterm",
		"TERM_PROGRAM=ghostty":        "ghostty",
		"KITTY_WINDOW_ID=1":           "kitty",
		"ALACRITTY_WINDOW_ID=7":       "alacritty",
	} {
		k, v, _ := strings.Cut(env, "=")
		got := Terminal(func(key string) string {
			if key == k {
				return v
			}
			return ""
		})
		if got != want {
			t.Errorf("%s: got %q, want %q", env, got, want)
		}
	}
	if got := Terminal(func(string) string { return "" }); got != "terminal" {
		t.Errorf("an unknown terminal opens Terminal.app, got %q", got)
	}
}

func TestEveryPlanSetsTheFontAndRunsClingonThroughALoginShell(t *testing.T) {
	for _, term := range []string{"terminal", "iterm", "ghostty", "kitty", "alacritty"} {
		p, err := PlanFor(term, "/opt/homebrew/bin/clingon", "/Users/me/my project", "/bin/zsh", "/Users/me")
		if err != nil {
			t.Fatalf("%s: %v", term, err)
		}
		all := strings.Join(p.Args, "\x00") + string(p.Contents)
		if !strings.Contains(all, fontFamily) && !strings.Contains(all, fontPS) {
			t.Errorf("%s: the font isn't set: %q", term, all)
		}
		if !strings.Contains(all, "'/Users/me/my project'") && !strings.Contains(all, "/Users/me/my project") {
			t.Errorf("%s: it should start in the folder: %q", term, all)
		}
		if !strings.Contains(all, "clingon") {
			t.Errorf("%s: it should run clingon: %q", term, all)
		}
		if term != "terminal" && !strings.Contains(all, "-l") { // Terminal's own windows are login shells
			t.Errorf("%s: clingon should run through a login shell, for Homebrew's PATH: %q", term, all)
		}
		if term != "terminal" && term != "iterm" && !strings.Contains(all, bg) {
			t.Errorf("%s: clingon's background should be set: %q", term, all)
		}
	}
	p, _ := PlanFor("iterm", "/c", "/d", "/bin/zsh", "/Users/me")
	if p.WriteFile != "/Users/me/Library/Application Support/iTerm2/DynamicProfiles/clingon.json" || !bytes.Contains(p.Contents, []byte(`"Name": "clingon"`)) {
		t.Errorf("iTerm gets its own profile, in its dynamic profiles folder: %s", p.WriteFile)
	}
	if _, err := PlanFor("", "/c", "/d", "/bin/zsh", "/h"); err == nil {
		t.Error("no known terminal should say which ones work")
	}
}

func TestQuotedFoldersSurviveTheShell(t *testing.T) {
	p, _ := PlanFor("kitty", "/c", "/tmp/it's here", "/bin/sh", "/h")
	if !strings.Contains(strings.Join(p.Args, " "), `cd '/tmp/it'\''s here' && CLINGON_WINDOW=kitty exec '/c'`) {
		t.Errorf("got %q", p.Args)
	}
}

func TestTempFilesRefuseWhatsThereAlready(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{target, link} {
		if err := writeNew(path, []byte("x")); err == nil {
			t.Errorf("%s should be refused", path)
		}
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Errorf("the file was changed: %q", b)
	}
	fresh := filepath.Join(dir, "fresh")
	if err := writeNew(fresh, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(fresh); fi.Mode().Perm() != 0o600 {
		t.Errorf("only you should read it: %v", fi.Mode())
	}
}

func TestRefusedPermissionSaysWhereToAllowIt(t *testing.T) {
	err := runError("osascript", "execution error: Not authorized to send Apple events to Terminal. (-1743)")
	if !strings.Contains(err.Error(), "Privacy & Security › Automation") {
		t.Errorf("got %v", err)
	}
	if err := runError("/usr/bin/osascript", "boom\n"); err.Error() != "osascript: boom" {
		t.Errorf("got %v", err)
	}
}

func TestInstallFontOnlyOnce(t *testing.T) {
	home := t.TempDir()
	stale := filepath.Join(fontDir(home), "ClingonMono-00000000.otf")
	if err := os.MkdirAll(fontDir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("an older build"), 0o644); err != nil {
		t.Fatal(err)
	}
	installed, err := InstallFont(home)
	if err != nil || !installed {
		t.Fatalf("first time: installed=%v err=%v", installed, err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("an older version of the font should be removed")
	}
	if b, _ := os.ReadFile(filepath.Join(fontDir(home), fontFileName)); !bytes.Equal(b, fontFile) {
		t.Fatal("the font file should be there, whole")
	}
	if b, _ := os.ReadFile(filepath.Join(fontDir(home), fontBoldFileName)); !bytes.Equal(b, fontBoldFile) {
		t.Fatal("its bold should be there beside it, whole")
	}
	if again, _ := InstallFont(home); again {
		t.Error("already there: nothing to do")
	}
	if err := os.Remove(filepath.Join(fontDir(home), fontBoldFileName)); err != nil {
		t.Fatal(err)
	}
	if again, _ := InstallFont(home); !again {
		t.Error("the bold gone missing: installed again")
	}
	if !bytes.HasPrefix(fontFile, []byte("OTTO")) {
		t.Error("the bundled font should be an OpenType file")
	}
}

func TestNoProfileIsEverDeleted(t *testing.T) {
	for _, script := range []string{terminalScript, itermScript, findScript, closeScript} {
		if strings.Contains(script, ".delete(") || strings.Contains(script, "delete ") {
			t.Errorf("clingon must never delete a profile: %s", script)
		}
	}
}

func TestTerminalTypesTheSameLineEveryTime(t *testing.T) {
	p, _ := PlanFor("terminal", "/c", "/d", "/bin/zsh", "/h")
	if !slices.Contains(p.Args, "/bin/sh "+p.Runner) {
		t.Errorf("Terminal should type just the fixed script, which ends up in shell history: %q", p.Args)
	}
	if strings.Contains(strings.Join(p.Args[:4], " "), p.WriteFile) {
		t.Errorf("what's typed shouldn't name this launch, or history fills up with them: %q", p.Args)
	}
	if !strings.Contains(terminalScript, `T.doScript(" " + command)`) {
		t.Error("a leading space keeps it out of history for those who've asked for that")
	}
	if !strings.HasPrefix(string(p.Contents), `rm -f "$0"`) || !strings.Contains(string(p.Contents), "exec '/bin/zsh' '-l' '-c'") {
		t.Errorf("a launch should delete itself, then run clingon in a login shell: %q", p.Contents)
	}
	if !slices.Contains(p.Args, ansi[4]) {
		t.Errorf("Terminal's profile should get clingon's 16 colours: %q", p.Args)
	}
}

// Each window takes the oldest launch waiting, once; run again from history,
// the line does nothing.
func TestTerminalRunnerTakesLaunchesInOrder(t *testing.T) {
	home := t.TempDir()
	p, _ := PlanFor("terminal", "/c", "/d", "/bin/sh", home)
	queue := filepath.Dir(p.WriteFile)
	if err := os.MkdirAll(queue, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Runner, p.RunnerContents, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(home, "out")
	for i, word := range []string{"first", "second"} {
		script := "rm -f \"$0\"\necho " + word + " >> " + quote(out) + "\n"
		if err := os.WriteFile(filepath.Join(queue, strconv.Itoa(100+i)+".sh"), []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"first\n", "first\nsecond\n", "first\nsecond\n"} {
		if b, err := exec.Command("/bin/sh", p.Runner).CombinedOutput(); err != nil || len(b) != 0 {
			t.Fatalf("the runner should be silent and succeed: %v %q", err, b)
		}
		if got, _ := os.ReadFile(out); string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if left, _ := os.ReadDir(queue); len(left) != 0 {
		t.Errorf("nothing should be left in the queue: %v", left)
	}
}

func TestStaleLaunchesAreDropped(t *testing.T) {
	queue := t.TempDir()
	old, fresh := filepath.Join(queue, "1.sh"), filepath.Join(queue, "2.sh")
	for _, f := range []string{old, fresh} {
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	then := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(old, then, then); err != nil {
		t.Fatal(err)
	}
	dropStale(queue)
	if _, err := os.Stat(old); err == nil {
		t.Error("a launch no window came for should go")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("one still waiting for its window should stay")
	}
}

// Terminal once imported clingon's profile a moment before it could see a
// just-installed font, and kept SF Mono instead: the window showed "?"
// boxes for every logo. The profile is named for the font installed, and
// the script sets the font on it until it sticks, every time.
func TestTerminalGetsTheInstalledFont(t *testing.T) {
	p, _ := PlanFor("terminal", "/c", "/d", "/bin/zsh", "/Users/me")
	if !slices.Contains(p.Args, fontPS) || !slices.Contains(p.Args, filepath.Join(fontDir("/Users/me"), fontFileName)) {
		t.Errorf("Terminal should get the font InstallFont puts in place: %q", p.Args)
	}
	if !strings.Contains(terminalProfile(), " ") || !slices.Contains(p.Args, terminalProfile()) {
		t.Errorf("the profile should be named for what's in it: %q", terminalProfile())
	}
	if !strings.Contains(terminalScript, "s.fontName = fontPS") || !strings.Contains(terminalScript, "s.fontName() === fontPS") {
		t.Error("the script should set the profile's font, and check it stuck, every launch")
	}
}
