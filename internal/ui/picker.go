package ui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/con-dog/clingon/internal/config"
)

type item struct {
	title    string
	icon     string // section glyph, drawn in a two-column slot
	color    string // the icon's colour (#rrggbb); grey if unset
	desc     string // shown in the info pane when highlighted; also searched
	keywords string // extra text matched by the filter
	value    string
	header   bool
	accent   bool   // header in the accent colour (clingon's own section)
	rule     bool   // header drawn as a divider line
	risk     string // config.RiskWarn or RiskDanger: drawn with a mark (danger in red)
	where    string // a search result's section › group, drawn faint after it
	disabled bool   // drawn greyed out; can be highlighted but not opened
	missing  string // a section's tools that aren't installed: drawn dim, naming them, but it opens as usual
	indent   bool   // sits under the row above it
	stepAt   int    // the one recipe position this row is, in the recipe's own list
	ref      any
	idx      int
}

// picker is a list with a filter / opens, single or multi select. With search set,
// a non-empty filter matches against search instead of items (the root menu
// lists sections but searches every command).
type picker struct {
	items   []item
	search  []item
	view    []item
	multi   bool
	chosen  map[int]bool
	iconW   int     // widest icon among items, so names line up
	risky   [2]bool // some row has a risk mark, in items [0] and search [1]: every row keeps a slot for one, so names line up
	cursor  int
	blurred bool             // focus is elsewhere: the cursor's row in grey, not pink
	step    func(item) []int // a command's places in the recipe, ascending, if this list shows them
	offset  int
	height  int
	noun    string // what the list counts, on the filter's row: "" for no count
	filter  textinput.Model
	// filtering: / has opened its filter, so keys type into it. / again closes it
	// with what's been typed still applied (filtered), to use the list as it is.
	filtering bool
}

func newPicker(items, search []item, multi bool) picker {
	iconW := 0
	for i := range items {
		items[i].idx = i
		iconW = max(iconW, ansi.StringWidth(items[i].icon))
	}
	if iconW > 0 {
		iconW = 2 // logos are drawn two cells wide: every icon gets that slot
	}
	f := newInput(accentStyle, "type to filter") // focused when / opens it
	hasRisk := func(it item) bool { return it.risk != "" }
	risky := [2]bool{slices.ContainsFunc(items, hasRisk), slices.ContainsFunc(search, hasRisk)}
	p := picker{items: items, search: search, multi: multi, chosen: map[int]bool{}, filter: f, height: 10, iconW: iconW, risky: risky}
	p.refilter()
	return p
}

// searching is 1 when the view is search results rather than the items.
func (p *picker) searching() int {
	if p.search != nil && p.query() != "" {
		return 1
	}
	return 0
}

func (p *picker) query() string { return strings.TrimSpace(p.filter.Value()) }

// filtered says whether a filter is open or still being applied: esc has it to clear.
func (p *picker) filtered() bool { return p.filtering || p.filter.Value() != "" }

// stopFiltering closes the filter and leaves what's been typed applied.
func (p *picker) stopFiltering() {
	p.filtering = false
	p.filter.Blur() // its cursor's gone: nothing to keep blinking
	if p.query() == "" {
		p.filter.SetValue("")
	}
}

func (p *picker) clearFilter() {
	p.filtering = false
	p.filter.Blur()
	p.filter.SetValue("")
	p.refilter()
}

