// Package ui is the menu that runs in clingon's left pane.
package ui

import (
	"fmt"
	"image/color"
	"math/rand/v2"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
	"github.com/con-dog/clingon/internal/tmpl"
)

type Options struct {
	Config *config.Config
	// Builtin builds clingon's own sections (at the foot of the main menu),
	// again whenever the packs change, since what's available depends on them.
	Builtin func() []*config.Section
	// Reload is called when the menu regains focus; it returns nil, nil
	// when nothing changed.
	Reload func() (*config.Config, error)
	Send   func(cmd string) error // nil: print mode, quit with Result
	Dir    func() string          // the shell's folder
	// WaitIdle blocks until a command sent to the shell has finished, then
	// FocusMenu brings focus back. Both nil in print mode.
	WaitIdle  func() bool
	FocusMenu func()
	// LastFailed says whether the command that just finished failed, so a
	// recipe stops there, and whether that could be told at all: if not, the
	// recipe stops too. Nil: it's taken to have worked.
	LastFailed func() (failed, known bool)
	// Recipes lists the saved recipes, and Self is clingon as the shell runs it,
	// which is what saves one. Recipes nil: recipes aren't saved.
	Recipes func() []config.Recipe
	Self    string
	// Shell says what the shell is doing: running something, scrolled back,
	// or with something typed at its prompt. ClearShell throws away what's
	// typed; FocusShell moves focus there. All nil in print mode.
	Shell      func() ShellState
	ClearShell func() error
	FocusShell func()
	// Jobs names what's running in the shell's background or stopped there,
	// which ends with the session. Nil in print mode.
	Jobs func() []string
	// Bar draws the status bar in a pane of its own, the whole window wide
	// (BarWidth cells; 0 when there's none yet). Nil: it's drawn at the
	// foot of the menu instead.
	Bar      func(line string)
	BarWidth func() int
	// Headline is the status bar's news on the top level, worked out each
	// time the top level is shown. Nil: nothing to say there.
	Headline func() Headline
	// Project finds the project packs (.clingon/packs) for the shell's
	// folder; nil when there's none. Nil func: projects aren't looked for.
	Project func(dir string) *config.Project
	// SecretDir is where secret values wait, in 0600 files, for the one
	// command that reads them.
	SecretDir string
	Version   string
}

// ShellState is what the shell is doing, as far as typing a command into
// it goes.
type ShellState struct {
	Busy      string // the program running, or "" at the prompt
	Scrolling bool   // scrolled back: keys would go to that instead
	Typed     string // typed at the prompt but not run
	Starting  bool   // Busy is the shell's rc file at work: no first prompt yet
}

// Headline is a line of news for the status bar, coloured by Kind.
type Headline struct {
	Kind HeadlineKind
	Text string
}

type HeadlineKind int

const (
	Good      HeadlineKind = iota // grey: all's well, nothing to do
	Info                          // purple glyph: worth knowing
	Attention                     // pink glyph: worth doing
)

type mode int

const (
	modeBrowse mode = iota
	modeFill
	modeConfirm
	modeTrust    // asking whether to trust a project pack
	modeTyped    // the shell has something typed: go there, or clear it and run
	modeQuit     // quitting closes the shell: stay, or quit
	modeSave     // about to run the recipe: save it first?
	modeSaveName // what to save it as
)

// projectRef is the top-level row for a project pack that isn't in use:
// untrusted (opening it asks) or broken (greyed out, saying why).
type projectRef struct{}

type cmdRef struct {
	section string
	sec     *config.Section // its own section, even if another has the same name
	cmd     *config.Command
}

// job is one command being filled in, an arg at a time.
type job struct {
	ref     cmdRef
	step    int
	vals    map[string]string
	said    map[string]string // what the chosen option(s) said about themselves, by arg
	input   textinput.Model
	pick    picker
	usePick bool
	invalid string // why the value just entered was refused
}

// pending is a command ready to send, held while risky ones ask No/Yes.
type pending struct {
	section, name, cmd string
	sec                *config.Section
	risk               string
	desc               string // the filled-in description, for the No/Yes screen
	shown              string // cmd as the user sees it (secrets as <name>), shown for risk: danger
	code               string // risk: danger: a random code you type to go on (see newCode)
	typed              bool   // risk: danger: code typed correctly, so on to No/Yes
	cleared            bool   // the shell's prompt was just cleared for it
}

// notice is shown in place of the list for a few seconds (or until a key),
// e.g. when a command has nothing to pick from.
type notice struct {
	title, text string
	left        int // seconds until it goes
	seq         int
}

type noticeTick struct{ seq int }

// busyGrace is how long a command from the menu runs before the status bar
// says so: most are done by then, and the band would only flash.
const busyGrace = 500 * time.Millisecond

type busyGraceMsg struct{}

// shellLook is how often the shell is looked at again while the status bar
// has a warning about it: it can end with no word to the menu (a quote closed
// by hand, say), and focus may not come back to prompt a look.
const shellLook = time.Second

type shellLookMsg struct{}

const noticeSeconds = 3

// showNotice leaves whatever was being filled in and shows n.
func (m *Model) showNotice(title, text string) tea.Cmd {
	m.job, m.pending, m.mode = nil, nil, modeBrowse
	m.refresh()
	m.notice = notice{title: title, text: text, left: noticeSeconds, seq: m.notice.seq + 1}
	return m.noticeTick()
}

func (m *Model) noticeTick() tea.Cmd {
	seq := m.notice.seq
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return noticeTick{seq} })
}

// idleMsg says the shell is back at its prompt; seq is the run that was
// being waited for, and ok is false if that couldn't be told.
type idleMsg struct {
	seq int
	ok  bool
}

type Model struct {
	opt       Options
	w, h      int
	mode      mode
	section   *config.Section
	list      picker
	job       *job
	pending   *pending
	builtin   []*config.Section
	busy      string                      // what's running in the shell: nothing can be sent until it ends
	looking   bool                        // a shellLookMsg is on its way
	recipe    []recipeStep                // the numbered commands, in order: at most 9
	run       *recipeRun                  // the recipe going through its steps, if it is
	stopped   int                         // the command the recipe stopped at, where its list starts; 0 if it didn't
	resume    tea.Cmd                     // cursors to start blinking again, after a key brought focus back
	quiet     bool                        // that's locked, but not worth a status bar's band yet
	waiting   bool                        // a wait for the shell to finish (started on focus) is under way
	scrolling bool                        // the shell is scrolled back: nothing can be sent until it stops
	typed     string                      // what's typed at the shell's prompt: sending asks first
	hint      string                      // a dim one-line tip, until the next key
	notice    notice                      // a short-lived message in place of the list
	yes       bool                        // on the risky-command screen, "Yes, run it" is highlighted
	errMsg    string                      // shown until the next key
	missing   map[string][]config.Missing // by section: tools its pack needs that can't be run
	here      *config.Here                // answers `when:` for the shell's folder
	project   *config.Project             // the project pack for the shell's folder, if any
	word      textinput.Model             // where the confirm word is typed
	wrong     bool                        // the confirm word typed didn't match
	nameWhy   string                      // why the recipe's name was refused
	listed    []config.Recipe             // the saved recipes the list was last drawn with
	opened    string                      // the saved recipe whose own list is showing
	removing  string                      // the saved recipe being removed, until the shell's done
	help      bool                        // the keys screen is showing
	blurred   bool                        // focus is in the shell: the cursor's row is drawn quietly
	news      *Headline                   // the top level's status bar
	intro     int                         // logo columns drawn so far; -1 once it's all there
	jobs      []string                    // what quitting would end, for the quit screen
	trustTop  int                         // the trust screen's first command line on screen
	trustMax  int                         // trustTop at the last line; -1 before it's drawn
	runSeq    int                         // bumped per command sent, and when the user comes back by hand
	barW      int                         // the bar pane's width, when Options.Bar draws it
	barLine   string                      // the line last sent there

	Quit    bool     // user escaped out of the top menu
	Result  string   // print mode: the chosen command
	Secrets []string // files holding secret values, for the caller to clean up
}

func New(opt Options) *Model {
	m := &Model{opt: opt}
	if opt.Builtin != nil {
		m.builtin = opt.Builtin()
	}
	m.showRoot("")
	return m
}

func (m *Model) Init() tea.Cmd { return introTick() }

// The logo draws itself in when the menu opens, a column at a time; keys
// work all the while.
type introMsg struct{}

func introTick() tea.Cmd {
	return tea.Tick(18*time.Millisecond, func(time.Time) tea.Msg { return introMsg{} })
}

// heading appends a header row, preceded by a blank one unless it's first.
func heading(items []item, title string) []item {
	if len(items) > 0 {
		items = append(items, item{header: true})
	}
	return append(items, item{title: title, header: true})
}

// showRoot lists sections under the fixed category headings, then clingon's
// own sections below a divider, under an accent heading. focus names the
// section to put the cursor on.
func (m *Model) showRoot(focus string) {
	var items, search []item
	m.look()
	m.checkTools() // looked again here: rows for sections missing tools are marked
	addSearch := func(s *config.Section) {
		add := func(where string, cmds []config.Command) {
			for i := range cmds {
				c := &cmds[i]
				search = append(search, item{title: c.Name, where: where, desc: s.Name, keywords: c.Cmd, risk: c.Risk, disabled: m.unavailable(c) != "", ref: cmdRef{s.Name, s, c}})
			}
		}
		add(s.Name, s.Commands)
		for g := range s.Groups {
			add(s.Name+" › "+s.Groups[g].Name, s.Groups[g].Commands)
		}
	}
	if p := m.project; p != nil {
		items = heading(items, "This project")
		switch {
		case p.Config != nil:
			for i := range p.Config.Sections {
				items = append(items, sectionItem(&p.Config.Sections[i]))
				addSearch(&p.Config.Sections[i])
			}
		case p.Err != nil:
			items = append(items, item{title: config.ProjectDir + " has problems", icon: "✗", disabled: true, ref: projectRef{}})
		default:
			items = append(items, item{title: "Use this folder's commands?", icon: warnGlyph, ref: projectRef{}})
		}
	}
	for _, cat := range config.Categories {
		started := false
		for i := range m.opt.Config.Sections {
			s := &m.opt.Config.Sections[i]
			if s.Category != cat {
				continue
			}
			if !started {
				items = heading(items, cat)
				started = true
			}
			it := sectionItem(s)
			for i, x := range m.missingFor(s) {
				if i > 0 {
					it.missing += ", "
				}
				it.missing += x.Tool
			}
			items = append(items, it)
			addSearch(s)
		}
		for _, s := range m.builtin { // clingon's own that belong among the packs
			if s.Category != cat {
				continue
			}
			if !started {
				items = heading(items, cat)
				started = true
			}
			items = append(items, sectionItem(s))
			addSearch(s)
		}
	}
	divided := false
	for _, s := range m.builtin {
		if slices.Contains(config.Categories, s.Category) {
			continue // listed above
		}
		if !divided {
			divided = true
			if len(items) > 0 {
				items = append(items, item{header: true}, item{header: true, rule: true})
			}
			items = append(items, item{title: s.Category, header: true, accent: true})
		}
		items = append(items, sectionItem(s))
		addSearch(s)
	}
	m.section, m.opened = nil, ""
	m.list = newPicker(items, search, false)
	m.list.noun = "command"
	m.list.step = m.itemSteps
	if m.opt.Headline != nil {
		h := m.opt.Headline()
		m.news = &h
	}
	if focus != "" {
		for i, it := range m.list.view {
			if s, ok := it.ref.(*config.Section); ok && s.Name == focus {
				m.list.cursor = i
			}
		}
	}
}

func sectionItem(s *config.Section) item {
	return item{title: s.Name, icon: slotIcon(s.Icon), color: s.Color, ref: s}
}

// sectionNamed finds a section by name, clingon's own included.
func (m *Model) sectionNamed(name string) *config.Section {
	for _, s := range m.builtin {
		if s.Name == name {
			return s
		}
	}
	for i := range m.opt.Config.Sections {
		if m.opt.Config.Sections[i].Name == name {
			return &m.opt.Config.Sections[i]
		}
	}
	if m.project != nil && m.project.Config != nil {
		for i := range m.project.Config.Sections {
			if m.project.Config.Sections[i].Name == name {
				return &m.project.Config.Sections[i]
			}
		}
	}
	return nil
}

// reload swaps in changed packs and rebuilds the view, keeping the
// section, filter and highlighted row where they still exist.
func (m *Model) reload() bool {
	if m.opt.Reload == nil || m.mode != modeBrowse {
		return false
	}
	cfg, err := m.opt.Reload()
	if err != nil {
		return false // keep the last working menu; `clingon pack edit` reports problems in the shell
	}
	if cfg == nil {
		return false
	}
	m.errMsg, m.hint = "", ""
	query, filtering := m.list.filter.Value(), m.list.filtering
	var at string
	if it := m.list.selected(); it != nil {
		at = it.title
	}
	m.opt.Config = cfg
	if m.opt.Builtin != nil {
		m.builtin = m.opt.Builtin()
	}
	if m.run == nil { // its steps can't move under it
		m.refindRecipe()
	}
	if s := m.section; s != nil && m.sectionNamed(s.Name) != nil {
		m.showSection(m.sectionNamed(s.Name))
	} else {
		m.showRoot("")
	}
	m.list.filtering = filtering
	m.list.filter.SetValue(query)
	m.list.refilter()
	for i, it := range m.list.view {
		if m.list.selectable(i) && it.title == at {
			m.list.cursor = i
		}
	}
	return true
}

