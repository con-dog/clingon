package config

import "testing"

// Reading the packs clingon starts with: it's done before the menu's first
// frame, and nearly all of it is opening files, so each pack's folder is read
// at the same time as the others.
func BenchmarkLoad(b *testing.B) {
	b.Setenv("CLINGON_CONFIG", b.TempDir())
	if err := EnsureDefaults(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Load(); err != nil {
			b.Fatal(err)
		}
	}
}
