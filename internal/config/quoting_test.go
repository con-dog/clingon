package config

import "testing"

// Answers go into the shell as they're typed, as if you'd typed the command
// yourself; a pack puts quotes in the command where a value must stay one word.
// Secrets are the exception: hidden, kept out of history, and quoted, so a
// password with a $ in it still works. A blank is a secret by its name.
func TestOnlySecretsAreQuoted(t *testing.T) {
	ps, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	secrets := 0
	for _, p := range ps {
		c, err := loadPack(p.Name)
		if err != nil {
			t.Fatal(err)
		}
		for i := range c.Sections {
			for _, cmd := range c.Sections[i].All() {
				for _, a := range cmd.Args {
					secret := IsSecretName(a.Name) && len(a.Options) == 0
					switch {
					case secret && (a.Kind() != "secret" || !a.Quote):
						t.Errorf("%s › %s: <%s> is named like a secret, so it should be hidden and quoted", p.Name, cmd.Name, a.Name)
					case !secret && a.Quote:
						t.Errorf("%s › %s: <%s> is quoted, but answers go in as typed", p.Name, cmd.Name, a.Name)
					}
					if secret {
						secrets++
					}
				}
			}
		}
	}
	if secrets < 4 {
		t.Errorf("found %d secret blanks; the built-in packs have at least 4", secrets)
	}
}
