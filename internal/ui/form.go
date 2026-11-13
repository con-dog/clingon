package ui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/mdcolor"
	"github.com/con-dog/clingon/internal/palette"
)

// PackForm edits one of your packs as a form, in place of its Markdown. It's
// one flat page, read top to bottom like a paper form: the pack's card, then
// each module's card with its commands' cards under it, each command's blanks
// and choices as rows of its card. Nothing opens onto another screen: ↑↓ move
// through every row, the page scrolling to follow, → or enter edits the row in
// place (a list drops down under it), ← closes what's open, and esc is done. A change that
// would break the pack is refused, saying why, so what you leave with can
// always be saved. Leaving with changes, it asks at its foot whether to
// save them, keep editing, or throw them away.
type PackForm struct {
	pack *config.EditablePack
	rows []formRow
	sel  int // the row the cursor's on
	w, h int

	// One of these at a time: a field being typed, or a list being picked
	// from (a field's options, a choice's actions, a remove's No/Yes).
	input    *textarea.Model
	editing  *formRow
	choosing *formRow
	options  []config.Option
	opt      int
	optTop   int // the first of the list's options in view: a long list shows listRows at a time

	// The filter, at the top as on the menu's lists: / opens it.
	filter    textinput.Model
	filtering bool

	issue string // why the last change wasn't made

	menu       bool   // opened from the menu: leaving keeps a draft for it to ask about
	unfinished string // what a pack still needs before it can be added (written); "" once it is
	escOnce    bool   // esc refused a typed value once: again, it undoes it
	help       bool   // the keys, in place of the form
	pageSize   int    // rows a page up or down moves

	// The part the cursor's in, shown whole; the others are folded to
	// their heading and name. -1 is the pack, -2 nothing (Add a module).
	lastSub  int             // the module the cursor was last in, for the preview to show under _pack.md
	toldKeys string          // the keys the menu's status bar was last told, joined
	toggled  map[string]bool // modules ("1") and commands ("1/2") opened or collapsed by hand; open unless said
	noFold   bool            // building every row, to measure them

	named string // the pack's name as last told to the menu

	// Focus moves focus out of the form, as tab and shift-tab do everywhere:
	// next, to the panel after it (the AI's, when it's open), saying whether
	// there was one; else back to the menu. Nil outside clingon's window.
	Focus func(next bool) bool

	Left string // how it was left: "saved" (changes written as they were made), "draft" (unfinished, kept to carry on from) or "" (nothing changed)
	// MenuHeight says how tall the menu beside it is, so its foot lines up
	// with the menu's: the AI's strip under the shell makes the form shorter.
	// Asked as its size changes. nil, or 0: as tall as the form.
	MenuHeight func() int
	menuH      int
	// AIWorking says whether the AI is driving: the foot, what a row's for,
	// is for people, so it goes, and the form has its rows. Asked as its size
	// changes (the AI's panel opening resizes it).
	AIWorking func() bool
	aiWorking bool
}

// formRow is one row of the page; a header starts a card, titled with it.
type formRow struct {
	key                      string // which row it is, to find it again once the page is rebuilt
	label, value, hint, help string
	note                     string // faint, after the value: what a folded part has in it
	required                 bool   // empty, it shows its hint wherever the cursor is: what's still to fill in
	header                   bool
	sub                      int    // how many levels it's set in under its command: a blank 1, a blank's own rows 2
	title                    bool   // a label with nothing of its own, over what's set in under it: the cursor passes it
	fold                     string // "open" or "closed" on a blank's What row, which opens and collapses the blank
	depth                    int    // 0 the pack's, 1 a module's, 2 a command's: how far in its tree guides go
	branch                   bool   // an add that ends its level, drawn on the tree's └─
	icon, color              string
	look                     func(string) string // the value coloured as it is in the Markdown, if it's coloured there
	risk                     string
	// One of: typed in, picked from options, or done (after a No/Yes, with
	// confirm as the Yes).
	text    func(string) error
	options []config.Option
	pick    func(string) error
	do      func() error
	confirm string
	then    string // after do: the row to go to, what it added
}

// NewPackForm is the form for your pack called name, carrying on from a draft
// of it if one's waiting. With menu, it was opened from the menu.
func NewPackForm(name string, menu bool) (*PackForm, error) {
	p, err := config.LoadEditable(name)
	if err != nil {
		return nil, err
	}
	if d, err := config.ReadDraft(); err == nil && p.SameAs(d) {
		p = d
	}
	return newPackForm(p, menu), nil
}

// NewPackFormForNew is the form for a new pack, carrying on from a draft of
// a new one if one's waiting.
func NewPackFormForNew(menu bool) (*PackForm, error) {
	if d, err := config.ReadDraft(); err == nil && d != nil && d.IsNew() {
		return newPackForm(d, menu), nil
	}
	p, err := config.NewEditable()
	if err != nil {
		return nil, err
	}
	return newPackForm(p, menu), nil
}

func newPackForm(p *config.EditablePack, menu bool) *PackForm {
	f := &PackForm{pack: p, sel: -1, filter: newInput(accentStyle, "type to filter"), menu: menu}
	f.refresh("")
	return f
}

// leave is done with the form: every change is written as it's made, so
// there's nothing to ask. A pack that can't be added yet (a new one without
// a name and a command, say) is kept as a draft to carry on from.
func (f *PackForm) leave() tea.Cmd {
	switch {
	case f.unfinished != "" && f.pack.Changed():
		if err := f.pack.WriteDraft(); err != nil {
			f.issue = err.Error()
			return nil
		}
		f.Left = "draft"
	case f.Left == "saved":
		_ = config.RemoveDraft()
	default:
		if d, _ := config.ReadDraft(); f.pack.SameAs(d) {
			_ = config.RemoveDraft()
		}
	}
	return tea.Quit
}

// persist writes the pack after a change, if it's complete: a new one is
// added the first time it is, in a folder named after it. Until then it's
// kept as a draft, and the form says what it still needs.
func (f *PackForm) persist() {
	if err := f.pack.CheckFilled(); err != nil {
		f.unfinished = err.Error()
		_ = f.pack.WriteDraft()
		return
	}
	if f.pack.IsNew() {
		f.pack.Folder = f.pack.NewFolder()
	}
	if err := f.pack.Save(); err != nil {
		f.issue = "Not written: " + err.Error()
		return
	}
	f.unfinished, f.Left = "", "saved"
	_ = config.RemoveDraft()
}

// rowHelp is what the row the cursor's on is for, or, once esc has asked,
// what saving does.
func (f *PackForm) rowHelp() string {
	if f.issue != "" { // in full, where there's room for it: the status bar cuts it short
		say := "✗ " + capitalFirst(strings.TrimSuffix(f.issue, ".")) + "."
		if f.editing != nil {
			say += " Still editing: fix it and press enter"
			if f.escOnce {
				say += ", or esc again to undo it"
			}
			say += "."
		}
		return say
	}
	help := ""
	if r := f.selectedRow(); r != nil {
		help = r.help
	}
	// Where your typing's going, when it isn't moving the cursor: said here,
	// first, so it's never a guess. (The keys for it are on the status bar.)
	switch {
	case f.editing != nil:
		help = strings.TrimSpace("Typing into " + rowName(f.editing) + ". " + help)
	case f.choosing != nil:
		help = strings.TrimSpace("Choosing from the list under " + rowName(f.choosing) + ". " + help)
	case f.filtering:
		help = "Typing in the filter: the rows narrow to what you type."
	}
	if f.unfinished != "" { // why it isn't in the menu yet
		help = strings.TrimSpace(help + " Not added yet: " + strings.TrimSuffix(f.unfinished, ".") + ".")
	}
	return help
}

// jump moves to the first row of the part before (or after) the one the
// cursor's in, as [ and ] jump between the menu's headings.
func (f *PackForm) jump(d int) {
	start := f.sel
	for start > 0 && !f.rows[start-1].header { // the first row of this part
		start--
	}
	i := start
	if d < 0 {
		if f.sel != start { // first, to the top of this part
			f.sel = start
			return
		}
		i -= 2 // past this part's heading
		for i > 0 && !f.rows[i-1].header {
			i--
		}
	} else {
		for i < len(f.rows) && !f.rows[i].header {
			i++
		}
		i++
	}
	if i >= 0 && i < len(f.rows) && !f.rows[i].header {
		f.sel = i
	}
}

// Pack is the pack being edited.
func (f *PackForm) Pack() *config.EditablePack { return f.pack }

func (f *PackForm) Init() tea.Cmd { return nil }

// change applies a change if the pack still checks out, or undoes it and
// says why. Nothing's saved until you leave.
func (f *PackForm) change(apply func() error) error {
	before := clonePack(f.pack)
	if err := apply(); err != nil {
		f.pack = before
		return err
	}
	if err := f.pack.Check(); err != nil {
		f.pack = before
		return err
	}
	f.persist() // written as it's made, once it's complete
	return nil
}

// refresh rebuilds the page with the cursor on the row keyed focus, or else
// the row it was on, or else as near to where it was as there is.
func (f *PackForm) refresh(focus string) {
	was := f.sel
	if r := f.selectedRow(); r != nil && focus == "" {
		focus = r.key
	}
	f.reveal(focus)
	f.rows = f.matching(f.pageRows())
	defer func() {
		if f.menu && f.toldKeys == "" { // the bar says the form's keys from the start: once the cursor's on its row, so they're that row's
			f.tellKeys()
		}
	}()
	// So the menu says what you're editing, by its name as it is, and where
	// in the menu it'll be.
	// And how many modules and commands it has, as they're added.
	modules, commands := len(f.pack.Files), 0
	for _, file := range f.pack.Files {
		commands += len(file.Group.Commands)
	}
	if place := config.SectionName(f.pack.Section); f.menu {
		if said := fmt.Sprintf("%s\n%s\n%d %d", f.pack.Title, place, modules, commands); said != f.named && config.WriteFormName(f.pack.Title, place, modules, commands) == nil {
			f.named = said
		}
	}
	f.sel = -1
	if len(f.rows) == 0 {
		return
	}
	for i, r := range f.rows {
		if focus != "" && r.key == focus && !r.still() {
			f.sel = i
		}
	}
	if f.sel >= 0 {
		return
	}
	f.sel = min(max(was, 0), len(f.rows)-1)
	if f.rows[f.sel].still() {
		f.step(1)
	}
	if f.rows[f.sel].still() {
		f.step(-1)
	}
}

// openPart is the part the row keyed key is in, to show whole: its module
// and command (-1 for none), the pack's own rows being module -1 and Add a
// module's -2.
func openPart(key string) (sub, cmd int) {
	if key == "add-module" {
		return -2, -1
	}
	sub, cmd = -1, -1
	if n, _ := fmt.Sscanf(key, "%d/%d/", &sub, &cmd); n == 1 {
		cmd = -1
	}
	return sub, cmd
}