func (m *Model) showSection(s *config.Section) {
	m.look()
	var items []item
	add := func(cmds []config.Command) {
		for i := range cmds {
			c := &cmds[i]
			items = append(items, item{title: c.Name, keywords: c.Cmd, risk: c.Risk, disabled: m.unavailable(c) != "", ref: cmdRef{s.Name, s, c}})
		}
	}
	if len(s.Commands) > 0 { // every list has headings, even a pack that skips groups
		items = heading(items, "Commands")
	}
	add(s.Commands)
	for _, g := range s.Groups {
		items = heading(items, g.Name)
		add(g.Commands)
	}
	if s != m.recipesSection() {
		m.opened = ""
	} else {
		saved := m.savedRecipes()
		m.listed = saved
		if i := slices.IndexFunc(saved, func(r config.Recipe) bool { return r.Name == m.opened }); i >= 0 {
			r := saved[i]
			items = append(heading(nil, r.Name),
				item{title: "Run", accent: true, ref: savedRecipeRef{r}},
				item{title: "Describe", ref: describeRecipeRef{r.Name}},
				item{title: "Remove", risk: config.RiskWarn, ref: removeRecipeRef{r.Name}})
		} else {
			m.opened = "" // gone, or never was
			if len(m.recipe) == 0 {
				items = append(items, item{title: "Nothing yet", header: true, note: true})
			} else {
				items = append(items, item{title: "Run recipe", accent: true, ref: runRecipeRef{}})
			}
			for i, st := range m.recipe {
				c := st.ref.cmd
				items = append(items, item{title: c.Name, stepAt: i + 1, indent: true, where: st.ref.section, keywords: c.Cmd, risk: c.Risk, disabled: m.unavailable(c) != "", ref: st.ref})
			}
			if len(saved) > 0 {
				items = heading(items, "All recipes")
				for _, r := range saved {
					items = append(items, item{title: r.Name, ref: openRecipeRef{r}})
				}
			}
		}
	}
	m.section = s
	m.list = newPicker(items, nil, false)
	m.list.noun = "command"
	m.list.step = m.itemSteps
	if s == m.recipesSection() && len(m.recipe) > 0 && m.opened == "" {
		m.list.cursor = 1 // Run recipe, under its heading
		if m.stopped > 0 && m.stopped <= len(m.recipe) {
			m.list.cursor = 1 + m.stopped // where it stopped: enter runs it again, and on from there
		}
	}
	if s == m.recipesSection() && m.removing != "" {
		if m.opened == "" {
			m.landOnRecipe(m.removing) // it's gone
		} else {
			m.list.cursor = slices.IndexFunc(m.list.view, func(it item) bool { _, ok := it.ref.(removeRecipeRef); return ok })
		}
	}
	m.checkTools()
}

// look takes stock of the shell's folder: which `when:` commands work
// there, and its project pack.
func (m *Model) look() {
	dir := ""
	if m.opt.Dir != nil {
		dir = m.opt.Dir()
	}
	m.here = config.NewHere(dir)
	if m.opt.Project != nil {
		m.project = m.opt.Project(dir)
	}
}

// unavailable says why a command can't be used here, or "": it doesn't
// fit this folder or this setup. Such rows are greyed out and skipped.
func (m *Model) unavailable(c *config.Command) string {
	if c.Disabled != "" {
		return c.Disabled
	}
	return m.here.Unmet(c)
}

// blocked says why nothing can be sent to the shell just now, or "". The
// menu still works as usual meanwhile; only running is held back, and the
// status bar says why.
func (m *Model) blocked() string {
	switch {
	case m.busy != "":
		return m.busy + " is running in the shell. Finish it there first."
	case m.scrolling:
		return "The shell is scrolled back. Press q there to stop scrolling."
	}
	return ""
}

// readShell asks what the shell is doing, and greys commands to match.
func (m *Model) readShell() {
	if m.opt.Shell == nil {
		return
	}
	st := m.opt.Shell()
	switch {
	case st.Busy == "":
		m.busy = ""
	case m.busy == "": // keep "Edit a pack" rather than the program's name
		m.busy, m.quiet = st.Busy, st.Starting
	}
	m.scrolling, m.typed = st.Scrolling, st.Typed
	m.regrey()
}

// watchShell keeps looking at the shell while a warning about it is up. One
// look at a time, and it stops by itself when there's nothing left to warn of.
func (m *Model) watchShell() tea.Cmd {
	if m.looking || m.opt.Shell == nil || (m.typed == "" && !m.scrolling) {
		return nil
	}
	m.looking = true
	return tea.Tick(shellLook, func(time.Time) tea.Msg { return shellLookMsg{} })
}

// regrey works out again which listed commands can't be used right now.
func (m *Model) regrey() {
	for _, l := range [][]item{m.list.items, m.list.view, m.list.search} {
		for i := range l {
			if r, ok := l[i].ref.(cmdRef); ok {
				l[i].disabled = m.unavailable(r.cmd) != ""
			}
		}
	}
}

// catchUp brings the menu up to date with what the shell may have changed: its
// packs, and what fits its folder. It's for when a command finishes and when the
// user comes back from the shell, the two ways the shell gets to change anything.
func (m *Model) catchUp() {
	if !m.reload() { // a reload has looked again already
		m.lookAgain()
	}
}

// lookAgain is for coming back to the menu, perhaps after a cd: the top
// level is rebuilt (the project may differ), a section just regreys.
func (m *Model) lookAgain() {
	if m.mode != modeBrowse || m.list.filtered() {
		return
	}
	if m.section == nil {
		focus := ""
		if it := m.list.selected(); it != nil {
			if s, ok := it.ref.(*config.Section); ok {
				focus = s.Name
			}
		}
		m.showRoot(focus)
		return
	}
	m.look()
	m.regrey()
}

// checkTools forgets which tools were found, so the status bar looks
// again: on entering a section, and on coming back to the top.
func (m *Model) checkTools() { m.missing = map[string][]config.Missing{} }

// missingFor is which of s's tools can't be run, looked up the first time
// the status bar shows s after checkTools.
func (m *Model) missingFor(s *config.Section) []config.Missing {
	if m.missing == nil {
		m.missing = map[string][]config.Missing{}
	}
	miss, ok := m.missing[s.Name]
	if !ok {
		miss = config.MissingTools(s.Needs)
		m.missing[s.Name] = miss
	}
	return miss
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	if m.resume != nil {
		cmd, m.resume = tea.Batch(cmd, m.resume), nil
	}
	// Every way of not going through with a step (←, esc, ctrl-c, "No") ends
	// back in the list with nothing sent: that stops the recipe, and keeps it.
	if m.run != nil && !m.run.sent && m.mode == modeBrowse {
		if m.errMsg == "" {
			m.hint = fmt.Sprintf("Recipe stopped at command %d%s", m.run.at+1, stoppedKeys)
		}
		m.stopRun()
	}
	if bar := m.drawBar(msg); bar != nil {
		return m, tea.Batch(cmd, bar)
	}
	return m, cmd
}

// drawBar sends the status bar to its own pane when it's changed, off the
// UI thread.
func (m *Model) drawBar(msg tea.Msg) tea.Cmd {
	if m.opt.Bar == nil {
		return nil
	}
	if _, ok := msg.(tea.WindowSizeMsg); ok || m.barW == 0 {
		m.barW = m.opt.BarWidth()
	}
	if m.barW <= 0 {
		return nil
	}
	line := m.statusBar(m.barW)
	if line == m.barLine {
		return nil
	}
	m.barLine = line
	draw, line := m.opt.Bar, in256(line)
	return func() tea.Msg { draw(line); return nil }
}

// in256 puts a line's colours into the 256 of clingon's palette. The bar goes
// straight to its pane, past the renderer that does this for the menu, and
// Terminal.app can't read 24-bit colour if tmux passes it on.
func in256(line string) string {
	var out strings.Builder
	w := colorprofile.Writer{Forward: &out, Profile: colorprofile.ANSI256}
	if _, err := w.WriteString(line); err != nil {
		return line
	}
	return out.String()
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		// Repaint from clean: a resize as the window opens can leave the
		// renderer's idea of the screen wrong, and scrolling then smears rows.
		return m, tea.ClearScreen
	case tea.BlurMsg:
		m.blurred = true
		m.blurInputs()
		return m, m.watchShell()
	case tea.FocusMsg:
		m.blurred = false
		m.runSeq++ // the user is back: no need to bring them
		// What the shell's doing first: the same look at it gets its folder,
		// which looking again (below) then needn't ask for.
		m.readShell()
		m.catchUp()
		m.relistSaved()
		var cmds []tea.Cmd
		for _, in := range m.shownInputs() {
			cmds = append(cmds, in.Focus())
		}
		// Something running in the shell (maybe started by hand) would get
		// anything the menu sends typed into it: hold off until it ends.
		if m.busy != "" && !m.waiting {
			m.waiting = true // one wait at a time, however often focus comes back
			cmds = append(cmds, m.waitIdle(-1))
		}
		return m, tea.Batch(cmds...)
	case introMsg:
		if m.intro++; m.intro >= LogoWidth {
			m.intro = -1
			return m, nil
		}
		return m, introTick()
	case busyGraceMsg:
		m.quiet = false
		return m, nil
	case noticeTick:
		if msg.seq != m.notice.seq || m.notice.text == "" {
			return m, nil
		}
		if m.notice.left--; m.notice.left <= 0 {
			m.notice = notice{seq: m.notice.seq}
			return m, nil
		}
		return m, m.noticeTick()
	case idleMsg:
		if msg.seq < 0 {
			m.waiting = false
		}
		m.busy = ""
		m.readShell()
		m.catchUp() // it may have added a pack, or changed folder
		m.relistSaved()
		m.removing = ""
		// Back to the menu only if the shell is really done: one left
		// mid-command (a quote still open, say) needs the user there.
		back := msg.ok && m.typed == "" && m.opt.FocusMenu != nil
		if m.run != nil && m.run.sent && msg.seq == m.run.seq {
			// Only if the recipe needs the menu next. A step going straight
			// to the shell would take focus right back, and the focus event
			// arriving late would read as the user coming back by themselves.
			cmd := m.stepDone(msg.ok)
			if back && (m.run == nil || m.mode != modeBrowse) {
				m.opt.FocusMenu()
			}
			return m, tea.Batch(cmd, m.watchShell())
		}
		if back && msg.seq == m.runSeq {
			m.opt.FocusMenu()
		}
		return m, m.watchShell()
	case shellLookMsg:
		m.looking = false
		if m.busy != "" { // the wait for it looks again as it ends
			return m, nil
		}
		if m.mode != modeTyped { // that question is about what it saw when asked
			m.readShell()
		}
		var wait tea.Cmd
		if m.busy != "" && !m.waiting { // what was typed is running now
			m.waiting = true
			wait = m.waitIdle(-1)
		}
		return m, tea.Batch(wait, m.watchShell())
	case tea.PasteMsg: // as if typed, wherever the keys are going
		return m.Update(typed(msg.Content))
	case tea.KeyPressMsg:
		if m.blurred { // keys mean focus is here, focus event or not: blurred inputs would drop them
			m.blurred = false
			var blinks []tea.Cmd
			for _, in := range m.shownInputs() {
				blinks = append(blinks, in.Focus())
			}
			m.resume = tea.Batch(blinks...)
		}
		if m.notice.text != "" { // any key dismisses it, and does nothing else
			m.notice = notice{seq: m.notice.seq}
			return m, nil
		}
		if m.help { // any key closes it, and does nothing else
			m.help = false
			return m, nil
		}
		if keyName(msg) == "ctrl+c" {
			return m, m.cancel()
		}
		switch m.mode {
		case modeTrust:
			return m, m.trustKey(msg)
		case modeTyped:
			return m, m.typedKey(msg)
		case modeQuit:
			return m, m.quitKey(msg)
		case modeSave:
			return m, m.saveKey(msg)
		case modeSaveName:
			return m, m.saveNameKey(msg)
		case modeBrowse:
			return m, m.browseKey(msg)
		case modeFill:
			return m, m.fillKey(msg)
		case modeConfirm:
			return m, m.confirmKey(msg)
		}
	}
	// cursor blinks and the like go to whichever input is showing
	var cmd tea.Cmd
	switch {
	case m.mode == modeFill && m.job.usePick:
		m.job.pick.filter, cmd = m.job.pick.filter.Update(msg)
	case m.mode == modeFill:
		m.job.input, cmd = m.job.input.Update(msg)
	case m.mode == modeSaveName, m.mode == modeConfirm && m.pending != nil && m.pending.risk == config.RiskDanger && !m.pending.typed:
		m.word, cmd = m.word.Update(msg)
	default:
		m.list.filter, cmd = m.list.filter.Update(msg)
	}
	return m, cmd
}

