package config

import (
	"strings"
	"testing"
)

func TestMarkdownCommand(t *testing.T) {
	src := "---\nneeds: docker\nwhen: compose.yaml, compose.yml\n---\n<!-- a note -->\n# Compose\nStart and stop\nservices.\n\n" +
		"## Up (detached)\n> [!WARNING]\n> Starts them.\n\n```sh when=./Dockerfile needs=git\ndocker compose \\\n  up -d <services>\n```\n\n" +
		"- `<services>` Which services\n\n" +
		"## Logs\nFollows logs.\n\n```\ndocker logs --tail <lines> <name> <flags>\n```\n\n" +
		"- `<name>` Which container\n  - (x) `web`\n- `<lines>` How many\n  - (x) `100` the last 100\n  - ( ) `all`\n" +
		"- `<flags>` Extra options\n  - [x] `-f` follow\n  - [ ] `-t`\n\n" +
		"## Log in\n\n```sh\nlogin <api-token>\n```\n\n- `<api-token>` Token\n"
	part, err := parsePackFile("compose.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	g := part.group
	if g.Name != "Compose" || g.Description != "Start and stop services." || strings.Join(g.Requires, ",") != "docker" || strings.Join(g.When, ",") != "compose.yaml,compose.yml" {
		t.Fatalf("file: %+v", g)
	}
	up, logs, login := g.Commands[0], g.Commands[1], g.Commands[2]
	if up.Name != "Up (detached)" || up.Risk != RiskWarn || up.Desc != "Starts them." || up.Cmd != "docker compose up -d <services>" ||
		strings.Join(up.When, ",") != "./Dockerfile" || strings.Join(up.Requires, ",") != "git" {
		t.Errorf("up: %+v", up)
	}
	if a := up.Args[0]; a.Kind() != "text" || a.Prompt != "Which services" || a.Placeholder != "" || !a.Optional || a.Quote {
		t.Errorf("a typed blank, with its question in the box, that can be empty and goes in as typed: %+v", a)
	}
	name, lines, flags := logs.Args[0], logs.Args[1], logs.Args[2]
	if name.Kind() != "text" || name.Default != "web" || len(name.Options) != 0 {
		t.Errorf("one picked choice is a default: %+v", name)
	}
	if lines.Kind() != "choose" || lines.Default != "100" || len(lines.Options) != 2 || lines.Options[0].Label != "the last 100" {
		t.Errorf("pick one: %+v", lines)
	}
	if flags.Kind() != "flags" || flags.Default != "-f" || len(flags.Options) != 2 {
		t.Errorf("tick any: %+v", flags)
	}
	if a := login.Args[0]; a.Kind() != "secret" || !a.Quote {
		t.Errorf("named like a token: hidden and quoted: %+v", a)
	}
	if login.Risk != "" || logs.Risk != "" {
		t.Error("no callout, no risk")
	}
	pc, err := parsePackFile("x.md", []byte("# X\n\n## Drop it\n> [!CAUTION]\n> Gone for good.\n\n```sh\nrm -rf x\n```\n"))
	if err != nil || pc.group.Commands[0].Risk != RiskDanger {
		t.Errorf("caution is danger: %v %+v", err, pc)
	}
	// A callout added to a command that has a description keeps it: the
	// callout's text and the description around it make one description.
	for _, src := range []string{
		"# X\n\n## Hi\n> [!WARNING]\n> It greets you.\nPrints a greeting.\n\n```sh\necho hi\n```\n",
		"# X\n\n## Hi\n> [!WARNING]\n> It greets you.\n\nPrints a greeting.\n\n```sh\necho hi\n```\n",
		"# X\n\n## Hi\nIt greets you.\n> [!WARNING]\n> Prints a greeting.\n\n```sh\necho hi\n```\n",
	} {
		pw, err := parsePackFile("x.md", []byte(src))
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if c := pw.group.Commands[0]; c.Risk != RiskWarn || c.Desc != "It greets you. Prints a greeting." {
			t.Errorf("%q: risk %q, desc %q", src, c.Risk, c.Desc)
		}
	}
}

func TestMarkdownPackFile(t *testing.T) {
	part, err := parsePackFile(PackInfoFile, []byte("---\nlogo: docker\nneeds: docker, git\n---\n# Docker\nContainers and images.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if i := part.info; i.Name != "Docker" || i.Logo != "docker" || i.Description != "Containers and images." || len(i.Requires) != 2 {
		t.Errorf("got %+v", i)
	}
	c := assemble("docker", []parsedFile{part, {group: &Group{Name: "G", Commands: []Command{{Name: "ps", Cmd: "docker ps"}}}}})
	if c.Sections[0].Color != "#0087ff" {
		t.Errorf("the logo brings its colour: %q", c.Sections[0].Color)
	}
}

// Anything outside the subset is a mistake to point at, with its line.
func TestMarkdownMistakes(t *testing.T) {
	cmd := "\n## Run\n\n```sh\nls\n```\n"
	for _, c := range []struct{ name, src, want string }{
		{"no title", "## Run\n\n```sh\nls\n```\n", ":1: every pack file starts with a # Name"},
		{"bad title", "# Run it!\n", "names use only letters"},
		{"risk in the name", "# T\n\n## Run !\n\n```sh\nls\n```\n", "not a ? or ! in its name"},
		{"no code", "# T\n\n## Run\nDoes it.\n", "has no code block"},
		{"two code blocks", "# T" + cmd + "\n```sh\nls\n```\n", "one code block"},
		{"two lines", "# T\n\n## Run\n\n```sh\ncd x\nmake\n```\n", "end a line with \\"},
		{"unclosed", "# T\n\n## Run\n\n```sh\nls\n", "no closing ```"},
		{"language", "# T\n\n## Run\n\n```python\nls\n```\n", "sh, bash, shell, zsh or nothing"},
		{"fence setting", "# T\n\n## Run\n\n```sh risk=high\nls\n```\n", "needs=… and when=…"},
		{"front matter", "---\nlogo: x\n---\n# T" + cmd, "can only set needs, when"},
		{"star bullet", "# T\n\n## Run\n\n```sh\nls <x>\n```\n\n* `<x>` X\n", "use - for bullets"},
		{"bare blank", "# T\n\n## Run\n\n```sh\nls <x>\n```\n\n- <x> X\n", "name in backticks"},
		{"unlisted blank", "# T\n\n## Run\n\n```sh\nls <x>\n```\n", "list it after the code block"},
		{"choice without code", "# T\n\n## Run\n\n```sh\nls <x>\n```\n\n- `<x>` X\n  - ( ) a\n", "then what it types in backticks"},
		{"mixed choices", "# T\n\n## Run\n\n```sh\nls <x>\n```\n\n- `<x>` X\n  - ( ) `a`\n  - [ ] `b`\n", "all ( ) (pick one) or all [ ]"},
		{"two picked", "# T\n\n## Run\n\n```sh\nls <x>\n```\n\n- `<x>` X\n  - (x) `a`\n  - (x) `b`\n", "only one ( ) choice"},
		{"other callout", "# T\n\n## Run\n> [!NOTE]\n> Hi.\n\n```sh\nls\n```\n", "the only callouts"},
		{"two callouts", "# T\n\n## Run\n> [!WARNING]\n> Hi.\n\n> [!CAUTION]\n> Hi.\n\n```sh\nls\n```\n", "one callout"},
		{"callout after code", "# T\n\n## Run\n\n```sh\nls\n```\n> [!WARNING]\n> Hi.\n", "before the code block"},
		{"bold", "# T\nVery **bold**.\n", "bold or italics"},
		{"small heading", "# T" + cmd + "\n### More\n", "nothing smaller"},
		{"list too early", "# T\n\n## Run\n\n- `<x>` X\n", "after the command's code block"},
	} {
		_, err := parsePackFile("t.md", []byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want %q, got %v", c.name, c.want, err)
		}
	}
	if _, err := parsePackFile(PackInfoFile, []byte("# P\nAbout.\n\n## A command\n")); err == nil || !strings.Contains(err.Error(), "go in a module file") {
		t.Errorf("_pack.md with a command: %v", err)
	}
}

func TestPackColour(t *testing.T) {
	part, err := parsePackFile(PackInfoFile, []byte("---\nlogo: rocket\ncolour: orange\n---\n# Launch\n"))
	if err != nil || part.info.Color != "#ff8700" {
		t.Fatalf("got %+v %v", part.info, err)
	}
	if _, err := parsePackFile(PackInfoFile, []byte("---\ncolour: mauve\n---\n# X\n")); err == nil || !strings.Contains(err.Error(), "no colour called mauve") {
		t.Errorf("an unknown colour: %v", err)
	}
	if _, err := parsePackFile("s.md", []byte("---\ncolour: orange\n---\n# S\n")); err == nil {
		t.Error("a module has no colour of its own")
	}
}

// The editor's checks: each problem at its line, parse errors where they are
// and others at their command's heading, checking text that isn't saved yet.
func TestCheckFile(t *testing.T) {
	path := "/tmp/mine/work/deploy.md"
	if ps := CheckFile(path, []byte("# Deploy\n\n## Ship it\n\n```sh\n./ship.sh\n```\n")); len(ps) != 0 {
		t.Errorf("a good file: %+v", ps)
	}
	ps := CheckFile(path, []byte("# Deploy\n\n## Ship it\n\n```sh\ncd x\nmake\n```\n"))
	if len(ps) != 1 || ps[0].Line != 6 || !strings.Contains(ps[0].Message, "one command") {
		t.Errorf("a parse error, at its line: %+v", ps)
	}
	ps = CheckFile(path, []byte("# Deploy\n\n## Ok\n\n```sh\nls\n```\n\n## Unused\n\n```sh\nls\n```\n\n- `<who>` Who\n"))
	if len(ps) != 1 || ps[0].Line != 9 || !strings.Contains(ps[0].Message, "never used") {
		t.Errorf("a check of the whole command, at its heading: %+v", ps)
	}
	if ps := CheckFile("/tmp/mine/work/_pack.md", []byte("---\nlogo: rocket\ncolour: mauve\n---\n# W\n")); len(ps) != 1 || ps[0].Line != 3 {
		t.Errorf("_pack.md: %+v", ps)
	}
}