func (p *picker) refilter() {
	q := strings.ToLower(p.query())
	src := p.items
	if q != "" && p.search != nil {
		src = p.search
	}
	p.view = p.view[:0]
	if q == "" {
		p.view = append(p.view, src...)
	} else {
		type hit struct {
			it    item
			score int
		}
		var hits []hit
		for _, it := range src {
			if it.header {
				continue
			}
			s := 2 * score(q, strings.ToLower(it.title))
			if k := score(q, strings.ToLower(it.title+" "+it.desc+" "+it.keywords)); k > s {
				s = k
			}
			if s > 0 {
				hits = append(hits, hit{it, s})
			}
		}
		// Best first, and what can be used here before what's greyed out.
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].it.disabled != hits[j].it.disabled {
				return !hits[i].it.disabled
			}
			return hits[i].score > hits[j].score
		})
		if isTree(src) {
			// A tree keeps its order, so nothing sorts a loose match to the bottom:
			// what's typed has to be there. Only if nothing has it do letters in the
			// right order count, and then only in a row's name: awsec for aws/ecs.
			exact := slices.DeleteFunc(slices.Clone(hits), func(h hit) bool { return !containsWords(h.it, q) })
			if len(exact) == 0 {
				exact = slices.DeleteFunc(slices.Clone(hits), func(h hit) bool { return score(q, strings.ToLower(h.it.title)) <= 0 })
			}
			hits = exact
			var matched []item
			for _, h := range hits {
				matched = append(matched, h.it)
			}
			p.view = treeOf(src, matched)
			p.cursor, p.offset = -1, 0
			if len(hits) > 0 { // start on the best match, wherever the tree puts it
				p.cursor = slices.IndexFunc(p.view, func(it item) bool { return it.idx == hits[0].it.idx })
			}
			return
		}
		for _, h := range hits {
			p.view = append(p.view, h.it)
		}
	}
	p.cursor, p.offset = -1, 0
	p.move(1)
	// Start on the first row that can be used, if there is one.
	for i := p.cursor; i >= 0 && i < len(p.view); i++ {
		if p.selectable(i) && !p.view[i].disabled {
			p.cursor = i
			break
		}
	}
}

