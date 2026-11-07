package ui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/micro"
)

// PackForm edits one of your packs as a form, in place of its Markdown. It's
// one flat page, read top to bottom like a paper form: the pack's card, then
// each module's card with its commands' cards under it, each command's blanks
// and choices as rows of its card. Nothing opens onto another screen: ↑↓ move
// through every row, the page scrolling to follow, → or enter edit the row in
// place (a list drops down under it), ← closes what's open, and esc is done. A change that
// would break the pack is refused, saying why, so what you leave with can
// always be saved. Opened from the menu, leaving keeps your changes as a
// draft for the menu to ask whether to save; otherwise leaving saves them.
type PackForm struct {
	pack *config.EditablePack
	rows []formRow
	sel  int // the row the cursor's on
	w, h int

	// One of these at a time: a field being typed, or a list being picked
	// from (a field's options, a choice's actions, a remove's No/Yes).
	input    *textinput.Model
	editing  *formRow
	choosing *formRow
	options  []config.Option
	opt      int

	// The filter, at the top as on the menu's lists: / opens it.
	filter    textinput.Model
	filtering bool

	issue string // why the last change wasn't made

	menu     bool // opened from the menu: leaving keeps a draft for it to ask about
	asking   bool // the menu's asking whether to save the draft; the form stays open meanwhile
	help     bool // the keys, in place of the form
	pageSize int  // rows a page up or down moves

	// The part the cursor's in, shown whole; the others are folded to
	// their heading and name. -1 is the pack, -2 nothing (Add a module).
	openSub, openCmd int
	noFold           bool // building every row, to measure them

	named string // the pack's name as last told to the menu

	// FocusMenu moves focus to the menu, beside the form.
	FocusMenu func()

	Left string // how it was left: "draft", "saved", "answered" (by the menu) or "" (nothing changed)
}

