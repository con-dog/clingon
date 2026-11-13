package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/palette"
)

// twoOptions gives a select blank the options one and two, as the tests
// build on (a blank made a select starts with one empty option to fill in).
func twoOptions(f *PackForm, i, j, b int) {
	f.blank(i, j, b).Options = []config.Option{{Value: "one"}, {Value: "two"}}
	f.setOpen(false, i, j, b, 0) // the empty one opened to be filled in; these start collapsed
	key := ""
	if r := f.selectedRow(); r != nil {
		key = r.key
	}
	f.refresh(key)
}

func newTestForm(t *testing.T) *PackForm {
	t.Helper()
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.NewPackWithLogo("demo", "rocket", "orange"); err != nil {
		t.Fatal(err)
	}
	f, err := NewPackForm("demo", true)
	if err != nil {
		t.Fatal(err)
	}
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	return f
}

func press(f *PackForm, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		f.Update(k)
	}
}

var (
	kDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	kUp    = tea.KeyPressMsg{Code: tea.KeyUp}
	kEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	kLeft  = tea.KeyPressMsg{Code: tea.KeyLeft}
	kRight = tea.KeyPressMsg{Code: tea.KeyRight}
	kEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
)

func (f *PackForm) screen() string { return ansi.Strip(f.view()) }

// page is the whole page, not just what's scrolled into view.
func (f *PackForm) page() string {
	lines, _, _, _ := f.cards(f.w)
	return ansi.Strip(strings.Join(lines, "\n"))
}

// onRow moves the cursor to the row keyed key, opening the part it's in.
func (f *PackForm) onRow(t *testing.T, key string) {
	t.Helper()
	f.refresh(key)
	for i, r := range f.rows {
		if r.key == key && !r.still() {
			f.sel = i
			return
		}
	}
	t.Fatalf("no row %q in:\n%s", key, f.screen())
}

func TestPackFormMoves(t *testing.T) {
	f := newTestForm(t)
	for i := 0; i < 12; i++ {
		press(f, kDown)
		_ = f.screen()
	}
	for i := 0; i < 12; i++ {
		press(f, kUp)
		_ = f.screen()
	}
}

// typeIn opens the highlighted field, replaces what's in it, and confirms with enter.
func typeIn(f *PackForm, text string) {
	press(f, kRight)
	f.input.SetValue(text)
	press(f, kEnter)
}

// pick opens the highlighted field's list and picks the option titled title.
func (f *PackForm) pick(t *testing.T, title string) {
	t.Helper()
	press(f, kRight)
	for i, o := range f.options {
		if o.Value == title {
			f.opt = i
			press(f, kRight)
			return
		}
	}
	t.Fatalf("no option %q", title)
}

func TestPackFormEditsThePack(t *testing.T) {
	f := newTestForm(t)
	// A field: typed in, shown, and written at once.
	f.onRow(t, "desc")
	typeIn(f, "Deploying our services")
	if p, _ := config.LoadEditable("demo"); !strings.Contains(f.screen(), "Deploying our services") || p.Description != "Deploying our services" {
		t.Fatalf("shown, and written as it's made:\n%s", f.screen())
	}
	// A section, picked from the list dropped down under it.
	f.onRow(t, "menu")
	press(f, kRight)
	if !strings.Contains(f.screen(), "Infrastructure") {
		t.Fatalf("the list should be under the row:\n%s", f.screen())
	}
	press(f, kLeft)
	f.pick(t, "infrastructure")

	// A new module, on the same page, the cursor on its title.
	f.onRow(t, "add-module")
	press(f, kRight)
	if r := f.selectedRow(); r == nil || r.key != "1/name" || f.editing != nil || r.value != "" || !strings.Contains(f.screen(), "Like Everyday") {
		t.Fatalf("on its name, empty with its hint, not typing: on %+v", r)
	}
	typeIn(f, "Deploy")
	f.onRow(t, "1/0/name")
	typeIn(f, "Ship it")
	f.onRow(t, "1/0/risk")
	f.pick(t, config.RiskWarn)
	f.onRow(t, "1/0/cmd")
	typeIn(f, "./ship.sh <env>")
	// The blank came with the command, as rows of its card: made a pick-one.
	f.onRow(t, "1/0/0/kind")
	f.pick(t, "choose")
	twoOptions(f, 1, 0, 0)
	f.onRow(t, "1/0/0/add")
	press(f, kRight)
	if r := f.selectedRow(); r == nil || r.key != "1/0/0/2/types" {
		t.Fatalf("Add a choice should go to what the new choice types, on %+v", r)
	}
	typeIn(f, "prod")
	// The choice's label, its Desc; and picked to begin with.
	f.onRow(t, "1/0/0/2/label")
	typeIn(f, "Production")
	f.onRow(t, "1/0/0/2/pick")
	f.pick(t, "yes")
	if page := f.page(); !strings.Contains(page, "MODULE") || !strings.Contains(page, "COMMAND") {
		t.Fatalf("cards should be titled with what they are:\n%s", page)
	}

	// A bad name: refused, said why, on the menu's status bar.
	f.onRow(t, "1/0/name")
	typeIn(f, "Ship it!")
	if keys := config.FormKeys(); len(keys) == 0 || !strings.HasPrefix(keys[0], config.FormIssuePrefix) || strings.Contains(f.screen(), "ISSUE:") || f.input == nil {
		t.Fatalf("a bad name should be said on the status bar, not the form, still being typed: %q\n%s", keys, f.screen())
	}
	press(f, kEsc, kEsc) // refused, then undone
	if f.pack.Files[1].Group.Commands[0].Name != "Ship it" {
		t.Error("the bad name shouldn't have been kept")
	}

	// Remove is a − REMOVE row, asking No or Yes: No is first, and does
	// nothing.
	f.onRow(t, "1/0/remove")
	if s := f.screen(); !strings.Contains(s, "− REMOVE") || strings.Contains(s, "Drop") {
		t.Fatalf("a − REMOVE row:\n%s", s)
	}
	press(f, kRight)
	if len(f.options) != 2 || f.options[0].Label != "No" || f.options[1].Label != "Yes" || f.opt != 0 {
		t.Fatalf("should ask, No first: %+v", f.options)
	}
	press(f, kRight) // No
	if len(f.pack.Files[1].Group.Commands) != 1 {
		t.Fatal("No should keep it")
	}

	// Every change was written as it was made: esc just leaves, and it loads.
	press(f, kEsc)
	if f.Left != "saved" {
		t.Fatalf("left %q", f.Left)
	}
	cfg, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Sections[0]
	if s.Category != "Infrastructure" || len(s.Groups) != 2 {
		t.Fatalf("got %+v", s)
	}
	var ship *config.Command
	for _, c := range s.All() {
		if c.Name == "Ship it" {
			ship = c
		}
	}
	if ship == nil || ship.Risk != config.RiskWarn || ship.Cmd != "./ship.sh <env>" || ship.Args[0].Kind() != "choose" || len(ship.Args[0].Options) != 3 {
		t.Fatalf("Ship it: %+v", ship)
	}
	if a := ship.Args[0]; !slices.Contains(a.Options, config.Option{Value: "prod", Label: "Production"}) || a.Default != "prod" {
		t.Fatalf("the new choice: %+v", a)
	}
}

// Yes to a remove takes it out, and the cursor stays on a row.
func TestPackFormRemoves(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/add")
	press(f, kRight)
	typeIn(f, "Second")
	f.onRow(t, "0/0/remove")
	press(f, kRight, kDown, kRight) // Yes
	if len(f.pack.Files[0].Group.Commands) != 1 || f.pack.Files[0].Group.Commands[0].Name != "Second" {
		t.Fatal("Yes should remove it")
	}
	if f.selectedRow() == nil || f.selectedRow().header {
		t.Error("the cursor should be on a row once it's gone")
	}
}

