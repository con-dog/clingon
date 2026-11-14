package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/con-dog/clingon/internal/config"
)

// A keystroke at the top level with every pack installed: filtering every
// command, then drawing the screen. It happens on each key, so it must stay
// far below a frame.
func BenchmarkTypingAtTheTop(b *testing.B) {
	b.Setenv("CLINGON_CONFIG", b.TempDir())
	ps, _ := config.Packs()
	for _, p := range ps {
		if err := config.Install(p.Name); err != nil {
			b.Fatal(err)
		}
	}
	cfg, _, err := config.Load()
	if err != nil {
		b.Fatal(err)
	}
	m := New(Options{Config: cfg, Dir: func() string { return b.TempDir() }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 50})
	m.Update(typed("/"))
	keys := []tea.KeyPressMsg{typed("d"), typed("o"), {Code: tea.KeyBackspace}, {Code: tea.KeyBackspace}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(keys[i%len(keys)])
		_ = m.view()
	}
}

// What the recipe screens ask for each time they're drawn, with every pack
// added and a few recipes: your saved recipes, and every command by its ID.
// Both are kept between frames (config.Recipes, config's sourceRef): once
// read, a frame's worth must stay a small part of a millisecond.
func BenchmarkRecipeScreenLookups(b *testing.B) {
	b.Setenv("CLINGON_CONFIG", b.TempDir())
	ps, _ := config.Packs()
	for _, p := range ps {
		if err := config.Install(p.Name); err != nil {
			b.Fatal(err)
		}
	}
	for i, n := range []string{"One", "Two", "Three", "Four", "Five"} {
		if err := config.SaveRecipe(n, []string{"remote/code/git/everyday.md#status", "remote/code/git/everyday.md#pull"}[:1+i%2], nil); err != nil {
			b.Fatal(err)
		}
	}
	cfg, _, err := config.Load()
	if err != nil {
		b.Fatal(err)
	}
	dir := b.TempDir()
	m := New(Options{Config: cfg, Dir: func() string { return dir }, Recipes: func() []config.Recipe { rs, _ := config.Recipes(); return rs }})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 50})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.savedRecipes()
		_ = m.commandsByID()
	}
}

// A key in the pack form, then drawing it: a pack of a few modules and
// commands, each value wrapped as typing it would wrap it. It happens on each
// key, so it must stay well under a frame. (Half of it is telling the menu the
// form's keys, written to a file when they change.)
func BenchmarkFormKey(b *testing.B) {
	b.Setenv("CLINGON_CONFIG", b.TempDir())
	for _, n := range []string{"demo", "work", "home", "tools"} {
		if err := config.NewPackWithLogo(n, "rocket", "orange"); err != nil {
			b.Fatal(err)
		}
		for i := 0; i < 6; i++ {
			_ = config.AddCommand(n, "Deploy", "Cmd "+string(rune('a'+i)), "echo <x> --flag "+string(rune('a'+i)), "Does a thing")
		}
	}
	f, err := NewPackForm("demo", true)
	if err != nil {
		b.Fatal(err)
	}
	f.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		_ = f.view()
	}
}