// fold opens or collapses the module or command whose heading r is, or the
// blank whose What row it is.
func (f *PackForm) fold(r *formRow, open bool) {
	var at [4]int // a blank's ("1/2/0/name") or a choice's ("1/2/0/3")
	n, _ := fmt.Sscanf(r.key, "%d/%d/%d/%d", &at[0], &at[1], &at[2], &at[3])
	if r.header {
		at[0], at[1] = openPart(r.key)
		n = 2
	}
	f.setOpen(open, at[:n]...)
	f.refresh(r.key)
}

// partKey is how a module ("1"), a command ("1/2") or a command's blank
// ("1/2/0") is kept open or collapsed: its indexes, as far as they go.
func partKey(at ...int) string {
	var s []string
	for _, n := range at {
		if n < 0 {
			break
		}
		s = append(s, fmt.Sprint(n))
	}
	return strings.Join(s, "/")
}

// isOpen says whether a module, command or blank is open: it is unless it's
// been collapsed.
func (f *PackForm) isOpen(at ...int) bool {
	open, said := f.toggled[partKey(at...)]
	return open || !said
}

// setOpen opens or collapses a module, command or blank.
func (f *PackForm) setOpen(open bool, at ...int) {
	if f.toggled == nil {
		f.toggled = map[string]bool{}
	}
	f.toggled[partKey(at...)] = open
}

// reveal opens what the row keyed key is in, so going to it (after adding
// it, say) shows it. A part's own heading shows either way.
func (f *PackForm) reveal(key string) {
	sub, cmd := openPart(key)
	if sub < 0 || strings.HasSuffix(key, "/head") && strings.Count(key, "/") == 1 {
		return
	}
	f.setOpen(true, sub)
	if cmd >= 0 && !(strings.HasSuffix(key, "/head") && strings.Count(key, "/") == 2) {
		f.setOpen(true, sub, cmd)
	}
	var b int
	if n, _ := fmt.Sscanf(key, "%d/%d/%d/", &sub, &cmd, &b); n == 3 && !strings.HasSuffix(key, "/name") { // a blank's row, past its What
		f.setOpen(true, sub, cmd, b)
	}
	var o int
	if n, _ := fmt.Sscanf(key, "%d/%d/%d/%d/", &sub, &cmd, &b, &o); n == 4 && strings.Count(key, "/") == 4 { // a choice's row, past its Opts
		f.setOpen(true, sub, cmd, b, o)
	}
}

// folded says whether a part is collapsed to its heading: as it's been left,
// unless the filter's on, which shows all it finds. The pack's never is.
func (f *PackForm) folded(sub, cmd int) bool {
	if f.noFold || f.filter.Value() != "" || sub < 0 {
		return false
	}
	return !f.isOpen(sub, cmd)
}

// unfolded is every row of the page, nothing folded.
func (f *PackForm) unfolded() []formRow {
	f.noFold = true
	defer func() { f.noFold = false }()
	return f.pageRows()
}

// foldTo is a part collapsed to its heading, its name beside it.
func foldTo(rows []formRow, note string) []formRow {
	rows[0].value, rows[0].look, rows[0].note = rows[1].value, rows[1].look, note
	return rows[:1]
}

// matching is the rows the filter leaves, each with the headings it's under:
// a row stays if each of the filter's words is in its label, its value or
// its part's heading, so "module name" is the module's Name and not the
// pack's.
func (f *PackForm) matching(rows []formRow) []formRow {
	words := strings.Fields(strings.ToLower(f.filter.Value()))
	if len(words) == 0 {
		return rows
	}
	var out []formRow
	var heads [3]*formRow // the heading each level's in, not yet shown
	var titles [3]string
	for i := range rows {
		r := &rows[i]
		if r.header {
			heads[r.depth], titles[r.depth] = r, strings.ToLower(r.label)
			for d := r.depth + 1; d < 3; d++ {
				heads[d], titles[d] = nil, ""
			}
			continue
		}
		own := strings.ToLower(r.label + " " + r.value)
		in := true
		for _, w := range words {
			in = in && (strings.Contains(own, w) || !r.branch && r.depth < 3 && strings.Contains(titles[r.depth], w))
		}
		if !in {
			continue
		}
		for d := 0; d <= min(r.depth, 2); d++ {
			if heads[d] != nil && (!r.branch || d < r.depth) {
				out = append(out, *heads[d])
				heads[d] = nil
			}
		}
		out = append(out, *r)
	}
	return out
}

// toLabel puts the cursor on the row the filter names: its label is the
// filter's text, else starts with it, else has it in it; words its part's
// heading has ("module" in "module name") are left out first, unless that
// leaves none ("command" is a heading and a field). Rows a heading keeps
// (every row under COMMAND, for "command") don't count, nor values. Of rows
// as good, the nearest to where the cursor was. With none, it stays.
func (f *PackForm) toLabel() {
	words := strings.Fields(strings.ToLower(f.filter.Value()))
	if len(words) == 0 {
		return
	}
	var titles [3]string
	best, bestRank, bestGap := -1, 0, 0
	for i, r := range f.rows {
		if r.header {
			if r.depth < 3 {
				titles[r.depth] = strings.ToLower(r.label)
			}
			continue
		}
		if r.still() {
			continue
		}
		var rest []string
		for _, w := range words {
			if r.branch || r.depth >= 3 || !strings.Contains(titles[r.depth], w) {
				rest = append(rest, w)
			}
		}
		if len(rest) == 0 {
			rest = words
		}
		q := strings.Join(rest, " ")
		label := strings.ToLower(strings.TrimSpace(r.label))
		rank := 0
		switch {
		case label == q:
			rank = 3
		case strings.HasPrefix(label, q):
			rank = 2
		case strings.Contains(label, q):
			rank = 1
		default:
			continue
		}
		gap := i - f.sel
		if gap < 0 {
			gap = -gap
		}
		if rank > bestRank || rank == bestRank && gap < bestGap {
			best, bestRank, bestGap = i, rank, gap
		}
	}
	if best >= 0 {
		f.sel = best
	}
}

// move puts the cursor on the next row (or, with -1, the one before) that
// isn't a card's title.
// still says whether the cursor passes a row by: a heading, or a title.
func (r formRow) still() bool { return r.header && r.key == "" || r.title }

// step takes the cursor d rows on, past those it can't be on, stopping at
// the form's ends. It says whether it moved.
func (f *PackForm) step(d int) bool {
	for i := f.sel + d; i >= 0 && i < len(f.rows); i += d {
		if !f.rows[i].still() {
			f.sel = i
			return true
		}
	}
	return false
}

// editable says whether a row is typed into: a field, not a list, a heading
// or a blank's own row.
func editable(r *formRow) bool {
	return r.text != nil && r.pick == nil && r.fold == "" && !r.header && r.do == nil && r.confirm == ""
}

// focusOut moves focus to the next panel, or back to the menu. With no
// panel after the form, tab says so.
func (f *PackForm) focusOut(next bool) {
	if f.Focus == nil {
		return
	}
	if !f.Focus(next) && next {
		f.issue = "There's no panel after the form: shift-tab goes back to the menu"
	}
}

// field takes the cursor to the next (d 1) or previous (d -1) field: a row
// you type in or pick from, an add, or a part folded up (to open), past
// headings, an open blank's or option's own row, and removes. It stops at
// the form's ends. It says whether it moved.
// taken is row r once it's been picked to act on (edited, opened, chosen
// from): a filter that found it has done its job, and goes, the cursor still
// on the row among all the rows again.
func (f *PackForm) taken(r *formRow) *formRow {
	if f.filter.Value() == "" && !f.filtering {
		return r
	}
	key := r.key
	f.filtering = false
	f.filter.Blur()
	f.filter.SetValue("")
	f.refresh(key)
	if now := f.selectedRow(); now != nil && now.key == key {
		return now
	}
	return r
}

// toNeeded moves the cursor to the next field the pack can't be added
// without and hasn't got: the pack's name, a module's, a command's, what it
// runs. After the row it's on first, then from the top. It says whether
// there was one: with none, the pack has all it needs.
func (f *PackForm) toNeeded() bool {
	n := len(f.rows)
	for step := 1; step <= n; step++ {
		i := (f.sel + step) % n
		if r := f.rows[i]; r.required && r.text != nil && strings.TrimSpace(r.value) == "" {
			f.sel = i
			return true
		}
	}
	return false
}

func (f *PackForm) field(d int) bool {
	for i := f.sel + d; i >= 0 && i < len(f.rows); i += d {
		r := f.rows[i]
		folded := r.header && r.value != "" || r.fold == "closed" // a part folded to its heading, its name beside it
		if r.still() || r.confirm != "" || r.fold == "open" || r.header && !folded {
			continue
		}
		if folded || r.text != nil || r.pick != nil || r.do != nil {
			f.sel = i
			return true
		}
	}
	return false
}

// move is ↑ (d -1) or ↓ (d 1): a row on, and past the form's last row round
// to its first, or before its first to its last, as every list does.
func (f *PackForm) move(d int) {
	if f.step(d) {
		return
	}
	i := 0 // ↓ at the foot: from the top
	if d < 0 {
		i = len(f.rows) - 1 // ↑ at the top: from the foot
	}
	for ; i != f.sel && i >= 0 && i < len(f.rows); i += d {
		if !f.rows[i].still() {
			f.sel = i
			return
		}
	}
}

// round is i moved one on by ↑ or ↓ among n choices, going round at the ends.
func round(i int, k string, n int) int {
	if n == 0 {
		return 0
	}
	if k == "up" {
		return (i - 1 + n) % n
	}
	return (i + 1) % n
}

func joined(l []string) string { return strings.Join(l, ", ") }

