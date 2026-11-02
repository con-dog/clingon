package tmpl

import "testing"

func TestRender(t *testing.T) {
	known := func(n string) bool { return n == "a" || n == "b" }
	cases := []struct {
		tmpl string
		vals map[string]string
		want string
	}{
		{"echo <a> <b>", map[string]string{"a": "1", "b": "2"}, "echo 1 2"},
		{"ls <a> <b>", map[string]string{"b": "dir"}, "ls dir"},
		{"ls <b> <a>", map[string]string{"b": "dir"}, "ls dir"},
		{"cat < file <a>", map[string]string{"a": "x"}, "cat < file x"},
		{"docker ps --format '{{.Names}}' <unknown>", nil, "docker ps --format '{{.Names}}' <unknown>"},
		{"<a><a>", map[string]string{"a": "z"}, "zz"},
		{`git commit -m "<a>"`, map[string]string{"a": "fix it"}, `git commit -m "fix it"`},
	}
	for _, c := range cases {
		if got := Render(c.tmpl, known, c.vals); got != c.want {
			t.Errorf("Render(%q) = %q, want %q", c.tmpl, got, c.want)
		}
	}
}
