// Package shell starts the user's shell for clingon's shell pane, with
// clingon's look: their own setup loads first, as usual, then clingon's
// prompt goes on top. clingon runs on zsh and bash, and no other shell:
// the menu reads that prompt to see what's typed. The user's files are never
// changed: clingon's own live in its config folder.
package shell

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

//go:embed all:files
var files embed.FS

// setup writes clingon's shell files into base/shell (when changed) and
// returns that folder.
func setup(base string) (string, error) {
	dir := filepath.Join(base, "shell")
	err := fs.WalkDir(files, "files", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, _ := files.ReadFile(path)
		dst := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(path, "files/")))
		if have, err := os.ReadFile(dst); err == nil && bytes.Equal(have, want) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, want, 0o644)
	})
	return dir, err
}

// Check says why clingon can't run on shell (a path, like $SHELL), if it can't.
func Check(shell string) error {
	switch filepath.Base(shell) {
	case "zsh", "bash":
		return nil
	}
	return fmt.Errorf("clingon needs zsh or bash as your login shell, and $SHELL is %q: `clingon print` works in any shell", shell)
}

// Command is how to start shell (a path, like $SHELL) with clingon's look:
// the program, its arguments (argv[0] included) and its environment.
func Command(base, shell string, env []string) (string, []string, []string, error) {
	if err := Check(shell); err != nil {
		return "", nil, nil, err
	}
	name := filepath.Base(shell)
	dir, err := setup(base)
	if err != nil {
		return "", nil, nil, err
	}
	switch name {
	case "zsh":
		// zsh reads its startup files from ZDOTDIR: clingon's, which read
		// the user's from wherever they'd normally be.
		user, had := lookup(env, "ZDOTDIR")
		if !had {
			user, _ = lookup(env, "HOME")
		}
		env = setEnv(env, "CLINGON_USER_ZDOTDIR", user)
		if had {
			env = setEnv(env, "CLINGON_HAD_ZDOTDIR", "1")
		}
		env = setEnv(env, "ZDOTDIR", filepath.Join(dir, "zsh"))
		return shell, []string{"-zsh"}, env, nil
	default: // bash
		// --rcfile only works for a non-login shell: clingon's reads the
		// login files itself.
		return shell, []string{"bash", "--rcfile", filepath.Join(dir, "bash", "bashrc"), "-i"}, env, nil
	}
}

// Exec replaces this process with shell, started with clingon's look.
func Exec(base, shell string) error {
	path, argv, env, err := Command(base, shell, os.Environ())
	if err != nil {
		return err
	}
	return syscall.Exec(path, argv, env)
}

func lookup(env []string, key string) (string, bool) {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

func setEnv(env []string, key, value string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+value)
}