// Left with nothing changed, nothing's kept; outside the menu too, a change
// is written as it's made.
func TestPackFormDrafts(t *testing.T) {
	f := newTestForm(t)
	press(f, kEsc)
	if f.Left != "" {
		t.Fatalf("nothing changed, but left %q", f.Left)
	}
	if d, _ := config.ReadDraft(); d != nil {
		t.Fatal("no draft without changes")
	}
	outside, _ := NewPackForm("demo", false)
	outside.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	outside.onRow(t, "desc")
	typeIn(outside, "Done")
	if p, _ := config.LoadEditable("demo"); p.Description != "Done" {
		t.Fatalf("written as it's made: %q", p.Description)
	}
	press(outside, kEsc)
	if outside.Left != "saved" {
		t.Fatalf("left %q", outside.Left)
	}
}

// A new pack starts empty, its names and command greyed hints to type over.
// It's added the moment it's complete; till then it's a draft, and the form
// says what it still needs.
func TestPackFormNewPack(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	f, err := NewPackFormForNew(true)
	if err != nil {
		t.Fatal(err)
	}
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	if f.pack.Title != "" || !f.pack.IsNew() || len(f.pack.Files) != 1 || len(f.pack.Files[0].Group.Commands) != 1 {
		t.Fatalf("got %+v", f.pack)
	}
	for _, hint := range []string{"Like My tools", "Like Everyday", "Like Say hello", `Like echo "Hello, <who>."`} {
		if !strings.Contains(f.page(), hint) { // the whole page: wherever the cursor is
			t.Errorf("no hint %q:\n%s", hint, f.page())
		}
	}
	if err := f.pack.Save(); err == nil || !strings.Contains(err.Error(), "needs a name") {
		t.Errorf("an empty pack can't be saved: %v", err)
	}
	f.onRow(t, "name")
	typeIn(f, "Deploy tools")
	if _, err := config.LoadEditable("deploy-tools"); err == nil || f.unfinished == "" || !strings.Contains(f.rowHelp(), "Not added yet") {
		t.Fatalf("named only: not added, kept as a draft, and said: %v, help %q", err, f.rowHelp())
	}
	if d, _ := config.ReadDraft(); d == nil || d.Title != "Deploy tools" {
		t.Fatalf("draft %+v", d)
	}
	f.onRow(t, "0/name")
	typeIn(f, "Everyday")
	f.onRow(t, "0/0/name")
	typeIn(f, "Today")
	f.onRow(t, "0/0/cmd")
	typeIn(f, "date")
	if f.Left != "saved" || f.unfinished != "" { // added the moment it was complete
		t.Fatalf("left %q, unfinished %q", f.Left, f.unfinished)
	}
	press(f, kEsc)
	p, err := config.LoadEditable("deploy-tools")
	if err != nil || p.Title != "Deploy tools" {
		t.Fatalf("%v %+v", err, p)
	}
	if d, _ := config.ReadDraft(); d != nil {
		t.Error("the draft should go once saved")
	}
}

// The form opens on its first row, the pack's name, with no path above it.
func TestPackFormOpensOnTheFirstRow(t *testing.T) {
	f := newTestForm(t)
	if r := f.selectedRow(); r == nil || r.key != "name" {
		t.Errorf("on %+v", r)
	}
	if first := strings.TrimSpace(strings.Split(f.screen(), "\n")[7]); first != "PACK" {
		t.Errorf("the page should start with the pack's card, not %q", first)
	}
}

// / opens the filter, which takes all text and leaves the rows with it in,
// under their headings; esc clears it. Its row is level with the menu's.
func TestPackFormFilters(t *testing.T) {
	f := newTestForm(t)
	lines := strings.Split(f.screen(), "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[3]), "My packs › ") || !strings.HasPrefix(strings.TrimSpace(lines[5]), "❯ / to filter") {
		t.Fatalf("the path and the filter should be on the menu's rows:\n%s", f.screen())
	}
	press(f, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "greet" {
		press(f, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	var keys []string
	for _, r := range f.rows {
		if !r.header {
			keys = append(keys, r.key)
		}
	}
	if strings.Join(keys, " ") != "0/0/desc 0/0/0/q" {
		t.Fatalf("greet should leave the description and <who>, got %v:\n%s", keys, f.screen())
	}
	if p := f.page(); !strings.Contains(p, "MODULE") || !strings.Contains(p, "COMMAND") {
		t.Fatalf("a row keeps its headings:\n%s", p)
	}
	press(f, tea.KeyPressMsg{Code: tea.KeyEscape})
	if f.filtering || f.filter.Value() != "" || len(f.rows) < 20 {
		t.Fatalf("esc should clear it:\n%s", f.screen())
	}
}

// The form's path, filter and first heading are level with the menu's, in the
// pane beside it.
func TestPackFormLinesUpWithTheMenu(t *testing.T) {
	m := New(Options{Config: &config.Config{Sections: []config.Section{{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "a", Cmd: "x"}}}}}})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 40})
	m.showSection(&m.opt.Config.Sections[0])
	menu := strings.Split(ansi.Strip(m.view()), "\n")
	rowOf := func(lines []string, from int, ok func(string) bool) int {
		for i := from; i < len(lines); i++ {
			if ok(strings.TrimSpace(strings.TrimPrefix(lines[i], "▌"))) {
				return i
			}
		}
		return -1
	}
	f := newTestForm(t)
	form := strings.Split(f.screen(), "\n")
	filter := func(l string) bool { return strings.HasPrefix(l, "❯ / to filter") }
	crumb := func(l string) bool { return strings.Contains(l, "›") }
	if a, b := rowOf(menu, 0, crumb), rowOf(form, 0, crumb); a != b || a < 0 {
		t.Errorf("path: the menu's on row %d, the form's on %d", a, b)
	}
	mf, ff := rowOf(menu, 0, filter), rowOf(form, 0, filter)
	if mf != ff || mf < 0 {
		t.Errorf("filter: the menu's on row %d, the form's on %d", mf, ff)
	}
	nonBlank := func(l string) bool { return l != "" }
	if a, b := rowOf(menu, mf+1, nonBlank), rowOf(form, ff+1, nonBlank); a != b {
		t.Errorf("first heading: the menu's on row %d, the form's on %d\n%s", a, b, strings.Join(menu[:10], "\n"))
	}
}

// Back from the pack form, the menu asks nothing: changes were kept as they
// were made. A new pack left unfinished is kept as a draft, and the menu
// says what it still needs.
func TestMenuSaysAnUnfinishedPackIsKept(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	m := New(Options{Config: &config.Config{}, Send: func(string) error { return nil }, Shell: func() ShellState { return ShellState{} }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	p, err := config.NewEditable()
	if err != nil {
		t.Fatal(err)
	}
	p.Title = "Half done"
	if err := p.WriteDraft(); err != nil {
		t.Fatal(err)
	}
	m.form = &pending{editor: true, name: "Create a pack", cmd: "clingon pack create --menu"}
	m.Update(idleMsg{seq: m.runSeq, ok: true})
	if m.mode != modeBrowse || !strings.Contains(m.hint, "Not added yet") || !strings.Contains(m.hint, "Create a pack carries on") {
		t.Fatalf("mode %v, hint %q", m.mode, m.hint)
	}
}

// Scrolling down a long list keeps the row it's for, pinned at the top.
func TestPackFormListKeepsItsQuestion(t *testing.T) {
	f := newTestForm(t)
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	f.onRow(t, "logo")
	press(f, kRight)
	for range 25 {
		press(f, kDown)
	}
	lines := strings.Split(f.screen(), "\n")
	if first := strings.TrimSpace(lines[9]); !strings.Contains(lines[7], "PACK") || !strings.Contains(lines[8], "Pack name") || !strings.Contains(first, "Logo") {
		t.Fatalf("the question should stay at the top, not %q:\n%s", first, f.screen())
	}
	if !strings.Contains(f.screen(), "› ") {
		t.Fatalf("and the list's cursor in view:\n%s", f.screen())
	}
}

// Far down the form, the headings of where you are stay at the top: the
// pack's, the module's and the command's.
func TestPackFormHeadingsStick(t *testing.T) {
	f := newTestForm(t)
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 18})
	f.onRow(t, "0/0/0/start")
	lines := strings.Split(f.screen(), "\n")
	for i, want := range []string{"PACK", "Pack name  ", "MODULE", "Module name  ", "COMMAND", "Command name  "} {
		if !strings.Contains(lines[7+i], want) {
			t.Fatalf("line %d should be %s:\n%s", 7+i, want, f.screen())
		}
	}
	if !strings.Contains(f.screen(), "Default") {
		t.Fatalf("and the cursor's row in view:\n%s", f.screen())
	}
}

