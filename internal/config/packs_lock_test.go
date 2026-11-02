package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// packs.lock records each built-in pack's version and a hash of its
// contents. Installed packs are copies, refreshed by `clingon pack update`
// only when the version changes, so a pack edited without a version bump
// never reaches anyone who has it installed. This test catches that.
//
// After bumping a version: UPDATE_PACKS_LOCK=1 go test ./internal/config/
const lockFile = "packs.lock"

func TestPackChangesBumpTheVersion(t *testing.T) {
	pks, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	locked := map[string][2]string{} // name → version, hash
	if b, err := os.ReadFile(lockFile); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if f := strings.Fields(line); len(f) == 3 {
				locked[f[0]] = [2]string{f[1], f[2]}
			}
		}
	}
	var lines []string
	for _, p := range pks {
		data, _ := packFS.ReadFile("packs/" + p.Name + ".yaml")
		hash := fmt.Sprintf("%x", sha256.Sum256(data))[:16]
		lines = append(lines, p.Name+" "+p.Version+" "+hash)
		if l, ok := locked[p.Name]; ok && l[1] != hash && l[0] == p.Version {
			t.Errorf("pack %s changed but is still version %s: bump its version", p.Name, p.Version)
		} else if !ok || l != [2]string{p.Version, hash} {
			if os.Getenv("UPDATE_PACKS_LOCK") == "" {
				t.Errorf("pack %s %s isn't in %s: run UPDATE_PACKS_LOCK=1 go test ./internal/config/", p.Name, p.Version, lockFile)
			}
		}
	}
	if os.Getenv("UPDATE_PACKS_LOCK") != "" && !t.Failed() {
		sort.Strings(lines)
		if err := os.WriteFile(lockFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