func splitComma(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// The pack's parts, looked up when a change is made: a change that fails puts
// back a copy of the pack, so rows hold where things are, not the things.
func (f *PackForm) group(i int) *config.Group        { return &f.pack.Files[i].Group }
func (f *PackForm) command(i, j int) *config.Command { return &f.group(i).Commands[j] }
func (f *PackForm) blank(i, j, b int) *config.Arg    { return &f.command(i, j).Args[b] }

// pageRows is the whole form, top to bottom.
func (f *PackForm) pageRows() []formRow {
	rows := f.packRows()
	for i := range f.pack.Files {
		rows = append(rows, f.moduleRows(i)...)
	}
	return append(rows,
		formRow{key: "add-module", depth: 1, branch: true, label: "Add module", help: "A new module, with a command to start from.", do: func() error {
			p := f.pack
			p.Files = append(p.Files, config.EditableFile{Slug: p.NewSlug(""), Group: config.Group{Commands: []config.Command{{}}}}) // empty: its rows show what to fill in
			return nil
		}, then: fmt.Sprintf("%d/name", len(f.pack.Files))},
	)
}

// mdLook colours a value as the preview colours it: the lines it's on in the
// Markdown, before and after being what's around it there, coloured as the
// preview colours the file, cut down to the value. A value over lines has each line
// after the first set in as the first is (> for a callout's).
func mdLook(before, after string) func(string) string {
	lead := before[strings.LastIndex(before, "\n")+1:]
	return func(v string) string {
		vs := strings.Split(v, "\n")
		lines := strings.Split(mdcolor.Markdown(before+strings.Join(vs, "\n"+lead)+after), "\n")
		first, pre := strings.Count(before, "\n"), ansi.StringWidth(lead)
		out := make([]string, len(vs))
		for i, l := range vs {
			cut := ansi.Cut(lines[first+i], pre, pre+ansi.StringWidth(l))
			out[i] = emptySGR.ReplaceAllString(cut, "") // what's left of the colour of what was cut away
		}
		return strings.Join(out, "\n")
	}
}

// emptySGR is a colour that's set and reset with nothing in it.
var emptySGR = regexp.MustCompile(`\x1b\[[0-9;]*m\x1b\[0?m`)

// frontLook colours a setting's value as it is in the front matter.
func frontLook(key string) func(string) string { return mdLook("---\n"+key+": ", "\n---") }

// capitalFirst is a hint as the form shows it, starting with a capital.
func capitalFirst(s string) string {
	for i, r := range s {
		return strings.ToUpper(string(r)) + s[i+len(string(r)):]
	}
	return s
}

// iconGap is the space after an icon before its text: one after a glyph that
// fills its cell, as the menu has after ⚠ and ☠; two after one of clingon's
// font's logos and centred twins, drawn across two cells.
func iconGap(icon string) string {
	for _, r := range icon {
		if r >= 0xE000 && r <= 0xF8FF { // the font's own, in the private use area
			return "  "
		}
	}
	return " "
}

// newSection is the Section list's last option: a section of your own, typed.
const newSection = "\x00new"

func (f *PackForm) packRows() []formRow {
	p := f.pack
	// The sections clingon suggests, then your own, then one more.
	var sections []config.Option
	for _, s := range append(config.SectionSlugs(), config.YourSections()...) {
		sections = append(sections, config.Option{Value: s, Label: config.SectionName(s)})
	}
	if !slices.ContainsFunc(sections, func(o config.Option) bool { return o.Value == p.Section }) {
		sections = append(sections, config.Option{Value: p.Section, Label: config.SectionName(p.Section)})
	}
	sections = append(sections, config.Option{Value: newSection, Label: "+ New menu"})
	var logos []config.Option
	for _, n := range config.PackLogos {
		logos = append(logos, config.Option{Value: n, Label: config.TitleWords(n), Icon: config.Logos[n]}) // all grey: a choice of shape, colour's the next row
	}
	var colours []config.Option
	for _, c := range config.PackColors {
		colours = append(colours, config.Option{Value: c.Name, Label: config.TitleWords(c.Name), Icon: config.Logos[p.Logo], Color: c.Hex})
	}
	colour, colourHex := p.Color, f.logoColour()
	for _, c := range config.PackColors { // with none set, the colour its logo has
		if colour == "" && c.Hex == colourHex {
			colour = c.Name
		}
	}
	rows := []formRow{
		{label: "Pack", header: true},
		{key: "name", label: "Pack name", value: p.Title, look: mdLook("# ", ""), hint: "like My tools", required: true, help: "What the menu calls the pack.", text: func(v string) error { f.pack.Title = v; return nil }},
		{key: "desc", label: "Description", value: p.Description, hint: "one line on what's in it", help: "One line on what's in it, shown by clingon pack.", text: func(v string) error { f.pack.Description = v; return nil }},
		{key: "menu", label: "Menu", value: config.SectionName(p.Section), look: frontLook("menu"), help: "Where the menu shows it: one of these, or + New menu for one of your own.", options: sections, pick: func(v string) error { f.pack.Section = v; return nil },
			text: func(v string) error {
				s, err := config.SectionSlug(v)
				if err == nil {
					f.pack.Section = s
				}
				return err
			}},
		{key: "logo", label: "Logo", value: config.TitleWords(p.Logo), look: frontLook("logo"), icon: config.Logos[p.Logo], color: f.logoColour(), help: "Its icon in the menu.", options: logos, pick: func(v string) error { f.pack.Logo = v; return nil }},
		{key: "tint", label: "Tint", value: config.TitleWords(colour), look: frontLook("tint"), icon: slotIcon("●"), color: colourHex, help: "The logo's colour.", options: colours, pick: func(v string) error { f.pack.Color = v; return nil }},
		{key: "needs", label: "Needs", value: joined(p.Needs), look: frontLook("needs"), hint: "programs, separated by commas", help: "Programs the pack needs, separated by commas: the status bar warns you while one's missing.", text: func(v string) error { f.pack.Needs = splitComma(v); return nil }},
	}
	if f.folded(-1, -1) {
		return foldTo(rows, "")
	}
	return rows
}

func (f *PackForm) moduleRows(i int) []formRow {
	g := f.group(i)
	k := func(s string) string { return fmt.Sprintf("%d/%s", i, s) }
	rows := []formRow{
		{key: k("head"), label: "Module", header: true},
		{key: k("name"), label: "Module name", value: g.Name, look: mdLook("# ", ""), hint: "like Everyday", required: true, help: "What the menu calls the module.", text: func(v string) error { f.group(i).Name = v; return nil }},
		{key: k("desc"), label: "Description", value: g.Description, hint: "what's in it", help: "What's in the module.", text: func(v string) error { f.group(i).Description = v; return nil }},
		{key: k("needs"), label: "Needs", value: joined(g.Requires), look: frontLook("needs"), hint: "programs, separated by commas", help: "Programs every command here needs, separated by commas.", text: func(v string) error { f.group(i).Requires = splitComma(v); return nil }},
		{key: k("when"), label: "When", value: joined(g.When), look: frontLook("when"), hint: "files, like .git", help: "Files that must be here or in a folder above for these commands, like .git; ./Dockerfile means this folder only.", text: func(v string) error { f.group(i).When = splitComma(v); return nil }},
		{key: k("remove"), label: "Remove", value: "No", risk: config.RiskWarn, help: "Yes removes the module, and every command in it.", confirm: "Yes", do: func() error {
			f.pack.Files = slices.Delete(f.pack.Files, i, i+1)
			return nil
		}},
	}
	for r := range rows {
		rows[r].depth = 1
	}
	if f.folded(i, -1) {
		return foldTo(rows, plural(len(g.Commands), "command"))
	}
	for j := range g.Commands {
		rows = append(rows, f.commandRows(i, j)...)
	}
	return append(rows,
		formRow{key: k("add"), depth: 2, branch: true, label: "Add command", help: "A new command in " + g.Name + ", to fill in.", do: func() error {
			g := f.group(i)
			g.Commands = append(g.Commands, config.Command{}) // empty: its rows show what to fill in
			return nil
		}, then: fmt.Sprintf("%d/%d/name", i, len(g.Commands))},
	)
}

func (f *PackForm) commandRows(i, j int) []formRow {
	c := f.command(i, j)
	k := func(s string) string { return fmt.Sprintf("%d/%d/%s", i, j, s) }
	// Each with the glyph the menu marks it with; none with none, as in the
	// menu (a blank cell in the list, so the names line up).
	risks := []config.Option{
		{Value: "", Label: "None", Icon: " "},
		{Value: config.RiskWarn, Label: "Warning", Icon: warnGlyph, Color: palette.Orange.Hex},
		{Value: config.RiskDanger, Label: "Dangerous", Icon: dangerGlyph, Color: palette.Red.Hex},
	}
	risk := map[string]string{"": "None", config.RiskWarn: "Warning", config.RiskDanger: "Dangerous"}[c.Risk]
	var descLook func(string) string // in a callout's > lines when it has a risk
	if c.Risk != "" {
		descLook = mdLook("> ", "")
	}
	riskIcon := risks[max(slices.IndexFunc(risks, func(o config.Option) bool { return o.Value == c.Risk }), 0)]
	if c.Risk == "" { // the row itself, no mark: None lines up with the values above and below
		riskIcon.Icon = ""
	}
	rows := []formRow{
		{key: k("head"), label: "Command", header: true},
		{key: k("name"), label: "Command name", value: c.Name, icon: strings.TrimSpace(riskIcon.Icon), color: riskIcon.Color, look: mdLook("## ", ""), hint: "like Say hello", required: true, help: "What the menu calls it: letters, digits, spaces and ( ).", text: func(v string) error { f.command(i, j).Name = v; return nil }},
		{key: k("risk"), label: "Risk", value: risk, icon: riskIcon.Icon, color: riskIcon.Color, help: "Whether clingon asks before it runs: warning asks first; dangerous has you type a short code, then asks.", options: risks, pick: func(v string) error { f.command(i, j).Risk = v; return nil }},
		{key: k("desc"), label: "Description", value: c.Desc, look: descLook, hint: "what it does", help: "What it does, shown when it's highlighted.", text: func(v string) error { f.command(i, j).Desc = v; return nil }},
		{key: k("needs"), label: "Needs", value: joined(c.Requires), hint: "programs, separated by commas", help: "Programs this command needs, separated by commas.", text: func(v string) error { f.command(i, j).Requires = splitComma(v); return nil }},
		{key: k("when"), label: "When", value: joined(c.When), hint: "files, like .git", help: "Files that must be here or in a folder above, like .git.", text: func(v string) error { f.command(i, j).When = splitComma(v); return nil }},
		{key: k("cmd"), label: "Runs", value: c.Cmd, look: mdLook("```sh\n", "\n```"), hint: `like echo "Hello, <who>."`, required: true, help: "What's typed into your shell. A <name> in it is a blank, asked for each time.", text: func(v string) error {
			c := f.command(i, j)
			c.Cmd = v
			config.SyncBlanks(c)
			return nil
		}},
	}
	for b := range c.Args {
		rows = append(rows, f.blankRows(i, j, b)...)
	}
	rows = append(rows, formRow{key: k("remove"), label: "Remove", value: "No", risk: config.RiskWarn, help: "Yes removes the command.", confirm: "Yes", do: func() error {
		g := f.group(i)
		g.Commands = slices.Delete(g.Commands, j, j+1)
		return nil
	}})
	for r := range rows {
		rows[r].depth = 2
	}
	if f.folded(i, j) {
		return foldTo(rows, "")
	}
	return rows
}

// blankRows are a blank's rows in its command's card: what it asks, how it's
// answered, and its default or its choices.
func (f *PackForm) blankRows(i, j, b int) []formRow {
	a := f.blank(i, j, b)
	k := func(s string) string { return fmt.Sprintf("%d/%d/%d/%s", i, j, b, s) }
	name := "<" + a.Name + ">"
	kinds := []config.Option{{Value: "text", Label: "Text"}, {Value: "choose", Label: "Select One"}, {Value: "flags", Label: "Select Many"}}
	kind := map[string]string{"text": "Text", "secret": "Text, Hidden", "choose": "Select One", "flags": "Select Many"}[a.Kind()]
	rows := []formRow{
		{key: k("name"), fold: "open", label: "Argument", value: name, look: mdLook("- `", "`"), help: "The blank: where it is in the command. Renaming it renames it there too.", text: func(v string) error {
			return config.RenameBlank(f.command(i, j), f.blank(i, j, b).Name, v)
		}},
		{key: k("q"), label: "Question", value: a.Prompt, hint: "like Who to greet", help: "What's asked for " + name + " when the command runs. Left empty, it's asked by its name.", text: func(v string) error { f.blank(i, j, b).Prompt = v; return nil }},
	}
	if a.Kind() == "secret" {
		rows = append(rows, formRow{key: k("kind"), label: "Type", value: kind, help: "Its name says it's a secret, token or password: typed hidden, and kept out of your history."})
	} else {
		rows = append(rows, formRow{key: k("kind"), label: "Type", value: kind, help: "How " + name + " is answered: text is typed in; select one picks one of its choices, or you type your own; select many picks any of them.", options: kinds, pick: func(v string) error {
			a := f.blank(i, j, b)
			switch v {
			case "text":
				a.Type, a.Options = "", nil
			default:
				a.Type, a.Default = v, ""
				if len(a.Options) == 0 {
					a.Options = []config.Option{{}} // one to fill in, open
					f.setOpen(true, i, j, b, 0)
				}
			}
			return nil
		}})
	}
	if kd := a.Kind(); kd == "text" || kd == "secret" {
		rows = append(rows, formRow{key: k("start"), label: "Default", value: a.Default, look: mdLook("  - (x) `", "`"), hint: "none", help: "What's filled in for " + name + " until you change it.", text: func(v string) error { f.blank(i, j, b).Default = v; return nil }})
	} else {
		for o := range a.Options {
			rows = append(rows, f.choiceRows(i, j, b, o)...)
		}
		rows = append(rows, formRow{key: k("add"), label: "Add option", help: "Another answer to offer for " + name + ".", do: func() error {
			a := f.blank(i, j, b)
			a.Options = append(a.Options, config.Option{}) // empty: its Value row says what to fill in
			return nil
		}, then: fmt.Sprintf("%d/%d/%d/%d/types", i, j, b, len(a.Options))}) // on to what it types, opened
	}
	if f.filter.Value() == "" && !f.noFold && !f.isOpen(i, j, b) { // collapsed to its What row, and what it is
		note := kind
		if len(a.Options) > 0 {
			note += ", " + plural(len(a.Options), "option")
		}
		rows[0].fold, rows[0].note = "closed", note
		return rows[:1]
	}
	return rows
}

// pickedFirst says if a choice is a default: picked (or ticked) until you change it.
func pickedFirst(a *config.Arg, v string) bool {
	return a.Kind() == "flags" && slices.Contains(a.Ticked(a.Default), v) || a.Kind() == "choose" && a.Default == v
}

// choiceRows are one of a blank's choices: its Opts row, opening and
// collapsing it as a blank's What row does, with what it types; and open, what
// it types (Runs), its label (Desc), whether it's picked to begin with (Pick),
// and removing it. They start collapsed: a line saying it all.
func (f *PackForm) choiceRows(i, j, b, o int) []formRow {
	a := f.blank(i, j, b)
	opt := a.Options[o]
	key := fmt.Sprintf("%d/%d/%d/%d", i, j, b, o)
	k := func(s string) string { return key + "/" + s }
	types := mdLook("  - ( ) `", "`")
	picked := "No"
	if pickedFirst(a, opt.Value) {
		picked = "Yes"
	}
	head := formRow{key: key, fold: "open", label: "Option", value: opt.Value, look: types, help: "One of the answers offered for <" + a.Name + ">: → opens it, ← collapses it."}
	if !f.optOpen(i, j, b, o) && f.filter.Value() == "" && !f.noFold {
		var note []string
		if opt.Label != "" {
			note = append(note, opt.Label)
		}
		if picked == "Yes" {
			note = append(note, "Default")
		}
		head.fold, head.note = "closed", strings.Join(note, " · ")
		return []formRow{head}
	}
	pickHelp := "Whether it's picked to begin with: one option can be."
	if a.Kind() == "flags" {
		pickHelp = "Whether it's ticked to begin with: any number can be."
	}
	return []formRow{head,
		{key: k("types"), label: "Value", value: opt.Value, look: types, hint: "what it types, like %H:%M", required: true, help: "What it types into the command, picked.", text: func(v string) error {
			a := f.blank(i, j, b)
			op := &a.Options[o]
			switch {
			case v == "":
				return fmt.Errorf("an option types something")
			case a.Kind() == "flags":
				on := a.Ticked(a.Default)
				if n := slices.Index(on, op.Value); n >= 0 {
					on[n] = v
					a.Default = strings.Join(on, a.Separator())
				}
			case a.Default == op.Value:
				a.Default = v
			}
			op.Value = v
			return nil
		}},
		{key: k("label"), label: "Label", value: opt.Label, hint: "what it means, if it needs saying", help: "Said under the list when the cursor's on it.", text: func(v string) error {
			f.blank(i, j, b).Options[o].Label = v
			return nil
		}},
		{key: k("pick"), label: "Default", value: picked, help: pickHelp, options: []config.Option{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}, pick: func(v string) error {
			a := f.blank(i, j, b)
			opt := a.Options[o]
			if (v == "yes") == pickedFirst(a, opt.Value) {
				return nil
			}
			if a.Kind() == "choose" {
				if v == "yes" {
					a.Default = opt.Value
				} else {
					a.Default = ""
				}
				return nil
			}
			on := slices.DeleteFunc(a.Ticked(a.Default), func(s string) bool { return s == opt.Value })
			if v == "yes" {
				on = append(on, opt.Value)
			}
			a.Default = strings.Join(on, a.Separator())
			return nil
		}},
		{key: k("remove"), label: "Remove", value: "No", risk: config.RiskWarn, help: "Yes removes the option.", confirm: "Yes", do: func() error {
			a := f.blank(i, j, b)
			opt := a.Options[o]
			if a.Kind() == "flags" {
				a.Default = strings.Join(slices.DeleteFunc(a.Ticked(a.Default), func(s string) bool { return s == opt.Value }), a.Separator())
			} else if a.Default == opt.Value {
				a.Default = ""
			}
			a.Options = slices.Delete(a.Options, o, o+1)
			return nil
		}},
	}
}

// optOpen says whether a choice is open: they start collapsed.
func (f *PackForm) optOpen(i, j, b, o int) bool { return f.toggled[partKey(i, j, b, o)] }

// selectedRow is the row under the cursor.
func (f *PackForm) selectedRow() *formRow {
	if f.sel < 0 || f.sel >= len(f.rows) {
		return nil
	}
	return &f.rows[f.sel]
}

func (f *PackForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.w, f.h = msg.Width, msg.Height
		f.menuH = 0
		if f.MenuHeight != nil {
			f.menuH = f.MenuHeight()
		}
		f.aiWorking = f.AIWorking != nil && f.AIWorking()
		f.pageSize = max(f.h-10, 1)
		return f, nil
	case tea.KeyPressMsg:
		cmd := f.key(msg)
		f.tellKeys()
		return f, cmd
	case tea.PasteMsg:
		return f, f.paste(msg)
	}
	return f, nil
}