// cancel is Ctrl-C: drop whatever is in progress, but never quit. Closing
// clingon on Ctrl-C would throw away the session, and the next key (often
// a Ctrl-D) would land in the user's own shell and log them out.
func (m *Model) cancel() tea.Cmd {
	m.errMsg, m.hint = "", ""
	switch {
	case m.mode == modeFill && m.job.usePick && m.job.pick.filtered(): // the filter first, as in the menu's list
		m.job.pick.clearFilter()
	case m.mode != modeBrowse:
		m.pending, m.job, m.mode = nil, nil, modeBrowse
	case m.list.filtered():
		m.list.clearFilter()
	case m.section != nil:
		m.showRoot(m.section.Name)
	default:
		m.hint = "Nothing to cancel · esc quit"
	}
	return nil
}

// quit closes clingon, and with it the shell and everything in it: never with
// a command running there, and only after asking. Print mode has no shell to
// lose, so it just goes.
func (m *Model) quit() tea.Cmd {
	if m.busy == "" {
		m.readShell() // one may have started since the last look
	}
	if m.busy != "" {
		m.hint = m.busy + " is running in the shell: finish it there before quitting"
		return nil
	}
	if m.opt.Send == nil {
		m.Quit = true
		return tea.Quit
	}
	m.jobs = nil
	if m.opt.Jobs != nil {
		m.jobs = m.opt.Jobs()
	}
	m.mode, m.yes = modeQuit, false
	return nil
}

// quitKey handles "quit clingon?": No (first) or Yes.
func (m *Model) quitKey(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "up", "down", "ctrl+p", "ctrl+n", "ctrl+k", "ctrl+j", "tab":
		m.yes = !m.yes
		return nil
	case "enter", "right":
		if !m.yes {
			m.mode = modeBrowse
			return nil
		}
	case "esc", "left":
		m.mode = modeBrowse
		return nil
	default:
		return nil
	}
	m.Quit = true
	return tea.Quit
}

// quitView asks whether to quit, saying what would end with it.
func (m *Model) quitView(w int) string {
	row := func(on bool, s string) string {
		if on {
			return cursorStyle.Render("› " + s)
		}
		return "  " + s
	}
	title, why := headerStyle.Render("Quit clingon?"), "This closes its shell."
	if len(m.jobs) > 0 {
		title = warnStyle.Bold(true).Render("⚠ Still running in the shell")
		why = "Quitting closes the shell, and ends: " + strings.Join(m.jobs, ", ")
	}
	return title + "\n" + lipgloss.NewStyle().Width(w).Render(why) + "\n\n" + panel([]string{row(!m.yes, "No, stay"), row(m.yes, "Yes, quit")}, w)
}

func (m *Model) browseKey(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "esc":
		switch {
		case m.list.filtered():
			m.list.clearFilter()
		case m.opened != "":
			m.closeRecipe()
		case m.section != nil:
			m.showRoot(m.section.Name)
		default:
			return m.quit()
		}
		return nil
	case "left":
		switch {
		case m.opened != "":
			m.closeRecipe()
		case m.section != nil:
			m.showRoot(m.section.Name)
		case m.list.filtered():
			m.list.clearFilter()
		}
		return nil
	case "right", "enter":
		return m.open()
	case "/":
		if m.list.filtering { // opening it is below, with the rest of what's typed
			m.list.stopFiltering()
			return nil
		}
	case "r":
		if s := m.recipesSection(); s != nil && !m.list.filtering {
			m.errMsg, m.hint = "", ""
			m.opened = ""
			m.showSection(s)
			return nil
		}
	case "x":
		if len(m.recipe) > 0 && !m.list.filtering {
			m.run, m.recipe, m.stopped = nil, nil, 0
			m.relistRecipe()
			m.errMsg, m.hint = "", "Recipe cleared"
			return nil
		}
	case "?":
		if !m.list.filtering {
			m.help = true
			return nil
		}
	}
	m.errMsg, m.hint = "", ""
	if !m.list.filtering {
		if m.numberKey(msg) {
			return nil
		}
		return m.keyOnClosedList(&m.list, msg)
	}
	return m.list.update(msg)
}

// numberKey is 1-9 on a command, giving it that step in the recipe. Steps run
// 1, 2, 3 with no holes, so a number past the next free one is refused.
func (m *Model) numberKey(msg tea.KeyPressMsg) bool {
	k := keyName(msg)
	if len(k) != 1 || k < "1" || k > "9" || m.opt.Send == nil { // print mode prints one command
		return false
	}
	it := m.list.selected()
	if it == nil {
		return false
	}
	r, ok := it.ref.(cmdRef)
	if !ok {
		return false
	}
	if m.run != nil { // the steps can't move under it
		m.hint = "The recipe is running: x clears it"
		return true
	}
	n := int(k[0] - '0')
	if n > len(m.recipe)+1 {
		m.hint = fmt.Sprintf("No gaps in a recipe: this can go up to position %d", len(m.recipe)+1)
		return true
	}
	m.setStep(r, n)
	m.stopped = 0 // it's a different recipe now
	m.relistRecipe()
	return true
}

// RecipesSection is the name of clingon's own section that lists the recipe.
const RecipesSection = "Recipes"

// CommandLineSection is clingon's own section for commands you type yourself, and
// CommandLineID is its one entry, Type a command.
const (
	CommandLineSection = "Commands"
	CommandLineID      = "freeform-00000000"
)

// recipeStep is a command with a number.
type recipeStep struct {
	key string // its ID
	ref cmdRef
}

// recipesSection is clingon's own section for the recipe, or nil where
// clingon's sections aren't shown (clingon print).
func (m *Model) recipesSection() *config.Section {
	for _, s := range m.builtin {
		if s.Name == RecipesSection {
			return s
		}
	}
	return nil
}

// recipesListed is that section with the recipe's commands in it, for the info
// pane, which says what's in a section before it's opened.
func (m *Model) recipesListed(s *config.Section) *config.Section {
	listed := *s
	listed.Groups = slices.Clone(s.Groups)
	for _, st := range m.recipe {
		listed.Groups[0].Commands = append(listed.Groups[0].Commands, *st.ref.cmd)
	}
	return &listed
}

// refindRecipe points the recipe at a reloaded config, whose commands are new
// copies. A command that's gone leaves the recipe, and the rest close up.
func (m *Model) refindRecipe() {
	now := m.commandsByID()
	before := len(m.recipe)
	kept := m.recipe[:0]
	for _, st := range m.recipe {
		if r, ok := now[st.key]; ok {
			kept = append(kept, recipeStep{st.key, r})
		}
	}
	if len(kept) != before {
		m.stopped = 0 // the steps have moved
	}
	m.recipe = kept
}

// commandsByID is every command there is to put in a recipe.
func (m *Model) commandsByID() map[string]cmdRef {
	now := map[string]cmdRef{}
	add := func(s *config.Section) {
		for _, c := range s.All() {
			r := cmdRef{s.Name, s, c}
			now[r.id()] = r
		}
	}
	for _, s := range m.builtin {
		add(s)
	}
	for i := range m.opt.Config.Sections {
		add(&m.opt.Config.Sections[i])
	}
	if m.project != nil && m.project.Config != nil {
		for i := range m.project.Config.Sections {
			add(&m.project.Config.Sections[i])
		}
	}
	return now
}

// setStep makes a command step n, and the chain ends there: every later step
// is cleared, and whichever had n makes way. Earlier steps stay, even ones of
// the same command: a command can run more than once. If it's already step n,
// that step comes off and the steps after close up, so a double press can't
// lose the rest of the chain.
func (m *Model) setStep(r cmdRef, n int) {
	key := r.id()
	if n <= len(m.recipe) && m.recipe[n-1].key == key {
		m.recipe = slices.Delete(m.recipe, n-1, n)
		return
	}
	m.recipe = append(m.recipe[:n-1], recipeStep{key, r})
}

// stepsOf is every step a command has in the recipe, ascending.
func (m *Model) stepsOf(r cmdRef) []int {
	if len(m.recipe) == 0 { // the usual case, asked for every row drawn
		return nil
	}
	var steps []int
	key := r.id()
	for i, s := range m.recipe {
		if s.key == key {
			steps = append(steps, i+1)
		}
	}
	return steps
}

// itemSteps is the numbers a list's row shows: its own, in the recipe's list,
// where a row is one step; else all its command's.
func (m *Model) itemSteps(it item) []int {
	if it.stepAt > 0 {
		return []int{it.stepAt}
	}
	if r, ok := it.ref.(cmdRef); ok {
		return m.stepsOf(r)
	}
	return nil
}

// recipeBar lists the numbered commands in order, as the list marks them, on
// the quiet band. With too little room names are shortened, longest first, so
// every number still shows.
func (m *Model) recipeBar(w int) string {
	longest, total := 0, 0
	for _, s := range m.recipe {
		nw := ansi.StringWidth(s.ref.cmd.Name)
		longest, total = max(longest, nw), total+nw
	}
	label := dimStyle.Render("Recipe")
	if m.recipesSection() != nil { // the keys in pink, as everywhere else
		label = strings.Join(keyHints([]string{"r recipe", "x clear"}), faintStyle.Render(" · "))
	}
	const arrow = " →"
	n := len(m.recipe)
	room := w - 2 - ansi.StringWidth(label) - n*(1+stepW+1) - (n-1)*ansi.StringWidth(arrow)
	limit := longest
	for ; limit > 0 && total > room; limit-- {
		total = 0
		for _, s := range m.recipe {
			total += min(ansi.StringWidth(s.ref.cmd.Name), limit-1)
		}
	}
	line := label
	for i, s := range m.recipe {
		if i > 0 {
			line += faintStyle.Render(arrow)
		}
		line += " " + stepBadge(i+1) + " " + valueStyle.Render(ansi.Truncate(s.ref.cmd.Name, limit, "…"))
	}
	return onBg(" "+ansi.Truncate(line, max(w-2, 0), "…"), raised, w)
}

// id names a command: a reload replaces the config it points into, so its place
// in the recipe can't hang on the pointer.
func (r cmdRef) id() string { return r.sec.CommandID(r.cmd) }

// open acts on the highlighted row.
func (m *Model) open() tea.Cmd {
	it := m.list.selected()
	if it == nil {
		return nil
	}
	m.errMsg, m.hint = "", ""
	switch r := it.ref.(type) {
	case *config.Section:
		m.showSection(r)
	case projectRef:
		if m.project != nil && m.project.Err == nil {
			m.mode, m.yes, m.trustTop, m.trustMax = modeTrust, false, 0, -1
		}
	case cmdRef:
		if m.run != nil {
			m.hint = "The recipe is running: x clears it"
			return nil
		}
		if it.stepAt > 0 { // a row of the recipe's own list: the recipe, from here
			return m.runFrom(it.stepAt - 1)
		}
		if m.blocked() != "" {
			m.readShell() // it may have finished without a word: look again
		}
		if m.unavailable(r.cmd) != "" || m.blocked() != "" {
			return nil // greyed out, or the shell isn't ready: the status bar says so
		}
		return m.startCommand(r)
	case runRecipeRef:
		if m.run != nil {
			m.hint = "The recipe is running: x clears it"
			return nil
		}
		if m.opt.Recipes != nil && !m.recipeSaved() {
			m.mode, m.yes = modeSave, false
			return nil
		}
		return m.startRecipe()
	case openRecipeRef:
		m.opened = r.Name
		m.showSection(m.section)
	case savedRecipeRef:
		if m.run != nil {
			m.hint = "The recipe is running: x clears it"
			return nil
		}
		now := m.commandsByID()
		var steps []recipeStep
		for i, id := range r.Steps {
			c, ok := now[id]
			if !ok {
				m.errMsg = fmt.Sprintf("✗ %s can't run: the command at position %d is gone (%s)", r.Name, i+1, id)
				return nil
			}
			steps = append(steps, recipeStep{id, c})
		}
		m.recipe, m.opened, m.stopped = steps, "", 0
		m.showSection(m.section)
		return m.startRecipe()
	case describeRecipeRef:
		if m.run != nil {
			m.hint = "The recipe is running: x clears it"
			return nil
		}
		if m.blocked() != "" {
			m.readShell()
		}
		if m.blocked() != "" {
			return nil // the status bar says so
		}
		cmd := m.describeCommand(r.name)
		done := m.finish(pending{section: m.section.Name, sec: m.section, name: "Describe " + r.name, cmd: cmd, shown: cmd})
		// Stay on its row, to read about another: the list's first row runs one.
		if i := slices.IndexFunc(m.list.view, func(it item) bool { return it.ref == any(r) }); i >= 0 {
			m.list.cursor = i
		}
		return done
	case removeRecipeRef:
		if m.run != nil {
			m.hint = "The recipe is running: x clears it"
			return nil
		}
		if m.blocked() != "" {
			m.readShell()
		}
		if m.blocked() != "" {
			return nil // the status bar says so
		}
		cmd := m.removeCommand(r.name)
		m.removing = r.name // No, or Yes, comes back to this row
		return m.finish(pending{section: m.section.Name, sec: m.section, name: "Remove " + r.name, desc: removeDesc, risk: config.RiskWarn, cmd: cmd, shown: cmd})
	}
	return nil
}

