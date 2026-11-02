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
	keys := []tea.KeyPressMsg{typed("d"), typed("o"), {Code: tea.KeyBackspace}, {Code: tea.KeyBackspace}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(keys[i%len(keys)])
		_ = m.view()
	}
}
