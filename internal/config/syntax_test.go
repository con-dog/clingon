package config

import (
	"os/exec"
	"testing"
)

// Every built-in command must at least parse in the shells clingon runs on.
// -n reads without running, and neither shell loads anyone's config.
func TestCommandsParseInEveryShell(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ps, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	for shell, flags := range map[string][]string{"zsh": {"-f", "-n", "-c"}, "bash": {"--noprofile", "--norc", "-n", "-c"}} {
		if _, err := exec.LookPath(shell); err != nil {
			t.Logf("%s isn't installed: skipped", shell)
			continue
		}
		for _, p := range ps {
			c, err := loadPack(p.Name)
			if err != nil {
				t.Fatal(err)
			}
			for i := range c.Sections {
				for _, cmd := range c.Sections[i].All() {
					line := placeholder.ReplaceAllString(cmd.Cmd, "X")
					if out, err := exec.Command(shell, append(flags, line)...).CombinedOutput(); err != nil {
						t.Errorf("%s: %s › %s doesn't parse: %s\n%s", shell, p.Name, cmd.Name, out, cmd.Cmd)
					}
				}
			}
		}
	}
}