// paste puts pasted text into the box that's open, the field being typed in
// or the filter, as if typed there; with no box open it does nothing, so a
// pasted ? or / can't act as a key. Lines pasted into a field are joined.
func (f *PackForm) paste(msg tea.PasteMsg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case f.help:
	case f.editing != nil:
		*f.input, cmd = f.input.Update(msg)
	case f.filtering:
		f.filter, cmd = f.filter.Update(msg)
		f.refresh("")
	}
	return cmd
}

// barKeys are the keys that work in the form right now, for the status bar:
// keys are said there and nowhere else.
func (f *PackForm) barKeys() []string {
	switch {
	case f.help:
		return []string{"any key back"}
	case f.editing != nil:
		return []string{"→ go", "← back", "enter next", "esc keep"} // through the text, then out of it, as in the menu; enter keeps it and moves on, esc keeps it and stays
	case f.choosing != nil:
		return []string{"↑↓ move", "→ go", "← back"}
	case f.filtering:
		return []string{"↑↓ move", "→ go", "/ stop filtering", "esc clear"}
	}
	keys := []string{"↑↓ move"}
	if r := f.selectedRow(); r != nil && editable(r) {
		keys = append(keys, "type to edit")
	}
	if r := f.selectedRow(); r != nil {
		if r.header || r.fold == "closed" || r.pick != nil || r.confirm != "" || r.do != nil || r.text != nil {
			keys = append(keys, "→ go")
		}
		if r.header || r.fold == "open" {
			keys = append(keys, "← back")
		}
	}
	if f.filter.Value() != "" { // closed, and still applied: esc clears it first
		keys = append(keys, "/ edit filter", "[ ] jump", "esc clear", "shift-tab menu")
	} else {
		keys = append(keys, "/ filter", "[ ] jump", "esc done", "shift-tab menu")
	}
	return keys
}

// state says what your keys are doing in the form now, in words, for
// clingon's AI: it sees the form as text, where the text cursor and the
// filter's pink ❯ (what says you're typing, to a person) don't show.
func (f *PackForm) state() string {
	switch {
	case f.editing != nil:
		return "typing into " + rowName(f.editing) + ": every key is typed into it, / included. Enter keeps it and moves to the next field the pack still needs (the next field, once it needs none); esc keeps it and stays."
	case f.choosing != nil:
		return "choosing from a list: ↑↓ move, enter takes the choice, ← closes it."
	case f.filtering:
		return "typing in the filter: every key is typed into it. Enter starts editing the field it's on (type the new value next); / closes the box, leaving the filter on; esc clears it."
	case f.filter.Value() != "":
		return fmt.Sprintf("the filter %q is on, its box closed: keys move through the rows it leaves, and typing on a field edits it. / opens the box again, adding to what's there; esc clears it.", strings.TrimSpace(f.filter.Value()))
	}
	return "on the rows: ↑↓ move, typing on a field edits it, / filters."
}

// rowName is a row as the AI's told of it: its label, and whose it is.
func rowName(r *formRow) string {
	whose := [...]string{"the pack's", "the module's", "the command's"}
	switch r.label { // the ones whose labels say whose they are
	case "Pack name", "Module name", "Command name":
		return "the " + strings.ToLower(r.label)
	case "Runs":
		return "what the command runs"
	}
	switch {
	case r.sub > 0:
		return "a blank's " + r.label
	case r.depth >= 0 && r.depth < len(whose):
		return whose[r.depth] + " " + r.label
	}
	return r.label
}

// tellKeys has the menu's status bar say the form's keys, when they change,
// and why a change was refused, while it was: the bar says it, not the form.
func (f *PackForm) tellKeys() {
	if !f.menu {
		return
	}
	lines := f.barKeys()
	if f.issue != "" {
		lines = append([]string{config.FormIssuePrefix + f.issue}, lines...)
	}
	if state := f.state(); state != "" {
		lines = append(lines, config.FormStatePrefix+state)
	}
	if keys := strings.Join(lines, "\n"); keys != f.toldKeys && config.WriteFormKeys(lines) == nil {
		f.toldKeys = keys
	}
}

