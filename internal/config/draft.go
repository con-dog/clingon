package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A draft is the pack form's work, kept when you leave the form until the
// menu has asked whether to save it. There's one at a time: the form writes
// it, the menu saves it or throws it away, and a form opened on the same pack
// meanwhile carries on from it.
type draft struct {
	Pack EditablePack
	From string // where the pack was loaded from; "" for a new one
}

// DraftFile is where the pack form's draft is kept.
func DraftFile() string { return filepath.Join(Dir(), "pack-draft.json") }

// NewEditable is a new pack, not yet saved anywhere: one module with one
// command, every name and command empty for the form's greyed hints to
// show what goes there. There's nothing of an example's to clear first.
func NewEditable() (*EditablePack, error) {
	p := &EditablePack{Section: "other", Logo: DefaultPackLogo, Color: "grey", Files: []EditableFile{{Slug: "commands", Group: Group{Commands: []Command{{}}}}}}
	p.Folder = p.NewFolder()
	return p, nil
}

// folderTaken says whether you have a pack in a folder called name.
func folderTaken(name string) bool { return exists(packDirIn(localDir(), name)) }

// slugOf is a pack name made a folder name: New pack → new-pack.
func slugOf(title string) string {
	s := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if s == "" {
		return "pack"
	}
	return s
}

// NewFolder is a folder name for a new pack, from its name, not already
// taken by one of yours.
func (p *EditablePack) NewFolder() string {
	base := slugOf(p.Title)
	name := base
	for n := 2; folderTaken(name); n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	return name
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsNew says whether the pack has never been saved.
func (p *EditablePack) IsNew() bool { return p.dir == "" }

// WriteDraft keeps the pack as the form's draft.
func (p *EditablePack) WriteDraft() error {
	data, err := json.Marshal(draft{Pack: *p, From: p.dir})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return writeWhole(DraftFile(), data)
}

// ReadDraft is the form's draft, or nil if there isn't one.
func ReadDraft() (*EditablePack, error) {
	data, err := os.ReadFile(DraftFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d draft
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("the pack form's draft (%s) can't be read: %w", DraftFile(), err)
	}
	d.Pack.dir = d.From
	return &d.Pack, nil
}

// RemoveDraft throws the form's draft away.
func RemoveDraft() error {
	if err := os.Remove(DraftFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// SameAs says whether the pack is draft d: a draft of the same pack (or, for
// a new pack, d is new too).
func (p *EditablePack) SameAs(d *EditablePack) bool {
	return d != nil && d.dir == p.dir
}

// Changed says whether the pack differs from what's saved: a new pack always
// does.
func (p *EditablePack) Changed() bool {
	if p.IsNew() {
		return true
	}
	saved, err := LoadEditable(filepath.Base(p.dir))
	if err != nil {
		return true
	}
	have, want := saved.Markdown(), p.Markdown()
	if len(have) != len(want) {
		return true
	}
	for name, text := range want {
		if have[name] != text {
			return true
		}
	}
	return false
}

// formNameFile holds the name of the pack the open form is editing, as it
// changes, for the menu beside it to say what's being edited.
func formNameFile() string { return filepath.Join(Dir(), "pack-form.name") }

// WriteFormName keeps the name of the pack the form is editing, and the
// menu heading it's under.
func WriteFormName(name, place string, modules, commands int) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return writeWhole(formNameFile(), []byte(fmt.Sprintf("%s\n%s\n%d %d", name, place, modules, commands)))
}

// FormName is the name of the pack the open form is editing, or "", and the
// menu heading it's under.
func FormName() (name, place string) {
	data, _ := os.ReadFile(formNameFile())
	lines := strings.SplitN(string(data), "\n", 3)
	for len(lines) < 2 {
		lines = append(lines, "")
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
}

// FormCounts is how many modules and commands the pack in the open form has,
// as it stands; ok is false with no form open.
func FormCounts() (modules, commands int, ok bool) {
	data, _ := os.ReadFile(formNameFile())
	lines := strings.SplitN(string(data), "\n", 3)
	if len(lines) < 3 {
		return 0, 0, false
	}
	n, _ := fmt.Sscanf(lines[2], "%d %d", &modules, &commands)
	return modules, commands, n == 2
}

// RemoveFormName forgets it, and its keys: the form's closed.
func RemoveFormName() {
	_ = os.Remove(formNameFile())
	_ = os.Remove(formKeysFile())
}

// formKeysFile holds the keys that work in the open form right now, for the
// status bar, which the menu draws, to say.
func formKeysFile() string { return filepath.Join(Dir(), "pack-form.keys") }

// WriteFormKeys keeps the keys that work in the form now, one a line.
func WriteFormKeys(keys []string) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return writeWhole(formKeysFile(), []byte(strings.Join(keys, "\n")))
}

// FormIssuePrefix starts the line of the form's keys that's why a change was
// refused, while it was, for the bar to say instead of the keys.
const FormIssuePrefix = "! "

// ChangedFile is where tmux and clingon's shell say something the menu shows
// has changed (focus moved, the shell scrolled or back at its prompt): each
// appends a byte, and the menu, watching it, wakes. $CLINGON_CHANGED in
// clingon's window.
func ChangedFile() string { return filepath.Join(Dir(), "changed") }

// ReadyFile is where clingon's shell says its prompt is up and waiting for a
// line: it appends a byte each time ($CLINGON_READY, in its rc files). How
// many bytes it holds is how many prompts there have been, so "the command
// just sent has finished" is "it's grown since": heard, not looked for.
func ReadyFile() string { return filepath.Join(Dir(), "ready") }

// WatchChanged starts watching ChangedFile, made first if it isn't there,
// and empties it when it's grown: it's only ever written to wake the menu.
func WatchChanged() (*Watch, error) {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(ChangedFile(), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err == nil && st.Size() > 1<<16 {
		_ = f.Truncate(0)
	}
	_ = f.Close()
	return WatchPath(ChangedFile())
}

// FormStatePrefix starts the line of the form's keys that says what your keys
// are doing in it now (typing into a field, or the filter), for clingon's AI,
// which sees the form as text, without the text cursor. The bar doesn't say it.
const FormStatePrefix = "@ "

// FormKeys are the keys that work in the open form now, or none.
func FormKeys() []string {
	var keys []string
	for _, l := range formKeyLines() {
		if !strings.HasPrefix(l, FormStatePrefix) {
			keys = append(keys, l)
		}
	}
	return keys
}

// FormState is what the open form says your keys are doing in it now, or "".
func FormState() string {
	for _, l := range formKeyLines() {
		if s, ok := strings.CutPrefix(l, FormStatePrefix); ok {
			return s
		}
	}
	return ""
}

func formKeyLines() []string {
	data, _ := os.ReadFile(formKeysFile())
	if s := strings.TrimSpace(string(data)); s != "" {
		return strings.Split(s, "\n")
	}
	return nil
}