// containsWords says whether every word of a lowercase query is in the row.
func containsWords(it item, q string) bool {
	text := strings.ToLower(it.title + " " + it.desc + " " + it.keywords)
	for _, w := range strings.Fields(q) {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// isTree says whether a list's indented rows belong to the row above them, rather
// than to a heading.
func isTree(items []item) bool {
	tree := false
	for _, it := range items {
		if it.header {
			return false
		}
		tree = tree || it.indent
	}
	return tree
}

// treeOf is a tree's rows for a filter: the ones that matched in their own order,
// each under its real parent, there even if it didn't match itself. Indentation
// says what belongs to what, so a filter can't reorder it.
func treeOf(items, matched []item) []item {
	keep := map[int]bool{}
	for _, it := range matched {
		keep[it.idx] = true
	}
	parent := -1
	var out []item
	for i, it := range items {
		if !it.indent {
			parent = i
			hasChild := false
			for _, c := range items[i+1:] {
				if !c.indent {
					break
				}
				hasChild = hasChild || keep[c.idx]
			}
			if keep[it.idx] || hasChild {
				out = append(out, it)
			}
			continue
		}
		if keep[it.idx] && parent >= 0 {
			out = append(out, it)
		}
	}
	return out
}

// score ranks substring matches (earlier and at word starts is better)
// above in-order subsequence matches; 0 means no match.
func score(q, s string) int {
	if i := strings.Index(s, q); i >= 0 {
		sc := 2000 - i
		if i == 0 || s[i-1] == ' ' {
			sc += 1000
		}
		return sc
	}
	j, last, gaps := 0, -1, 0
	for i := 0; i < len(s) && j < len(q); i++ {
		if q[j] == ' ' {
			j++
		}
		if j < len(q) && s[i] == q[j] {
			if last >= 0 {
				gaps += i - last - 1
			}
			last = i
			j++
		}
	}
	if j < len(q) {
		return 0
	}
	return max(1, 1000-gaps)
}

// selectable reports whether the cursor can land on row i: any but a
// heading. Greyed out rows too, so the info pane can say why they are.
func (p *picker) selectable(i int) bool { return !p.view[i].header }

// move steps the cursor delta rows, skipping headings.
func (p *picker) move(delta int) {
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	for ; delta > 0; delta-- {
		next := p.cursor + step
		for next >= 0 && next < len(p.view) && !p.selectable(next) {
			next += step
		}
		if next < 0 || next >= len(p.view) {
			break
		}
		p.cursor = next
	}
	if p.cursor < 0 || p.cursor >= len(p.view) {
		p.cursor = -1
	}
}

// nestedOnly says whether a list's sections are parents with children under them,
// rather than headings: a list of packs and their subpacks.
func (p *picker) nestedOnly() bool {
	nested := false
	for _, it := range p.view {
		if it.header {
			return false
		}
		nested = nested || it.indent
	}
	return nested
}

// jump moves to the first item under the next (dir 1) or current/previous
// (dir -1) heading. With no headings it goes to the top or bottom.
func (p *picker) jump(dir int) {
	var starts []int // the first usable row under each heading
	under := true    // no usable row found yet under the current heading
	for i := range p.view {
		if p.view[i].header {
			under = true
		} else if under && p.selectable(i) {
			starts, under = append(starts, i), false
		}
	}
	if p.nestedOnly() { // no headings: each parent, with its children under it, is one
		starts = nil
		for i := range p.view {
			if !p.view[i].indent {
				starts = append(starts, i)
			}
		}
	}
	if len(starts) <= 1 {
		p.cursor = -1
		if dir > 0 {
			p.cursor = len(p.view)
			p.move(-1)
		} else {
			p.move(1)
		}
		return
	}
	target := starts[0]
	if dir > 0 {
		target = starts[len(starts)-1]
		for _, s := range starts {
			if s > p.cursor {
				target = s
				break
			}
		}
	} else {
		for _, s := range starts {
			if s < p.cursor {
				target = s
			}
		}
	}
	p.cursor = target
}

func (p *picker) selectValue(v string) {
	for i, it := range p.view {
		if p.selectable(i) && it.value == v {
			p.cursor = i
		}
	}
}

func (p *picker) selected() *item {
	if p.cursor < 0 || p.cursor >= len(p.view) {
		return nil
	}
	return &p.view[p.cursor]
}

// toggle is space on an option. A filter narrows a parent to the children it's
// showing: ticking one ticks just those, and the parent only once that's all of them.
func (p *picker) toggle(idx int) {
	shown := p.shownChildrenOf(idx)
	if p.query() == "" || p.items[idx].indent || len(shown) == 0 {
		p.tick(idx, !p.chosen[idx]) // a parent showing none matched by itself: all of it
		return
	}
	on := !p.allChosen(shown)
	for _, c := range shown {
		p.chosen[c] = on
	}
	p.chosen[idx] = p.allChosen(p.childrenOf(idx))
}

// shownChildrenOf lists an option's children that the filter is showing.
func (p *picker) shownChildrenOf(idx int) []int {
	var out []int
	for _, c := range p.childrenOf(idx) {
		if slices.ContainsFunc(p.view, func(it item) bool { return it.idx == c }) {
			out = append(out, c)
		}
	}
	return out
}

// tick sets an option's tick. A parent takes its indented children with it, and
// a parent is ticked exactly when all of its children are.
func (p *picker) tick(idx int, on bool) {
	p.chosen[idx] = on
	if !p.items[idx].indent {
		for _, c := range p.childrenOf(idx) {
			p.chosen[c] = on
		}
		return
	}
	parent := idx
	for parent > 0 && p.items[parent].indent {
		parent--
	}
	if !p.items[parent].indent {
		p.chosen[parent] = p.allChosen(p.childrenOf(parent))
	}
}

// childrenOf lists the indented options under an option.
func (p *picker) childrenOf(idx int) []int {
	var out []int
	for i := idx + 1; i < len(p.items) && p.items[i].indent; i++ {
		out = append(out, i)
	}
	return out
}

func (p *picker) allChosen(idxs []int) bool {
	for _, i := range idxs {
		if !p.chosen[i] {
			return false
		}
	}
	return len(idxs) > 0
}

// partly says whether some of an option's children are ticked, but not all.
func (p *picker) partly(idx int) bool {
	if p.items[idx].indent || p.chosen[idx] {
		return false
	}
	for _, c := range p.childrenOf(idx) {
		if p.chosen[c] {
			return true
		}
	}
	return false
}

// picked lists the ticked options that count: a ticked parent stands for its
// children, so asking for the whole of something says so once.
func (p *picker) picked() []item {
	var out []item
	covered := false
	for _, it := range p.items {
		if !it.indent {
			covered = p.chosen[it.idx]
		}
		if p.chosen[it.idx] && !(it.indent && covered) {
			out = append(out, it)
		}
	}
	return out
}

func (p *picker) values() []string {
	var out []string
	for _, it := range p.picked() {
		out = append(out, it.value)
	}
	return out
}

func (p *picker) update(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "up", "ctrl+p", "ctrl+k":
		p.move(-1)
		return nil
	case "down", "ctrl+n", "ctrl+j":
		p.move(1)
		return nil
	// [ and ] jump between headings in every terminal; Shift+↑/↓ too, in
	// terminals that send it (Terminal.app sends them as plain ↑/↓).
	case "[", "shift+up":
		if !p.filtering { // while it's open, the search box gets every key that's text
			p.jump(-1)
			return nil
		}
	case "]", "shift+down":
		if !p.filtering {
			p.jump(1)
			return nil
		}
	case "pgup":
		p.move(-p.height)
		return nil
	case "pgdown":
		p.move(p.height)
		return nil
	case "space":
		if p.multi && !p.filtering {
			if it := p.selected(); it != nil {
				p.toggle(it.idx)
			}
			return nil
		}
	}
	if !p.filtering {
		return nil // only / opens the filter, in every list
	}
	before := p.filter.Value()
	cmd := typeInto(&p.filter, msg)
	if p.filter.Value() != before {
		p.refilter()
	}
	return cmd
}

func (p *picker) render(width, height int) string {
	height = max(1, height)
	// A list that doesn't fit gives its last row to saying so.
	overflows := len(p.view) > height && height >= minForMoreRow
	if overflows {
		height--
	}
	p.height = height
	if len(p.view) == 0 {
		return dimStyle.Render("  no matches")
	}
	if p.cursor >= 0 {
		if p.cursor < p.offset {
			p.offset = p.cursor
		}
		if p.cursor >= p.offset+p.height {
			p.offset = p.cursor - p.height + 1
		}
		// keep a group's header visible when its first item is on top
		if p.height > 1 && p.offset > 0 && p.offset == p.cursor && p.view[p.offset-1].header {
			p.offset--
		}
	}
	rows := p.height
	var lines []string
	if s := p.sticky(); s >= 0 {
		if p.cursor >= p.offset+rows-1 { // the pinned heading takes a row
			p.offset++
		}
		if s = p.sticky(); s >= 0 {
			lines, rows = append(lines, p.row(p.view[s], false, width)), rows-1
		}
	}
	end := min(len(p.view), p.offset+rows)
	for i := p.offset; i < end; i++ {
		lines = append(lines, p.row(p.view[i], i == p.cursor, width))
	}
	if overflows {
		lines = append(lines, p.moreRow(end))
	}
	return strings.Join(lines, "\n")
}

// minForMoreRow is the least room a list needs to give a row to saying there's
// more: any less, and rows of the list come first.
const minForMoreRow = 3

// moreRow says what's off-screen: what's below, or once there's nothing
// below, what's above.
func (p *picker) moreRow(end int) string {
	if n := rowCount(p.view[end:]); n > 0 {
		return faintStyle.Render(fmt.Sprintf("  ↓ %d more", n))
	}
	if n := rowCount(p.view[:min(p.offset, len(p.view))]); n > 0 {
		return faintStyle.Render(fmt.Sprintf("  ↑ %d more", n))
	}
	return ""
}

// rowCount is how many rows there are, leaving out headings.
func rowCount(items []item) int {
	n := 0
	for i := range items {
		if !items[i].header {
			n++
		}
	}
	return n
}

// tally is the list's size for the filter's row, in its noun: "34 commands",
// or "12 of 34 commands" while a filter is applied. Rows that aren't what the
// noun names, like a recipe's Run recipe, don't count.
func (p *picker) tally() string {
	if p.noun == "" {
		return ""
	}
	count := func(items []item) int {
		n := 0
		for i := range items {
			if _, isCmd := items[i].ref.(cmdRef); !items[i].header && (isCmd || p.noun != "command") {
				n++
			}
		}
		return n
	}
	pool := p.items
	if p.search != nil {
		pool = p.search
	}
	total := count(pool)
	if total == 0 {
		return ""
	}
	if p.query() != "" {
		return fmt.Sprintf("%d of %s", count(p.view), plural(total, p.noun))
	}
	return plural(total, p.noun)
}

// sticky is the heading of the group scrolled into, pinned above its
// rows so you can still see whose they are: -1 if the top row is a
// heading anyway, or a divider is the nearest one.
func (p *picker) sticky() int {
	if p.height < 2 || p.offset <= 0 || p.offset >= len(p.view) || p.view[p.offset].header {
		return -1
	}
	for i := p.offset - 1; i >= 0; i-- {
		if p.view[i].header {
			if p.view[i].rule {
				return -1
			}
			return i
		}
	}
	return -1
}

// stepW is the width of a recipe number's badge.
const stepW = 3

// stepBadge is a recipe number, marked the same wherever it shows.
func stepBadge(n int) string { return stepStyle.Render(" " + strconv.Itoa(n) + " ") }

// minNameW is the room a row keeps for its name, however many badges it has.
const minNameW = 12

// stepBadges draws a row's numbers in the order given, and how wide they are:
// at least one badge's width, so empty rows share the column. What won't fit in
// room is left off, with an ellipsis in its place.
func stepBadges(steps []int, room int) (string, int) {
	if len(steps) == 0 {
		return "", stepW
	}
	const ellipsis = 2 // a space and …
	width := func(n int) int { return n*(stepW+1) - 1 }
	shown := len(steps)
	if width(shown) > room {
		for shown > 1 && width(shown)+ellipsis > room {
			shown--
		}
	}
	parts := make([]string, shown)
	for i, n := range steps[:shown] {
		parts[i] = stepBadge(n)
	}
	line, w := strings.Join(parts, " "), width(shown)
	if shown < len(steps) {
		line += faintStyle.Render(" …")
		w += ellipsis
	}
	return line, w
}

// StepBadge is a step's number as the menu shows it, for clingon's other screens.
func StepBadge(n int) string { return stepBadge(n) }

// CommandBlock is a command as the Command area shows it, w cells wide.
func CommandBlock(c *config.Command, w int) []string {
	lines := wrapSpaces(preview(c, nil, -1, ""), w)
	for i, l := range lines {
		lines[i] = onBg(l, raised, w)
	}
	return lines
}

func (p *picker) row(it item, active bool, width int) string {
	if it.rule {
		return faintStyle.Render(strings.Repeat("─", width))
	}
	if it.header {
		st := headerStyle
		if it.accent {
			st = cursorStyle
		}
		return st.Render(ansi.Truncate(strings.ToUpper(it.title), width, "…"))
	}
	// Commands keep the last cells for their number's badge, set or not, so
	// badges share a column and a name doesn't change where it's cut. More
	// badges, for a command that's more than one step, grow to the left.
	full := width
	_, isCmd := it.ref.(cmdRef)
	numbered := isCmd && p.step != nil
	var badges string
	badgesW := stepW
	if numbered {
		badges, badgesW = stepBadges(p.step(it), width-minNameW)
		width -= badgesW + 2 // a cell either side, off the name and the frame
	}
	cur := cursorStyle
	if p.blurred {
		cur = labelStyle
	}
	prefix := "  "
	if active {
		prefix = cur.Render("▌ ")
	}
	if it.indent {
		prefix += "  "
	}
	if p.multi {
		if p.chosen[it.idx] {
			prefix += accentStyle.Render("✓ ")
		} else if p.partly(it.idx) {
			prefix += accentStyle.Render("− ")
		} else {
			prefix += faintStyle.Render("• ")
		}
	}
	switch {
	case it.risk != "" && it.disabled: // greyed out, mark and all
		prefix += faintStyle.Render(map[string]string{config.RiskWarn: warnGlyph, config.RiskDanger: dangerGlyph}[it.risk] + " ")
	case it.risk == config.RiskWarn:
		prefix += warnStyle.Render(warnGlyph + " ")
	case it.risk == config.RiskDanger:
		prefix += dangerStyle.Render(dangerGlyph + " ")
	default:
		if p.risky[p.searching()] {
			prefix += "  "
		}
	}
	if it.icon != "" {
		prefix += iconStyle(it.color).Render(it.icon) + strings.Repeat(" ", max(0, p.iconW-ansi.StringWidth(it.icon))) + " "
	}
	// Descriptions live in the info pane, never inline.
	title := ansi.Truncate(it.title, width-ansi.StringWidth(prefix), "…")
	switch {
	case it.disabled:
		title = faintStyle.Render(title)
	case it.risk == config.RiskDanger: // the whole row in red, highlighted or not
		title = dangerStyle.Render(title)
	case active && p.blurred: // the bar dims with focus; the name stays readable
	case active, it.accent:
		title = cur.Render(title)
	case badges != "": // ties the name to its badges, across the gap
		title = valueStyle.Render(title)
	case it.missing != "":
		title = dimStyle.Render(title)
	}
	if room := width - ansi.StringWidth(prefix) - ansi.StringWidth(it.title) - 9; it.missing != "" && room > 4 {
		title += faintStyle.Render(" · needs " + ansi.Truncate(it.missing, room, "…"))
	}
	if room := width - ansi.StringWidth(prefix) - ansi.StringWidth(it.title) - 3; it.where != "" && room > 4 {
		title += dimStyle.Render(" · " + ansi.Truncate(it.where, room, "…"))
	}
	line := prefix + title
	if badges != "" {
		line += strings.Repeat(" ", max(0, full-badgesW-1-ansi.StringWidth(line))) + badges + " "
	}
	if active { // the cursor's row is a bar across the list
		return onBg(line, raised, full)
	}
	return line
}

// keyName is msg's key name, or "" for a burst of typed text. Fast typing
// (and tmux) can deliver "end" or "delete" as one message whose name would
// otherwise collide with the End or Delete key.
func keyName(msg tea.KeyPressMsg) string {
	if utf8.RuneCountInString(msg.Text) > 1 {
		return ""
	}
	return msg.String()
}

// typed is text arriving as one key message: a burst of typing, or a paste.
func typed(text string) tea.KeyPressMsg {
	if r := []rune(text); len(r) == 1 {
		return tea.KeyPressMsg{Code: r[0], Text: text}
	}
	return tea.KeyPressMsg{Code: tea.KeyExtended, Text: text}
}

// typeInto feeds msg to a text input, a rune at a time for bursts.
func typeInto(in *textinput.Model, msg tea.KeyPressMsg) tea.Cmd {
	if keyName(msg) != "" {
		var cmd tea.Cmd
		*in, cmd = in.Update(msg)
		return cmd
	}
	var cmds []tea.Cmd
	for _, r := range msg.Text {
		var cmd tea.Cmd
		*in, cmd = in.Update(typed(string(r)))
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// inputView draws a text input w cells wide. (Without a width, Bubbles
// cuts its placeholder to one letter.)
func inputView(in *textinput.Model, w int) string {
	in.SetWidth(max(w-4, 8)) // the prompt, and the cursor past the end
	return in.View()
}

// newInput is a text input in clingon's look: a ❯ prompt and cursor in st,
// a faint placeholder, and a blinking cursor.
func newInput(st lipgloss.Style, placeholder string) textinput.Model {
	in := textinput.New()
	in.Prompt = "❯ "
	s := textinput.DefaultStyles(true)
	s.Focused.Prompt, s.Blurred.Prompt = st, st
	s.Focused.Placeholder, s.Blurred.Placeholder = faintStyle, faintStyle
	s.Cursor.Color = st.GetForeground()
	s.Cursor.Blink = true
	in.SetStyles(s)
	in.Placeholder = placeholder
	return in
}
