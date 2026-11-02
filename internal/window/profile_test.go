package window

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// The profile is written by hand: it must still be the JSON iTerm reads.
func TestItermProfileIsTheJSONItWas(t *testing.T) {
	rgb := func(hex string) map[string]any {
		var r, g, b int
		_, _ = fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
		return map[string]any{"Red Component": float64(r) / 255, "Green Component": float64(g) / 255, "Blue Component": float64(b) / 255, "Color Space": "sRGB"}
	}
	p := map[string]any{
		"Name": "clingon", "Guid": "6c696e67-6f6e-4000-8000-636c696e676f",
		"Normal Font": fmt.Sprintf("%s %d", fontPS, fontSize), "ASCII Anti Aliased": true, "Use Non-ASCII Font": false,
		"Columns": cols, "Rows": rows, "Background Color": rgb(bg), "Foreground Color": rgb(fg),
		"Bold Color": rgb(bold), "Cursor Color": rgb(cursor), "Selection Color": rgb(selection),
	}
	for i, c := range ansi {
		p[fmt.Sprintf("Ansi %d Color", i)] = rgb(c)
	}
	wantJSON, _ := json.Marshal(map[string]any{"Profiles": []any{p}})
	var want, got any
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(itermProfile(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, itermProfile())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%s", itermProfile())
	}
}
