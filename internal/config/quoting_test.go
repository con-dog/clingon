package config

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// meantToBeWords are the only built-in blanks typed into a command as they are,
// with neither quoting nor a pattern: what's typed is meant to be several words.
var meantToBeWords = []string{
	"Add MCP server/command",
	"Add a package/package",
	"Install/name",
	"Rebuild and start/services",
	"Run a package without installing/package",
	"Run image/command",
	"Run in the project/command",
	"Time a command/command",
	"Up (detached)/services",
}

// A value typed into a blank is one word to the shell, so a space can't split it
// and a ; can't end the command. Anything else has to say why not, above.
func TestTypedValuesAreQuotedUnlessMeantToBeWords(t *testing.T) {
	ps, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	digits := regexp.MustCompile(`^[\[\]0-9a-zA-Z_.+*?{},-]+$`) // a pattern with no room for a space or a ;
	for _, p := range ps {
		c, err := loadPack(p.Name)
		if err != nil {
			t.Fatal(err)
		}
		for i := range c.Sections {
			for _, cmd := range c.Sections[i].All() {
				for _, a := range cmd.Args {
					if a.Kind() != "text" || a.Quote {
						continue
					}
					if a.Pattern != "" && digits.MatchString(a.Pattern) {
						continue
					}
					where := cmd.Name + "/" + a.Name
					if !slices.Contains(meantToBeWords, where) {
						t.Errorf("%s: %s goes in as typed: give it quote: true, or add it to meantToBeWords (cmd: %s)", p.Name, where, strings.TrimSpace(cmd.Cmd))
					}
				}
			}
		}
	}
}
