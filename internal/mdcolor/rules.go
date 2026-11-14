package mdcolor

// A rule colours what its pattern (a regular expression) matches as its
// group, whose colour colours.txt gives. Rules apply in order, a later match
// winning.
type rule struct{ group, pattern string }

// A region runs from a match of its start to the next of its end, over lines
// if need be, coloured as its group, with rules of its own inside it.
type region struct {
	group, start, end string
	rules             []rule
}

// packRules is a pack's lines: only the Markdown a pack uses, in the menu's
// colours. Anything a pack doesn't allow is drawn as an error.
var packRules = []rule{
	// Text with a mark of its own; plain text stays the default colour.
	{"statement", `^\s*- `},
	// A choice's mark: its ( ) or [ ] lavender, the x picking it plain, as
	// text is (later rules win: the whole mark, then the x, then its opening
	// bracket again, then the bullet).
	{"special.mark", `^\s*- (\([ x]\)|\[[ x]\])`},
	{"special.tick", `^\s*- [(\[]x`},
	{"special.mark", `^\s*- [(\[]`},
	{"statement", `^\s*- `},
	{"constant.string", "`[^`]*`"},
	{"special.blank", "`<[A-Za-z_][A-Za-z0-9_-]*>`"},
	{"comment", `^>.*$`},
	{"special.warning", `^> \[!WARNING\]\s*$`},
	{"special.caution", `^> \[!CAUTION\]\s*$`},
	{"special.heading", `^#{1,2} .*$`},
	{"special.hash", `^#{1,2} `},

	// Not part of a pack: other bullets, smaller headings, numbered lists,
	// bold and italics, and other callouts.
	{"error", `^\s*[*+] `},
	{"error", `^\s*[0-9]+[.)] `},
	{"error", `^#{3,}.*$`},
	{"error", `\*\*[^*]+\*\*|__[^_]+__`},
	{"error", `^> \[!(NOTE|TIP|IMPORTANT)\].*$`},
}

// packRegions is what runs over lines in a pack: a command's code block, the
// front matter and comments.
var packRegions = []region{
	// The command: coloured as the menu colours it (internal/ui/highlight.go):
	// programs green, flags periwinkle, strings tan, operators grey, blanks
	// sky blue.
	{"default", "^```[a-z]*", "^```", []rule{
		{"special.program", `^\s*[A-Za-z0-9_./~$][A-Za-z0-9_./~$-]*`},
		{"special.program", `(\|\||&&|\||;)\s*[A-Za-z0-9_./~$][A-Za-z0-9_./~$-]*`},
		{"special.program", `\b(sudo|exec|time|nohup|xargs|env)\s+[A-Za-z0-9_./~$][A-Za-z0-9_./~$-]*`},
		{"identifier", `(^|\s)--?[A-Za-z0-9_=.:,/-]*`},
		{"symbol", `\|\||&&|\||;|>>?|<`},
		{"constant.string", `"(\\.|[^"\\])*"|'[^']*'`},
		{"special.blank", `<[A-Za-z_][A-Za-z0-9_-]*>`},
		{"identifier", `\b(needs|when)=`},
	}},
	// Front matter: settings for the whole file, their values plain text.
	{"comment", `^---$`, `^---$`, []rule{
		{"special.value", `^[a-z]+:.*$`},
		{"identifier", `^[a-z]+:`},
	}},
	{"comment", `<!--`, `-->`, nil},
}
