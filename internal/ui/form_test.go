package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
)

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

// typeIn opens the highlighted field, replaces what's in it, and confirms.
func typeIn(f *PackForm, text string) {
	press(f, kRight)
	f.input.SetValue(text)
	press(f, kEsc)
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
	// A field: typed in, and only kept in the form until it's left.
	f.onRow(t, "desc")
	typeIn(f, "Deploying our services")
	if p, _ := config.LoadEditable("demo"); !strings.Contains(f.screen(), "Deploying our services") || p.Description == "Deploying our services" {
		t.Fatalf("shown, and not saved yet:\n%s", f.screen())
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
	if r := f.selectedRow(); r == nil || r.key != "1/name" || f.editing != nil || r.value != "New module" {
		t.Fatalf("on its name, named already, not typing: on %+v", r)
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

	// A bad name: refused, said why.
	f.onRow(t, "1/0/name")
	typeIn(f, "Ship it!")
	if !strings.Contains(f.screen(), "ISSUE:") || f.input == nil {
		t.Fatalf("a bad name should be an issue, still being typed:\n%s", f.screen())
	}
	press(f, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}) // gives up on it
	if f.pack.Files[1].Group.Commands[0].Name != "Ship it" {
		t.Error("the bad name shouldn't have been kept")
	}

	// Drop is a row like the rest, No until you say Yes: No is first, and
	// does nothing.
	f.onRow(t, "1/0/remove")
	if !strings.Contains(f.screen(), "Drop  No ▾") {
		t.Fatalf("a Drop row:\n%s", f.screen())
	}
	press(f, kRight)
	if len(f.options) != 2 || f.options[0].Label != "No" || f.options[1].Label != "Yes" || f.opt != 0 {
		t.Fatalf("should ask, No first: %+v", f.options)
	}
	press(f, kRight) // No
	if len(f.pack.Files[1].Group.Commands) != 1 {
		t.Fatal("No should keep it")
	}

	// Esc asks first; Save saves them all, and it loads.
	press(f, kEsc)
	if p, _ := config.LoadEditable("demo"); !f.saving || p.Description == "Deploying our services" {
		t.Fatal("esc shouldn't save: it asks first")
	}
	press(f, kRight) // Save
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

// Left with nothing changed, there's nothing to ask about, and nothing's
// kept; outside the menu, the form asks just the same.
func TestPackFormDrafts(t *testing.T) {
	f := newTestForm(t)
	press(f, kEsc)
	if f.Left != "" || f.saving {
		t.Fatalf("nothing changed, but left %q", f.Left)
	}
	if d, _ := config.ReadDraft(); d != nil {
		t.Fatal("no draft without changes")
	}
	outside, _ := NewPackForm("demo", false)
	outside.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	outside.onRow(t, "desc")
	typeIn(outside, "Done")
	press(outside, kEsc, kRight) // Save
	if p, _ := config.LoadEditable("demo"); outside.Left != "saved" || p.Description != "Done" {
		t.Fatalf("outside the menu, Save saves: %q %q", outside.Left, p.Description)
	}
}

// A new pack starts from the example, named so it's not taken, and is only
// made once saved.
func TestPackFormNewPack(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	f, err := NewPackFormForNew(true)
	if err != nil {
		t.Fatal(err)
	}
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	if f.pack.Title != "New pack" || !f.pack.IsNew() {
		t.Fatalf("got %+v", f.pack)
	}
	f.onRow(t, "name")
	typeIn(f, "Deploy tools")
	press(f, kEsc)
	if !f.saving {
		t.Fatal("a new pack always asks")
	}
	press(f, kRight) // Save
	if f.Left != "saved" {
		t.Fatalf("left %q", f.Left)
	}
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
	if first := strings.TrimSpace(strings.Split(f.screen(), "\n")[6]); first != "PACK" {
		t.Errorf("the page should start with the pack's card, not %q", first)
	}
}

// / opens the filter, which takes all text and leaves the rows with it in,
// under their headings; esc clears it. Its row is level with the menu's.
func TestPackFormFilters(t *testing.T) {
	f := newTestForm(t)
	lines := strings.Split(f.screen(), "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[2]), "My packs › ") || !strings.HasPrefix(strings.TrimSpace(lines[4]), "❯ / to filter") {
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

// Back from the pack form with a draft, the menu asks: No throws the changes
// away, Yes saves them, ← opens the form again to carry on.
func TestMenuAsksToSaveTheForm(t *testing.T) {
	t.Setenv("CLINGON_CONFIG", t.TempDir())
	if err := config.NewPackWithLogo("demo", "rocket", ""); err != nil {
		t.Fatal(err)
	}
	var sent []string
	m := New(Options{Config: &config.Config{}, Send: func(c string) error { sent = append(sent, c); return nil },
		Shell: func() ShellState { return ShellState{} }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	draft := func(desc string) {
		p, _ := config.LoadEditable("demo")
		p.Description = desc
		if err := p.WriteDraft(); err != nil {
			t.Fatal(err)
		}
		m.form = &pending{editor: true, name: "Edit a pack", cmd: "clingon pack edit demo --menu"}
		m.Update(idleMsg{seq: m.runSeq, ok: true})
		if m.mode != modeSavePack || !strings.Contains(ansi.Strip(m.view()), "Save the changes to Demo?") {
			t.Fatalf("should ask:\n%s", ansi.Strip(m.view()))
		}
	}
	draft("Thrown away")
	m.Update(kEnter) // No, first
	if p, _ := config.LoadEditable("demo"); p.Description == "Thrown away" {
		t.Fatal("No shouldn't save")
	}
	if d, _ := config.ReadDraft(); d != nil {
		t.Fatal("No throws the draft away")
	}
	draft("Kept")
	m.Update(kLeft)
	if len(sent) != 1 || sent[0] != "clingon pack edit demo --menu" {
		t.Fatalf("← should open the form again: %q", sent)
	}
	draft("Kept")
	m.Update(kDown)
	m.Update(kEnter) // Yes
	if p, _ := config.LoadEditable("demo"); p.Description != "Kept" {
		t.Fatalf("Yes should save: %q", p.Description)
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
	if first := strings.TrimSpace(lines[8]); !strings.Contains(lines[6], "PACK") || !strings.Contains(lines[7], "Name") || !strings.Contains(first, "Logo") {
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
	for i, want := range []string{"PACK", "Name  ", "MODULE", "Name  ", "COMMAND", "Name  "} {
		if !strings.Contains(lines[6+i], want) {
			t.Fatalf("line %d should be %s:\n%s", 6+i, want, f.screen())
		}
	}
	if !strings.Contains(f.screen(), "Base") {
		t.Fatalf("and the cursor's row in view:\n%s", f.screen())
	}
}

// Esc with changes asks, at the foot of the form, whether to save them:
// Save (first) saves and closes, Keep editing (or ←, or esc) goes back to the
// form, and Throw them away closes without them. Without changes, esc just
// closes; ← at the form's top does nothing; ctrl+c asks too.
func TestPackFormAsksToSave(t *testing.T) {
	f := newTestForm(t)
	if _, cmd := f.Update(kLeft); cmd != nil || f.saving {
		t.Fatal("← at the top does nothing: only esc leaves")
	}
	if _, cmd := f.Update(kEsc); cmd == nil || f.saving {
		t.Fatal("no changes: esc closes")
	}

	f = newTestForm(t)
	if s := f.screen(); !strings.Contains(s, "SAVE") || !strings.Contains(s, "Save changes") || strings.Contains(f.view(), cursorBar(cursorStyle)+" "+cursorStyle.Render("Save changes")) {
		t.Fatalf("the question's always there, greyed out:\n%s", s)
	}
	f.onRow(t, "desc")
	typeIn(f, "Thrown away")
	f.Update(kEsc)
	if s := f.screen(); !f.saving || !strings.Contains(s, "Save changes") || !strings.Contains(s, "Keep editing") || !strings.Contains(s, "Discard changes") {
		t.Fatalf("should ask:\n%s", s)
	}
	f.Update(kEsc)
	if f.saving {
		t.Fatal("esc goes back to the form")
	}
	f.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !f.saving {
		t.Fatal("ctrl+c asks too")
	}
	press(f, kDown, kRight) // Keep editing
	if f.saving || f.Left != "" {
		t.Fatal("Keep editing goes back to the form")
	}
	f.Update(kEsc)
	press(f, kDown, kDown)
	if _, cmd := f.Update(kRight); cmd == nil || f.Left != "discarded" {
		t.Fatal("Throw them away closes")
	}
	if p, _ := config.LoadEditable("demo"); p.Description == "Thrown away" {
		t.Fatal("thrown away, not saved")
	}

	f, _ = NewPackForm("demo", true)
	f.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	f.onRow(t, "desc")
	typeIn(f, "Kept")
	f.Update(kEsc)
	if _, cmd := f.Update(kEnter); cmd == nil || f.Left != "saved" {
		t.Fatal("Save closes")
	}
	if p, _ := config.LoadEditable("demo"); p.Description != "Kept" {
		t.Fatalf("Save should save: %q", p.Description)
	}
}

// The menu's keys: [ ] jump between headings, ? shows the keys, ctrl-c
// cancels what's open.
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
	if f.choosing != nil || f.Left != "" {
		t.Fatal("ctrl-c should cancel the list, not leave")
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
	if p := plain(); !order(p, "_PACK.MD", "# Demo", "EXAMPLES.MD", "## Say hello") {
		t.Errorf("on the pack's rows, the pack and its first module:\n%s", p)
	}
	f.onRow(t, "1/name")
	if p := plain(); !order(p, "_PACK.MD", "# Demo", "MORE.MD", "# More") || strings.Contains(p, "EXAMPLES.MD") {
		t.Errorf("in the second module, it under the pack:\n%s", p)
	}
	f.onRow(t, "needs")
	if p := plain(); !strings.Contains(p, "MORE.MD") {
		t.Errorf("back on the pack's rows, the module last in:\n%s", p)
	}
	f.onRow(t, "1/0/cmd")
	got := strings.Join(f.preview(60, 40), "\n")
	if bar, mod := strings.Index(got, cursorBar(cursorStyle)), strings.Index(got, "MORE.MD"); strings.Count(got, cursorBar(cursorStyle)) != 1 || bar < mod {
		t.Errorf("the command's line marked, in the module's file only:\n%s", got)
	}
}

// Values start in the same column whatever's open: a command opening, with
// its labels further in, doesn't move them.
func TestPackFormValuesStayPut(t *testing.T) {
	f := newTestForm(t)
	col := func() int {
		for _, l := range strings.Split(f.page(), "\n") {
			if strings.Contains(l, "Name") && strings.Contains(l, "Demo") {
				return strings.Index(l, "Demo")
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

// The form tells the menu its pack's name as it changes, and forgets it once closed.
func TestPackFormTellsTheMenuItsName(t *testing.T) {
	f := newTestForm(t)
	if got := config.FormName(); got != "Demo" {
		t.Errorf("opened: %q", got)
	}
	f.onRow(t, "name")
	typeIn(f, "Deploy tools")
	if got := config.FormName(); got != "Deploy tools" {
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
	press(f, kEsc)
	if f.pack.Section != "hello-there" || !strings.Contains(f.pack.Markdown()[config.PackInfoFile], "menu: hello-there\n") {
		t.Fatalf("section %q", f.pack.Section)
	}
	// It's in the list from then on; the list opens at its top, as every list does.
	press(f, kRight)
	if f.opt != 0 || !slices.ContainsFunc(f.options, func(o config.Option) bool { return o.Value == "hello-there" }) {
		t.Errorf("on %d of %+v", f.opt, f.options)
	}
	press(f, kLeft)
	// A name it can't be is refused, saying why.
	f.pick(t, newSection)
	f.input.SetValue("clingon")
	press(f, kEsc)
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
	for key, want := range map[string]string{
		"name":     "\x1b[1;38;5;212mDemo",
		"0/0/name": "\x1b[1;38;5;212mSay hello",
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
	if !strings.Contains(page, "▾ What") || col("Runs") != col("What") || col("Type") != col("Runs") || strings.Contains(page, "Args") {
		t.Fatalf("What, with its arrow, then its rows, all in line with Runs:\n%s", page)
	}
	press(f, kLeft)
	if p := f.page(); !strings.Contains(p, "▸ What") || !strings.Contains(p, "<who>  · Select One, 2 opts") || strings.Contains(p, "Who to greet") {
		t.Fatalf("← collapses it:\n%s", p)
	}
	press(f, kRight)
	if p := f.page(); !strings.Contains(p, "▾ What") || !strings.Contains(p, "Who to greet") || f.editing != nil {
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
	for _, g := range []string{warnGlyph, dangerGlyph, "●"} {
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
	press(f, kEsc)
	if done := words(); len(done) < 2 || strings.Join(done, "\n") != strings.Join(typing, "\n") {
		t.Errorf("a long value wraps, as it did typing it:\n%s\n--\n%s", strings.Join(typing, "\n"), strings.Join(done, "\n"))
	}
	if d := f.pack.Files[0].Group.Description; strings.Contains(d, "\n") || !strings.HasPrefix(d, "word word") {
		t.Errorf("kept on one line: %q", d)
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
	a := &f.pack.Files[0].Group.Commands[0].Args[0]
	a.Options[0].Label, a.Default = "the first", "one"
	f.refresh("0/0/0/0")
	if p := f.page(); !strings.Contains(p, "▸ Opts  one  · the first · Picked") || strings.Contains(p, "Pick  ") {
		t.Fatalf("collapsed, a line saying it all:\n%s", p)
	}
	press(f, kRight)
	p := f.page()
	if !strings.Contains(p, "▾ Opts  one") || !strings.Contains(p, "Runs  one") || !strings.Contains(p, "Desc  the first") || !strings.Contains(p, "Pick  Yes") {
		t.Fatalf("→ opens it:\n%s", p)
	}
	f.onRow(t, "0/0/0/0/types")
	typeIn(f, "uno")
	if a.Options[0].Value != "uno" || a.Default != "uno" {
		t.Errorf("what it types, and picked still: %+v %q", a.Options[0], a.Default)
	}
	f.onRow(t, "0/0/0/0")
	press(f, kLeft)
	if p := f.page(); !strings.Contains(p, "▸ Opts  uno") || strings.Contains(p, "Runs  uno") {
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

// An add is a ghost of what it adds, where that will be: + COMMAND at the end
// of its module, + MODULE after the last, + Opts after a blank's choices.
// Taking one adds it, named already, the cursor on its name, not typing.
func TestPackFormGhostAdds(t *testing.T) {
	f := newTestForm(t)
	f.onRow(t, "0/0/0/kind")
	f.pick(t, "choose")
	p := f.page()
	for _, want := range []string{"+ COMMAND", "+ MODULE", "+ Opts"} {
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
	if len(f.pack.Files[0].Group.Commands) != n+1 || r == nil || r.key != fmt.Sprintf("0/%d/name", n) || r.value != "New command" || f.editing != nil {
		t.Fatalf("added and named, on its name, not typing: %+v, editing %v", r, f.editing != nil)
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
	for range len(config.PackLogos) {
		press(f, kDown)
	}
	if p := lines(); !strings.Contains(p, "↑ ") || !strings.Contains(p, config.TitleWords(config.PackLogos[len(config.PackLogos)-1])) {
		t.Errorf("at its end, the last in view, and what's above:\n%s", p)
	}
}

// The form's foot has a rule level with the menu's info pane's, beside it, at
// any height: the two panes read as one.
func TestPackFormRuleLevelWithTheMenus(t *testing.T) {
	ruleAt := func(lines []string, w int) int {
		at := -1
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(ansi.Strip(ansi.Cut(l, 0, w))), "────") {
				at = i
			}
		}
		return at
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
	has("a heading", "→ open", "← collapse", "esc done")
	press(f, kDown)
	has("a field", "→ edit", "/ filter")
	press(f, kRight)
	has("typing", "enter done", "ctrl-c cancel")
	press(f, kEsc)
	f.onRow(t, "menu")
	press(f, kDown, kUp, kRight)
	has("a list", "↑↓ choose", "→ take", "← close")
	press(f, kLeft)
	f.onRow(t, "desc")
	typeIn(f, "changed")
	press(f, kEsc)
	has("asking", "↑↓ choose", "→ take", "← back")
}
