package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestAddAndRemoveCommandKeepsComments(t *testing.T) {
	userDir(t)
	if err := AddCommand("mine", "Deploy", "Tail logs", "kubectl logs -f deploy/<app> -n <ns>", "Streams logs."); err != nil {
		t.Fatal(err)
	}
	if err := AddCommand("mine", "Deploy", "Tail logs", "x", ""); err == nil {
		t.Error("a duplicate name should be refused")
	}
	if err := AddCommand("mine", "Deploy", "Status", "kubectl get pods", ""); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(PackPath("mine"))
	if !strings.Contains(string(b), "# Your own clingon pack") {
		t.Error("the file's comments were lost")
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Sections[0].All()
	if len(c) != 2 || len(c[1].Args) != 2 || c[1].Args[0].Name != "app" || c[1].Desc != "Streams logs." {
		t.Fatalf("got %+v", c)
	}
	if err := RemoveCommand("mine", "Deploy", "Tail logs"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCommand("mine", "Deploy", "Status"); err != nil {
		t.Fatal(err)
	}
	if got := SectionNames("mine"); len(got) != 0 {
		t.Fatalf("empty section should go too, got %v", got)
	}
}

func TestPackNames(t *testing.T) {
	userDir(t)
	for _, bad := range []string{"My Tools", "../x", "", "-x", "_installed", "a_b", "a.b"} {
		if NewPack(bad) == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	for _, good := range []string{"work-tools", "K8s", "mine2", "installed", "a-"} {
		if err := NewPack(good); err != nil {
			t.Errorf("%q should be allowed: %v", good, err)
		}
	}
}

func TestClingonFilesAreNotPacks(t *testing.T) {
	userDir(t)
	if err := Install("git"); err != nil {
		t.Fatal(err)
	}
	if err := WriteAvailable(); err != nil {
		t.Fatal(err)
	}
	if err := NewPack("available"); err != nil { // same word as a reserved file
		t.Fatal(err)
	}
	files, _ := Files()
	if len(files) != 1 || filepath.Base(files[0]) != "available.yaml" {
		t.Fatalf("only the user's pack should load, got %v", files)
	}
	names, _ := Installed()
	if strings.Join(names, ",") != "git" {
		t.Fatalf("got %v", names)
	}
	b, _ := os.ReadFile(availableFile())
	all, _ := Packs()
	if strings.Count(string(b), "- name: ") != len(all) {
		t.Fatalf("_available.yaml should list all %d packs:\n%s", len(all), b)
	}
}

func TestInstallUpdateUninstall(t *testing.T) {
	userDir(t)
	if err := Install("git"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(remotePath("git"))
	if !strings.HasPrefix(string(b), "# Installed by clingon.") || !strings.Contains(string(b), "version: ") {
		t.Fatalf("installed copy should say where it came from and carry its version:\n%.200s", b)
	}
	// an older copy installed: an update is available until it's reinstalled
	_ = os.WriteFile(remotePath("git"), regexp.MustCompile(`(?m)^version: \S+`).ReplaceAll(b, []byte("version: 0.0.1")), 0o644)
	ups, _ := PackUpdates()
	if len(ups) != 1 || ups[0].Name != "git" || ups[0].From != "0.0.1" {
		t.Fatalf("got %v", ups)
	}
	if err := Install("git"); err != nil {
		t.Fatal(err)
	}
	if again, _ := PackUpdates(); len(again) != 0 {
		t.Fatalf("still out of date after update: %v", again)
	}
	if err := Uninstall("git"); err != nil {
		t.Fatal(err)
	}
	if names, _ := Installed(); len(names) != 0 {
		t.Fatalf("uninstall should delete the file, got %v", names)
	}
	// a local pack can share a remote one's name
	if err := Install("git"); err != nil {
		t.Fatal(err)
	}
	if err := NewPack("git"); err != nil {
		t.Fatal(err)
	}
	if names, _ := Installed(); len(names) != 1 {
		t.Fatalf("got %v", names)
	}
}

func TestSplitRef(t *testing.T) {
	for ref, want := range map[string][2]string{"git": {"", "git"}, "remote/git": {"remote", "git"}, "local/my-tools": {"local", "my-tools"}, "other/x": {"", "other/x"}} {
		if w, n := SplitRef(ref); w != want[0] || n != want[1] {
			t.Errorf("SplitRef(%q) = %q, %q", ref, w, n)
		}
	}
}

func TestNewPackIsAWorkingExample(t *testing.T) {
	userDir(t)
	if err := NewPack("work-tools"); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sections) != 1 || cfg.Sections[0].Name != "Work tools" || len(cfg.Sections[0].All()) != 1 {
		t.Fatalf("got %+v", cfg.Sections)
	}
}

// A pack clingon stops shipping (databases, split into one per engine)
// stays installed until something removes it, still in the menu: so it's
// an update, and updating removes it. Your own packs are never touched.
func TestRetiredPacksAreUpdatesThatRemoveThem(t *testing.T) {
	userDir(t)
	if err := os.MkdirAll(remoteDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	old := "version: 1.0.0\nsections:\n  - name: Databases\n    groups: [{name: G, commands: [{name: c, cmd: ls}]}]\n"
	if err := os.WriteFile(remotePath("databases"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewPack("databases"); err != nil { // a local one of the same name: yours
		t.Fatal(err)
	}
	ups, _ := PackUpdates()
	if len(ups) != 1 || ups[0].Name != "databases" || !ups[0].Retired {
		t.Fatalf("a pack clingon no longer ships should be an update that removes it, got %+v", ups)
	}
}