// Esc while typing keeps what's typed, never throwing it away; refused, a
// second esc undoes it. Esc on the form leaves it at once: there's nothing to
// ask, every change already written. ← at the top does nothing, nor ctrl+c.
func TestPackFormEscKeeps(t *testing.T) {
	f := newTestForm(t)
	if _, cmd := f.Update(kLeft); cmd != nil {
		t.Fatal("← at the top does nothing: only esc leaves")
	}
	f.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if f.Left != "" {
		t.Fatal("ctrl+c does nothing in the form")
	}
	f.onRow(t, "desc")
	press(f, kRight)
	f.input.SetValue("Kept on esc")
	press(f, kEsc)
	if p, _ := config.LoadEditable("demo"); f.editing != nil || p.Description != "Kept on esc" {
		t.Fatalf("esc keeps what's typed, written at once: editing %v, %q", f.editing != nil, p.Description)
	}
	f.onRow(t, "0/0/name")
	press(f, kRight)
	f.input.SetValue("Bad!")
	press(f, kEsc)
	if f.editing == nil || f.issue == "" || !strings.Contains(f.rowHelp(), "esc again to undo") {
		t.Fatalf("refused: still editing, said why: editing %v, help %q", f.editing != nil, f.rowHelp())
	}
	press(f, kEsc)
	if f.editing != nil || f.pack.Files[0].Group.Commands[0].Name != "Say hello" {
		t.Fatalf("esc again undoes it: %q", f.pack.Files[0].Group.Commands[0].Name)
	}
	if s := f.screen(); strings.Contains(s, "Save changes") || strings.Contains(s, "Discard") {
		t.Errorf("no save box:\n%s", s)
	}
	if _, cmd := f.Update(kEsc); cmd == nil || f.Left != "saved" {
		t.Errorf("esc leaves, its changes written: left %q", f.Left)
	}
}

// The menu's keys: [ ] jump between headings, ? shows the keys, esc cancels
// what's open, and ctrl-c does nothing.
func TestPackFormHasTheMenusKeys(t *testing.T) {
	f := newTestForm(t)
	press(f, tea.KeyPressMsg{Code: ']', Text: "]"})
	if r := f.selectedRow(); r == nil || r.key != "0/name" {
		t.Fatalf("] should go to the module, on %+v", r)
	}
	press(f, tea.KeyPressMsg{Code: '[', Text: "["})
	if r := f.selectedRow(); r == nil || r.key != "name" {
		t.Fatalf("[ should go back to the pack, on %+v", r)
	}
	press(f, tea.KeyPressMsg{Code: '?', Text: "?"})
	if !strings.Contains(f.screen(), "KEYS") {
		t.Fatalf("? should show the keys:\n%s", f.screen())
	}
	press(f, kDown) // any key closes it, and does nothing else
	if r := f.selectedRow(); f.help || r.key != "name" {
		t.Fatal("a key should just close the keys")
	}
	f.onRow(t, "menu")
	press(f, kRight, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if f.choosing == nil || f.Left != "" {
		t.Fatal("ctrl-c should do nothing")
	}
	press(f, kEsc)
	if f.choosing != nil || f.Left != "" {
		t.Fatal("esc should cancel the list, not leave")
	}
}

// Modules and commands open (▾) and collapse (▸) on their headings: → opens,
// ← collapses, enter does either. Collapsed, a part is its heading with its
// name, a module saying how many commands it has. Going to a row in one
// opens it.
func TestPackFormCollapses(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "needs")
	press(f, kDown)
	if r := f.selectedRow(); r == nil || r.key != "0/head" {
		t.Fatalf("down from the pack's last row, onto the module's heading: %+v", r)
	}
	if p := f.page(); !strings.Contains(p, "▾ MODULE") || !strings.Contains(p, "Say hello") {
		t.Fatalf("open to begin with:\n%s", p)
	}
	press(f, kLeft)
	p := f.page()
	if !strings.Contains(p, "▸ MODULE") || !strings.Contains(p, "Examples  · 1 command") || strings.Contains(p, "Say hello") || strings.Contains(p, "A friendly example.") {
		t.Fatalf("← collapses it:\n%s", p)
	}
	if r := f.selectedRow(); r == nil || r.key != "0/head" {
		t.Fatalf("still on its heading: %+v", r)
	}
	press(f, kRight)
	if p := f.page(); !strings.Contains(p, "▾ MODULE") || !strings.Contains(p, "Say hello") {
		t.Fatalf("→ opens it:\n%s", p)
	}
	f.onRow(t, "0/0/head")
	press(f, kLeft)
	if p := f.page(); !strings.Contains(p, "▸ COMMAND") || strings.Contains(p, "Prints a greeting.") || !strings.Contains(p, "Say hello") {
		t.Fatalf("enter collapses a command, its name beside its heading:\n%s", p)
	}
	f.onRow(t, "0/0/name")
	if p := f.page(); !strings.Contains(p, "▾ COMMAND") || !strings.Contains(p, "Prints a greeting.") {
		t.Errorf("going to a row in it opens it:\n%s", p)
	}
}

// A hint is only shown where the cursor is: elsewhere an empty field is a dash.
func TestPackFormHintsWhereYouAre(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "name")
	if p := f.page(); strings.Contains(p, "Programs, separated by commas") {
		t.Errorf("a hint away from the cursor:\n%s", p)
	}
	f.onRow(t, "needs")
	if p := f.page(); !strings.Contains(p, "Programs, separated by commas") {
		t.Errorf("no hint on the cursor's row:\n%s", p)
	}
}

// The preview is the pack's _pack.md, always, and under it the module the
// cursor's in, or was last in; the file the cursor's row writes is the one
// with its lines marked.
func TestPackFormPreviewsItsFile(t *testing.T) {
	f := newTestForm(t)
	f.pack.Files = append(f.pack.Files, config.EditableFile{Slug: "more", Group: config.Group{Name: "More", Commands: []config.Command{{Name: "Ls", Cmd: "ls"}}}})
	f.refresh("name")
	plain := func() string { return ansi.Strip(strings.Join(f.preview(60, 40), "\n")) }
	order := func(p string, names ...string) bool {
		at := 0
		for _, n := range names {
			k := strings.Index(p[at:], n)
			if k < 0 {
				return false
			}
			at += k
		}
		return true
	}
	f.onRow(t, "name")
	if p := plain(); !order(p, "_pack.md", "# Demo", "examples.md", "## Say hello") {
		t.Errorf("on the pack's rows, the pack and its first module:\n%s", p)
	}
	f.onRow(t, "1/name")
	if p := plain(); !order(p, "_pack.md", "# Demo", "more.md", "# More") || strings.Contains(p, "examples.md") {
		t.Errorf("in the second module, it under the pack:\n%s", p)
	}
	f.onRow(t, "needs")
	if p := plain(); !strings.Contains(p, "more.md") {
		t.Errorf("back on the pack's rows, the module last in:\n%s", p)
	}
	f.onRow(t, "1/0/cmd")
	got := strings.Join(f.preview(60, 40), "\n")
	if bar, mod := strings.Index(got, cursorBar(cursorStyle)), strings.Index(got, "more.md"); strings.Count(got, cursorBar(cursorStyle)) != 1 || bar < mod {
		t.Errorf("the command's line marked, in the module's file only:\n%s", got)
	}
}

