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

// NewEditable is a new pack, not yet saved anywhere: the starter pack's
// example module, under a name not already taken.
func NewEditable() (*EditablePack, error) {
	part, err := parsePackFile("examples.md", Starter)
	if err != nil {
		return nil, err
	}
	p := &EditablePack{Section: "other", Logo: DefaultPackLogo, Color: "grey", Files: []EditableFile{{Slug: "examples", Group: *part.group}}}
	p.Title = uniqueTitle("New pack")
	p.Folder = p.NewFolder()
	return p, nil
}

// uniqueTitle is base, or base 2, 3, … if you have a pack called that.
func uniqueTitle(base string) string {
	name := base
	for n := 2; folderTaken(slugOf(name)); n++ {
		name = fmt.Sprintf("%s %d", base, n)
	}
	return name
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

// SaveDraft saves the form's draft as a pack, and removes the draft. It says
// which pack, by name.
func SaveDraft() (string, error) {
	p, err := ReadDraft()
	if err != nil || p == nil {
		return "", err
	}
	if p.IsNew() {
		p.Folder = p.NewFolder()
	}
	if err := p.Save(); err != nil {
		return p.Title, err
	}
	return p.Title, RemoveDraft()
}

// askFile marks the draft as waiting on the menu's question, while the form
// stays open beside it.
func askFile() string { return filepath.Join(Dir(), "pack-draft.ask") }

// Ask marks the form's draft as waiting for the menu to ask whether to save it.
func Ask() error { return writeWhole(askFile(), nil) }

// Asking says whether the form's draft is waiting for the menu's question.
func Asking() bool { return exists(askFile()) }

// StopAsking unmarks it: answered, or gone back to the form.
func StopAsking() {
	_ = os.Remove(askFile())
}

// formNameFile holds the name of the pack the open form is editing, as it
// changes, for the menu beside it to say what's being edited.
func formNameFile() string { return filepath.Join(Dir(), "pack-form.name") }

// WriteFormName keeps the name of the pack the form is editing.
func WriteFormName(name string) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return writeWhole(formNameFile(), []byte(name))
}

// FormName is the name of the pack the open form is editing, or "".
func FormName() string {
	data, _ := os.ReadFile(formNameFile())
	return strings.TrimSpace(string(data))
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

// FormKeys are the keys that work in the open form now, or none.
func FormKeys() []string {
	data, _ := os.ReadFile(formKeysFile())
	if s := strings.TrimSpace(string(data)); s != "" {
		return strings.Split(s, "\n")
	}
	return nil
}