// formRow is one row of the page; a header starts a card, titled with it.
type formRow struct {
	key                      string // which row it is, to find it again once the page is rebuilt
	label, value, hint, help string
	note                     string // faint, after the value: what a folded part has in it
	header                   bool
	sub                      bool // one of a blank's rows, under it
	depth                    int  // 0 the pack's, 1 a module's, 2 a command's: how far in its tree guides go
	branch                   bool // an add that ends its level, drawn on the tree's └─
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

// leave is done with the form. Nothing changed, nothing's kept; otherwise,
// from the menu the changes are kept as a draft for it to ask about, and
// without the menu they're saved.
func (f *PackForm) leave() tea.Cmd {
	switch {
	case !f.pack.Changed():
		if d, _ := config.ReadDraft(); f.pack.SameAs(d) {
			_ = config.RemoveDraft()
		}
	case f.menu:
		if err := f.pack.WriteDraft(); err != nil {
			f.issue = err.Error()
			return nil
		}
		f.Left = "draft"
	default:
		if f.pack.IsNew() {
			f.pack.Folder = f.pack.NewFolder()
		}
		if err := f.pack.Save(); err != nil {
			f.issue = err.Error()
			return nil
		}
		_ = config.RemoveDraft()
		f.Left = "saved"
	}
	return tea.Quit
}

// done closes the form: with changes, after the menu has asked about them.
func (f *PackForm) done() tea.Cmd {
	if f.menu && f.FocusMenu != nil && f.pack.Changed() {
		return f.ask()
	}
	return f.leave()
}

// ask keeps the changes as a draft and has the menu ask whether to save
// them, the form staying open beside it until it's answered.
func (f *PackForm) ask() tea.Cmd {
	if err := f.pack.WriteDraft(); err != nil {
		f.issue = err.Error()
		return nil
	}
	if err := config.Ask(); err != nil {
		f.issue = err.Error()
		return nil
	}
	f.asking = true
	f.FocusMenu()
	return askTick()
}

// askTickMsg is a look at whether the menu has answered.
type askTickMsg struct{}

func askTick() tea.Cmd {
	return tea.Tick(askEvery, func(time.Time) tea.Msg { return askTickMsg{} })
}

const askEvery = 200 * time.Millisecond

// answered sees whether the menu has answered: the draft gone (saved, or
// thrown away) closes the form; still there, and no longer asked about, is
// back to the form.
func (f *PackForm) answered() tea.Cmd {
	if !f.asking {
		return nil
	}
	if d, _ := config.ReadDraft(); d == nil {
		f.asking, f.Left = false, "answered"
		return tea.Quit
	}
	if !config.Asking() {
		f.asking = false
		return nil
	}
	return askTick()
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
	return nil
}

// refresh rebuilds the page with the cursor on the row keyed focus, or else
// the row it was on, or else as near to where it was as there is.
func (f *PackForm) refresh(focus string) {
	was := f.sel
	if r := f.selectedRow(); r != nil && focus == "" {
		focus = r.key
	}
	f.openSub, f.openCmd = openPart(focus)
	f.rows = f.matching(f.pageRows())
	if f.menu && f.pack.Title != f.named { // so the menu says what you're editing, by its name as it is
		if config.WriteFormName(f.pack.Title) == nil {
			f.named = f.pack.Title
		}
	}
	f.sel = -1
	if len(f.rows) == 0 {
		return
	}
	for i, r := range f.rows {
		if focus != "" && r.key == focus && !r.header {
			f.sel = i
		}
	}
	if f.sel >= 0 {
		return
	}
	f.sel = min(max(was, 0), len(f.rows)-1)
	if f.rows[f.sel].header {
		f.move(1)
	}
	if f.rows[f.sel].header {
		f.move(-1)
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

// refold opens the part the cursor's moved into, folding the one it left.
func (f *PackForm) refold() {
	if r := f.selectedRow(); r != nil {
		if sub, cmd := openPart(r.key); sub != f.openSub || cmd != f.openCmd {
			f.refresh(r.key)
		}
	}
}

// folded says whether a part is folded to its heading and name: any but the
// one the cursor's in, unless the filter's on, which shows all it finds.
func (f *PackForm) folded(sub, cmd int) bool {
	if f.noFold || f.filter.Value() != "" {
		return false
	}
	if cmd < 0 {
		return sub != f.openSub
	}
	return sub != f.openSub || cmd != f.openCmd
}

// unfolded is every row of the page, nothing folded.
func (f *PackForm) unfolded() []formRow {
	f.noFold = true
	defer func() { f.noFold = false }()
	return f.pageRows()
}

// foldTo is a part's heading and Name row alone, the rest folded away.
func foldTo(rows []formRow, note string) []formRow {
	rows[1].note = note
	return rows[:2]
}

// matching is the rows the filter leaves, each with the headings it's under:
// a row stays if its label, its value or its part's heading has the filter's
// text in it.
func (f *PackForm) matching(rows []formRow) []formRow {
	q := strings.ToLower(strings.TrimSpace(f.filter.Value()))
	if q == "" {
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
		in := strings.Contains(strings.ToLower(r.label+" "+r.value), q)
		for d := 0; d <= r.depth && d < 3 && !r.branch; d++ {
			in = in || d == r.depth && strings.Contains(titles[d], q)
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

// move puts the cursor on the next row (or, with -1, the one before) that
// isn't a card's title.
func (f *PackForm) move(d int) {
	for i := f.sel + d; i >= 0 && i < len(f.rows); i += d {
		if !f.rows[i].header {
			f.sel = i
			return
		}
	}
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

func uniqueName(base string, taken func(string) bool) string {
	name := base
	for n := 2; taken(name); n++ {
		name = fmt.Sprintf("%s %d", base, n)
	}
	return name
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
		formRow{key: "add-module", depth: 1, branch: true, label: "Add a module", help: "A new module, with a command to start from.", do: func() error {
			p := f.pack
			name := uniqueName("New module", func(n string) bool {
				return slices.ContainsFunc(p.Files, func(e config.EditableFile) bool { return e.Group.Name == n })
			})
			p.Files = append(p.Files, config.EditableFile{Slug: p.NewSlug(name), Group: config.Group{Name: name, Commands: []config.Command{{Name: "New command", Cmd: "echo hello"}}}})
			return nil
		}, then: fmt.Sprintf("%d/name", len(f.pack.Files))},
	)
}

// mdLook colours a value as the preview colours it: the line it's on in the
// Markdown, before and after being what's around it there, coloured as micro
// colours the file, cut down to the value.
func mdLook(before, after string) func(string) string {
	return func(v string) string {
		lines := strings.Split(micro.Markdown(before+v+after), "\n")
		pre := ansi.StringWidth(before[strings.LastIndex(before, "\n")+1:])
		cut := ansi.Cut(lines[strings.Count(before, "\n")], pre, pre+ansi.StringWidth(v))
		return emptySGR.ReplaceAllString(cut, "") // what's left of the colour of what was cut away
	}
}

// emptySGR is a colour that's set and reset with nothing in it.
var emptySGR = regexp.MustCompile(`\x1b\[[0-9;]*m\x1b\[0?m`)

// frontLook colours a setting's value as it is in the front matter.
func frontLook(key string) func(string) string { return mdLook("---\n"+key+": ", "\n---") }

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
		sections = append(sections, config.Option{Value: s})
	}
	if !slices.ContainsFunc(sections, func(o config.Option) bool { return o.Value == p.Section }) {
		sections = append(sections, config.Option{Value: p.Section})
	}
	sections = append(sections, config.Option{Value: newSection, Label: "+ New section"})
	var logos []config.Option
	for _, n := range config.PackLogos {
		logos = append(logos, config.Option{Value: n, Icon: config.Logos[n]}) // all grey: a choice of shape, colour's the next row
	}
	var colours []config.Option
	for _, c := range config.PackColors {
		colours = append(colours, config.Option{Value: c.Name, Icon: config.Logos[p.Logo], Color: c.Hex})
	}
	colour, colourHex := p.Color, f.logoColour()
	for _, c := range config.PackColors { // with none set, the colour its logo has
		if colour == "" && c.Hex == colourHex {
			colour = c.Name
		}
	}
	rows := []formRow{
		{label: "Pack", header: true},
		{key: "name", label: "Name", value: p.Title, look: mdLook("# ", ""), help: "What the menu calls the pack.", text: func(v string) error { f.pack.Title = v; return nil }},
		{key: "desc", label: "Desc", value: p.Description, hint: "one line on what's in it", help: "One line on what's in it, shown by clingon pack.", text: func(v string) error { f.pack.Description = v; return nil }},
		{key: "section", label: "Menu", value: p.Section, look: frontLook("section"), help: "Where the menu shows it: one of these, or + New section for one of your own.", options: sections, pick: func(v string) error { f.pack.Section = v; return nil },
			text: func(v string) error {
				s, err := config.SectionSlug(v)
				if err == nil {
					f.pack.Section = s
				}
				return err
			}},
		{key: "logo", label: "Logo", value: p.Logo, look: frontLook("logo"), icon: config.Logos[p.Logo], color: f.logoColour(), help: "Its icon in the menu.", options: logos, pick: func(v string) error { f.pack.Logo = v; return nil }},
		{key: "colour", label: "Tint", value: colour, look: frontLook("colour"), icon: slotIcon("●"), color: colourHex, help: "The logo's colour.", options: colours, pick: func(v string) error { f.pack.Color = v; return nil }},
		{key: "needs", label: "Need", value: joined(p.Needs), look: frontLook("needs"), hint: "programs, separated by commas", help: "Programs the pack needs, separated by commas: the status bar warns you while one's missing.", text: func(v string) error { f.pack.Needs = splitComma(v); return nil }},
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
		{label: "Module", header: true},
		{key: k("name"), label: "Name", value: g.Name, look: mdLook("# ", ""), help: "What the menu calls the module.", text: func(v string) error { f.group(i).Name = v; return nil }},
		{key: k("desc"), label: "Desc", value: g.Description, hint: "what's in it", help: "What's in the module.", text: func(v string) error { f.group(i).Description = v; return nil }},
		{key: k("needs"), label: "Need", value: joined(g.Requires), look: frontLook("needs"), hint: "programs, separated by commas", help: "Programs every command here needs, separated by commas.", text: func(v string) error { f.group(i).Requires = splitComma(v); return nil }},
		{key: k("when"), label: "When", value: joined(g.When), look: frontLook("when"), hint: "files, like .git", help: "Files that must be here or in a folder above for these commands, like .git; ./Dockerfile means this folder only.", text: func(v string) error { f.group(i).When = splitComma(v); return nil }},
		{key: k("remove"), label: "Remove this module", risk: config.RiskWarn, help: "Removes the module and every command in it.", confirm: "Yes, remove it", do: func() error {
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
		formRow{key: k("add"), depth: 2, branch: true, label: "Add a command", help: "A new command in " + g.Name + ", to fill in.", do: func() error {
			g := f.group(i)
			name := uniqueName("New command", func(n string) bool {
				return slices.ContainsFunc(g.Commands, func(c config.Command) bool { return c.Name == n })
			})
			g.Commands = append(g.Commands, config.Command{Name: name, Cmd: "echo hello"})
			return nil
		}, then: fmt.Sprintf("%d/%d/name", i, len(g.Commands))},
	)
}

func (f *PackForm) commandRows(i, j int) []formRow {
	c := f.command(i, j)
	k := func(s string) string { return fmt.Sprintf("%d/%d/%s", i, j, s) }
	// Each with the glyph the menu marks it with; none with a grey dot.
	risks := []config.Option{
		{Value: "", Label: "none", Icon: "●", Color: greyHex}, // one cell, as ⚠ and ☠ are: not the logos' centred twin
		{Value: config.RiskWarn, Label: "warning", Icon: warnGlyph, Color: orangeHex},
		{Value: config.RiskDanger, Label: "dangerous", Icon: dangerGlyph, Color: redHex},
	}
	risk := map[string]string{"": "none", config.RiskWarn: "warning", config.RiskDanger: "dangerous"}[c.Risk]
	var descLook func(string) string // in a callout's > lines when it has a risk
	if c.Risk != "" {
		descLook = mdLook("> ", "")
	}
	riskIcon := risks[max(slices.IndexFunc(risks, func(o config.Option) bool { return o.Value == c.Risk }), 0)]
	rows := []formRow{
		{label: "Command", header: true},
		{key: k("name"), label: "Name", value: c.Name, look: mdLook("## ", ""), help: "What the menu calls it: letters, digits, spaces and ( ).", text: func(v string) error { f.command(i, j).Name = v; return nil }},
		{key: k("risk"), label: "Risk", value: risk, icon: riskIcon.Icon, color: riskIcon.Color, help: "Whether clingon asks before it runs: warning asks first; dangerous has you type a short code, then asks.", options: risks, pick: func(v string) error { f.command(i, j).Risk = v; return nil }},
		{key: k("desc"), label: "Desc", value: c.Desc, look: descLook, hint: "what it does", help: "What it does, shown when it's highlighted.", text: func(v string) error { f.command(i, j).Desc = v; return nil }},
		{key: k("needs"), label: "Need", value: joined(c.Requires), hint: "programs, separated by commas", help: "Programs this command needs, separated by commas.", text: func(v string) error { f.command(i, j).Requires = splitComma(v); return nil }},
		{key: k("when"), label: "When", value: joined(c.When), hint: "files, like .git", help: "Files that must be here or in a folder above, like .git.", text: func(v string) error { f.command(i, j).When = splitComma(v); return nil }},
		{key: k("cmd"), label: "Runs", value: c.Cmd, look: mdLook("```sh\n", "\n```"), help: "What's typed into your shell. A <name> in it is a blank, asked for each time.", text: func(v string) error {
			c := f.command(i, j)
			c.Cmd = v
			config.SyncBlanks(c)
			return nil
		}},
	}
	for b := range c.Args {
		rows = append(rows, f.blankRows(i, j, b)...)
	}
	rows = append(rows, formRow{key: k("remove"), label: "Remove this command", risk: config.RiskWarn, help: "Removes the command.", confirm: "Yes, remove it", do: func() error {
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
	kinds := []config.Option{{Value: "text", Label: "text"}, {Value: "choose", Label: "select one"}, {Value: "flags", Label: "select many"}}
	kind := map[string]string{"text": "text", "secret": "text, hidden", "choose": "select one", "flags": "select many"}[a.Kind()]
	rows := []formRow{
		{key: k("q"), label: name, value: a.Prompt, hint: "the question asked", help: "What's asked for " + name + " when the command runs.", text: func(v string) error { f.blank(i, j, b).Prompt = v; return nil }},
	}
	if a.Kind() == "secret" {
		rows = append(rows, formRow{key: k("kind"), sub: true, label: "Type", value: kind, help: "Its name says it's a secret, token or password: typed hidden, and kept out of your history."})
	} else {
		rows = append(rows, formRow{key: k("kind"), sub: true, label: "Type", value: kind, help: "How " + name + " is answered: text is typed in; select one picks one of its choices, or you type your own; select many picks any of them.", options: kinds, pick: func(v string) error {
			a := f.blank(i, j, b)
			switch v {
			case "text":
				a.Type, a.Options = "", nil
			default:
				a.Type, a.Default = v, ""
				if len(a.Options) == 0 {
					a.Options = []config.Option{{Value: "one"}, {Value: "two"}}
				}
			}
			return nil
		}})
	}
	if kd := a.Kind(); kd == "text" || kd == "secret" {
		return append(rows, formRow{key: k("start"), sub: true, label: "Base", value: a.Default, look: mdLook("  - (x) `", "`"), hint: "none", help: "What's filled in for " + name + " until you change it.", text: func(v string) error { f.blank(i, j, b).Default = v; return nil }})
	}
	for o := range a.Options {
		rows = append(rows, f.choiceRow(i, j, b, o))
	}
	return append(rows, formRow{key: k("add"), sub: true, label: "Add a choice", help: "Another answer to offer for " + name + ".", do: func() error {
		a := f.blank(i, j, b)
		a.Options = append(a.Options, config.Option{Value: uniqueName("choice", func(n string) bool { return a.HasOption(n) })})
		return nil
	}, then: fmt.Sprintf("%d/%d/%d/%d", i, j, b, len(a.Options))})
}

// pickedFirst says if a choice is a default: picked (or ticked) until you change it.
func pickedFirst(a *config.Arg, v string) bool {
	return a.Kind() == "flags" && slices.Contains(a.Ticked(a.Default), v) || a.Kind() == "choose" && a.Default == v
}

// choiceRow is one of a blank's choices, on one row: what it types, its label,
// and whether it's a default. Enter lists what can be done to it.
func (f *PackForm) choiceRow(i, j, b, o int) formRow {
	a := f.blank(i, j, b)
	opt := a.Options[o]
	v := opt.Value
	if opt.Label != "" {
		v += "  " + opt.Label
	}
	first, notFirst := "Make it the default", "Not the default"
	if a.Kind() == "flags" { // any number can be: selected by default
		first, notFirst = "Selected by default", "Not selected by default"
	}
	if pickedFirst(a, opt.Value) {
		v += "  · default"
		first = notFirst
	}
	look := func(string) string { // what it types as in the file, its label plain, and a faint note
		s := mdLook("  - ( ) `", "`")(opt.Value)
		if opt.Label != "" {
			s += "  " + fieldStyle.Render(opt.Label)
		}
		if pickedFirst(a, opt.Value) {
			s += faintStyle.Render("  · default")
		}
		return s
	}
	return formRow{key: fmt.Sprintf("%d/%d/%d/%d", i, j, b, o), sub: true, label: "Args", value: v, look: look,
		help:    "One of the answers offered: what it types, then its label.",
		options: []config.Option{{Value: "types", Label: "Change what it types"}, {Value: "label", Label: "Change its label"}, {Value: "first", Label: first}, {Value: "remove", Label: "Remove it"}},
		pick: func(what string) error {
			a := f.blank(i, j, b)
			opt := a.Options[o]
			switch what {
			case "first":
				if a.Kind() == "choose" {
					if a.Default == opt.Value {
						a.Default = ""
					} else {
						a.Default = opt.Value
					}
					return nil
				}
				on := slices.DeleteFunc(a.Ticked(a.Default), func(s string) bool { return s == opt.Value })
				if !pickedFirst(a, opt.Value) {
					on = append(on, opt.Value)
				}
				a.Default = strings.Join(on, a.Separator())
			case "remove":
				if a.Kind() == "flags" {
					a.Default = strings.Join(slices.DeleteFunc(a.Ticked(a.Default), func(s string) bool { return s == opt.Value }), a.Separator())
				} else if a.Default == opt.Value {
					a.Default = ""
				}
				a.Options = slices.Delete(a.Options, o, o+1)
			}
			return nil
		}}
}

// choiceText is the field for changing a choice's value (what "types") or its
// label, typed on the choice's own row.
func (f *PackForm) choiceText(r *formRow, what string) (*formRow, string) {
	var i, j, b, o int
	_, _ = fmt.Sscanf(r.key, "%d/%d/%d/%d", &i, &j, &b, &o)
	field := *r
	field.text = func(s string) error {
		a := f.blank(i, j, b)
		op := &a.Options[o]
		if what == "label" {
			op.Label = s
			return nil
		}
		switch {
		case a.Kind() == "flags":
			on := a.Ticked(a.Default)
			if k := slices.Index(on, op.Value); k >= 0 {
				on[k] = s
				a.Default = strings.Join(on, a.Separator())
			}
		case a.Default == op.Value:
			a.Default = s
		}
		op.Value = s
		return nil
	}
	opt := f.blank(i, j, b).Options[o]
	if what == "label" {
		return &field, opt.Label
	}
	return &field, opt.Value
}

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
		f.pageSize = max(f.h-10, 1)
		return f, nil
	case askTickMsg:
		return f, f.answered()
	case tea.KeyPressMsg:
		return f, f.key(msg)
	}
	return f, nil
}

// edit starts typing into a row's field, in place.
func (f *PackForm) edit(r *formRow, value string) tea.Cmd {
	in := newInput(accentStyle, "")
	in.Prompt = ""
	in.SetValue(value)
	in.CursorEnd()
	f.input, f.editing = &in, r
	return f.input.Focus()
}

func (f *PackForm) key(msg tea.KeyPressMsg) tea.Cmd {
	k := keyName(msg)
	if f.asking { // back from the menu's question by itself: carry on
		f.asking = false
		config.StopAsking()
	}
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
	if k == "ctrl+c" {
		switch {
		case f.editing != nil: // cancel what you're doing
			f.editing, f.input, f.issue = nil, nil, ""
			return nil
		case f.choosing != nil:
			f.choosing = nil
			return nil
		case f.filtering:
			f.filtering = false
			f.filter.SetValue("")
			f.filter.Blur()
			f.refresh("")
			return nil
		}
		return f.leave() // out of the form, changes kept for the menu to ask about
	}
	switch {
	case f.editing != nil:
		switch k {
		case "enter", "right":
			v := strings.TrimSpace(f.input.Value())
			r := f.editing
			if err := f.change(func() error { return r.text(v) }); err != nil {
				f.issue = err.Error()
				return nil
			}
			f.editing, f.input, f.issue = nil, nil, ""
			f.refresh(r.key)
		case "esc", "left":
			f.editing, f.input, f.issue = nil, nil, ""
		default:
			return typeInto(f.input, msg)
		}
		return nil
	case f.choosing != nil:
		switch k {
		case "up":
			f.opt = max(f.opt-1, 0)
		case "down":
			f.opt = min(f.opt+1, len(f.options)-1)
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
	if f.filtering { // it takes all text; the arrows still move, and enter edits
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
			return cmd
		}
	}
	switch k {
	case "up":
		f.move(-1)
		f.refold()
	case "down":
		f.move(1)
		f.refold()
	case "pgup":
		for range max(f.pageSize, 1) {
			f.move(-1)
		}
		f.refold()
	case "pgdown":
		for range max(f.pageSize, 1) {
			f.move(1)
		}
		f.refold()
	case "[":
		f.jump(-1)
		f.refold()
	case "]":
		f.jump(1)
		f.refold()
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
		return f.done()
	case "left": // back, as at the menu's top: nothing above this but a filter left on
		if f.filter.Value() != "" {
			f.filter.SetValue("")
			f.refresh("")
		}
		return nil
	case "enter", "right":
		r := f.selectedRow()
		if r == nil {
			return nil
		}
		f.issue = ""
		switch {
		case r.pick != nil: // a list, even if it can also be typed (from the list): from the top
			f.choosing, f.options, f.opt = r, r.options, 0
		case r.text != nil:
			return f.edit(r, r.value)
		case r.confirm != "":
			f.choosing, f.opt = r, 0
			f.options = []config.Option{{Value: "no", Label: "No, keep it"}, {Value: "yes", Label: r.confirm}}
		case r.do != nil:
			if err := f.change(r.do); err != nil {
				f.issue = err.Error()
				f.refresh("")
				return nil
			}
			f.refresh(r.then) // on to what was added, to fill it in
		}
	}
	return nil
}

// chose acts on the option picked from the list under a row.
func (f *PackForm) chose() tea.Cmd {
	r, v := f.choosing, f.options[f.opt].Value
	f.choosing, f.issue = nil, ""
	switch {
	case r.confirm != "": // a No/Yes: only Yes does it
		if v != "yes" {
			return nil
		}
		if err := f.change(r.do); err != nil {
			f.issue = err.Error()
		}
		f.refresh("")
		return nil
	case v == "types" || v == "label": // a choice's, typed on its row
		field, now := f.choiceText(r, v)
		return f.edit(field, now)
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
	// lighter, a page laid on top. Inside each, the same padding all round.
	formW, previewW := f.w, 0
	if f.w >= minPreviewWidth {
		formW = f.w / 2 // the menu's a third of the window: the form and its preview a third each
		previewW = f.w - formW
	}
	inner := formW - 2*panelPad
	foot := f.footer(inner)
	// Rows level with the menu's: blank by its logo, where you are by its
	// path (row 2), the filter by its filter (row 4), the form from row 6 by
	// its list; the help and keys at the bottom, a row from the edge.
	top := []string{"", "", f.crumb(inner), "", f.filterLine(inner), ""}
	room := max(f.h-len(top)-len(foot)-1, 1)
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
	form = append(append(form, foot...), "")
	var prev []string
	if previewW > 0 { // its title level with where you are
		prev = append([]string{"", ""}, f.preview(previewW-2*panelPad, f.h-3)...) // each with its own left padding, the gutter for a mark
	}
	pad := strings.Repeat(" ", panelPad)
	lines := make([]string, 0, f.h)
	for k := range f.h {
		l, p := "", ""
		if k < len(form) {
			l = form[k]
		}
		if k < len(prev) {
			p = prev[k]
		}
		line := pad + l
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
	parts := []string{"My packs", f.pack.Title}
	if r := f.selectedRow(); r != nil {
		var i, j int
		if n, _ := fmt.Sscanf(r.key, "%d/%d/", &i, &j); n >= 1 && i < len(f.pack.Files) {
			parts = append(parts, f.group(i).Name)
			if n == 2 && j < len(f.group(i).Commands) {
				parts = append(parts, f.command(i, j).Name)
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
		return ansi.Truncate(accentStyle.Render("❯ ")+strings.TrimSpace(f.filter.Value())+faintStyle.Render("  / to edit · esc to clear"), w, "…")
	}
	return accentStyle.Render("❯ /") + faintStyle.Render(" to filter")
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
	fieldStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#eeeeee")) // 255: a field's value, bright
)

// guide is a row's tree guide: what's to the left of it, showing which
// module and command it's part of. A field's ends in its level's │, with
// the cell after it for the cursor's bar; an add that ends its level is on
// the level's └, the bar in place of its ─.
func guide(r *formRow) string {
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
		{"→ or enter", "edit, or take the choice"},
		{"←", "back: close what's open"},
		{"[ ]", "jump to the previous or next heading"},
		{"/", "filter; again stops"},
		{"esc", "clear the filter, go back, or close the form"},
		{"ctrl-c", "cancel what you're doing"},
		{"tab/shift-tab", "switch panes"},
		{"?", "this screen"},
	}
	lines := []string{headerStyle.Render("KEYS"), ""}
	for _, k := range keys {
		lines = append(lines, ansi.Truncate(cursorStyle.Render(fmt.Sprintf("%-13s", k[0]))+" "+k[1], w, "…"))
	}
	lines = append(lines, "")
	lines = append(lines, wrapSpaces(dimStyle.Render("Leaving with changes, the menu asks whether to save them; the form stays open until it's answered."), w)...)
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
	out := make([]string, 0, room)
	for _, l := range pinned {
		out = append(out, lines[l])
	}
	return append(out, lines[start:min(start+rest, len(lines))]...)
}

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
				out = append(out, faintStyle.Render(strings.TrimRight(strings.Repeat("│  ", max(r.depth-1, 0))+"│", " ")))
			}
		}
		if r.branch {
			heads = append(heads, formHead{line: len(out), depth: r.depth, name: -1, ends: true})
		}
		if r.header {
			heads = append(heads, formHead{line: len(out), depth: r.depth, name: -1})
			out = append(out, faintStyle.Render(guide(r))+ansi.Truncate(formHeading(r.label), max(w-ansi.StringWidth(guide(r)), 1), "…"))
			continue
		}
		on := i == f.sel
		if on {
			at, row = len(out), len(out)
		}
		if n := len(heads) - 1; n >= 0 && i > 0 && f.rows[i-1].header && r.label == "Name" {
			heads[n].name = len(out) // pinned with its heading
		}
		open := on && f.choosing != nil
		out = append(out, f.rowLine(r, on && !open, open, col, w))
		if open {
			for o, opt := range f.options { // the row's list, dropped down under it, with the cursor
				if o == f.opt {
					at = len(out) // the list's cursor, to keep in view
				}
				out = append(out, f.optionLine(r, opt, o == f.opt, col, w))
			}
		}
	}
	return out, at, row, heads
}

// labelAt is the column a row's label starts in: past its guide, the bar's
// cell and a space, and set in under its blank for a blank's own rows.
func (f *PackForm) labelAt(r *formRow) int {
	n := ansi.StringWidth(guide(r)) + 2
	if r.sub {
		n += 2
	}
	return n
}

// rowLine is a row as the menu draws one, the cursor's row a bar across the
// form: its label, and its value (or the field being typed), with a periwinkle ▾
// after a value that's picked from a list. A blank's label is pink, as blanks
// are in commands; an add is pink; a remove is faint until the cursor's on it.
// A row with its list open keeps its label pink, the cursor in the list.
func (f *PackForm) rowLine(r *formRow, on, open bool, col, w int) string {
	g := guide(r)
	slot := " "
	switch {
	case on:
		slot = cursorBar(cursorStyle)
	case r.branch:
		slot = "─"
	}
	indent := ""
	if r.sub {
		indent = "  "
	}
	rest := " " + indent
	room := max(w-f.labelAt(r), 1)
	switch {
	case r.confirm != "": // a remove
		st := faintStyle
		if on {
			st = dangerStyle
		}
		rest += st.Render(ansi.Truncate(dangerGlyph+" "+r.label, room, "…"))
	case r.do != nil: // an add
		st := accentStyle
		if on {
			st = cursorStyle
		}
		rest += st.Render(ansi.Truncate("+ "+r.label, room, "…"))
	default:
		label := ansi.Truncate(r.label, max(col-f.labelAt(r)-2, 1), "…")
		pad := strings.Repeat(" ", max(col-f.labelAt(r)-ansi.StringWidth(label), 1))
		switch {
		case on || open:
			label = cursorStyle.Render(label)
		case strings.HasPrefix(r.label, "<"):
			label = accentStyle.Render(label)
		default:
			label = dimStyle.Render(label)
		}
		rest += label + pad
		room = max(w-col-2, 1) // and the ▾
		switch {
		case f.editing != nil && f.editing.key == r.key:
			rest += inputView(f.input, room)
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
			rest += ansi.Truncate(v, room, "…")
		case r.hint != "" && on: // only where you are: elsewhere they'd fill the form with what isn't yours
			rest += dimStyle.Italic(true).Render(ansi.Truncate(r.hint, room, "…"))
		case r.text != nil:
			rest += faintStyle.Render("—")
		}
		if r.note != "" {
			rest += faintStyle.Render(ansi.Truncate("  · "+r.note, max(w-ansi.StringWidth(g)-1-ansi.StringWidth(rest), 0), ""))
		}
		if r.pick != nil {
			rest += lipgloss.NewStyle().Foreground(soft).Render(" ▾") // periwinkle: plainly there, under the white value
		}
	}
	if on {
		return faintStyle.Render(g) + onBg(slot+rest, formCursorBg, max(w-ansi.StringWidth(g), 1))
	}
	return faintStyle.Render(g+slot) + rest
}

// optionLine is one of the options in the list dropped down under row r, set
// in under its value. The list's cursor is the form's, set in with it: the bar
// just before the option, and the band from there across.
func (f *PackForm) optionLine(r *formRow, o config.Option, on bool, col, w int) string {
	title := o.Label
	if title == "" {
		title = o.Value
	}
	switch {
	case on && (o.Value == "yes" || o.Value == "remove"):
		title = dangerStyle.Render(title)
	case on:
		title = cursorStyle.Render(title)
	default:
		title = dimStyle.Render(title)
	}
	g := strings.Repeat("│  ", r.depth) + "│"
	if r.branch {
		g = strings.Repeat("│  ", max(r.depth-1, 0))
	}
	in := max(col-ansi.StringWidth(g)-2, 0) // set in under the value, the bar a cell before it
	rest := " "
	if o.Icon != "" {
		rest += iconStyle(o.Color).Render(o.Icon) + iconGap(o.Icon)
	}
	rest += title
	lead := faintStyle.Render(g) + strings.Repeat(" ", in)
	if on {
		return lead + onBg(cursorBar(cursorStyle)+rest, formCursorBg, max(w-ansi.StringWidth(g)-in, 1))
	}
	return lead + " " + rest
}

// preview is the Markdown file the cursor's row is in, as it'll be saved,
// coloured as micro colours it: the pack's _pack.md, or a module's file
// for its rows and its commands'.
func (f *PackForm) preview(w, h int) []string {
	r := f.selectedRow()
	if r == nil {
		return nil
	}
	file := config.PackInfoFile
	switch sub, _ := openPart(r.key); {
	case sub == -2:
		return nil
	case sub >= 0 && sub < len(f.pack.Files):
		file = f.pack.Files[sub].Slug + ".md"
	}
	return filePreview(file, f.pack.Markdown()[file], marked(r.key, strings.Split(f.pack.Markdown()[file], "\n")), w, h)
}

func fit2(lines []string, h int) []string {
	if len(lines) > h {
		return lines[:h]
	}
	return lines
}

// filePreview is a pack's file, as micro shows it, h lines at most: the
// lines in on marked, each with a bar in the gutter on a band of the
// window's own colour, scrolled into view. Every line has the panel's
// padding at its left, the gutter the bar's in.
func filePreview(name, text string, on map[int]bool, w, h int) []string {
	pad := strings.Repeat(" ", panelPad)
	rule := pad + faintStyle.Render(strings.Repeat("─", w))
	head := []string{pad + labelStyle.Render(ansi.Truncate(strings.ToUpper(name), w, "…")), rule}
	var body []string
	first := -1
	for i, l := range strings.Split(strings.TrimRight(micro.Markdown(text), "\n"), "\n") {
		l = ansi.Truncate(l, w, "…")
		if on[i] {
			if first < 0 {
				first = i
			}
			body = append(body, marked1(l, w))
			continue
		}
		body = append(body, pad+l)
	}
	body = append(body, rule)
	room := max(h-len(head), 1)
	if len(body) > room { // the marked lines in view, a little way down
		start := 0
		if first >= 0 {
			start = min(max(first-room/3, 0), len(body)-room)
		}
		body = body[start : start+room]
	}
	return append(head, body...)
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
		case "section", "logo", "colour", "needs":
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
		switch _, err := fmt.Sscanf(field, "%d", &o); {
		case err == nil: // a choice
			mark(item+1+o, item+1+o)
		case field == "q":
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
	if f.issue == "" {
		return nil
	}
	return []string{issueChip(f.issue, w)}
}

// issueChip is a problem as an orange chip, like the status bar and micro's.
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
