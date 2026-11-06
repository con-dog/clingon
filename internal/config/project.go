package config

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// ProjectDir holds the packs that live in a repository, for the commands that
// only make sense there. clingon finds it in the shell's folder or one above
// it, and only uses it once you've trusted those exact files.
const ProjectDir = ".clingon/packs"

// Project is the project packs for a folder.
type Project struct {
	Path    string
	Config  *Config // nil if it has problems or isn't trusted
	Err     error   // what's wrong with it
	Trusted bool
	Changed bool     // trusted before, but edited since
	Cmds    []string // every command it would run, for the trust screen
	Values  []string // what its options, defaults and joins put into them, for the same screen
	sum     string
}

// isOwnFolder says whether .clingon in folder d is your own clingon folder
// rather than a project's: its packs are already yours, and needn't be trusted.
func isOwnFolder(d string) bool {
	here, err := os.Stat(filepath.Join(d, ".clingon"))
	if err != nil {
		return false
	}
	if own, err := os.Stat(Dir()); err == nil && os.SameFile(here, own) {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	own, err := os.Stat(filepath.Join(home, ".clingon"))
	return err == nil && os.SameFile(here, own)
}

// FindProject returns the project packs for dir, or nil if there are none.
func FindProject(dir string) *Project {
	if dir == "" {
		return nil
	}
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		path := filepath.Join(d, filepath.FromSlash(ProjectDir))
		if fi, err := os.Stat(path); err == nil && fi.IsDir() && !isOwnFolder(d) {
			return loadProject(path)
		}
		if d == filepath.Dir(d) {
			return nil
		}
	}
}

// readProject reads everything under a project's packs folder into memory, so
// that what's hashed for trust is just what's parsed. A repository is someone
// else's until it's trusted, so only plain files and folders are read: a link
// could point anywhere, and reading a named pipe would hang the menu.
func readProject(path string) (map[string][]parsedFile, string, error) {
	h := sha256.New()
	out := map[string][]parsedFile{}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, "", err
	}
	for _, e := range entries {
		switch {
		case e.Type()&os.ModeSymlink != 0 || !e.IsDir() && !e.Type().IsRegular():
			return nil, "", fmt.Errorf("%s isn't a plain file or folder", filepath.Join(path, e.Name()))
		case !e.IsDir() && strings.HasPrefix(e.Name(), "."):
			continue
		case !e.IsDir() && isMarkdown(e.Name()):
			return nil, "", fmt.Errorf("%s: a pack is a folder, so this belongs in one, like %s", filepath.Join(path, e.Name()), filepath.Join(path, "deploy", e.Name()))
		case !e.IsDir() || skippedName(e.Name()):
			continue
		}
		files, err := os.ReadDir(filepath.Join(path, e.Name()))
		if err != nil {
			return nil, "", err
		}
		for _, f := range files {
			full := filepath.Join(path, e.Name(), f.Name())
			if f.Type()&os.ModeSymlink != 0 || !f.IsDir() && !f.Type().IsRegular() {
				return nil, "", fmt.Errorf("%s isn't a plain file or folder", full)
			}
			if f.IsDir() || !isMarkdown(f.Name()) || !isPackFile(full) {
				continue
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return nil, "", err
			}
			// The names count: a file's name can be its module's.
			fmt.Fprintf(h, "%s/%s\x00%d\x00", e.Name(), f.Name(), len(data))
			h.Write(data)
			part, err := parsePackFile(full, data)
			if err != nil {
				return nil, "", err
			}
			out[e.Name()] = append(out[e.Name()], part)
		}
	}
	return out, hex.EncodeToString(h.Sum(nil)), nil
}

func loadProject(path string) *Project {
	p := &Project{Path: path}
	known := trusted()
	packs, sum, err := readProject(path)
	if err != nil {
		p.Err = err
		return p
	}
	p.sum = sum
	p.Trusted = known[path] == p.sum
	p.Changed = known[path] != "" && !p.Trusted
	cfg := &Config{}
	names := make([]string, 0, len(packs))
	for name := range packs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		cfg.merge(assemble(name, packs[name]))
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		p.Err = errs[0]
		return p
	}
	for i := range cfg.Sections {
		for _, c := range cfg.Sections[i].All() {
			p.Cmds = append(p.Cmds, c.Cmd)
			for _, a := range c.Args {
				// These go into commands as they are, so they're shown too.
				for _, o := range a.Options {
					p.Values = append(p.Values, a.Name+" option: "+o.Value)
				}
				if a.Default != "" {
					p.Values = append(p.Values, a.Name+" default: "+a.Default)
				}
				if a.Join != nil {
					p.Values = append(p.Values, a.Name+" join: "+*a.Join)
				}
			}
		}
	}
	if p.Trusted {
		cfg.keepAvailable()
		cfg.sort()
		p.Config = cfg
	}
	return p
}

func trustFile() string { return filepath.Join(StateDir(), "trusted-projects") }

// trusted maps each trusted project's packs folder to the sha256 it had when
// you trusted it.
func trusted() map[string]string {
	out := map[string]string{}
	f, err := os.Open(trustFile())
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "#") {
			continue
		} else if sum, path, ok := strings.Cut(line, "  "); ok {
			out[path] = sum
		}
	}
	return out
}

// Trust records the project's packs, exactly as they are now, as trusted.
func (p *Project) Trust() error {
	if strings.ContainsAny(p.Path, "\n\r") {
		return errors.New("can't trust a project whose path has a line break in it")
	}
	known := trusted()
	known[p.Path] = p.sum
	var b strings.Builder
	b.WriteString("# Project packs (" + ProjectDir + ") you've trusted, by sha256: editing one means trusting them again.\n")
	paths := make([]string, 0, len(known))
	for path := range known {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		b.WriteString(known[path] + "  " + path + "\n")
	}
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		return err
	}
	return writeWhole(trustFile(), []byte(b.String()))
}