// recipeFromHere says what running a row of the recipe's own list does.
func recipeFromHere(at, total int) string {
	switch {
	case total == 1:
		return "Runs this recipe's one command."
	case at == 1:
		return "Runs the recipe from here, its first command."
	case at == total:
		return fmt.Sprintf("Runs the recipe from here, just its last command. The %s before it don't run.", plural(at-1, "command"))
	}
	return fmt.Sprintf("Runs the recipe from here, command %d of %d. The ones before it don't run.", at, total)
}

// runRecipeRef is the row that runs the recipe.
type runRecipeRef struct{}

// openRecipeRef is a saved recipe's row in All recipes: it opens the list of
// what can be done with it.
type openRecipeRef struct{ config.Recipe }

// savedRecipeRef is that list's Run: it becomes the recipe, and runs.
type savedRecipeRef struct{ config.Recipe }

// describeRecipeRef is its Describe: the shell says what it does.
type describeRecipeRef struct{ name string }

// removeRecipeRef is its Remove: it asks, then removes the recipe.
type removeRecipeRef struct{ name string }

const removeDesc = "Removes this recipe. The commands in it stay where they are."

func (m *Model) removeCommand(name string) string {
	return m.opt.Self + " recipe remove " + shellQuote(name)
}

// closeRecipe goes back from a recipe's own list to its row in All recipes.
func (m *Model) closeRecipe() {
	name := m.opened
	m.opened = ""
	m.showSection(m.section)
	m.landOnRecipe(name)
}

// landOnRecipe puts the cursor on that row in All recipes. If it's gone that's
// the one after it, or the last.
func (m *Model) landOnRecipe(name string) {
	last := -1
	for i, it := range m.list.view {
		r, ok := it.ref.(openRecipeRef)
		if !ok {
			continue
		}
		last = i
		if strings.ToLower(r.Name) >= strings.ToLower(name) { // they're sorted
			break
		}
	}
	if last >= 0 {
		m.list.cursor = last
	}
}

// relistSaved redraws the recipes' list if the saved ones aren't what it shows:
// the shell has saved or removed one, from the menu or typed by hand. A filter
// being typed is left alone, and it's looked at again next time.
func (m *Model) relistSaved() {
	if m.mode != modeBrowse || m.list.filtered() || m.section == nil || m.section != m.recipesSection() {
		return
	}
	if !slices.EqualFunc(m.listed, m.savedRecipes(), func(a, b config.Recipe) bool { return a.Name == b.Name }) {
		m.relistRecipe()
	}
}

func (m *Model) describeCommand(name string) string {
	return m.opt.Self + " recipe describe " + shellQuote(name)
}

func (m *Model) startRecipe() tea.Cmd { return m.runFrom(0) }

// runFrom sets the recipe going from a step.
func (m *Model) runFrom(at int) tea.Cmd {
	m.run = &recipeRun{at: at}
	m.clearStopped()
	return m.runStep()
}

// clearStopped forgets where the recipe stopped.
func (m *Model) clearStopped() {
	if m.stopped > 0 {
		m.stopped = 0
		m.relistStopped()
	}
}

// stoppedKeys ends what's said about a recipe that's stopped at a command.
const stoppedKeys = " · r recipe · x clear"

// stopRun ends the run at the command it's on, where the recipe's list then starts.
func (m *Model) stopRun() {
	if !m.run.saving {
		m.stopped = m.run.at + 1
	}
	m.run = nil
	m.relistStopped()
}

// relistStopped puts the recipe's list where it stopped. A filter keeps its list.
func (m *Model) relistStopped() {
	if !m.list.filtered() {
		m.relistRecipe()
	}
}

func (m *Model) savedRecipes() []config.Recipe {
	if m.opt.Recipes == nil {
		return nil
	}
	return m.opt.Recipes()
}

func (m *Model) recipeIDs() []string {
	ids := make([]string, len(m.recipe))
	for i, st := range m.recipe {
		ids[i] = st.key
	}
	return ids
}

// recipeSaved says whether these same steps are saved already: then there's
// nothing to ask, however many times it's run.
func (m *Model) recipeSaved() bool {
	ids := m.recipeIDs()
	return slices.ContainsFunc(m.savedRecipes(), func(r config.Recipe) bool { return slices.Equal(r.Steps, ids) })
}

// saveKey handles "save this recipe?": No (first) runs it, Yes asks for a name.
func (m *Model) saveKey(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "up", "down", "ctrl+p", "ctrl+n", "ctrl+k", "ctrl+j", "tab":
		m.yes = !m.yes
		return nil
	case "esc", "left":
		m.mode = modeBrowse
		return nil
	case "enter", "right":
		if m.yes {
			return m.askName()
		}
	default:
		return nil
	}
	m.mode = modeBrowse
	return m.startRecipe()
}

func (m *Model) askName() tea.Cmd {
	m.mode, m.nameWhy = modeSaveName, ""
	m.word = newInput(accentStyle, "My AWS ECS flow")
	return m.word.Focus()
}

// saveNameKey takes the recipe's name. Saving is a command like any other, there
// to see in the shell, and it goes first: the recipe's steps follow when it's done.
func (m *Model) saveNameKey(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "esc", "left":
		m.mode, m.yes = modeSave, true
		return nil
	case "enter", "right":
		name := recipeName(m.word.Value())
		if name == "" {
			return nil // the box's placeholder says what goes there
		}
		if config.HasRecipe(m.savedRecipes(), name) {
			m.nameWhy = "You already have a recipe called that."
			return nil
		}
		cmd := m.saveCommand(shellQuote(name))
		m.run = &recipeRun{saving: true}
		return m.finish(pending{section: m.section.Name, sec: m.section, name: "Add recipe", cmd: cmd, shown: cmd})
	}
	m.nameWhy = ""
	return typeInto(&m.word, msg)
}

// recipeName tidies what was typed (or pasted) into one line.
func recipeName(s string) string { return strings.Join(strings.Fields(s), " ") }

// saveCommand is the command that saves the recipe under name, already quoted.
func (m *Model) saveCommand(name string) string {
	return m.opt.Self + " recipe add " + name + " " + strings.Join(m.recipeIDs(), " ")
}

// startCommand asks for what a command needs, if anything, and runs it.
func (m *Model) startCommand(r cmdRef) tea.Cmd {
	c := r.cmd
	if len(c.Args) == 0 {
		return m.finish(pending{section: r.section, sec: r.sec, name: c.Name, cmd: c.Cmd, shown: c.Cmd, risk: c.Risk, desc: describe(c, nil, nil, -1, "", "")})
	}
	m.job = &job{ref: r, vals: map[string]string{}, said: map[string]string{}}
	m.mode = modeFill
	return m.prepare()
}

// recipeRun is how far a recipe has got.
type recipeRun struct {
	at   int  // the step going now, an index into the recipe
	sent bool // it's in the shell; before that it's still being filled in or confirmed
	seq  int  // the wait for it to finish
	// saving: what's in the shell is the save, with step 1 still to come.
	saving bool
}

// runStep starts the step the run is at, like any other command: blanks are
// asked for now, when earlier steps have had their effect, and risky ones ask.
func (m *Model) runStep() tea.Cmd {
	r := m.recipe[m.run.at].ref
	m.run.sent = false
	m.look() // an earlier step may have changed folder
	if m.blocked() != "" {
		m.readShell()
	}
	why := m.unavailable(r.cmd)
	if why == "" {
		why = m.blocked()
	}
	if why != "" {
		m.errMsg = fmt.Sprintf("✗ Recipe stopped at command %d, %s, can't run: %s%s", m.run.at+1, r.cmd.Name, why, stoppedKeys)
		m.stopRun()
		return nil
	}
	return m.startCommand(r)
}

// stepDone moves the recipe on from a step that's finished: to the next, or
// it stops there if that step didn't work out. Either way the recipe stays, to
// run again: only x clears it.
func (m *Model) stepDone(ok bool) tea.Cmd {
	n, name := m.run.at+1, m.recipe[m.run.at].ref.cmd.Name
	why := ""
	failed, known := false, true
	if m.opt.LastFailed != nil {
		failed, known = m.opt.LastFailed()
	}
	switch {
	case !ok:
		why = "couldn't tell if it finished"
	case m.typed != "":
		why = "the shell is still waiting for the rest of it"
	case !known:
		why = "couldn't tell if it worked"
	case failed:
		why = "it failed"
	}
	if m.run.saving {
		m.run.saving = false
		if why != "" {
			said := "not added"
			if !failed {
				said = "maybe not added"
			}
			m.errMsg = fmt.Sprintf("✗ Recipe %s, so not run: %s", said, why)
			m.run = nil
			return nil
		}
		m.refresh() // it's on the list now
		m.clearStopped()
		return m.runStep()
	}
	if why != "" {
		m.errMsg = fmt.Sprintf("✗ Recipe stopped at command %d, %s: %s%s", n, name, why, stoppedKeys)
		m.stopRun()
		return nil
	}
	if m.run.at++; m.run.at < len(m.recipe) {
		return m.runStep()
	}
	m.run = nil
	m.hint = fmt.Sprintf("✓ Recipe finished: %s", plural(n, "command"))
	return nil
}

// relistRecipe redraws the recipe's own list, if that's what's showing, with
// the cursor where it was.
func (m *Model) relistRecipe() {
	if m.section == nil || m.section != m.recipesSection() {
		return
	}
	at := m.list.cursor
	m.showSection(m.section)
	if len(m.list.view) > 1 && m.removing == "" && m.stopped == 0 { // stopped, showSection puts it where
		m.list.cursor = min(at, len(m.list.view)-1)
	}
}

func (m *Model) arg() config.Arg { return m.job.ref.cmd.Args[m.job.step] }

// prepare sets up the widget for the current arg.
func (m *Model) prepare() tea.Cmd {
	j, a := m.job, m.arg()
	j.usePick = false
	prev := m.initial(a)
	j.invalid = ""
	if k := a.Kind(); k == "text" || k == "secret" {
		return m.textStep(a, prev)
	}
	var items []item
	for _, o := range a.Options {
		items = append(items, item{title: o.Value, desc: o.Label, value: o.Value, indent: o.Indent})
	}
	m.pickStep(items, a, prev)
	return nil
}

// initial is what an arg starts as: the answer given before, if it was
// answered (even with nothing), else its default.
func (m *Model) initial(a config.Arg) string {
	if v, answered := m.job.vals[a.Name]; answered {
		return v
	}
	return a.Default
}

func (m *Model) textStep(a config.Arg, prev string) tea.Cmd {
	in := newInput(accentStyle, a.Placeholder)
	if in.Placeholder == "" {
		in.Placeholder = "type a value"
		if a.Optional {
			in.Placeholder = "optional: enter to skip"
		}
	}
	if a.Kind() == "secret" {
		in.EchoMode, in.EchoCharacter = textinput.EchoPassword, '•'
		if a.Placeholder == "" {
			in.Placeholder = "hidden as you type, and kept out of your history"
		}
	}
	in.SetValue(prev)
	in.CursorEnd()
	m.job.input = in
	return m.job.input.Focus()
}

func (m *Model) pickStep(items []item, a config.Arg, prev string) {
	j := m.job
	j.pick = newPicker(items, nil, a.Multi())
	j.pick.noun = "option"
	if a.CountAs != "" {
		j.pick.noun = a.CountAs
	}
	j.usePick = true
	if a.Multi() {
		for _, v := range strings.Split(prev, a.Separator()) {
			for _, it := range j.pick.items {
				if v != "" && it.value == v {
					j.pick.tick(it.idx, true)
				}
			}
		}
	} else if prev != "" {
		j.pick.selectValue(prev)
	}
}

func oneLine(s string) string { return strings.ReplaceAll(s, "\n", " ⏎ ") }

// accept records v for the arg being asked and moves on: to the next arg,
// or to running the command.
func (m *Model) accept(v string) tea.Cmd {
	j := m.job
	j.vals[j.ref.cmd.Args[j.step].Name] = v
	j.said[j.ref.cmd.Args[j.step].Name] = j.optionSays()
	j.step++
	if j.step < len(j.ref.cmd.Args) {
		return m.prepare()
	}
	c := j.ref.cmd
	vals, shown := shellValues(c, j.vals), shellValues(c, j.vals)
	for _, a := range c.Args {
		if v := j.vals[a.Name]; a.Kind() == "secret" && v != "" {
			ref, err := m.keepSecret(v)
			if err != nil {
				return m.showNotice("Couldn't keep the secret", err.Error())
			}
			vals[a.Name], shown[a.Name] = ref, "<"+a.Name+">"
		}
	}
	desc := describe(c, j.vals, j.said, len(c.Args), "", "")
	return m.finish(pending{section: j.ref.section, sec: j.ref.sec, name: c.Name, cmd: tmpl.Render(c.Cmd, c.Known, vals),
		shown: tmpl.Render(c.Cmd, c.Known, shown), risk: c.Risk, desc: desc})
}