// The preview wraps a long line rather than cutting it off: prose plainly, a
// list item's rows under its text, a code line's after a faint ↪ so they
// aren't taken for lines of their own. A marked line is marked on every row.
func TestPackFormPreviewWraps(t *testing.T) {
	f := newTestForm(t)
	c := &f.pack.Files[0].Group.Commands[0]
	c.Desc = strings.TrimSpace(strings.Repeat("greets ", 12)) + " end."
	c.Cmd = `echo "` + strings.Repeat("hello ", 10) + `<who>."`
	c.Args[0].Prompt = strings.TrimSpace(strings.Repeat("who ", 12)) + " last"
	f.refresh("")
	f.onRow(t, "0/0/desc")
	got := f.preview(30, 60)
	plain := ansi.Strip(strings.Join(got, "\n"))
	for _, l := range strings.Split(plain, "\n") {
		if ansi.StringWidth(l) > 30+panelPad {
			t.Errorf("a row wider than the pane: %q", l)
		}
	}
	if strings.Contains(plain, "…") || !strings.Contains(plain, "end.") || !strings.Contains(plain, "last") || !strings.Contains(plain, `<who>."`) {
		t.Errorf("nothing cut off:\n%s", plain)
	}
	if !strings.Contains(plain, "↪ ") {
		t.Errorf("a code line's rows go on after ↪:\n%s", plain)
	}
	if n := strings.Count(strings.Join(got, "\n"), cursorBar(cursorStyle)); n < 2 {
		t.Errorf("every row of the marked description marked, got %d:\n%s", n, plain)
	}
	lines := strings.Split(plain, "\n")
	for i, l := range lines {
		if strings.Contains(l, "`<who>`") && i+1 < len(lines) {
			at := strings.Index(l, "`<who>`")
			if next := lines[i+1]; len(next) <= at || strings.TrimSpace(next[:at]) != "" || next[at] == ' ' {
				t.Errorf("a list item's rows hang under its text:\n%s\n%s", l, next)
			}
		}
	}
}

// The form's foot says what the row the cursor's on is for, over a second
// rule with the save question under it, and is as tall as the menu's info
// pane, so nothing below the rows moves.
func TestPackFormFootSaysWhatARowIsFor(t *testing.T) {
	f := newTestForm(t)
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	f.onRow(t, "0/0/risk")
	lines := strings.Split(ansi.Strip(f.view()), "\n")
	var rules []int
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "────") {
			rules = append(rules, i)
		}
	}
	if len(rules) != 1 {
		t.Fatalf("one rule over the foot, got %d:\n%s", len(rules), strings.Join(lines, "\n"))
	}
	if under := strings.Join(lines[rules[0]:], " "); !strings.Contains(under, "Whether clingon asks before it runs") || strings.Contains(under, "SAVE") {
		t.Errorf("what the row is for, under the rule, and no save box:\n%s", strings.Join(lines[rules[0]:], "\n"))
	}
	if got := len(lines) - rules[0]; got != infoTop(40) {
		t.Errorf("the foot as tall as the menu's info pane: %d, want %d", got, infoTop(40))
	}
}

// Values start in the same column whatever's open: a command opening, with
// its labels further in, doesn't move them.
func TestPackFormValuesStayPut(t *testing.T) {
	f := newTestForm(t)
	col := func() int {
		for _, l := range strings.Split(f.page(), "\n") {
			if strings.Contains(l, "Pack name") && strings.Contains(l, "Demo") {
				return ansi.StringWidth(l[:strings.Index(l, "Demo")]) // in cells: the cursor's bar is a character
			}
		}
		t.Fatalf("no pack name in:\n%s", f.page())
		return 0
	}
	f.onRow(t, "name")
	before := col()
	f.onRow(t, "0/0/name")
	if after := col(); after != before {
		t.Errorf("the pack's name moved from column %d to %d", before, after)
	}
}

// The form tells the menu its pack's name as it changes, and the menu
// heading it's under, and forgets them once closed.
func TestPackFormTellsTheMenuItsName(t *testing.T) {
	f := newTestForm(t)
	if got, place := config.FormName(); got != "Demo" || place == "" {
		t.Errorf("opened: %q under %q", got, place)
	}
	f.onRow(t, "name")
	typeIn(f, "Deploy tools")
	if got, _ := config.FormName(); got != "Deploy tools" {
		t.Errorf("renamed: %q", got)
	}
}

// Each field marks the lines of the file it writes, and only those.
// A section is one clingon suggests, or one of your own: + New section, at
// the end of the list, types one on the row, starting empty.
func TestPackFormSectionOfYourOwn(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "menu")
	press(f, kRight)
	last := f.options[len(f.options)-1]
	if f.options[0].Value != "ai" || last.Label != "+ New menu" {
		t.Fatalf("options: %+v", f.options)
	}
	press(f, kLeft)
	f.pick(t, newSection)
	if f.editing == nil || f.input.Value() != "" {
		t.Fatalf("a blank box on the row: %+v", f.editing)
	}
	f.input.SetValue("Hello There!")
	press(f, kEnter)
	if f.pack.Section != "hello-there" || !strings.Contains(f.pack.Markdown()[config.PackInfoFile], "menu: hello-there\n") {
		t.Fatalf("section %q", f.pack.Section)
	}
	// Enter moved on to the next field; back on Menu, it's in the list from
	// then on, and the list opens at its top, as every list does.
	if r := f.selectedRow(); r == nil || r.key != "logo" {
		t.Errorf("enter should move on to the next field, on %+v", r)
	}
	f.onRow(t, "menu")
	press(f, kRight)
	if f.opt != 0 || !slices.ContainsFunc(f.options, func(o config.Option) bool { return o.Value == "hello-there" }) {
		t.Errorf("on %d of %+v", f.opt, f.options)
	}
	press(f, kLeft)
	// A name it can't be is refused, saying why.
	f.pick(t, newSection)
	f.input.SetValue("clingon")
	press(f, kEnter)
	if f.pack.Section != "hello-there" || !strings.Contains(f.issue, "clingon's own heading") {
		t.Errorf("section %q, issue %q", f.pack.Section, f.issue)
	}
}

// Any number of a select-many blank's choices can be picked to begin with,
// even ones with a space in them.
func TestPackFormTicksSeveral(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/0/kind")
	f.pick(t, "flags")
	twoOptions(f, 0, 0, 0)
	a := &f.pack.Files[0].Group.Commands[0].Args[0]
	a.Options[0].Value = "-n 5"
	for _, o := range []string{"0/0/0/0/pick", "0/0/0/1/pick"} {
		f.onRow(t, o)
		f.pick(t, "yes")
	}
	if md := f.pack.Markdown()["examples.md"]; !strings.Contains(md, "  - [x] `-n 5`\n  - [x] `two`\n") {
		t.Fatalf("both ticked:\n%s", md)
	}
	f.onRow(t, "0/0/0/0/pick")
	f.pick(t, "no")
	if a.Default != "two" {
		t.Errorf("unticked: %q", a.Default)
	}
}

// A value is coloured as the preview colours it in the Markdown: a name as a
// heading, the command as code (its blanks pink), a choice as what it types.
func TestPackFormColoursAsTheMarkdown(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/0/kind")
	f.pick(t, "flags")
	twoOptions(f, 0, 0, 0)
	for key, want := range map[string]string{
		"name":     "\x1b[1;38;5;99mDemo",
		"0/0/name": "\x1b[1;38;5;99mSay hello",
		"0/0/cmd":  "\x1b[38;5;117m<who>",
		"0/0/0/0":  "\x1b[38;5;180mone",
		"logo":     fieldStyle.Render("Rocket"), // plain in the file: the form's white
	} {
		f.onRow(t, key)
		r := f.selectedRow()
		if got := f.rowLine(r, false, false, 20, 100); !strings.Contains(got, want) {
			t.Errorf("%s: want %q in %q", key, want, got)
		}
	}
}

