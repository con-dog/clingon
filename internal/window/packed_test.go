package window

import (
	"bytes"
	"os"
	"testing"
)

// The fonts as the tests read them: unpacked from what the binary carries.
var fontFile, fontBoldFile = mustUnpack(fontPacked), mustUnpack(fontBoldPacked)

func mustUnpack(p packed) []byte {
	b, err := p.unpack()
	if err != nil {
		panic(err)
	}
	return b
}

// The gzipped fonts in the binary are the .otf files beside them, whole
// (font/build.py writes both), and are told from anything else without
// unpacking them.
func TestPackedFontsAreTheFonts(t *testing.T) {
	for file, p := range map[string]packed{"font/ClingonMono-Regular.otf": fontPacked, "font/ClingonMono-Bold.otf": fontBoldPacked} {
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.unpack()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s.gz isn't %s gzipped (err %v): run font/build.py", file, file, err)
		}
		if !p.holds(want) {
			t.Errorf("%s: its own font isn't taken as it", file)
		}
		changed := bytes.Clone(want)
		changed[len(changed)/2] ^= 1
		if p.holds(changed) || p.holds(want[:len(want)-1]) || p.holds(nil) {
			t.Errorf("%s: a changed, cut short or empty file is taken as the font", file)
		}
	}
	if fontPacked.holds(fontBoldFile) || packed(nil).holds(nil) {
		t.Error("the bold isn't the regular, and nothing packed holds nothing")
	}
}