// keepSecret puts a secret value in a file only you can read, and returns
// what goes in the command instead: a substitution that reads it once and
// deletes it. So the value is never on screen or in shell history.
func (m *Model) keepSecret(v string) (string, error) {
	dir := m.opt.SecretDir
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "secret-")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(v); err != nil {
		return "", err
	}
	m.Secrets = append(m.Secrets, f.Name())
	q := shellQuote(f.Name())
	return `"$(cat ` + q + ` && rm -f ` + q + `)"`, nil
}

// current is the value the active widget would give right now.
func (j *job) current(a config.Arg) string {
	if !j.usePick {
		return strings.TrimSpace(j.input.Value())
	}
	if j.pick.multi {
		return strings.Join(j.pick.values(), a.Separator())
	}
	if it := j.pick.selected(); it != nil {
		return it.value
	}
	return j.pick.query() // nothing matches: take what was typed
}

// lastStep reports whether the arg being filled is the final one.
func (j *job) lastStep() bool { return j.step == len(j.ref.cmd.Args)-1 }

// keyOnClosedList is a key on a list with its filter closed, the same in every
// list: / opens it, starting its cursor, and any other text says that's how. Fast
// typing can bring more with the /, which goes on to be typed in. The rest goes to
// the list.
func (m *Model) keyOnClosedList(p *picker, msg tea.KeyPressMsg) tea.Cmd {
	k := keyName(msg)
	switch {
	case strings.HasPrefix(msg.Text, "/"):
		p.filtering = true
		blink := p.filter.Focus()
		if msg.Text == "/" {
			return blink
		}
		return tea.Batch(blink, p.update(typed(msg.Text[1:])))
	case msg.Text != "" && k != "[" && k != "]" && !(p.multi && k == "space"): // nothing's listening for text
		m.hint = "Press / to filter"
		return nil
	}
	return p.update(msg)
}

func (m *Model) fillKey(msg tea.KeyPressMsg) tea.Cmd {
	j := m.job
	m.hint = ""
	// Arrows always navigate, text boxes included: ← back, → forward.
	switch keyName(msg) {
	case "esc":
		if j.usePick && j.pick.filtered() { // as in the menu's list: the filter goes first
			j.pick.clearFilter()
			return nil
		}
		return m.backFrom(j.step)
	case "left":
		return m.backFrom(j.step)
	case "/":
		if j.usePick && j.pick.filtering {
			j.pick.stopFiltering()
			return nil
		}
	case "enter", "right":
		a := m.arg()
		v := j.current(a)
		if v == "" && !a.Optional && !a.Multi() {
			return nil // the box's placeholder says a value is needed
		}
		if v == "" && a.AtLeastOne {
			m.hint = "Press space to tick at least one"
			return nil
		}
		if v != "" && (!j.usePick || j.pick.selected() == nil) && !a.Matches(v) {
			j.invalid = "That doesn't look like a " + a.Label() + "."
			if a.Placeholder != "" {
				j.invalid += " For example: " + a.Placeholder
			}
			return nil
		}
		return m.accept(v)
	}
	j.invalid = ""
	if j.usePick {
		if !j.pick.filtering {
			return m.keyOnClosedList(&j.pick, msg)
		}
		return j.pick.update(msg)
	}
	return typeInto(&j.input, msg)
}

func (m *Model) finish(p pending) tea.Cmd {
	m.pending, m.yes, m.wrong = &p, false, false
	switch p.risk {
	case config.RiskDanger: // type the code first
		m.pending.code = newCode()
		m.word = newInput(dangerStyle, "")
		m.mode = modeConfirm
		return m.word.Focus()
	case config.RiskWarn:
		m.mode = modeConfirm
		return nil
	}
	return m.send()
}

// confirmKey handles the risky-command screen: a two-item list, "No, go
// back" (highlighted first) and "Yes, run it".
func (m *Model) confirmKey(msg tea.KeyPressMsg) tea.Cmd {
	if p := m.pending; p.risk == config.RiskDanger && !p.typed { // type the code first
		switch keyName(msg) {
		case "esc", "left":
			return m.goBack()
		case "enter":
			typed := m.word.Value()
			if typed == "" {
				return nil
			}
			if typed == p.code { // exactly: no spaces, lowercase
				p.typed, m.yes = true, false
				return nil
			}
			p.code, m.wrong = newCode(), true // a fresh one: no retyping the old one blind
			m.word.SetValue("")
			return nil
		}
		m.wrong = false
		return typeInto(&m.word, msg)
	}
	switch keyName(msg) {
	case "up", "down", "ctrl+p", "ctrl+n", "ctrl+k", "ctrl+j", "tab":
		m.yes = !m.yes
	case "enter", "right":
		if m.yes {
			return m.send()
		}
		return m.goBack()
	case "esc", "left":
		return m.goBack()
	}
	return nil
}

// typedKey handles "the shell has something typed": No, take me there
// (first) or Yes, clear it and run.
func (m *Model) typedKey(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "up", "down", "ctrl+p", "ctrl+n", "ctrl+k", "ctrl+j", "tab":
		m.yes = !m.yes
		return nil
	case "esc", "left":
		return m.goBack()
	case "enter", "right":
		if !m.yes {
			return m.toShell()
		}
	default:
		return nil
	}
	if m.opt.ClearShell != nil {
		if err := m.opt.ClearShell(); err != nil {
			m.errMsg = oneLine(err.Error())
			return m.goBack()
		}
	}
	m.typed, m.pending.cleared = "", true
	return m.send()
}

// toShell drops the command and sends the user to the shell.
func (m *Model) toShell() tea.Cmd {
	m.pending, m.job, m.mode = nil, nil, modeBrowse
	m.refresh()
	if m.opt.FocusShell != nil {
		m.opt.FocusShell()
	}
	return nil
}

// typedView asks what to do about what's typed in the shell.
func (m *Model) typedView(w int) string {
	row := func(on bool, s string) string {
		if on {
			return cursorStyle.Render("› " + s)
		}
		return "  " + s
	}
	head, why := "✎ The shell has something typed", "It has “"+ansi.Truncate(m.typed, max(w-8, 8), "…")+"” at its prompt, not run yet. Running this would type straight after it."
	if m.typed == unfinishedCommand {
		head, why = "✎ The shell is in the middle of a command", "It's waiting for the rest of one (a quote left open, say). Running this would become part of it."
	}
	return warnStyle.Bold(true).Render(head) + "\n" + lipgloss.NewStyle().Width(w).Render(why) + "\n\n" +
		panel([]string{row(!m.yes, "No, take me to the shell"), row(m.yes, "Yes, clear it and run this")}, w)
}

// trustKey handles "use this folder's commands?": No (first) or Yes.
func (m *Model) trustKey(msg tea.KeyPressMsg) tea.Cmd {
	yes := false
	// Yes only opens up once every command has been on screen.
	atEnd := m.trustMax >= 0 && m.trustTop >= m.trustMax
	switch keyName(msg) {
	case "up", "ctrl+p", "ctrl+k":
		if m.yes {
			m.yes = false
		} else if m.trustTop > 0 {
			m.trustTop--
		}
		return nil
	case "down", "ctrl+n", "ctrl+j":
		if atEnd {
			m.yes = true
		} else {
			m.trustTop++
		}
		return nil
	case "tab":
		if atEnd {
			m.yes = !m.yes
		} else {
			m.trustTop++
		}
		return nil
	case "enter", "right":
		yes = m.yes
	case "esc", "left":
	default:
		return nil
	}
	m.mode = modeBrowse
	if yes {
		if err := m.project.Trust(); err != nil {
			m.errMsg = oneLine(err.Error())
		}
	}
	m.showRoot("")
	return nil
}