// A command's blanks are rows of its own, each opening and collapsing on its
// What row, arrow and all, as a heading does: ← collapses it to a line saying
// what it is, → opens it again, and then → renames it, as on any row.
func TestPackFormArgs(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/0/kind")
	f.pick(t, "choose")
	twoOptions(f, 0, 0, 0)
	f.onRow(t, "0/0/cmd")
	press(f, kDown)
	if r := f.selectedRow(); r == nil || r.key != "0/0/0/name" {
		t.Fatalf("down from Runs, onto the blank: %+v", r)
	}
	page := f.page()
	col := func(label string) int {
		at := strings.Index(page, label)
		return ansi.StringWidth(page[strings.LastIndex(page[:at], "\n")+1 : at])
	}
	if !strings.Contains(page, "▾ Argument") || col("Command") != col("Argument") || col("Type") != col("Command") || strings.Contains(page, "Args") {
		t.Fatalf("Argument, with its arrow, then its rows, all in line with Command:\n%s", page)
	}
	press(f, kLeft)
	if p := f.page(); !strings.Contains(p, "▸ Argument") || !strings.Contains(p, "<who>  · Select One, 2 options") || strings.Contains(p, "Who to greet") {
		t.Fatalf("← collapses it:\n%s", p)
	}
	press(f, kRight)
	if p := f.page(); !strings.Contains(p, "▾ Argument") || !strings.Contains(p, "Who to greet") || f.editing != nil {
		t.Fatalf("→ opens it:\n%s", p)
	}
	press(f, kRight)
	if f.editing == nil || f.editing.key != "0/0/0/name" {
		t.Errorf("open, → renames it")
	}
}

// Renaming a blank renames it in the command too; a name a blank can't have,
// or another blank's, is refused.
func TestPackFormRenamesABlank(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/0/name")
	typeIn(f, "<name>")
	c := f.pack.Files[0].Group.Commands[0]
	if c.Cmd != `echo "Hello, <name>."` || c.Args[0].Name != "name" || c.Args[0].Prompt != "Who to greet" {
		t.Fatalf("renamed: %q %+v", c.Cmd, c.Args[0])
	}
	f.onRow(t, "0/0/0/name")
	typeIn(f, "two words")
	if f.issue == "" || f.pack.Files[0].Group.Commands[0].Args[0].Name != "name" {
		t.Errorf("refused: %q", f.issue)
	}
}

func TestPackFormMarksItsLines(t *testing.T) {
	pack := strings.Split("---\nmenu: code\nlogo: rocket\ntint: orange\n---\n# Demo\nTools.\n", "\n")
	module := strings.Split("---\nwhen: Dockerfile\n---\n# Deploy\nShip things.\n\n"+
		"## Build\n\n```sh\ndocker build .\n```\n\n"+
		"## Push it\n> [!WARNING]\n> Pushes.\n\n```sh needs=docker\ndocker push <image> <tag>\n```\n\n"+
		"- `<image>` Which image\n- `<tag>` Which tag\n  - (x) `latest`\n  - ( ) `dev`\n", "\n")
	for _, c := range []struct {
		key   string
		lines []string
		want  string // the marked lines, joined
	}{
		{"name", pack, "# Demo"},
		{"desc", pack, "Tools."},
		{"tint", pack, "tint: orange"},
		{"menu", pack, "menu: code"},
		{"0/when", module, "when: Dockerfile"},
		{"0/desc", module, "Ship things."},
		{"0/0/name", module, "## Build"},
		{"0/1/risk", module, "> [!WARNING]"},
		{"0/1/desc", module, "> Pushes."},
		{"0/1/cmd", module, "docker push <image> <tag>"},
		{"0/1/needs", module, "```sh needs=docker"},
		{"0/1/0/q", module, "- `<image>` Which image"},
		{"0/1/1/kind", module, "- `<tag>` Which tag|  - (x) `latest`|  - ( ) `dev`"},
		{"0/1/1/1", module, "  - ( ) `dev`"},
		{"0/0/remove", module, "## Build||```sh|docker build .|```"},
	} {
		on := marked(c.key, c.lines)
		var got []string
		for i, l := range c.lines {
			if on[i] {
				got = append(got, l)
			}
		}
		if strings.Join(got, "|") != c.want {
			t.Errorf("%s: marked %q, want %q", c.key, strings.Join(got, "|"), c.want)
		}
	}
}

// One space after a glyph that fills its cell, as the menu has after ⚠ and ☠;
// two after a logo, drawn across two cells.
func TestIconGap(t *testing.T) {
	for _, g := range []string{warnGlyph, dangerGlyph, " "} {
		if iconGap(g) != " " {
			t.Errorf("%q: %q", g, iconGap(g))
		}
	}
	for n, g := range config.Logos {
		if iconGap(g) != "  " {
			t.Errorf("logo %s: %q", n, iconGap(g))
		}
	}
}

// Enter does as → does: on a row it edits, and typing it's done, keeping
// what's typed, as esc is. A value is one line, a long one wrapping as it's
// typed and the same once it's left; anything pasted over lines is joined.
func TestPackFormLines(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/desc")
	press(f, kEnter)
	if f.editing == nil {
		t.Fatal("enter on a row should edit it")
	}
	f.input.SetValue("Prints\na greeting.")
	press(f, kEnter)
	if c := f.pack.Files[0].Group.Commands[0]; f.editing != nil || c.Desc != "Prints a greeting." {
		t.Fatalf("enter is done, on one line: desc %q, still editing %v", c.Desc, f.editing != nil)
	}

	f.onRow(t, "0/desc")
	press(f, kRight)
	f.input.SetValue(strings.TrimSpace(strings.Repeat("word ", 30)))
	words := func() []string {
		var out []string
		for _, l := range strings.Split(f.page(), "\n") {
			if strings.Contains(l, "word") {
				out = append(out, strings.TrimRight(l, " "))
			}
		}
		return out
	}
	typing := words()
	press(f, kEnter)
	if done := words(); len(done) < 2 || strings.Join(done, "\n") != strings.Join(typing, "\n") {
		t.Errorf("a long value wraps, as it did typing it:\n%s\n--\n%s", strings.Join(typing, "\n"), strings.Join(done, "\n"))
	}
	if d := f.pack.Files[0].Group.Description; strings.Contains(d, "\n") || !strings.HasPrefix(d, "word word") {
		t.Errorf("kept on one line: %q", d)
	}
}

// Pasted text goes into the box that's open, as if typed there: a field
// being edited (lines joined) or the filter. With no box open a paste does
// nothing, so a pasted ? or / doesn't act as a key.
func TestPackFormPaste(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/cmd")
	press(f, kEnter)
	f.input.SetValue("")
	f.Update(tea.PasteMsg{Content: "git log\n--oneline"})
	press(f, kEnter)
	if c := f.pack.Files[0].Group.Commands[0].Cmd; c != "git log --oneline" {
		t.Errorf("pasted into the command: %q", c)
	}

	f.Update(tea.PasteMsg{Content: "?"})
	if f.help || f.filtering || f.editing != nil {
		t.Error("a paste with no box open should do nothing")
	}

	press(f, tea.KeyPressMsg{Code: '/', Text: "/"})
	f.Update(tea.PasteMsg{Content: "git"})
	if f.filter.Value() != "git" {
		t.Errorf("pasted into the filter: %q", f.filter.Value())
	}
}

// The arrows move through what's typed and then on out of it, as they move
// everywhere: ← past its start and → past its end leave the field, keeping
// what's typed.
func TestPackFormArrowsLeaveTheText(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/name")
	press(f, kRight) // in, at its end
	f.input.InsertString("!x")
	press(f, kLeft, kLeft)
	if f.editing == nil || f.input.Column() != len("Say hello") {
		t.Fatalf("← moves back through the text: editing %v, at %d", f.editing != nil, f.input.Column())
	}
	f.input.SetValue("Hi there")
	f.input.CursorStart()
	press(f, kRight)
	if f.editing == nil {
		t.Fatal("→ inside the text moves through it")
	}
	f.input.CursorStart()
	press(f, kLeft)
	if f.editing != nil || f.pack.Files[0].Group.Commands[0].Name != "Hi there" {
		t.Fatalf("← past its start leaves, keeping it: %q", f.pack.Files[0].Group.Commands[0].Name)
	}
	press(f, kRight)
	f.input.SetValue("Hello")
	f.input.MoveToEnd()
	press(f, kRight)
	if f.editing != nil || f.pack.Files[0].Group.Commands[0].Name != "Hello" {
		t.Errorf("→ past its end leaves, keeping it: %q", f.pack.Files[0].Group.Commands[0].Name)
	}
}

