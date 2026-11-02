package config

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectFile is a pack that lives in a repository, for the commands that
// only make sense there. clingon finds it in the shell's folder or one
// above it, and only uses it once you've trusted that exact file.
const ProjectFile = ".clingon.yaml"

// Project is the project pack for a folder.
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

// FindProject returns the project pack for dir, or nil if there's none.
func FindProject(dir string) *Project {
	if dir == "" {
		return nil
	}
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		path := filepath.Join(d, ProjectFile)
		if data, err := os.ReadFile(path); err == nil {
			return loadProject(path, data)
		}
		if d == filepath.Dir(d) {
			return nil
		}
	}
}

func loadProject(path string, data []byte) *Project {
	sum := sha256.Sum256(data)
	p := &Project{Path: path, sum: hex.EncodeToString(sum[:])}
	known := trusted()
	p.Trusted = known[path] == p.sum
	p.Changed = known[path] != "" && !p.Trusted
	cfg, err := parse(path, data)
	if err == nil {
		if errs := cfg.Validate(); len(errs) > 0 {
			err = errs[0]
		}
	}
	if err != nil {
		p.Err = err
		return p
	}
	for i := range cfg.Sections {
		cfg.Sections[i].Needs = cfg.Requires
		cfg.Sections[i].pushDown()
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

func trustFile() string { return filepath.Join(Dir(), "trusted-projects") }

// trusted maps each trusted project pack's path to the sha256 it had when
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

// Trust records the project pack, exactly as it is now, as trusted.
func (p *Project) Trust() error {
	if strings.ContainsAny(p.Path, "\n\r") {
		return errors.New("can't trust a project whose path has a line break in it")
	}
	known := trusted()
	known[p.Path] = p.sum
	var b strings.Builder
	b.WriteString("# Project packs (.clingon.yaml) you've trusted, by sha256: editing one means trusting it again.\n")
	paths := make([]string, 0, len(known))
	for path := range known {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		b.WriteString(known[path] + "  " + path + "\n")
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return writeWhole(trustFile(), []byte(b.String()))
}