// visible spells out anything but plain printable characters, like \t, so
// it can't take up room on screen, or none, without being seen.
func visible(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString(`\t`)
		case r < ' ' || r >= 0x7f:
			fmt.Fprintf(&b, `\u{%x}`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// trustView shows what a project pack would run, before it's used.
func (m *Model) trustView(w, avail int) string {
	p := m.project
	row := func(on bool, s string) string {
		if on {
			return cursorStyle.Render("› " + s)
		}
		return "  " + s
	}
	head := "This folder has its own commands"
	if p.Changed {
		head = "This folder's commands have changed since you trusted them"
	}
	lines := []string{
		warnStyle.Bold(true).Render(warnGlyph + " " + head),
		dimStyle.Render(ansi.Hardwrap(tildify(p.Path), max(w, 1), false)),
		"",
		"It would run these in your shell:",
	}
	lines = strings.Split(strings.Join(lines, "\n"), "\n") // the folder may have wrapped
	// Every command in full: what's cut off or scrolled away can't be judged.
	var cmds []string
	for _, c := range p.Cmds {
		for _, line := range strings.Split(c, "\n") {
			for _, part := range wrapSpaces(highlight(visible(line)), max(w-2, 1)) {
				cmds = append(cmds, "  "+part)
			}
		}
	}
	if len(p.Values) > 0 {
		cmds = append(cmds, "", "With these values going into them:")
		for _, v := range p.Values {
			for _, part := range wrapSpaces(visible(v), max(w-2, 1)) {
				cmds = append(cmds, "  "+part)
			}
		}
	}
	choices := panel([]string{row(!m.yes, "No, not now"), row(m.yes, "Yes, use them")}, w)
	foot := strings.Split(strings.Join([]string{"", dimStyle.Render(ansi.Wrap("Only use them if you trust where this folder came from. If the file changes, clingon asks again.", w, "")), "", choices}, "\n"), "\n")
	// Short of room, the note goes first: the choices must stay on screen.
	if len(lines)+len(foot)+2 > avail {
		foot = strings.Split("\n"+choices, "\n")
	}
	room := max(avail-len(lines)-len(foot), 1)
	if len(cmds) > room {
		room = max(room-1, 1) // for the line saying there's more
	}
	m.trustMax = max(len(cmds)-room, 0)
	m.trustTop = min(m.trustTop, m.trustMax)
	end := min(m.trustTop+room, len(cmds))
	lines = append(lines, cmds[m.trustTop:end]...)
	if end < len(cmds) {
		lines = append(lines, accentStyle.Render(fmt.Sprintf("  ↓ %d more to read before you can say yes", len(cmds)-end)))
	}
	lines = append(lines, foot...)
	return strings.Join(lines, "\n")
}

// helpView lists every key.
func (m *Model) helpView(w int) string {
	keys := [][2]string{
		{"↑ ↓", "move"},
		{"→ or enter", "open, next blank, run, or take the choice"},
		{"←", "back"},
		{"[ ]", "jump to the previous or next heading"},
		{"/", "filter (on the top level, every command); again stops"},
		{"esc", "clear the filter, go back, or quit at the top"},
		{"ctrl-c", "cancel what you're doing"},
		{"space", "tick or untick, in a checklist"},
		{"1-9", "recipe position: clears later ones; again removes it"},
		{"r", "go to the current recipe"},
		{"x", "clear the recipe"},
		{"tab/shift-tab", "switch panes: tab to the shell, shift-tab back"},
		{"?", "this screen"},
	}
	lines := []string{headerStyle.Render("Keys"), ""}
	for _, k := range keys {
		lines = append(lines, ansi.Truncate(cursorStyle.Render(fmt.Sprintf("%-13s", k[0]))+" "+k[1], w, "…"))
	}
	return strings.Join(lines, "\n")
}

// goBack leaves the risky-command screen without running: to the last
// thing filled in (answers kept), or the list if there was nothing to fill.
func (m *Model) goBack() tea.Cmd {
	m.pending = nil
	if j := m.job; j != nil && len(j.ref.cmd.Args) > 0 {
		m.mode = modeFill
		return m.backFrom(len(j.ref.cmd.Args))
	}
	m.job, m.mode = nil, modeBrowse
	m.refresh()
	m.removing = ""
	return nil
}

// backFrom goes back from step to the one before it. Before the first, it
// leaves the command.
func (m *Model) backFrom(step int) tea.Cmd {
	j := m.job
	if step == 0 {
		m.job, m.mode = nil, modeBrowse
		m.refresh()
		return nil
	}
	j.step = step - 1
	return m.prepare()
}

// refresh rebuilds the current list and clears the filter.
func (m *Model) refresh() {
	if m.section != nil {
		m.showSection(m.section)
	} else {
		m.list.clearFilter()
	}
}

func (m *Model) send() tea.Cmd {
	p := m.pending
	if m.opt.Send != nil && !p.cleared {
		// Look again right before typing anything into the shell.
		m.readShell()
		switch {
		case m.busy != "" || m.scrolling:
			m.pending, m.job, m.mode = nil, nil, modeBrowse
			m.refresh()
			var wait tea.Cmd
			if m.busy != "" {
				wait = m.waitIdle(-1)
			}
			return tea.Batch(wait, m.showNotice("Not sent", m.blocked()))
		case m.typed != "":
			m.mode, m.yes = modeTyped, false
			return nil
		}
	}
	m.pending, m.job, m.mode = nil, nil, modeBrowse
	if m.opt.Send == nil {
		m.Result = p.cmd
		return tea.Quit
	}
	if err := m.opt.Send(p.cmd); err != nil {
		m.errMsg = oneLine(err.Error())
		return nil
	}
	if m.opt.WaitIdle == nil {
		m.refresh()
		if m.run != nil {
			return m.stepDone(true)
		}
		return nil
	}
	m.busy, m.quiet = p.name, true
	m.refresh()
	wait := m.awaitIdle()
	if m.run != nil {
		m.run.sent, m.run.seq = true, m.runSeq
	}
	return tea.Batch(wait, tea.Tick(busyGrace, func(time.Time) tea.Msg { return busyGraceMsg{} }))
}

// awaitIdle waits, off the UI thread, for the command just sent to finish,
// so focus can come back to the menu.
func (m *Model) awaitIdle() tea.Cmd {
	if m.opt.WaitIdle == nil {
		return nil
	}
	m.runSeq++
	return m.waitIdle(m.runSeq)
}

// waitIdle waits off the UI thread for the shell to be back at its prompt.
// It always reports back, so a lock can't outlive a failed wait.
func (m *Model) waitIdle(seq int) tea.Cmd {
	if m.opt.WaitIdle == nil {
		return func() tea.Msg { return idleMsg{seq, false} }
	}
	wait := m.opt.WaitIdle
	return func() tea.Msg { return idleMsg{seq, wait()} }
}

// ---- view ----

// View is the menu, full screen; it hears when focus comes back to it
// (from the shell pane), so it can check what happened there.
func (m *Model) View() tea.View {
	v := tea.NewView(m.view())
	v.AltScreen = true
	v.ReportFocus = m.opt.Send != nil
	return v
}

// crumbSection is a section as the breadcrumb names it: its icon, then its
// name three cells along.
func crumbSection(s *config.Section) string {
	icon := slotIcon(s.Icon)
	return iconStyle(s.Color).Render(icon) + strings.Repeat(" ", 3-ansi.StringWidth(icon)) + s.Name
}

func (m *Model) view() string {
	if m.w == 0 {
		return ""
	}
	if m.blurred {
		m.blurInputs()
	}
	w := m.w
	top := banner(w, m.intro, "", "")
	// Category › icon Section, then the command once one is being run.
	var parts []string
	in := func(s *config.Section, name string) []string {
		if s == nil {
			return []string{name}
		}
		return []string{s.Category, crumbSection(s)}
	}
	switch {
	case m.mode != modeBrowse && m.pending != nil:
		parts = append(in(m.pending.sec, m.pending.section), m.pending.name)
	case m.job != nil:
		parts = append(in(m.job.ref.sec, m.job.ref.section), m.job.ref.cmd.Name)
	case m.section != nil:
		parts = in(m.section, m.section.Name)
		if m.opened != "" {
			parts = append(parts, m.opened)
		}
	}
	crumb := ""
	for i, p := range parts { // where you are bright, the way there dim
		if i < len(parts)-1 {
			crumb += dimStyle.Render(p) + faintStyle.Render(" › ")
		} else {
			crumb += crumbStyle.Render(p)
		}
	}
	head := []string{top, ansi.Truncate(crumb, w, "…"), ""}
	var foot []string
	if keys := m.footer(w); keys != "" {
		foot = append(foot, keys)
	}
	if m.opt.Bar == nil {
		foot = append(foot, m.statusBar(w))
	}
	info := m.infoPane(w)
	if len(foot) == 0 { // the pane's own border ends it: no rule to go above nothing
		info = info[:strings.LastIndex(info, "\n")]
	}
	avail := m.h - lipgloss.Height(strings.Join(head, "\n")) - lipgloss.Height(info)
	if len(foot) > 0 {
		avail -= lipgloss.Height(strings.Join(foot, "\n"))
	}
	// The filter's row is on every screen, greyed out where it can't be used, so
	// nothing below it moves from one screen to the next. A list draws its own.
	full := avail
	greyFilter := ""
	need := minForFilterRow
	if m.mode == modeTrust && m.notice.text == "" && !m.help {
		need = minForFilterRowOnTrust
	}
	if list := m.notice.text == "" && !m.help && (m.mode == modeBrowse || m.mode == modeFill && m.job.usePick); !list && avail >= need {
		greyFilter = faintStyle.Render("❯ / to filter") + "\n\n"
		avail -= 2
	}

	var body string
	switch {
	case m.notice.text != "":
		body = m.noticeView(w)
	case m.help:
		body = m.helpView(w)
	case m.mode == modeTrust:
		body = m.trustView(w, avail)
	case m.mode == modeTyped:
		body = m.typedView(w)
	case m.mode == modeQuit:
		body = m.quitView(w)
	case m.mode == modeSave:
		body = m.saveView(w)
	case m.mode == modeSaveName:
		body = m.saveNameView(w)
	case m.mode == modeBrowse:
		body = m.browseView(w, avail)
	case m.mode == modeFill:
		body = m.fillView(w, avail)
	case m.mode == modeConfirm:
		body = m.confirmView(w, avail)
	}
	if full <= 0 {
		body = "" // no room (MaxHeight(0) would mean no limit at all)
	} else {
		body = lipgloss.NewStyle().Height(full).MaxHeight(full).Render(greyFilter + body)
	}
	return strings.Join(append(append(head, body, info), foot...), "\n")
}

// stepsNote says how many blanks the command on show has to fill in, by its
// heading. One that just runs has no steps to mention.
func (m *Model) stepsNote() string {
	var c *config.Command
	switch {
	case m.notice.text != "" || m.help:
	case m.mode == modeFill:
		c = m.job.ref.cmd
	case m.mode == modeBrowse:
		if it := m.list.selected(); it != nil {
			if r, ok := it.ref.(cmdRef); ok {
				c = r.cmd
			}
		}
	}
	if c == nil || len(c.Args) == 0 {
		return ""
	}
	return labelStyle.Italic(true).Render(" (" + strings.ToUpper(plural(len(c.Args), "step")) + ")")
}

// infoPane is the same on every screen: what will run, then what it's
// about, in a fixed-height area at the foot of the menu.
func (m *Model) infoPane(w int) string {
	n := 3 // lines each for the command and the description
	switch {
	case m.h < 22:
		n = 1
	case m.h < 32:
		n = 2
	}
	rule := faintStyle.Render(strings.Repeat("─", w))
	cmd, desc := m.details()
	if m.notice.text != "" || m.help || m.mode == modeTrust || m.mode == modeTyped || m.mode == modeQuit {
		cmd, desc = "", ""
	} else if s := m.highlightedSection(); s != nil {
		head, body := strings.ToUpper(plural(len(s.All()), "command")), ""
		if s.Name == RecipesSection && len(m.recipe) == 0 {
			head, body = m.noCurrentRecipeInfo(w, 2*n+2)
		} else {
			// A section runs nothing itself: what's in it gets the whole area.
			body = m.sectionInfo(s, w, 2*n+2)
		}
		return strings.Join([]string{rule, labelStyle.Render(head), body, rule}, "\n")
	} else if m.onEmptyRecipe() {
		return strings.Join([]string{rule, labelStyle.Render("HOW RECIPES WORK"), fit(howRecipesWork(w), w, 2*n+2, false), rule}, "\n")
	}
	empty := cmd == ""
	if empty {
		cmd = faintStyle.Render("—")
	}
	// While filling in, the newest part (the arg being asked for) is at the
	// end, so long text keeps its end and loses its start.
	tail := m.mode == modeFill
	// The command sits in a block of its own, like code in a doc; with no
	// command there's no block, just the dash, at the same height.
	code := strings.Split(block(cmd, w-2, n, tail), "\n")
	for i := range code {
		if empty {
			code[i] = " " + code[i]
		} else {
			code[i] = onBg(" "+code[i], raised, w)
		}
	}
	return strings.Join([]string{
		rule,
		labelStyle.Render("COMMAND") + m.stepsNote(),
		strings.Join(code, "\n"),
		"", // room between the two
		labelStyle.Render("DESCRIPTION"),
		block(dimStyle.Render(desc), w, n, tail),
		rule,
	}, "\n")
}

// noCurrentRecipeInfo is what the menu's Recipes row says with nothing numbered:
// the recipes you have, or how to start.
func (m *Model) noCurrentRecipeInfo(w, n int) (head, body string) {
	saved := m.savedRecipes()
	if len(saved) == 0 {
		lines := wrapSpaces(dimStyle.Render("A recipe runs several commands in order."), w)
		lines = append(lines, wrapSpaces(dimStyle.Render("Press 1-9 on any command to build one."), w)...)
		return "NO RECIPES YET", fit(lines, w, n, false)
	}
	var names []string
	for _, r := range saved {
		names = append(names, r.Name)
	}
	line := words(crumbStyle, "All recipes:") + " " + words(dimStyle, strings.Join(names, ", "))
	return strings.ToUpper(plural(len(saved), "recipe")), fit(wrapSpaces(line, w), w, n, false)
}

// onEmptyRecipe says whether the Recipes screen has nothing to describe: no
// current recipe, and the cursor isn't on one you've added.
func (m *Model) onEmptyRecipe() bool {
	return m.mode == modeBrowse && m.section != nil && m.section == m.recipesSection() && m.opened == "" &&
		len(m.recipe) == 0 && !m.list.filtered() && m.list.selected() == nil
}

// howRecipesWork is the steps to build one, w wide.
func howRecipesWork(w int) []string {
	var lines []string
	for _, step := range []string{
		"1. Open a section and press 1 on the first command",
		"2. Press 2 on the next, from any section",
		"3. Press r, then enter on Run recipe",
	} {
		lines = append(lines, wrapSpaces(dimStyle.Render(step), w)...)
	}
	return lines
}

// block wraps s to width w and makes it exactly n lines, marking a cut
// with "…": at the end, or with tail at the start, keeping the last lines.
// It breaks only at spaces, so a command never appears split mid-flag
// (`-print -` / `delete`).
func block(s string, w, n int, tail bool) string { return fit(wrapSpaces(s, w), w, n, tail) }

// fit makes lines, already wrapped to width w, exactly n lines, as block.
func fit(lines []string, w, n int, tail bool) string {
	switch {
	case len(lines) > n && tail:
		lines = lines[len(lines)-n:]
		cut := 1 // drop the first, partial, word so the "…" isn't mid-word
		if plain := ansi.Strip(lines[0]); strings.IndexByte(plain, ' ') >= 0 {
			cut = ansi.StringWidth(plain[:strings.IndexByte(plain, ' ')])
		}
		lines[0] = "…" + ansi.TruncateLeft(lines[0], cut, "")
	case len(lines) > n:
		lines = lines[:n]
		lines[n-1] = ansi.Truncate(lines[n-1], w-1, "") + "…"
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// wrapSpaces word-wraps s (which may hold colour codes) at spaces only;
// a word wider than w is split hard.
func wrapSpaces(s string, w int) []string {
	var lines []string
	line, lineW := "", 0
	for _, word := range strings.Split(s, " ") {
		ww := ansi.StringWidth(word)
		switch {
		case lineW == 0 && ww <= w:
			line, lineW = word, ww
		case lineW+1+ww <= w:
			line, lineW = line+" "+word, lineW+1+ww
		default:
			if lineW > 0 {
				lines = append(lines, line)
			}
			parts := strings.Split(ansi.Hardwrap(word, w, true), "\n")
			lines = append(lines, parts[:len(parts)-1]...)
			line = parts[len(parts)-1]
			lineW = ansi.StringWidth(line)
		}
	}
	return append(lines, line)
}

// details says what will run and what it's about, for whatever is
// highlighted or being filled in.
func (m *Model) details() (cmd, desc string) {
	switch m.mode {
	case modeConfirm:
		// The screen itself asks No/Yes, so skip the "asks before running" note.
		desc := strings.TrimSuffix(strings.TrimSuffix(m.pending.desc, " "+warnNote), " "+dangerNote)
		return highlight(m.pending.shown), desc
	case modeFill:
		j, a := m.job, m.arg()
		cmd = preview(j.ref.cmd, j.vals, j.step, j.current(a))
		return cmd, j.describe()
	case modeSaveName:
		name := "<name>"
		if v := recipeName(m.word.Value()); v != "" {
			name = shellQuote(v)
		}
		return highlight(m.saveCommand(name)), "Adds the recipe under this name, then runs it."
	}
	it := m.list.selected()
	if it == nil {
		return "", ""
	}
	switch r := it.ref.(type) {
	case cmdRef:
		if why := m.unavailable(r.cmd); why != "" {
			return preview(r.cmd, nil, -1, ""), "Not available: " + why
		}
		if it.stepAt > 0 {
			return preview(r.cmd, nil, -1, ""), recipeFromHere(it.stepAt, len(m.recipe))
		}
		return preview(r.cmd, nil, -1, ""), describe(r.cmd, nil, nil, -1, "", "")
	case runRecipeRef:
		if m.stopped > 0 {
			return "", "Starts again from command 1, running every command in order, including the ones that already ran."
		}
		return "", "Runs " + plural(len(m.recipe), "command") + " in order, asking for any blanks as each is reached. Stops if one fails."
	case openRecipeRef:
		return "", upperFirst(plural(len(r.Steps), "command")) + ". Run it, describe it or remove it."
	case savedRecipeRef:
		return strings.Join(r.Steps, " "), "Makes this the current recipe and runs its " + plural(len(r.Steps), "command") + ", asking for any blanks as each is reached. Stops if one fails."
	case describeRecipeRef:
		return highlight(m.describeCommand(r.name)), "Lists this recipe's commands in the shell: what each one runs, what it does and what it will ask. Runs none of them."
	case removeRecipeRef:
		return highlight(m.removeCommand(r.name)), removeDesc + " " + warnNote
	}
	return "", ""
}

// describe is a command's description, extended by a sentence for each arg
// filled so far (through step, whose value is still tentative).
func describe(c *config.Command, vals, said map[string]string, step int, tentative, tentativeSays string) string {
	parts := []string{c.Desc}
	filled := map[string]string{} // the command's own description can name what's filled in
	for i, a := range c.Args {
		v, says := vals[a.Name], said[a.Name]
		switch {
		case i == step:
			v, says = tentative, tentativeSays
		case i > step:
			continue
		}
		if a.Kind() == "secret" && v != "" {
			v = secretMask
		}
		if a.CountAs != "" && v != "" {
			if n := len(strings.Split(v, a.Separator())); n >= countFrom { // too many to name
				v = plural(n, a.CountAs)
				if i != step { // and too many to say about, once they're chosen
					says = ""
				}
			}
		}
		if v != "" {
			filled[a.Name] = spoken(v)
		}
		switch {
		case v == "":
		case a.Desc != "":
			parts = append(parts, tmpl.Render(a.Desc, func(n string) bool { return n == a.Name }, map[string]string{a.Name: spoken(v)}))
		case says != "":
			parts = append(parts, sentence(says))
		}
	}
	switch c.Risk {
	case config.RiskWarn:
		parts = append(parts, warnNote)
	case config.RiskDanger:
		parts = append(parts, dangerNote)
	}
	parts[0] = tmpl.Render(c.Desc, func(n string) bool { _, ok := filled[n]; return ok }, filled)
	return strings.TrimSpace(strings.Join(parts, " "))
}

// countFrom is how many ticks a checklist's description names before it counts.
const countFrom = 3

// describe for the command being filled in, including the arg in progress.
func (j *job) describe() string {
	a := j.ref.cmd.Args[j.step]
	says := j.optionSays()
	// Too many to name in the room there is: about the one you're on instead.
	if j.usePick && a.CountAs != "" && len(j.pick.picked()) >= countFrom {
		says = ""
		if it := j.pick.selected(); it != nil && j.pick.chosen[it.idx] {
			says = it.desc
		}
	}
	d := describe(j.ref.cmd, j.vals, j.said, j.step, j.current(a), says)
	// In a checklist, say what the highlighted option does before it's ticked.
	if j.usePick && j.pick.multi {
		if it := j.pick.selected(); it != nil && it.desc != "" && !j.pick.chosen[it.idx] && !j.pick.filtering {
			d += " " + dimStyle.Italic(true).Render("Space adds: "+it.desc+".")
		}
	}
	return d
}

// optionSays is what the chosen option(s) of the current arg say about
// themselves: their labels.
func (j *job) optionSays() string {
	if !j.usePick {
		return ""
	}
	if j.pick.multi {
		var says []string
		for _, it := range j.pick.picked() {
			if it.desc != "" {
				says = append(says, it.desc)
			}
		}
		return strings.Join(says, ", ")
	}
	if it := j.pick.selected(); it != nil {
		return it.desc
	}
	return ""
}

// shellValues is vals as they go into the command line, shell-quoted where
// the arg says quote. Checklists stay as separate words.
func shellValues(c *config.Command, vals map[string]string) map[string]string {
	out := make(map[string]string, len(vals))
	for k, v := range vals {
		out[k] = v
	}
	for _, a := range c.Args {
		v := out[a.Name]
		switch k := a.Kind(); {
		case v == "":
		case k == "secret": // never shown: accept swaps in where it's kept
			out[a.Name] = secretMask
		case k == "choose" && !a.HasOption(v):
			out[a.Name] = shellValue(v) // typed in rather than picked
		case a.Quote:
			out[a.Name] = shellValue(v)
		}
	}
	return out
}

const secretMask = "••••••"

// unfinishedCommand is what ShellState.Typed says when the shell is
// waiting for the rest of a command rather than showing typed text.
const unfinishedCommand = "an unfinished command"

// The last sentence of a risky command's description.
const (
	warnNote   = warnGlyph + " Asks before running."
	dangerNote = dangerGlyph + " Dangerous: you type a code, then it asks before running."
)

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,~-]+$`) // ~ so `cd ~` still expands

// shellQuote makes s one shell word: as is when that's safe, otherwise in
// single quotes.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// homePath finds a ~ or ~user folder at the start of a path.
var homePath = regexp.MustCompile(`^~[A-Za-z0-9_.-]*/`)

// shellValue makes a value typed into a blank one shell word, like shellQuote,
// leaving a leading ~/ outside the quotes: in them, it wouldn't be your home.
func shellValue(s string) string {
	if !shellSafe.MatchString(s) {
		if home := homePath.FindString(s); home != "" {
			return home + shellQuote(s[len(home):])
		}
	}
	return shellQuote(s)
}

// spoken reads a value aloud in a sentence: "." is this folder.
func spoken(v string) string {
	switch v {
	case ".", "./", "$PWD":
		return "this folder"
	case "..", "../":
		return "the parent folder"
	case "../..":
		return "two folders up"
	case "~":
		return "your home folder"
	case "-":
		return "where you just were"
	}
	return v
}

// sentence capitalises s and ends it with a full stop.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	s = string(unicode.ToUpper(r)) + s[size:]
	if !strings.ContainsAny(s[len(s)-1:], ".!?") {
		s += "."
	}
	return s
}

// highlightedSection is the section on the cursor's row in the list, if
// that's what it is.
func (m *Model) highlightedSection() *config.Section {
	if m.mode != modeBrowse {
		return nil
	}
	if it := m.list.selected(); it != nil {
		s, _ := it.ref.(*config.Section)
		if s != nil && s == m.recipesSection() {
			return m.recipesListed(s)
		}
		return s
	}
	return nil
}

// sectionInfo is what's in a section, n lines w wide: any tools it's
// missing, then each group's commands on lines of their own.
func (m *Model) sectionInfo(s *config.Section, w, n int) string {
	var lines []string
	if miss := m.missingFor(s); len(miss) > 0 {
		lines = append(lines, wrapSpaces(words(warnStyle, upperFirst(config.Needs(miss))), w)...)
	}
	add := func(group string, cmds []config.Command) {
		var names []string
		for _, c := range cmds {
			names = append(names, c.Name)
		}
		line := words(dimStyle, strings.Join(names, ", "))
		if group != "" {
			line = words(crumbStyle, group+":") + " " + line
		}
		lines = append(lines, wrapSpaces(line, w)...)
	}
	if len(s.Commands) > 0 {
		group := ""
		if len(s.Groups) > 0 {
			group = "Commands"
		}
		add(group, s.Commands)
	}
	for _, g := range s.Groups {
		add(g.Name, g.Commands)
	}
	return fit(lines, w, n, false)
}

// words styles each word of s on its own, so it keeps its colour however
// it's wrapped.
func words(st lipgloss.Style, s string) string {
	f := strings.Split(s, " ")
	for i := range f {
		f[i] = st.Render(f[i])
	}
	return strings.Join(f, " ")
}

func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

func (m *Model) footer(w int) string {
	var keys []string
	if m.notice.text != "" || m.help {
		return accentStyle.Render("any key") + " " + dimStyle.Render("back")
	}
	switch m.mode {
	case modeBrowse:
		return "" // they're on the status bar
	case modeFill:
		j := m.job
		keys = []string{"→ next", "← back"}
		if j.lastStep() {
			keys[0] = "→ run"
		}
		switch {
		case j.usePick && j.pick.filtering:
			keys = append(keys, "/ stop filtering", "esc clear")
		case j.usePick && j.pick.filtered(): // the filter's own row says how
		case j.usePick:
			keys = append(keys, "/ filter")
		}
		if j.usePick && j.pick.nestedOnly() && !j.pick.filtering {
			keys = append(keys, "[ ] jump")
		}
		if j.usePick && j.pick.multi && !j.pick.filtering {
			keys = append([]string{"space tick"}, keys...)
		}
	case modeConfirm:
		keys = []string{"↑↓ choose", "enter select"}
		if p := m.pending; p.risk == config.RiskDanger && !p.typed {
			keys = []string{"type the code", "enter go on", "← back"}
		}
	case modeTrust, modeTyped, modeQuit, modeSave:
		keys = []string{"↑↓ choose", "enter select"}
	case modeSaveName:
		keys = []string{"→ add and run", "← back"}
	}
	return wrapJoin(keyHints(keys), faintStyle.Render(" · "), w)
}

// keyHints styles "key what it does": the key in pink, the rest in grey.
func keyHints(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		key, what, _ := strings.Cut(k, " ")
		if key == "[" { // "[ ] jump"
			key, what = "[ ]", strings.TrimPrefix(k, "[ ] ")
		}
		out[i] = accentStyle.Render(key) + " " + dimStyle.Render(what)
	}
	return out
}

// barKeys is what the status bar shows when it has nothing else to say: the
// keys that work right now. Prompts keep theirs in the menu, by the question.
func (m *Model) barKeys() []string {
	inClingon := m.opt.FocusMenu != nil // not in print mode: there's no shell pane
	if m.blurred {
		if inClingon {
			return []string{"shift-tab menu"}
		}
		return nil
	}
	if m.mode != modeBrowse || m.help || m.notice.text != "" {
		return nil
	}
	recipes := m.recipesSection() != nil
	keys := []string{"→ go"}
	if m.section != nil || m.list.filtered() {
		keys = append(keys, "← back")
	}
	switch {
	case m.list.filtering:
		keys = append(keys, "/ stop filtering", "esc clear filter")
	case m.list.filtered():
		keys = append(keys, "/ edit filter", "esc clear filter")
	default:
		keys = append(keys, "/ filter")
	}
	if !m.list.filtering {
		keys = append(keys, "[ ] jump")
	}
	if m.section == nil && !m.list.filtered() {
		keys = append(keys, "esc quit")
	}
	if !m.list.filtering && recipes {
		if m.section != nil && m.section != m.recipesSection() {
			keys = append(keys, "1-9 add")
		}
		keys = append(keys, "r recipe")
	}
	if inClingon {
		keys = append(keys, "tab shell")
	}
	return keys
}

// keysBar is the status bar with nothing else to say.
func (m *Model) keysBar(w int) string {
	line := strings.Join(keyHints(m.barKeys()), faintStyle.Render(" · "))
	return onBg(" "+ansi.Truncate(line, max(w-2, 0), "…"), raised, w)
}

// statusBar is the always-there line at the very bottom: the way to every key at
// its left, the version at its right, and whatever barBody has to say between
// them. The version gives way first if there's no room.
func (m *Model) statusBar(w int) string {
	chip := lipgloss.NewStyle().Background(accent).Foreground(ink).Bold(true).Padding(0, 1)
	notch := chip.Render("? help")
	nw := ansi.StringWidth(notch)
	if w <= nw {
		return ansi.Truncate(notch, max(w, 0), "")
	}
	version := ""
	if m.opt.Version != "" {
		version = chip.Render(m.opt.Version)
	}
	vw := ansi.StringWidth(version)
	if w-nw-vw < minBarBody {
		version, vw = "", 0
	}
	return notch + m.barBody(w-nw-vw) + version
}

// minBarBody is the least room the status bar's message gets before the version
// makes way for it.
const minBarBody = 40

// barBody is the status bar beside its notch: a send error or tip
// if there is one, what's stopping the shell taking a command, the recipe, the
// tools missing for what's on screen, news that needs acting on, and otherwise
// the keys.
func (m *Model) barBody(w int) string {
	switch {
	case m.errMsg != "": // only when a command couldn't be sent to the shell at all
		return bar(red, m.errMsg, w)
	case m.hint != "":
		return bar(raised, m.hint, w)
	case m.busy != "" && !m.quiet:
		return bar(orange, "◔ "+m.busy+" is running in the shell · tab to go there", w)
	case m.scrolling:
		return bar(orange, "‖ The shell is scrolled back · press q there to stop", w)
	case m.typed == unfinishedCommand && m.mode != modeTyped:
		return bar(orange, "✎ The shell is in the middle of a command · tab to finish it there", w)
	case m.typed != "" && m.mode != modeTyped:
		return bar(orange, "✎ Typed in the shell: "+m.typed+" · running a command asks first", w)
	}
	s := m.section
	switch {
	case m.pending != nil:
		s = m.pending.sec
	case m.job != nil:
		s = m.job.ref.sec
	}
	if len(m.recipe) > 0 { // wherever you've gone, you can see what you've numbered
		if s != nil {
			if miss := m.missingFor(s); len(miss) > 0 { // both: the recipe gets what's left of the bar
				msg := "✗ " + upperFirst(config.Needs(miss))
				left := min(ansi.StringWidth(msg)+2, w/2)
				return bar(red, msg, left) + m.recipeBar(w-left)
			}
		}
		return m.recipeBar(w)
	}
	if s == nil { // the top level: news, not problems
		// Only what needs doing. A suggestion is on the welcome card already.
		if m.news != nil && m.news.Kind == Attention {
			return newsBar(*m.news, w)
		}
		return m.keysBar(w)
	}
	if miss := m.missingFor(s); len(miss) > 0 {
		return bar(red, "✗ "+upperFirst(config.Needs(miss)), w)
	}
	return m.keysBar(w)
}

// upperFirst capitalises s, to start a sentence.
func upperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

// bar is text on a full-width band of colour: dark text on amber or red,
// light text on grey (all's well) or purple.
func bar(bg color.Color, msg string, w int) string {
	fg := ink
	switch bg {
	case raised:
		fg = grey
	case purple:
		fg = text
	}
	st := lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(bg != raised).Padding(0, 1).Width(w)
	return st.Render(ansi.Truncate(msg, max(w-2, 0), "…"))
}

// newsBar is the top level's news on the quiet band: only its glyph is
// coloured, so it reads as a note, not an alarm.
func newsBar(h Headline, w int) string {
	glyph, rest, _ := strings.Cut(h.Text, " ")
	st := lipgloss.NewStyle().Foreground(text)
	if h.Kind == Good {
		st = dimStyle
	}
	c := map[HeadlineKind]color.Color{Good: grey, Info: purple, Attention: accent}[h.Kind]
	if r, _ := utf8.DecodeRuneInString(glyph); r >= 0xE000 && r <= 0xF8FF {
		glyph += " " // a logo from clingon's font, drawn across two cells
	}
	msg := lipgloss.NewStyle().Foreground(c).Bold(true).Render(glyph) + " " + st.Render(rest)
	return onBg(" "+ansi.Truncate(msg, max(w-2, 0), "…"), raised, w)
}

func tildify(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// wrapJoin joins parts with sep, breaking lines between parts only.
func wrapJoin(parts []string, sep string, w int) string {
	var lines []string
	line := ""
	for _, p := range parts {
		switch {
		case line == "":
			line = p
		case ansi.StringWidth(line+sep+p) <= w:
			line += sep + p
		default:
			lines = append(lines, line)
			line = p
		}
	}
	return strings.Join(append(lines, line), "\n")
}

// inputs are the text inputs on screen: the list's filter, and whatever is
// being filled in or typed out.
func (m *Model) inputs() []*textinput.Model {
	ins := []*textinput.Model{&m.list.filter}
	// Only the ones this screen made: focusing one that's never been set up panics.
	switch {
	case m.mode == modeFill && m.job.usePick:
		ins = append(ins, &m.job.pick.filter)
	case m.mode == modeFill:
		ins = append(ins, &m.job.input)
	case m.mode == modeSaveName, m.mode == modeConfirm && m.pending != nil && m.pending.risk == config.RiskDanger && !m.pending.typed:
		ins = append(ins, &m.word)
	}
	return ins
}

// blurInputs hides the inputs' cursors while focus is in the shell, so the
// only pink cursor is the one that types. (The view does it again, for an
// input made since.)
// shownInputs are the inputs with a cursor on screen: all of them but a filter
// that's closed. Only these blink, and only while the menu has focus.
func (m *Model) shownInputs() []*textinput.Model {
	var out []*textinput.Model
	for _, in := range m.inputs() {
		closed := in == &m.list.filter && !m.list.filtering || m.mode == modeFill && m.job.usePick && in == &m.job.pick.filter && !m.job.pick.filtering
		if !closed {
			out = append(out, in)
		}
	}
	return out
}

func (m *Model) blurInputs() {
	for _, in := range m.inputs() {
		in.Blur()
	}
}

func (m *Model) browseView(w, h int) string {
	m.list.blurred = m.blurred
	return filterLine(&m.list, w) + "\n\n" + m.list.render(w, h-2)
}

// minForFilterRow is the least room a screen needs to give two rows to a filter
// it can't use: any less, and what it's asking comes first.
const minForFilterRow = 6

// minForFilterRowOnTrust is that for the trust screen, whose choices must stay on
// screen under a folder's path and a command, however long.
const minForFilterRowOnTrust = 16

// filterLine is the same at the top of every list: how to filter it, or the
// filter once / has opened it.
func filterLine(p *picker, w int) string {
	// The list's size sits at the far end, while there's room for it.
	tally := p.tally()
	tallyW := ansi.StringWidth(tally)
	if w < tallyW+minFilterW {
		tally, tallyW = "", 0
	}
	room := w
	if tally != "" {
		room = w - tallyW - 2
	}
	var left string
	switch {
	case p.filtering:
		left = inputView(&p.filter, room)
	case p.filtered(): // closed, and still applied: it should be plain that it is
		left = ansi.Truncate(accentStyle.Render("❯ ")+p.query()+faintStyle.Render("  / to edit · esc to clear"), max(room, 1), "…")
	default:
		left = accentStyle.Render("❯ /") + faintStyle.Render(" to filter")
	}
	if tally == "" {
		return left
	}
	return left + strings.Repeat(" ", max(1, w-ansi.StringWidth(left)-tallyW)) + faintStyle.Render(tally)
}

// minFilterW is the least room the filter's row keeps for the filter, when
// deciding if the list's size fits beside it.
const minFilterW = 16

// preview renders the command with filled values, the current arg (showing
// its tentative value) and the args still to come.
func preview(c *config.Command, vals map[string]string, step int, tentative string) string {
	index := map[string]int{}
	for i, a := range c.Args {
		index[a.Name] = i
	}
	var b strings.Builder
	h := newHighlighter()
	for _, s := range tmpl.Parse(c.Cmd, c.Known) {
		i := index[s.Arg]
		if s.Arg != "" {
			h.placeholder()
		}
		switch {
		case s.Arg == "":
			b.WriteString(h.text(s.Text))
		case step >= 0 && i < step:
			b.WriteString(valueStyle.Render(shellValues(c, vals)[s.Arg]))
		case i == step && tentative != "":
			b.WriteString(pendingStyle.Render(shellValues(c, map[string]string{s.Arg: tentative})[s.Arg]))
		case i == step:
			b.WriteString(pendingStyle.Render("<" + s.Arg + ">"))
		default:
			b.WriteString(accentStyle.Faint(true).Render("<" + s.Arg + ">"))
		}
	}
	return b.String()
}

func (m *Model) fillView(w, h int) string {
	j := m.job
	a := m.arg()
	inner := panelWidth(w)
	step := fmt.Sprintf("Step %d of %d", j.step+1, len(j.ref.cmd.Args))
	if r := m.recipeStep(); r != "" {
		step = r + " · " + step
	}
	label := cursorStyle.Render(a.Label()) + dimStyle.Italic(true).Render(" ("+inputHint(a)+")")
	if a.Optional {
		label += dimStyle.Render(" · optional")
	}
	// How far along stands alone, like a list's filter; the blank and its answer
	// go together.
	head := []string{dimStyle.Render(ansi.Truncate(step, inner, "…")), "", label}
	room := h
	if !j.usePick {
		room += 2 // the filter's row is above, out of what it was given: the same either way
	}
	if room < 8 { // no room: the blank and how far along, on one line
		head = []string{ansi.Truncate(label+dimStyle.Render("  "+step), inner, "…")}
	}
	if j.usePick {
		j.pick.blurred = m.blurred
		// The filter's where it is on every list. A list needs no panel: its
		// highlighted row already looks like one.
		used := len(head) + 2
		return strings.Join(append(append([]string{filterLine(&j.pick, w), ""}, head...), j.pick.render(w, h-used)), "\n")
	}
	lines := append(head, panel([]string{inputView(&j.input, inner)}, w))
	if j.invalid != "" {
		lines = append(lines, errStyle.Render(ansi.Wrap(j.invalid, w, "")))
	}
	return strings.Join(lines, "\n")
}

// inputHint says how to answer a blank.
func inputHint(a config.Arg) string {
	switch a.Kind() {
	case "choose":
		return "↑↓ to choose"
	case "flags":
		return "space to tick"
	}
	return "type"
}

// recipeStep says which command of the recipe this is, if it's part of one.
func (m *Model) recipeStep() string {
	if m.run == nil {
		return ""
	}
	return fmt.Sprintf("Recipe command %d of %d", m.run.at+1, len(m.recipe))
}

// panelWidth is the room for what's in a panel, inside its edge and padding.
func panelWidth(w int) int { return max(w-3, 1) }

// panel marks where your answer goes, the same wherever that is: a grey block
// with a pink edge. What's being asked stays outside it. Long lines wrap: nothing
// in it may be cut off.
func panel(lines []string, w int) string {
	inner := panelWidth(w)
	var out []string
	for _, line := range lines {
		for _, l := range strings.Split(ansi.Wrap(line, inner, ""), "\n") {
			out = append(out, accentStyle.Render("▌")+onBg(" "+l, raised, max(w-1, 1)))
		}
	}
	return strings.Join(out, "\n")
}

// saveView asks whether to save the recipe that's about to run.
func (m *Model) saveView(w int) string {
	row := func(on bool, s string) string {
		if on {
			return cursorStyle.Render("› " + s)
		}
		return "  " + s
	}
	return cursorStyle.Render("Add this recipe first?") + "\n\n" + panel([]string{row(!m.yes, "No, just run it"), row(m.yes, "Yes, add it and run it")}, w)
}

// saveNameView asks what to call it.
func (m *Model) saveNameView(w int) string {
	lines := []string{cursorStyle.Render("Name this recipe"), panel([]string{inputView(&m.word, panelWidth(w))}, w)}
	if m.nameWhy != "" {
		lines = append(lines, errStyle.Render(ansi.Wrap(m.nameWhy, w, "")))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) noticeView(w int) string {
	return lipgloss.NewStyle().Width(w).Render(warnStyle.Bold(true).Render(warnGlyph+" "+m.notice.title) + "\n" +
		m.notice.text + "\n\n" + dimStyle.Render(fmt.Sprintf("Back in %ds · any key to go back now", m.notice.left)))
}

// confirmView asks about a risky command, under its name, room allowing:
// the question and its answers matter more.
func (m *Model) confirmView(w, h int) string {
	body := m.confirmBody(w)
	if r := m.recipeStep(); r != "" && strings.Count(body, "\n")+3 <= h {
		body = dimStyle.Render(r) + "\n\n" + body
	}
	return body
}

func (m *Model) confirmBody(w int) string {
	row := func(on bool, s string) string {
		if on {
			return cursorStyle.Render("› " + s)
		}
		return "  " + s
	}
	p := m.pending
	if p.risk == config.RiskDanger {
		if !p.typed {
			out := ansi.Wrap(highlight(p.shown), w, "") + "\n\n" +
				ansi.Wrap(dangerStyle.Render(dangerGlyph)+" It can't be undone. Type "+dangerStyle.Render(p.code)+" to go on:", w, "") + "\n\n" + panel([]string{inputView(&m.word, panelWidth(w))}, w)
			if m.wrong {
				out += "\n" + errStyle.Render("That wasn't the code: here's a new one. Or esc to go back.")
			}
			return out
		}
		return dangerStyle.Render(dangerGlyph+" Last check: run it?") + "\n\n" +
			panel([]string{row(!m.yes, "No, go back"), row(m.yes, "Yes, run it")}, w)
	}
	return warnStyle.Bold(true).Render(warnGlyph+" This command is risky. Run it?") + "\n\n" +
		panel([]string{row(!m.yes, "No, go back"), row(m.yes, "Yes, run it")}, w)
}

// newCode is the code you type before a risk: danger command runs: four
// lowercase letters, new every time so it can't become habit. Letters that
// read alike (i, l, o) are left out.
func newCode() string {
	const letters = "abcdefghjkmnpqrstuvwxyz"
	b := make([]byte, 4)
	for i := range b {
		b[i] = letters[rand.IntN(len(letters))]
	}
	return string(b)
}