// A choice is an Opts row, collapsed to start with, saying what it types, its
// label and whether it's picked; → opens it to its Runs, Desc and Pick rows,
// and ← collapses it again, as a blank's What row does.
func TestPackFormOpts(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/0/kind")
	f.pick(t, "choose")
	twoOptions(f, 0, 0, 0)
	a := &f.pack.Files[0].Group.Commands[0].Args[0]
	a.Options[0].Label, a.Default = "the first", "one"
	f.refresh("0/0/0/0")
	if p := f.page(); !strings.Contains(p, "▸ Option        one  · the first · Default") || strings.Contains(p, "Default       Yes") {
		t.Fatalf("collapsed, a line saying it all:\n%s", p)
	}
	press(f, kRight)
	p := f.page()
	if !strings.Contains(p, "▾ Option        one") || !strings.Contains(p, "Value         one") || !strings.Contains(p, "Label         the first") || !strings.Contains(p, "Default       Yes") {
		t.Fatalf("→ opens it:\n%s", p)
	}
	f.onRow(t, "0/0/0/0/types")
	typeIn(f, "uno")
	if a.Options[0].Value != "uno" || a.Default != "uno" {
		t.Errorf("what it types, and picked still: %+v %q", a.Options[0], a.Default)
	}
	f.onRow(t, "0/0/0/0")
	press(f, kLeft)
	if p := f.page(); !strings.Contains(p, "▸ Option        uno") || strings.Contains(p, "Value     uno") {
		t.Errorf("← collapses it:\n%s", p)
	}
	f.onRow(t, "0/0/0/1/remove")
	press(f, kRight, kDown, kRight) // Yes
	if len(a.Options) != 1 {
		t.Errorf("removed: %+v", a.Options)
	}
}

// With more commands than the preview has room for, it shows the one the
// cursor's in, even on a row that writes nothing there yet (no risk, no
// description): scrolled to its heading. On Add a command, the file's end,
// where it goes.
func TestPackFormPreviewFollows(t *testing.T) {
	f := newTestForm(t)
	g := &f.pack.Files[0].Group
	for n := 2; n <= 12; n++ {
		g.Commands = append(g.Commands, config.Command{Name: fmt.Sprintf("Command %d", n), Cmd: "echo hello"})
	}
	f.refresh("0/11/risk")
	for _, key := range []string{"0/11/risk", "0/11/desc", "0/11/name", "0/add"} {
		f.onRow(t, key)
		if p := ansi.Strip(strings.Join(f.preview(60, 30), "\n")); !strings.Contains(p, "## Command 12") {
			t.Errorf("%s: the cursor's command should be in view:\n%s", key, p)
		}
	}
}

// An add is a ghost of what it adds, where that will be: + ADD COMMAND at the end
// of its module, + ADD MODULE after the last, + Opts after a blank's choices.
// Taking one adds it, empty with its hint, the cursor on its name, not typing.
func TestPackFormGhostAdds(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/0/kind")
	f.pick(t, "choose")
	twoOptions(f, 0, 0, 0)
	p := f.page()
	for _, want := range []string{"+ ADD COMMAND", "+ ADD MODULE", "+ Option"} {
		if !strings.Contains(p, want) {
			t.Errorf("no %q in:\n%s", want, p)
		}
	}
	if strings.Contains(p, "Add a") {
		t.Errorf("the adds should be ghosts, not sentences:\n%s", p)
	}
	n := len(f.pack.Files[0].Group.Commands)
	f.onRow(t, "0/add")
	press(f, kRight)
	r := f.selectedRow()
	if len(f.pack.Files[0].Group.Commands) != n+1 || r == nil || r.key != fmt.Sprintf("0/%d/name", n) || r.value != "" || r.hint == "" || f.editing != nil {
		t.Fatalf("added, empty with its hint, on its name, not typing: %+v, editing %v", r, f.editing != nil)
	}
}

// A long list shows ten of its options at a time, following the cursor, and
// says how many more there are, as the menu's lists do.
func TestPackFormLongList(t *testing.T) {
	f := newTestForm(t)
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 60})
	f.onRow(t, "logo")
	press(f, kRight)
	count := func(p string) int {
		n := 0
		for _, l := range config.PackLogos {
			if strings.Contains(p, " "+config.TitleWords(l)+"\n") || strings.HasSuffix(strings.TrimRight(p, " "), config.TitleWords(l)) {
				n++
			}
		}
		return n
	}
	lines := func() string {
		var out []string
		for _, l := range strings.Split(f.page(), "\n") {
			out = append(out, strings.TrimRight(l, " "))
		}
		return strings.Join(out, "\n")
	}
	p := lines()
	if n := count(p); n != 10 || !strings.Contains(p, fmt.Sprintf("↓ %d more", len(config.PackLogos)-10)) {
		t.Fatalf("ten at a time, %d shown:\n%s", n, p)
	}
	for range len(config.PackLogos) - 1 {
		press(f, kDown)
	}
	if p := lines(); !strings.Contains(p, "↑ ") || !strings.Contains(p, config.TitleWords(config.PackLogos[len(config.PackLogos)-1])) {
		t.Errorf("at its end, the last in view, and what's above:\n%s", p)
	}
	press(f, kDown) // round to the first
	if f.opt != 0 {
		t.Errorf("↓ on the last option goes round to the first: on %d", f.opt)
	}
	press(f, kUp)
	if f.opt != len(f.options)-1 {
		t.Errorf("↑ on the first option goes round to the last: on %d", f.opt)
	}
}

// The form's foot has a rule level with the menu's info pane's, beside it, at
// any height: the two panes read as one. (The form's foot has a second rule,
// over its save question, below it.)
func TestPackFormRuleLevelWithTheMenus(t *testing.T) {
	ruleAt := func(lines []string, w int) int {
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(ansi.Strip(ansi.Cut(l, 0, w))), "────") {
				return i
			}
		}
		return -1
	}
	for _, h := range []int{20, 30, 45} {
		sec := config.Section{Name: "S", Icon: "•", Category: config.Other, Commands: []config.Command{{Name: "Edit a pack", Cmd: "x"}}}
		m := New(Options{Config: &config.Config{Sections: []config.Section{sec}}, Bar: func(string) {}, BarWidth: func() int { return 0 }})
		m.Update(tea.WindowSizeMsg{Width: 60, Height: h})
		m.showSection(&m.opt.Config.Sections[0])
		m.busy, m.editing, m.form, m.blurred = "Edit a pack", "Demo · esc when done", &pending{}, true
		f := newTestForm(t)
		f.Update(tea.WindowSizeMsg{Width: 70, Height: h})
		menu, form := ruleAt(strings.Split(m.view(), "\n"), 60), ruleAt(strings.Split(f.view(), "\n"), 70)
		if menu < 0 || menu != form {
			t.Errorf("height %d: the menu's rule on line %d, the form's on %d", h, menu, form)
		}
	}
}

// The form tells the menu's status bar the keys that work right now, and
// only those: on a heading, on a field, typing, in a list, asking to save.
func TestPackFormTellsItsKeys(t *testing.T) {
	f := newTestForm(t)
	has := func(when string, want ...string) {
		t.Helper()
		got := config.FormKeys()
		for _, w := range want {
			if !slices.Contains(got, w) {
				t.Errorf("%s: no %q in %q", when, w, got)
			}
		}
	}
	f.onRow(t, "0/head")
	press(f, kDown, kUp)
	has("a heading", "→ go", "← back", "esc done")
	has("shift-tab, back to the menu", "shift-tab menu")
	press(f, kDown)
	has("a field", "→ go", "/ filter")
	press(f, kRight)
	has("typing", "→ go", "← back", "enter next", "esc keep")
	press(f, kEsc)
	f.onRow(t, "menu")
	press(f, kDown, kUp, kRight)
	has("a list", "↑↓ move", "→ go", "← back")
	press(f, kLeft)
}