// edit starts typing into a row's field, in place.
func (f *PackForm) edit(r *formRow, value string) tea.Cmd {
	in := newEditor()
	if f.aiWorking { // the AI typing: its words in its gold, as they grow
		st := in.Styles()
		st.Focused.Text = st.Focused.Text.Foreground(gold)
		st.Focused.CursorLine = st.Focused.CursorLine.Foreground(gold)
		in.SetStyles(st)
	}
	in.SetWidth(max(f.w/2, 20))
	in.SetValue(value)
	in.MoveToEnd()
	f.input, f.editing = &in, r
	return f.input.Focus()
}

// inputAt says whether the cursor's at the end of what's typed (or, with end
// false, its start): where → (or ←) leaves the field, as the arrows always
// move you on.
func (f *PackForm) inputAt(end bool) bool {
	if !end {
		return f.input.Line() == 0 && f.input.Column() == 0
	}
	lines := strings.Split(f.input.Value(), "\n")
	return f.input.Line() == len(lines)-1 && f.input.Column() >= len([]rune(lines[len(lines)-1]))
}

// newEditor is a field being typed in: plain, no prompt or line numbers,
// as tall as what's typed, wrapped.
func newEditor() textarea.Model {
	in := textarea.New()
	in.Prompt, in.ShowLineNumbers, in.Placeholder = "", false, ""
	in.DynamicHeight, in.MinHeight, in.MaxHeight = true, 1, 0
	st := in.Styles()
	for _, s := range []*textarea.StyleState{&st.Focused, &st.Blurred} {
		s.Base, s.CursorLine, s.EndOfBuffer = lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle()
		s.Text = fieldStyle
	}
	st.Cursor.Color = accentStyle.GetForeground()
	in.SetStyles(st)
	return in
}

// wrapAsTyped wraps a value w wide where typing it wraps it, so a field
// doesn't move when it's opened or left: each of plain's lines through an
// editor that w wide, and styled (plain, coloured) cut where it breaks it.
func wrapAsTyped(plain, styled string, w int) []string {
	ed := newEditor()
	ed.SetWidth(w)
	sls := strings.Split(styled, "\n")
	var out []string
	for i, pl := range strings.Split(plain, "\n") {
		sl := pl
		if i < len(sls) {
			sl = sls[i]
		}
		ed.SetValue(pl)
		at, end := 0, ansi.StringWidth(pl)
		for _, l := range strings.Split(ansi.Strip(ed.View()), "\n") {
			n := ansi.StringWidth(strings.TrimRight(l, " "))
			if n == 0 || at >= end {
				continue
			}
			out = append(out, ansi.Cut(sl, at, at+n))
			for at += n; at < end && ansi.Cut(pl, at, at+1) == " "; at++ { // the spaces it broke at
			}
		}
		if end == 0 {
			out = append(out, "")
		}
	}
	return out
}

// typedIn is what's been typed into a field: one line (a long one wraps as
// it's typed, and anything pasted over lines is joined), trimmed.
func typedIn(v string) string { return strings.Join(strings.Fields(v), " ") }

func (f *PackForm) key(msg tea.KeyPressMsg) tea.Cmd {
	k := keyName(msg)
	if f.help { // any key closes it, and does nothing else
		f.help = false
		return nil
	}
	switch k { // the same keys as the menu's lists
	case "ctrl+p", "ctrl+k":
		k = "up"
	case "ctrl+n", "ctrl+j":
		k = "down"
	case "shift+up":
		k = "["
	case "shift+down":
		k = "]"
	}
	if k == "ctrl+c" { // does nothing in the form: esc cancels
		return nil
	}
	switch {
	case f.editing != nil:
		switch {
		case k == "esc" && f.escOnce: // refused, and esc again: undone, back as it was
			f.editing, f.input, f.issue, f.escOnce = nil, nil, "", false
			return nil
		case k == "left" && f.inputAt(false), k == "right" && f.inputAt(true), // past its ends: out of it, kept
			k == "enter", k == "tab", k == "shift+tab", k == "esc": // done: kept (never lost); enter moves on to the next field, tab and shift-tab to the next panel
			v := typedIn(f.input.Value())
			r := f.editing
			if err := f.change(func() error { return r.text(v) }); err != nil {
				f.issue, f.escOnce = err.Error(), k == "esc"
				return nil
			}
			f.editing, f.input, f.issue, f.escOnce = nil, nil, "", false
			f.refresh(r.key)
			switch k {
			case "enter":
				if !f.toNeeded() { // on to what the pack still needs; with nothing needed, the next field
					f.field(1)
				}
			case "tab", "shift+tab":
				f.focusOut(k == "tab")
			}
			return nil
		}
		var cmd tea.Cmd
		*f.input, cmd = f.input.Update(msg) // the arrows move in what's typed
		return cmd
	case f.choosing != nil:
		switch k {
		case "up", "down":
			f.opt = round(f.opt, k, len(f.options))
		case "pgup", "[":
			f.opt = max(f.opt-max(f.pageSize, 1), 0)
		case "pgdown", "]":
			f.opt = min(f.opt+max(f.pageSize, 1), len(f.options)-1)
		case "enter", "right":
			return f.chose()
		case "esc", "left":
			f.choosing = nil
		}
		return nil
	}
	if f.filtering && inText(&f.filter, k) { // through the filter's text, then out of it
		return typeInto(&f.filter, msg)
	}
	if f.filtering { // it takes all text; the arrows still move, and enter or → edits
		switch k {
		case "up", "down", "enter", "right":
			if k == "enter" || k == "right" {
				f.filtering = false
				f.filter.Blur()
			}
		case "esc":
			f.filter.SetValue("")
			fallthrough
		case "/", "left":
			f.filtering = false
			f.filter.Blur()
			f.refresh("")
			return nil
		default:
			cmd := typeInto(&f.filter, msg)
			f.refresh("")
			f.toLabel()
			return cmd
		}
	}
	// Typing on a field starts editing it, what's typed in place of what was
	// there (enter edits it as it is). /, ?, [ and ] keep their jobs.
	if r := f.selectedRow(); r != nil && editable(r) && msg.Text != "" && !strings.ContainsAny(msg.Text[:1], "/?[] ") && (k == "" || len([]rune(k)) == 1) {
		f.issue = ""
		r = f.taken(r)
		cmd := f.edit(r, "")
		f.input.InsertString(msg.Text)
		return cmd
	}
	switch k {
	case "up":
		f.move(-1)
	case "down":
		f.move(1)
	case "pgup": // a page stops at the end
		for range max(f.pageSize, 1) {
			f.step(-1)
		}
	case "pgdown":
		for range max(f.pageSize, 1) {
			f.step(1)
		}
	case "[":
		f.jump(-1)
	case "]":
		f.jump(1)
	case "tab", "shift+tab": // to the next panel (the AI's), or back to the menu
		f.focusOut(k == "tab")
	case "?":
		f.help = true
	case "/":
		f.filtering = true
		return f.filter.Focus()
	case "esc": // as the menu's: clear the filter, else done
		if f.filter.Value() != "" {
			f.filter.SetValue("")
			f.refresh("")
			return nil
		}
		return f.leave()
	case "left": // back, as at the menu's top: nothing above this but a filter left on
		if r := f.selectedRow(); r != nil && (r.header || r.fold == "open") { // on a heading, or an open blank: collapse it
			f.fold(r, false)
			return nil
		}
		if f.filter.Value() != "" {
			f.filter.SetValue("")
			f.refresh("")
		}
		return nil
	case "enter", "right": // edit, open a list, or open what's collapsed
		r := f.selectedRow()
		if r == nil {
			return nil
		}
		f.issue = ""
		r = f.taken(r)
		if r.header { // a module's or command's heading: opens it
			f.fold(r, true)
			return nil
		}
		if r.fold == "closed" { // a collapsed blank: opens it; open, renames it
			f.fold(r, true)
			return nil
		}
		switch {
		case r.pick != nil: // a list, even if it can also be typed (from the list): from the top
			f.choosing, f.options, f.opt, f.optTop = r, r.options, 0, 0
		case r.text != nil:
			return f.edit(r, r.value)
		case r.confirm != "":
			f.choosing, f.opt, f.optTop = r, 0, 0
			f.options = []config.Option{{Value: "no", Label: "No"}, {Value: "drop", Label: r.confirm}}
		case r.do != nil:
			if err := f.change(r.do); err != nil {
				f.issue = err.Error()
				f.refresh("")
				return nil
			}
			f.refresh(r.then) // on to what was added, named already: → renames it
		}
	}
	return nil
}

// chose acts on the option picked from the list under a row.
func (f *PackForm) chose() tea.Cmd {
	r, v := f.choosing, f.options[f.opt].Value
	f.choosing, f.issue = nil, ""
	switch {
	case r.confirm != "": // a remove's No/Yes: only Yes does it
		if v != "drop" {
			return nil
		}
		if err := f.change(r.do); err != nil {
			f.issue = err.Error()
		}
		f.refresh("")
		return nil
	case v == newSection: // typed on its row, starting empty
		return f.edit(r, "")
	}
	if err := f.change(func() error { return r.pick(v) }); err != nil {
		f.issue = err.Error()
	}
	f.refresh(r.key)
	return nil
}

func (f *PackForm) View() tea.View {
	v := tea.NewView(f.view())
	v.AltScreen = true
	return v
}

