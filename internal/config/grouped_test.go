package config

import "testing"

// Every built-in section lists its commands under group headings, so
// every menu looks the same.
func TestBuiltInPacksAreGrouped(t *testing.T) {
	ps, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		c, err := loadPack(p.Name)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range c.Sections {
			if len(s.Commands) > 0 || len(s.Groups) == 0 {
				t.Errorf("pack %s, section %s: put its commands in groups", p.Name, s.Name)
			}
		}
	}
}