// A filter left on, closed, says how to edit and clear it on the status bar,
// as the menu's does: esc clears it before it's done.
func TestPackFormFilterKeys(t *testing.T) {
	f := newTestForm(t)
	for _, r := range "/greet/" {
		press(f, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := config.FormKeys(); !slices.Contains(got, "/ edit filter") || !slices.Contains(got, "esc clear") || slices.Contains(got, "esc done") {
		t.Errorf("filtered: %q", got)
	}
}

// ↑ and ↓ go round the form's ends, as every list does; a page (pgup,
// pgdown) stops at them, and so do the save question's answers go round.
func TestPackFormWraps(t *testing.T) {
	f := newTestForm(t)
	f.sel = -1
	f.step(1) // the first row the cursor can be on
	first := f.sel
	press(f, kUp)
	last := f.sel
	if last == first {
		t.Fatalf("↑ on the first row should go round to the last")
	}
	press(f, kDown)
	if f.sel != first {
		t.Errorf("↓ on the last row should go round to the first: on %d, want %d", f.sel, first)
	}
	press(f, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if f.sel != first {
		t.Errorf("pgup at the top stops: on %d", f.sel)
	}
}

// Rows scrolled away under the pinned headings are marked, not just gone;
// the row the cursor's on stays in view.
func TestScrollMarksRowsAbove(t *testing.T) {
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("row %d", i))
	}
	heads := []formHead{{line: 0, depth: 0, name: 1}, {line: 10, depth: 2, name: 11}}
	out := scroll(lines, 35, 35, 12, false, heads)
	got := ansi.Strip(strings.Join(out, "\n"))
	if !strings.Contains(got, "row 10\nrow 11\n"+strings.TrimRight(moreAbove, " ")) || !strings.Contains(got, "row 35") || len(out) != 12 {
		t.Errorf("pinned headings, the mark, then the rows around the cursor:\n%s", got)
	}
	if out := scroll(lines[:8], 3, 3, 12, false, heads); strings.Contains(strings.Join(out, "\n"), "more above") {
		t.Errorf("nothing scrolled away: no mark")
	}
}

// Tab and shift-tab move focus out of the form, as from any panel: tab to
// the panel after it (the AI's), shift-tab back to the menu; what's being
// typed is kept first. Enter, done typing, moves on to the next field.
func TestPackFormTabMovesOn(t *testing.T) {
	f := newTestForm(t)
	var moves []bool
	panel := true
	f.Focus = func(next bool) bool { moves = append(moves, next); return !next || panel }
	f.onRow(t, "desc")
	press(f, tea.KeyPressMsg{Code: tea.KeyTab})
	press(f, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if len(moves) != 2 || !moves[0] || moves[1] || f.selectedRow().key != "desc" {
		t.Fatalf("tab to the next panel, shift-tab to the menu, the cursor where it was: %v, on %q", moves, f.selectedRow().key)
	}
	press(f, kRight)
	f.input.SetValue("Kept on tab")
	press(f, tea.KeyPressMsg{Code: tea.KeyTab})
	if f.editing != nil || f.pack.Description != "Kept on tab" || len(moves) != 3 {
		t.Errorf("what's typed is kept: %q", f.pack.Description)
	}
	panel = false
	press(f, tea.KeyPressMsg{Code: tea.KeyTab})
	if !strings.Contains(f.issue, "no panel after the form") {
		t.Errorf("no panel: said so, %q", f.issue)
	}
	f.onRow(t, "name")
	press(f, kRight)
	press(f, kEnter)
	if r := f.selectedRow(); r.key != "desc" || f.editing != nil {
		t.Errorf("enter keeps it and moves on: on %q", r.key)
	}
}

// A change that's refused is said in full under the form, with how to go on;
// the field stays open.
func TestPackFormSaysWhyInFull(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/cmd")
	press(f, kRight)
	f.input.SetValue(`echo "Hello, <who>!"`)
	press(f, kEnter)
	if f.editing == nil || f.issue == "" {
		t.Fatalf("the ! should be refused, the field still open: issue %q", f.issue)
	}
	if help := f.rowHelp(); !strings.Contains(help, f.issue[:min(20, len(f.issue))]) || !strings.Contains(help, "Still editing") {
		t.Errorf("under the form: %q", help)
	}
	if !strings.Contains(strings.Join(strings.Fields(f.screen()), " "), "Still editing") {
		t.Errorf("shown:\n%s", f.screen())
	}
}

// Typing on a field starts editing it, what's typed in place of what was
// there; / still opens the filter, and a list's row doesn't take text.
func TestPackFormTypingEdits(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "name")
	f.Update(typed("Handy"))
	if f.editing == nil || f.input.Value() != "Handy" {
		t.Fatalf("typing should edit the name, in place of Demo: editing %v, %q", f.editing != nil, f.input.Value())
	}
	press(f, kEnter)
	if f.pack.Title != "Handy" {
		t.Errorf("title %q", f.pack.Title)
	}
	f.onRow(t, "desc")
	press(f, tea.KeyPressMsg{Code: '/', Text: "/"})
	if !f.filtering || f.editing != nil {
		t.Errorf("/ filters: filtering %v, editing %v", f.filtering, f.editing != nil)
	}
	press(f, kEsc)
	f.onRow(t, "menu")
	f.Update(typed("x"))
	if f.editing != nil {
		t.Errorf("a list's row isn't typed into")
	}
	if keys := f.barKeys(); slices.Contains(keys, "type to edit") {
		t.Errorf("not on a list: %q", keys)
	}
	f.onRow(t, "desc")
	if keys := f.barKeys(); !slices.Contains(keys, "type to edit") {
		t.Errorf("on a field: %q", keys)
	}
}

// A blank's question is called Question.
func TestPackFormCallsItQuestion(t *testing.T) {
	f := newTestForm(t)
	if !strings.Contains(f.page(), "Question") || strings.Contains(f.page(), "Description  Who to greet") {
		t.Errorf("a blank's question is called Question:\n%s", f.page())
	}
}

// From the moment it opens, the form tells the menu the keys of the row its
// cursor's on: on the first field, typing edits it.
func TestPackFormTellsTheFirstRowsKeys(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	f, err := NewPackFormForNew(true)
	if err != nil {
		t.Fatal(err)
	}
	_ = f
	if keys := config.FormKeys(); !slices.Contains(keys, "type to edit") || !slices.Contains(keys, "→ go") {
		t.Errorf("on opening: %q", keys)
	}
}

// The preview shows a field as it's typed, before enter; the pack itself
// changes only once it's kept.
func TestPackFormPreviewsAsYouType(t *testing.T) {
	f := newTestForm(t)
	f.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	f.onRow(t, "0/0/cmd")
	press(f, kRight)
	f.input.SetValue(`echo "Hi, <name>"`)
	if got := ansi.Strip(strings.Join(f.preview(60, 36), "\n")); !strings.Contains(got, `echo "Hi, <name>"`) || !strings.Contains(got, "<name>") {
		t.Errorf("the preview as typed:\n%s", got)
	}
	if c := f.pack.Files[0].Group.Commands[0]; c.Cmd != `echo "Hello, <who>."` {
		t.Errorf("not changed till it's kept: %q", c.Cmd)
	}
}

// The add rows are found by what they say: "add command" finds + ADD COMMAND.
func TestPackFormFiltersToTheAdds(t *testing.T) {
	f := newTestForm(t)
	press(f, tea.KeyPressMsg{Code: '/', Text: "/"})
	f.Update(typed("add command"))
	found := false
	for _, r := range f.rows {
		found = found || r.key == "0/add"
	}
	if !found {
		t.Errorf("no + ADD COMMAND row in:\n%s", f.page())
	}
}

// Filtering for a field's name puts the cursor on that field, not on the
// first row its heading keeps: "runs" is what a command runs, and "command"
// alone is the command's name, the field that has it in its own label.
func TestPackFormFilterGoesToTheNamedRow(t *testing.T) {
	f := newTestForm(t)
	press(f, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "runs" {
		press(f, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if r := f.selectedRow(); r == nil || r.label != "Runs" {
		t.Fatalf("on %+v:\n%s", r, f.screen())
	}
	press(f, kEsc, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "command" {
		press(f, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if r := f.selectedRow(); r == nil || r.label != "Command name" {
		t.Fatalf("command: on %+v:\n%s", r, f.screen())
	}
	press(f, kEsc, tea.KeyPressMsg{Code: '/', Text: "/"}) // a field's name started is enough
	for _, r := range "desc" {
		press(f, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if r := f.selectedRow(); r == nil || r.label != "Description" {
		t.Fatalf("desc: on %+v:\n%s", r, f.screen())
	}
}

// Words narrow it: "module name" is the module's Name, not the pack's or the
// command's, and the cursor goes there.
func TestPackFormFilterTakesWords(t *testing.T) {
	f := newTestForm(t)
	press(f, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "module name" {
		press(f, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	var names []string
	for _, r := range f.rows {
		if !r.header && strings.HasSuffix(r.label, " name") {
			names = append(names, r.key)
		}
	}
	if r := f.selectedRow(); len(names) != 1 || r == nil || r.key != names[0] || r.depth != 1 {
		t.Fatalf("names %v, on %+v:\n%s", names, r, f.screen())
	}
	press(f, kEsc, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "command name" {
		press(f, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if r := f.selectedRow(); r == nil || r.label != "Command name" || r.depth != 2 {
		t.Fatalf("command name: on %+v:\n%s", r, f.screen())
	}
}

// The form says where your keys are going, for the AI, which sees it as
// text without the text cursor: typing into a field (so / is typed too), in
// the filter, or a filter left on with its box closed. The bar doesn't say it.
func TestPackFormSaysWhereKeysGo(t *testing.T) {
	f := newTestForm(t)
	state := func(want string) {
		t.Helper()
		if got := config.FormState(); !strings.Contains(got, want) {
			t.Fatalf("state %q, want %q", got, want)
		}
		for _, k := range config.FormKeys() {
			if strings.HasPrefix(k, config.FormStatePrefix) {
				t.Fatalf("the state's in the bar's keys: %q", k)
			}
		}
	}
	press(f, kDown) // off the first row and back, so the keys are said
	press(f, kUp)
	state("on the rows")
	press(f, tea.KeyPressMsg{Code: 'x', Text: "x"})
	state("typing into the pack name: every key is typed into it, / included")
	press(f, kEnter, tea.KeyPressMsg{Code: '/', Text: "/"})
	state("typing in the filter")
	for _, r := range "module name" {
		press(f, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	press(f, tea.KeyPressMsg{Code: '/', Text: "/"})
	state(`the filter "module name" is on, its box closed`)
	press(f, tea.KeyPressMsg{Code: '/', Text: "/"}, kEnter) // enter in the filter: editing the field it's on
	state("typing into the module name")
}

// The form lines up with the menu beside it: its foot's rule on the row of
// the menu's info pane's, even with the AI's strip under the shell making it
// shorter; the preview's title on the row of the form's path; and the form
// edge to edge, as the menu's lists are.
func TestPackFormFootAndPreviewLineUp(t *testing.T) {
	f := newTestForm(t)
	const menuH = 40
	f.MenuHeight = func() int { return menuH }
	f.Update(tea.WindowSizeMsg{Width: 140, Height: menuH - 2}) // the AI's strip and its border under it
	lines := strings.Split(f.screen(), "\n")
	formW := 140 / 2
	ruleAt := -1
	for i, l := range lines {
		r := []rune(l)
		if len(r) >= formW && strings.Trim(string(r[:formW]), "─") == "" {
			ruleAt = i
		}
	}
	if want := menuH - infoTop(menuH); ruleAt != want {
		t.Errorf("the foot's rule on row %d, the menu's on %d:\n%s", ruleAt, want, f.screen())
	}
	crumbAt, titleAt := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "My packs") {
			crumbAt = i
		}
		if strings.Contains(l, "_pack.md") && titleAt < 0 {
			titleAt = i
		}
	}
	if crumbAt != 3 || titleAt != crumbAt {
		t.Errorf("path on row %d (want 3, at the edge), preview's title on %d:\n%s", crumbAt, titleAt, f.screen())
	}
}

// While the AI drives the form, what a row's for (for people) goes: no rule,
// no help, and the form has the rows. Back with people, it's there again.
func TestPackFormHidesHelpForTheAI(t *testing.T) {
	f := newTestForm(t)
	working := true
	f.AIWorking = func() bool { return working }
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	if s := f.screen(); strings.Contains(s, "What the menu calls the pack") || strings.Contains(s, strings.Repeat("─", 30)) {
		t.Errorf("the AI driving: no help, no rule:\n%s", s)
	}
	working = false
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	if s := f.screen(); !strings.Contains(s, "What the menu calls the pack") {
		t.Errorf("people again: the help's back:\n%s", s)
	}
}

// Risk reads as it does in the menu: a risky command's name has its ⚠ or
// ☠ before it, and a command with none has no mark at all.
func TestFormRiskMarks(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/name")
	typeIn(f, "Ship it")
	nameLine := func() string {
		for _, l := range strings.Split(ansi.Strip(f.screen()), "\n") {
			if strings.Contains(l, "Ship it") && strings.Contains(l, "Command name") {
				return l
			}
		}
		return ""
	}
	if l := nameLine(); strings.ContainsAny(l, warnGlyph+dangerGlyph+"●") {
		t.Errorf("no risk, no mark: %q", l)
	}
	if strings.Contains(ansi.Strip(f.screen()), "●") {
		t.Error("None has no dot: the menu marks nothing there")
	}
	f.onRow(t, "0/0/risk")
	f.pick(t, config.RiskWarn)
	if l := nameLine(); !strings.Contains(l, warnGlyph+" Ship it") {
		t.Errorf("a risky command's name should carry its mark: %q", l)
	}
}

// What the AI types into a field is in its gold as it's typed; what you
// type is the form's plain white.
func TestFormAITypesInGold(t *testing.T) {
	goldText := "38;5;" + fmt.Sprint(palette.Gold.Num)
	for _, ai := range []bool{false, true} {
		f := newTestForm(t)
		f.AIWorking = func() bool { return ai }
		f.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
		f.onRow(t, "0/0/cmd")
		press(f, kEnter)
		f.input.InsertString("figlet hi")
		got := strings.Contains(in256(f.view()), goldText)
		if got != ai {
			t.Errorf("AI working %v: typed text in gold %v", ai, got)
		}
	}
}

// Enter on a field goes to the next one the pack can't be added without and
// hasn't got: a new pack is four values, each followed by enter. With
// nothing needed, enter goes to the next field, as it always did.
func TestPackFormEnterGoesToWhatsNeeded(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	f, err := NewPackFormForNew(false)
	if err != nil {
		t.Fatal(err)
	}
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	var went []string
	for _, v := range []string{"Quick checks", "Everyday", "Disk free", "df -h /"} {
		went = append(went, f.selectedRow().label)
		f.Update(typed(v))
		press(f, kEnter)
	}
	if got := strings.Join(went, ", "); got != "Pack name, Module name, Command name, Runs" {
		t.Errorf("four values, four enters, went through %q", got)
	}
	if err := f.pack.CheckFilled(); err != nil {
		t.Errorf("that's all a pack needs: %v", err)
	}
	if r := f.selectedRow(); r == nil || r.required && r.value == "" {
		t.Errorf("nothing's needed now: enter just moves on, to %+v", r)
	}
}

// Acting on a row the filter found clears the filter: it's done its job, and
// the row's among all the rows again. And the foot says where typing goes.
func TestPackFormFilterGoesOnceARowIsTaken(t *testing.T) {
	f := newTestForm(t)
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	press(f, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "module name" {
		press(f, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !strings.Contains(f.rowHelp(), "Typing in the filter") {
		t.Errorf("in the filter's box, the foot should say so: %q", f.rowHelp())
	}
	press(f, kEnter) // out of the box and into the row it's on
	if f.filter.Value() != "" || f.filtering {
		t.Errorf("the filter should be gone once its row's taken: %q", f.filter.Value())
	}
	if f.editing == nil || f.editing.label != "Module name" {
		t.Fatalf("editing %+v", f.editing)
	}
	if !strings.HasPrefix(f.rowHelp(), "Typing into the module name.") {
		t.Errorf("the foot should say where typing goes: %q", f.rowHelp())
	}
	if p := f.page(); !strings.Contains(p, "Pack name") || !strings.Contains(p, "Command name") {
		t.Errorf("every row should be back:\n%s", p)
	}
}