// view is the form, scrolled to keep the cursor in view, the help below it,
// and, with the room, a live preview beside it of what the cursor's on.
func (f *PackForm) view() string {
	if f.w == 0 {
		return ""
	}
	// The form fills the pane, on the window's own background as the menu
	// is; with the room, its preview is beside it, edge to edge, a step
	// lighter, a page laid on top, with padding inside it; the form itself
	// runs edge to edge, as the menu's lists do.
	formW, previewW := f.w, 0
	if f.w >= minPreviewWidth {
		formW = f.w / 2 // the menu's a third of the window: the form and its preview a third each
		previewW = f.w - formW
	}
	inner := formW // edge to edge, as the menu's lists are: no padding of its own
	foot := f.footer(inner)
	// Rows level with the menu's: blank by its logo, where you are by its
	// path (row 3), the filter by its filter (row 5), the form from row 7 by
	// its list; the help and keys at the bottom, a row from the edge.
	top := []string{"", "", "", f.crumb(inner), "", f.filterLine(inner), ""} // level with the menu's: the logo, a gap, the path
	// Its foot, under a rule level with the menu's info pane's, stays put:
	// what the row the cursor's on is for, then under a rule of its own the
	// save question, its rows and no more. With no room for both, the
	// question.
	// Level with the menu's: as far from the menu's foot as its info pane's
	// rule is, so less the rows under the form (the AI's strip).
	menuH := f.h
	if f.menuH > 0 {
		menuH = f.menuH
	}
	footRows := max(infoTop(menuH)-(menuH-f.h), 0)
	if f.aiWorking { // what a row's for is for people: the AI reads the rows themselves
		footRows = 0
	}
	rule := faintStyle.Render(strings.Repeat("─", inner))
	var panel []string
	if footRows > 0 {
		panel = []string{rule}
	}
	if k := footRows - 1; k > 0 { // what the row's for, or what went wrong, whole
		st := dimStyle
		if f.issue != "" { // in the status bar's amber
			st = warnStyle
		}
		panel = append(panel, strings.Split(styledBlock(st, f.rowHelp(), inner, k), "\n")...)
	}
	room := max(f.h-len(top)-len(foot)-footRows, 1)
	body, at, row, heads := f.cards(inner)
	if f.help {
		body, at, row, heads = f.keys(inner), 0, 0, nil
	} else if len(f.rows) == 0 {
		body = []string{faintStyle.Render("Nothing here has " + strings.TrimSpace(f.filter.Value()) + " in it.")}
	}
	body = scroll(body, at, row, room, f.choosing != nil, heads)
	form := append(top, body...)
	for len(form) < len(top)+room {
		form = append(form, "")
	}
	form = append(append(form, foot...), panel...)
	var prev []string
	if previewW > 0 { // its title level with where you are (the form's path, row 3)
		prev = append([]string{"", "", ""}, f.preview(previewW-2*panelPad, f.h-4)...) // each with its own left padding, the gutter for a mark
	}
	lines := make([]string, 0, f.h)
	for k := range f.h {
		l, p := "", ""
		if k < len(form) {
			l = form[k]
		}
		if k < len(prev) {
			p = prev[k]
		}
		line := l
		if previewW > 0 {
			line += strings.Repeat(" ", max(formW-ansi.StringWidth(line), 0)) + onBg(p, previewBg, previewW)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// The preview is raised, as the menu's command block and cursor's row are;
// a block set into it is the window's own background again.
var (
	previewBg    = raised
	formCursorBg = raised
)

const panelPad = 2 // columns inside each panel's sides

// crumb is where you are in the form, as the menu's path says where you are
// in it: the way there dim, where you are bright.
func (f *PackForm) crumb(w int) string {
	icon := slotIcon(config.Logos[f.pack.Logo])
	or := func(name, unnamed string) string { // not named yet: what it is
		if strings.TrimSpace(name) == "" {
			return unnamed
		}
		return name
	}
	parts := []string{"My packs", or(f.pack.Title, "New pack")}
	if r := f.selectedRow(); r != nil {
		var i, j int
		if n, _ := fmt.Sscanf(r.key, "%d/%d/", &i, &j); n >= 1 && i < len(f.pack.Files) {
			parts = append(parts, or(f.group(i).Name, "New module"))
			if n == 2 && j < len(f.group(i).Commands) {
				parts = append(parts, or(f.command(i, j).Name, "New command"))
			}
		}
	}
	crumb := ""
	for i, p := range parts {
		if i == 1 { // the pack's logo, before its name: styled on its own, so it's drawn the same bright or dim
			crumb += iconStyle(f.logoColour()).Render(icon) + strings.Repeat(" ", max(3-ansi.StringWidth(icon), 1))
		}
		if i < len(parts)-1 {
			crumb += dimStyle.Render(p) + faintStyle.Render(" › ")
		} else {
			crumb += crumbStyle.Render(p)
		}
	}
	return ansi.Truncate(crumb, w, "…")
}

// filterLine is the form's filter, drawn as the menu's.
func (f *PackForm) filterLine(w int) string {
	switch {
	case f.filtering:
		return inputView(&f.filter, w)
	case f.filter.Value() != "":
		return ansi.Truncate(faintStyle.Render("❯ ")+strings.TrimSpace(f.filter.Value()), w, "…") // the status bar says how to edit or clear it; pink only while it's typed in
	}
	return faintStyle.Render("❯ ") + renderKey("/") + faintStyle.Render(" to filter")
}

// logoColour is the colour the pack's logo is drawn in.
func (f *PackForm) logoColour() string {
	for _, c := range config.PackColors {
		if c.Name == f.pack.Color {
			return c.Hex
		}
	}
	return config.LogoColor(f.pack.Logo)
}

// The preview needs the panel's room: below this, it's just the form.
const minPreviewWidth = 100

var (
	fieldStyle = lipgloss.NewStyle().Foreground(text) // a field's value: the menu's white
)

// guide is a row's tree guide: what's to the left of it, showing which
// module and command it's part of. A field's ends in its level's │, with
// the cell after it for the cursor's bar; an add that ends its level is on
// the level's └, the bar in place of its ─.
// flatForm draws the form without its tree: the pack's, a module's and a
// command's rows all start in one column, under their headings. A trial.
const flatForm = true

func guide(r *formRow) string {
	if flatForm {
		return ""
	}
	cont := strings.Repeat("│  ", max(r.depth-1, 0))
	switch {
	case r.header && r.depth == 0:
		return ""
	case r.header:
		return cont + "├─ "
	case r.branch:
		return cont + "└"
	}
	return strings.Repeat("│  ", r.depth) + "│"
}

// keys is the form's keys, as the menu's ? lists its own: the same keys,
// doing the same things.
func (f *PackForm) keys(w int) []string {
	keys := [][2]string{
		{"↑ ↓", "move"},
		{"→ or enter", "go: edit, open, or take the choice; typing, done and on"},
		{"←", "back: close what's open"},
		{"[ ]", "jump to the previous or next heading"},
		{"/", "filter; again stops"},
		{"esc", "cancel typing, clear the filter, go back, or close the form"},
		{"tab/shift-tab", "the next or previous field"},
		{"?", "this screen"},
	}
	lines := []string{headerStyle.Render("KEYS"), ""}
	for _, k := range keys {
		lines = append(lines, ansi.Truncate(renderKey(fmt.Sprintf("%-13s", k[0]))+" "+k[1], w, "…"))
	}
	lines = append(lines, "")
	lines = append(lines, wrapSpaces(dimStyle.Render("Changes are kept as you make them: a new pack is added once it has a name and a command. Esc leaves the form."), w)...)
	return lines
}

// formHeading is a part's heading: what it is in the menu's heading style, and
// its name as it was written, in white: COMMAND · Say hello.
func formHeading(label string) string {
	kind, name, ok := strings.Cut(label, " · ")
	if !ok {
		return headerStyle.Render(strings.ToUpper(label))
	}
	return headerStyle.Render(strings.ToUpper(kind)) + faintStyle.Render(" · ") + fieldStyle.Render(name)
}

// formHead is where a part of the form starts, on the page: a heading (the
// pack, a module, a command), or an add that ends its level.
type formHead struct {
	line, depth int
	name        int // its Name row's line, pinned with it; -1 if it isn't shown
	ends        bool
}

// scroll is the room lines of the form to show, keeping line at in view.
// The headings of what's at the top stay pinned there, nested, so you can
// always see which pack, module and command you're in; and with a list open
// (sticky), the row it's for is pinned under them, so what's being asked
// stays on screen.
func scroll(lines []string, at, row, room int, sticky bool, heads []formHead) []string {
	if len(lines) <= room {
		return lines
	}
	window := func(rest int) int { return min(max(at-rest/2, 0), len(lines)-rest) }
	pins := func(start int) []int {
		var chain [3]*formHead
		for i := range heads {
			h := &heads[i]
			if h.line >= start {
				break
			}
			for d := h.depth; d < len(chain); d++ {
				chain[d] = nil
			}
			if !h.ends {
				chain[h.depth] = h
			}
		}
		var out []int
		for _, h := range chain { // each heading, with its Name under it
			if h == nil {
				continue
			}
			out = append(out, h.line)
			if h.name >= 0 && h.name < start {
				out = append(out, h.name)
			}
		}
		if sticky && row < start {
			out = append(out, row)
		}
		return out
	}
	start := window(room)
	var pinned []int
	for range 3 { // pinning takes room, which moves what's shown, which can change what's pinned
		pinned = pins(start)
		rest := max(room-len(pinned), 1)
		next := max(window(rest), 0)
		if len(pinned) > 0 {
			next = max(next, pinned[len(pinned)-1]+1)
		}
		next = min(next, len(lines)-rest)
		if next == start && len(pins(next)) == len(pinned) {
			break
		}
		start = next
	}
	pinned = pins(start)
	rest := max(room-len(pinned), 1)
	start = min(max(start, 0), len(lines)-rest)
	// Rows scrolled away between the pinned headings and what's shown: a
	// line says so, or they'd look gone (a command showing its Name, then
	// its Default, with nothing between).
	gap := len(pinned) > 0 && start > pinned[len(pinned)-1]+1 && rest > 1
	if gap {
		rest--
		if at >= start+rest { // what's kept in view stays in view
			start = min(at-rest+1, len(lines)-rest)
		}
	}
	out := make([]string, 0, room)
	for _, l := range pinned {
		out = append(out, lines[l])
	}
	if gap {
		out = append(out, faintStyle.Render(moreAbove))
	}
	return append(out, lines[start:min(start+rest, len(lines))]...)
}

// moreAbove marks rows scrolled out of view above, under the pinned headings.
const moreAbove = "      … more above"

// cards draws the form as the menu draws its lists: uppercase headings, a
// row per field with grey labels and bright values, tree guides on the left
// showing what's part of what. Every value starts in one column, down the
// page. It says on which line the cursor is, to keep it in view, and which
// line the cursor's row is (the same, unless a list is open under it).
func (f *PackForm) cards(w int) ([]string, int, int, []formHead) {
	col := 0 // where values start: as if every part were open, so it stays put as they open and fold
	all := f.unfolded()
	for i := range all {
		if r := &all[i]; !r.header && r.do == nil {
			col = max(col, f.labelAt(r)+ansi.StringWidth(r.label)+2)
		}
	}
	col = min(col, max(w/2, 12))
	var out []string
	var heads []formHead
	at, row := 0, 0
	for i := range f.rows {
		r := &f.rows[i]
		if r.header || r.branch { // a new part of the tree, a line apart
			if len(out) > 0 {
				gap := strings.TrimRight(strings.Repeat("│  ", max(r.depth-1, 0))+"│", " ")
				if flatForm {
					gap = ""
				}
				out = append(out, faintStyle.Render(gap))
			}
		}
		if r.branch {
			heads = append(heads, formHead{line: len(out), depth: r.depth, name: -1, ends: true})
		}
		if r.header {
			heads = append(heads, formHead{line: len(out), depth: r.depth, name: -1})
			if i == f.sel {
				at, row = len(out), len(out)
			}
			if flatForm {
				out = append(out, f.headLine(r, i == f.sel, col, w))
			} else {
				out = append(out, faintStyle.Render(guide(r))+ansi.Truncate(formHeading(r.label), max(w-ansi.StringWidth(guide(r)), 1), "…"))
			}
			continue
		}
		on := i == f.sel
		if on {
			at, row = len(out), len(out)
		}
		if n := len(heads) - 1; n >= 0 && i > 0 && f.rows[i-1].header && strings.HasSuffix(r.key, "name") && r.required {
			heads[n].name = len(out) // pinned with its heading
		}
		open := on && f.choosing != nil
		out = append(out, strings.Split(f.rowLine(r, on && !open, open, col, w), "\n")...) // wrapped over lines, a long value
		if on && f.editing != nil {                                                        // typing: the line the cursor's on, in view
			at += f.input.Line()
		}
		if open { // the row's list, dropped down under it, with the cursor; a long one listRows at a time
			f.optTop = min(f.optTop, f.opt)
			f.optTop = max(f.optTop, f.opt-listRows+1)
			end := min(f.optTop+listRows, len(f.options))
			for o := f.optTop; o < end; o++ {
				if o == f.opt {
					at = len(out) // the list's cursor, to keep in view
				}
				out = append(out, f.optionLine(r, f.options[o], o == f.opt, col, w))
			}
			if len(f.options) > listRows { // what's out of view, as the menu's lists say it
				more := ""
				switch {
				case end < len(f.options):
					more = fmt.Sprintf("↓ %d more", len(f.options)-end)
				case f.optTop > 0:
					more = fmt.Sprintf("↑ %d more", f.optTop)
				}
				out = append(out, faintStyle.Render(guide(r)+strings.Repeat(" ", max(col-ansi.StringWidth(guide(r)), 1))+more))
			}
		}
	}
	return out, at, row, heads
}

// headLine is a heading in the flat form: the cursor's bar, then for a
// module or command an arrow saying whether it's open (▾) or collapsed (▸),
// then the heading; collapsed, its name beside it in the values' column.
// cur is the style of the form's own cursor.
func (f *PackForm) cur() lipgloss.Style { return cursorStyle }

func (f *PackForm) headLine(r *formRow, on bool, col, w int) string {
	slot := " "
	if on {
		slot = cursorBar(f.cur())
	}
	arrow := " "
	if r.key != "" {
		arrow = "▾"
		if r.value != "" {
			arrow = "▸"
		}
	}
	head := strings.ToUpper(r.label)
	line := slot + " " + lipgloss.NewStyle().Foreground(soft).Render(arrow) + " " + headerStyle.Render(head)
	if r.value != "" {
		v := fieldStyle.Render(r.value)
		if r.look != nil {
			v = r.look(r.value)
		}
		line += strings.Repeat(" ", max(col-4-ansi.StringWidth(head), 2)) + v
		if r.note != "" {
			line += faintStyle.Render("  · " + r.note)
		}
	}
	line = ansi.Truncate(line, w, "…")
	if on {
		return onBg(line, formCursorBg, w)
	}
	return line
}

// labelAt is the column a row's label starts in: past its guide, the bar's
// cell and a space, and set in under its blank for a blank's own rows.
func (f *PackForm) labelAt(r *formRow) int {
	n := ansi.StringWidth(guide(r)) + 2
	if flatForm {
		n += 2 // under its heading, past the heading's arrow
	}
	return n + 2*r.sub
}

// rowLine is a row as the menu draws one, the cursor's row a bar across the
// form: its label, and its value (or the field being typed), with a periwinkle ▾
// after a value that's picked from a list. A blank's label is sky, as blanks
// are in commands; an add is a ghost of the headings' purple; a remove is a
// muted red until the cursor's on it. A row with its list open keeps its label
// pink, the cursor in the list.
func (f *PackForm) rowLine(r *formRow, on, open bool, col, w int) string {
	var more []string // a value's lines past its first, wrapped or typed
	at := 0           // the column they're in: the value's
	g := guide(r)
	slot := " "
	switch {
	case on:
		slot = cursorBar(f.cur())
	case r.branch && !flatForm:
		slot = "─"
	}
	rest := " " + strings.Repeat("  ", r.sub)
	if flatForm {
		switch r.fold { // a blank's arrow, in the column the headings' are in
		case "open":
			rest += lipgloss.NewStyle().Foreground(soft).Render("▾") + " "
		case "closed":
			rest += lipgloss.NewStyle().Foreground(soft).Render("▸") + " "
		default:
			rest += "  "
		}
	}
	room := max(w-f.labelAt(r), 1)
	switch {
	case r.do != nil && r.confirm == "" && flatForm: // an add, as a ghost of what it adds, where it'll be
		// A dim purple (61): a ghost of the headings, readable but not one yet.
		word, st := "", lipgloss.NewStyle().Foreground(ghostPurple)
		switch {
		case r.key == "add-module":
			word = "ADD MODULE"
		case strings.Count(r.key, "/") == 1:
			word = "ADD COMMAND"
		default: // an option: in the Option rows' place
			word, st = "Option", dimStyle
		}
		if on {
			st = f.cur()
		}
		rest = " " + st.Render("+") + " " + st.Render(word)
	case r.confirm != "" && flatForm: // a remove: a muted red − and REMOVE, as an add's + and ADD; enter asks No or Yes under it
		st := lipgloss.NewStyle().Foreground(ghostRed)
		if on || open {
			st = f.cur()
		}
		rest = " " + st.Render("−") + " " + st.Render("REMOVE")
	case r.do != nil && r.confirm == "": // an add: a ghost of the headings, as the flat form's are
		st := lipgloss.NewStyle().Foreground(ghostPurple)
		if on {
			st = f.cur()
		}
		rest += st.Render(ansi.Truncate("+ "+r.label, room, "…"))
	default:
		label := ansi.Truncate(r.label, max(col-f.labelAt(r)-2, 1), "…")
		pad := strings.Repeat(" ", max(col-f.labelAt(r)-ansi.StringWidth(label), 1))
		switch {
		case on || open:
			label = f.cur().Render(label)
		case strings.HasPrefix(r.label, "<"): // a blank: sky, as blanks are
			label = lipgloss.NewStyle().Foreground(sky).Render(label)
		default:
			label = dimStyle.Render(label)
		}
		rest += label + pad
		room = max(w-col-2, 1)                                 // and the ▾
		at = ansi.StringWidth(g+slot) + ansi.StringWidth(rest) // where the value starts, and its lines after the first
		switch {
		case f.editing != nil && f.editing.key == r.key:
			f.input.SetWidth(room)
			more = strings.Split(f.input.View(), "\n")
			rest, more = rest+more[0], more[1:]
		case r.confirm != "": // a remove, were the form not flat: faint, its No, until you're on it
			st := faintStyle
			if on {
				st = fieldStyle
			}
			rest += st.Render(r.value)
		case r.value != "":
			v := fieldStyle.Render(r.value)
			if r.look != nil {
				if l := r.look(r.value); l != ansi.Strip(l) { // plain in the file: the form's plain white
					v = l
				}
			}
			if r.icon != "" {
				rest += iconStyle(r.color).Render(r.icon) + iconGap(r.icon)
				room = max(room-1-len(iconGap(r.icon)), 1)
			}
			if r.text == nil { // a pick: one line
				rest += ansi.Truncate(v, room, "…")
				break
			}
			lines := wrapAsTyped(r.value, v, room) // typed: wrapped, as it's typed in
			rest, more = rest+lines[0], lines[1:]
		case r.hint != "" && (on || r.required): // only where you are, or what has to be filled in: elsewhere they'd fill the form with what isn't yours
			rest += faintStyle.Italic(true).Render(ansi.Truncate(capitalFirst(r.hint), room, "…")) // faint, as placeholders are: in the values' column, it mustn't pass for one
		case r.text != nil:
			rest += faintStyle.Render("—")
		}
		if r.note != "" {
			rest += faintStyle.Render(ansi.Truncate("  · "+r.note, max(w-ansi.StringWidth(g)-1-ansi.StringWidth(rest), 0), ""))
		}
		if r.pick != nil || r.confirm != "" {
			rest += lipgloss.NewStyle().Foreground(soft).Render(" ▾") // periwinkle: plainly there, under the white value
		}
	}
	lines := []string{rest}
	for _, l := range more {
		lines = append(lines, strings.Repeat(" ", max(at-ansi.StringWidth(g+slot), 0))+l)
	}
	for i, l := range lines {
		s := slot
		if i > 0 && on {
			s = cursorBar(f.cur())
		} else if i > 0 {
			s = " "
		}
		if on {
			lines[i] = faintStyle.Render(g) + onBg(s+l, formCursorBg, max(w-ansi.StringWidth(g), 1))
		} else {
			lines[i] = faintStyle.Render(g+s) + l
		}
	}
	return strings.Join(lines, "\n")
}

// listRows is how many of a list's options are in view at once.
const listRows = 10

// optionLine is one of the options in the list dropped down under row r, set
// in under its value. The list's cursor is the form's, as on every row: the
// bar at the left, and the band across.
func (f *PackForm) optionLine(r *formRow, o config.Option, on bool, col, w int) string {
	title := o.Label
	if title == "" {
		title = o.Value
	}
	switch {
	case on && o.Value == "drop": // a remove's Yes
		title = dangerStyle.Render(title)
	case on:
		title = cursorStyle.Render(title)
	default: // white, as values are
		title = fieldStyle.Render(title)
	}
	g := strings.Repeat("│  ", r.depth) + "│"
	switch {
	case flatForm:
		g = ""
	case r.branch:
		g = strings.Repeat("│  ", max(r.depth-1, 0))
	}
	rest := strings.Repeat(" ", max(col-ansi.StringWidth(g)-1, 1)) // the option under the value; the bar where every row's is
	if o.Icon != "" {
		st := iconStyle(o.Color)
		if o.Color == "" { // one without a colour of its own, as its title
			st = fieldStyle
		}
		rest += st.Render(o.Icon) + iconGap(o.Icon)
	}
	rest += title
	if on {
		return faintStyle.Render(g) + onBg(cursorBar(cursorStyle)+rest, formCursorBg, max(w-ansi.StringWidth(g), 1))
	}
	return faintStyle.Render(g) + " " + rest
}

// preview is the pack's _pack.md, and under it the module the cursor's in
// (or was last in), as they'll be saved,
// coloured as internal/mdcolor colours it: the pack's _pack.md, or a module's file
// for its rows and its commands'.
func (f *PackForm) preview(w, h int) []string {
	md := f.pack.Markdown()
	// While a field's being typed in, the preview shows it as typed: the
	// change is made to a copy of the pack, which is then thrown away.
	if r := f.editing; r != nil && f.input != nil && r.text != nil {
		real := f.pack
		f.pack = clonePack(real)
		if r.text(typedIn(f.input.Value())) == nil {
			md = f.pack.Markdown()
		}
		f.pack = real
	}
	key, sub := "", -2
	if r := f.selectedRow(); r != nil {
		key = r.key
		sub, _ = openPart(key)
	}
	if sub >= 0 {
		f.lastSub = sub
	}
	f.lastSub = min(f.lastSub, len(f.pack.Files)-1)
	lines := func(file string) []string { return strings.Split(md[file], "\n") }
	// The pack's own file, always, whole unless it would take over half.
	var on map[int]bool
	if sub == -1 {
		on = marked(key, lines(config.PackInfoFile))
	}
	out := filePreview(config.PackInfoFile, md[config.PackInfoFile], on, -1, w, max(h/2, 1))
	if f.lastSub < 0 {
		return out
	}
	// Under it, the module you're in, or were last in, in what's left.
	file := f.pack.Files[f.lastSub].Slug + ".md"
	on, near := nil, -1
	if sub == f.lastSub {
		on, near = marked(key, lines(file)), partLine(key, lines(file))
	}
	return append(append(out, ""), filePreview(file, md[file], on, near, w, max(h-len(out)-1, 3))...)
}

func fit2(lines []string, h int) []string {
	if len(lines) > h {
		return lines[:h]
	}
	return lines
}

// filePreview is a pack's file, coloured, h lines at most: the
// lines in on marked, each with a bar in the gutter on a band of the
// window's own colour, scrolled into view. Every line has the panel's
// padding at its left, the gutter the bar's in.
func filePreview(name, text string, on map[int]bool, near, w, h int) []string {
	pad := strings.Repeat(" ", panelPad)
	rule := pad + faintStyle.Render(strings.Repeat("─", w))
	head := []string{pad + headerStyle.Render(ansi.Truncate(name, w, "…")), rule} // as it is: a file's name is case-sensitive
	var body []string
	first, last := -1, -1 // the marked rows, first and last
	plain := strings.Split(text, "\n")
	code := false   // in a fenced code block
	var rowOf []int // each line's first row
	for i, l := range strings.Split(strings.TrimRight(mdcolor.Markdown(text), "\n"), "\n") {
		src := ""
		if i < len(plain) {
			src = plain[i]
		}
		fence := strings.HasPrefix(strings.TrimSpace(src), "```")
		rowOf = append(rowOf, len(body))
		rows := wrapPreview(l, src, code && !fence, w)
		if fence {
			code = !code
		}
		for _, r := range rows {
			if on[i] {
				if first < 0 {
					first = len(body)
				}
				last = len(body)
				body = append(body, marked1(r, w))
				continue
			}
			body = append(body, pad+r)
		}
	}
	if near >= len(rowOf) { // past the file's end: its end
		near = len(body) - 1
	} else if near >= 0 {
		near = rowOf[near]
	}
	body = append(body, rule)
	room := max(h-len(head), 1)
	if first < 0 { // nothing of the row's in the file yet (no risk, say): where it goes, in view
		first, last = near, near
	}
	if len(body) > room { // the marked rows in view, a little way down, all of them if they fit
		start := 0
		if first >= 0 {
			start = min(max(first-room/3, 0), len(body)-room)
			if last >= start+room {
				start = min(last-room+1, first)
			}
		}
		body = body[start : start+room]
	}
	return append(head, body...)
}

// listItem is a Markdown list item's marker, and a heading's: what a long
// one's wrapped rows hang under.
var listItem = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)]|#{1,6})\s+`)

// wrapPreview is one line of a pack's file, coloured, in rows w wide,
// broken between words (and inside a word too long for a row). A list
// item's or heading's rows hang under its text; a code line's go on after a
// faint ↪, so they can't be taken for lines of their own.
func wrapPreview(styled, plain string, code bool, w int) []string {
	if ansi.StringWidth(styled) <= w {
		return []string{styled}
	}
	lead := ""
	switch {
	case code:
		ind := len(plain) - len(strings.TrimLeft(plain, " \t"))
		lead = strings.Repeat(" ", ind) + faintStyle.Render("↪") + " "
	default:
		lead = strings.Repeat(" ", ansi.StringWidth(listItem.FindString(plain)))
	}
	if ansi.StringWidth(lead) > w/2 {
		lead = ""
	}
	rows := strings.Split(lipgloss.Wrap(styled, max(w-ansi.StringWidth(lead), 1), ""), "\n")
	for i := 1; i < len(rows); i++ {
		rows[i] = lead + rows[i]
	}
	return rows
}

// partLine is the line of a module's file the row keyed key is part of, for
// the preview to show even when the row writes nothing there yet: its blank's
// list item, its command's heading, or for Add a command the file's end,
// where it goes; -1 for the module's own rows.
func partLine(key string, lines []string) int {
	parts := strings.Split(key, "/")
	var on map[int]bool
	switch {
	case len(parts) == 2 && parts[1] == "add":
		return len(lines) - 1
	case len(parts) >= 4:
		on = marked(strings.Join(parts[:3], "/")+"/q", lines)
	case len(parts) == 3:
		on = marked(strings.Join(parts[:2], "/")+"/name", lines)
	}
	first := -1
	for i := range on {
		if first < 0 || i < first {
			first = i
		}
	}
	return first
}

// marked1 is a marked line: a pink bar in the gutter, and the line on a
// band of the window's own colour, set into the raised preview, w wide.
func marked1(line string, w int) string {
	bar := cursorBar(cursorStyle) + strings.Repeat(" ", panelPad-1)
	if lipgloss.NewStyle().Background(raised).Render(" ") == " " { // no colour here (tests)
		return bar + line
	}
	if n := w - ansi.StringWidth(line); n > 0 {
		line += strings.Repeat(" ", n)
	}
	// Not 49m alone: the panel's onBg puts its own colour back after that.
	const def = "\x1b[0;49m"
	return cursorBar(cursorStyle) + def + strings.Repeat(" ", panelPad-1) +
		strings.NewReplacer("\x1b[0m", "\x1b[0m"+def, "\x1b[m", "\x1b[m"+def).Replace(line) + "\x1b[0m"
}

// marked is which lines of a pack's file the row keyed key writes: its
// heading, front matter, callout, code or list item. A remove marks all
// it removes. Lines count from 0, as the file's do from 1.
func marked(key string, lines []string) map[int]bool {
	on := map[int]bool{}
	mark := func(from, to int) {
		for i := from; i <= to && i >= 0 && i < len(lines); i++ {
			on[i] = true
		}
	}
	find := func(prefix string, from, to int) int {
		for i := max(from, 0); i <= to && i < len(lines); i++ {
			if strings.HasPrefix(lines[i], prefix) {
				return i
			}
		}
		return -1
	}
	last := len(lines) - 1
	for last > 0 && lines[last] == "" {
		last--
	}
	fmEnd := -1 // the front matter's closing ---
	if len(lines) > 0 && lines[0] == "---" {
		fmEnd = find("---", 1, last)
	}
	title := find("# ", fmEnd+1, last)
	heads := []int{}
	for i := title + 1; i <= last && title >= 0; i++ {
		if strings.HasPrefix(lines[i], "## ") {
			heads = append(heads, i)
		}
	}
	descOf := func(from, to int) { // the text under a heading, up to the next thing
		for i := from; i <= to && lines[i] != "" && !strings.HasPrefix(lines[i], "## "); i++ {
			on[i] = true
		}
	}
	parts := strings.Split(key, "/")
	switch field := parts[len(parts)-1]; {
	case len(parts) == 1: // the pack's _pack.md
		switch field {
		case "name":
			mark(title, title)
		case "desc":
			descOf(title+1, last)
		case "menu", "logo", "tint", "needs":
			mark(find(field+":", 1, fmEnd-1), find(field+":", 1, fmEnd-1))
		}
	case len(parts) == 2: // a module's own
		switch field {
		case "name":
			mark(title, title)
		case "desc":
			descOf(title+1, last)
		case "needs", "when":
			mark(find(field+":", 1, fmEnd-1), find(field+":", 1, fmEnd-1))
		case "remove":
			mark(0, last)
		}
	default: // a command's, or one of its blanks'
		var j int
		if _, err := fmt.Sscanf(parts[1], "%d", &j); err != nil || j >= len(heads) {
			return on
		}
		start, end := heads[j], last
		if j+1 < len(heads) {
			end = heads[j+1] - 1
		}
		for end > start && lines[end] == "" {
			end--
		}
		open := find("```", start+1, end)
		shut := find("```", open+1, end)
		if len(parts) == 3 {
			switch field {
			case "name":
				mark(start, start)
			case "risk":
				mark(find("> [!", start+1, start+1), find("> [!", start+1, start+1))
			case "desc":
				for i := start + 1; i < open; i++ {
					if lines[i] != "" && !strings.HasPrefix(lines[i], "> [!") {
						on[i] = true
					}
				}
			case "cmd":
				mark(open+1, shut-1)
			case "needs", "when": // set on the code block's first line, or to be
				mark(open, open)
			case "remove":
				mark(start, end)
			}
			return on
		}
		var b int
		if _, err := fmt.Sscanf(parts[2], "%d", &b); err != nil {
			return on
		}
		item, n := -1, -1 // the blank's list item
		for i := shut + 1; i <= end && shut >= 0; i++ {
			if strings.HasPrefix(lines[i], "- `<") {
				if n++; n == b {
					item = i
					break
				}
			}
		}
		if item < 0 {
			return on
		}
		subEnd := item // its choices, under it
		for subEnd+1 <= end && strings.HasPrefix(lines[subEnd+1], "  - ") {
			subEnd++
		}
		var o int
		if len(parts) == 5 { // one of a choice's own rows: its line
			if _, err := fmt.Sscanf(parts[3], "%d", &o); err == nil {
				mark(item+1+o, item+1+o)
			}
			return on
		}
		switch _, err := fmt.Sscanf(field, "%d", &o); {
		case err == nil: // a choice
			mark(item+1+o, item+1+o)
		case field == "q" || field == "name":
			mark(item, item)
		case field == "kind":
			mark(item, subEnd)
		case field == "start": // its default, or where it goes
			if subEnd > item {
				mark(subEnd, subEnd)
			} else {
				mark(item, item)
			}
		}
	}
	return on
}

// footer is only there for a change that couldn't be saved: why, as an
// orange chip like the status bar's.
func (f *PackForm) footer(w int) []string {
	if f.issue != "" && !f.menu { // on its own, with no status bar to say it on
		return []string{issueChip(f.issue, w)}
	}
	return nil // beside the menu, its status bar says it
}

// issueChip is a problem as an orange chip, like the status bar's.
func issueChip(msg string, w int) string {
	return stepStyle.Background(orange).Render(ansi.Truncate(" ISSUE: "+msg+" ", w, "…"))
}

func lineCount(s string) int { return strings.Count(s, "\n") + 1 }

func padLines(s string, n int) string {
	if c := lineCount(s); c < n {
		s += strings.Repeat("\n", n-c)
	}
	return s
}

// clonePack is a copy of p to go back to, every slice its own.
func clonePack(p *config.EditablePack) *config.EditablePack {
	c := *p
	c.Needs = slices.Clone(p.Needs)
	c.Files = make([]config.EditableFile, len(p.Files))
	for i, file := range p.Files {
		g := file.Group
		g.Requires, g.When = slices.Clone(g.Requires), slices.Clone(g.When)
		g.Commands = make([]config.Command, len(file.Group.Commands))
		for j, cmd := range file.Group.Commands {
			cmd.Requires, cmd.When = slices.Clone(cmd.Requires), slices.Clone(cmd.When)
			cmd.Args = make([]config.Arg, len(cmd.Args))
			for k, a := range file.Group.Commands[j].Args {
				a.Options = slices.Clone(a.Options)
				cmd.Args[k] = a
			}
			g.Commands[j] = cmd
		}
		c.Files[i] = config.EditableFile{Slug: file.Slug, Group: g}
	}
	return &c
}
