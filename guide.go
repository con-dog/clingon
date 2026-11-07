package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/con-dog/clingon/internal/micro"
)

// guideExample is the pack file `clingon pack guide` shows: harmless commands,
// since people copy from it.
// guidePackInfo is the guide's _pack.md, shown indented in guideRest.
const guidePackInfo = "---\nsection: infrastructure\nlogo: terminal\n---\n# Deploy tools\n"

const guideExample = "# Deploy\n" +
	"Look around before you ship.\n" +
	"\n" +
	"## Show the branch\n" +
	"Prints the branch you're on.\n" +
	"\n" +
	"```sh when=.git\n" +
	"git branch --show-current\n" +
	"```\n" +
	"\n" +
	"## Say hello\n" +
	"Prints a greeting.\n" +
	"\n" +
	"```sh\n" +
	"echo \"Hello, <who>.\" <flags>\n" +
	"```\n" +
	"\n" +
	"- `<who>` Who to greet\n" +
	"  - (x) `world`\n" +
	"  - ( ) `there`\n" +
	"- `<flags>` Extra options\n" +
	"  - [ ] `-n` no new line\n"

// guideTop and guideRest go either side of the example.
const guideTop = `A pack is a folder in ~/.clingon/packs/mine/ with a _pack.md and a Markdown
file for each module, like mine/work/deploy.md. Folder and file names are
lowercase, with no spaces.

A module file, like mine/work/deploy.md:
`

const guideRest = `What each line means:

  # Name            the module's name: every file starts with one
  ## Name           a command (letters, digits, spaces and ( ) only)
  > [!WARNING]      under the name: asks No/Yes before it runs
  > [!CAUTION]      under the name: dangerous, you type a code first
  ` + "```sh" + `             what's typed into your shell; \ at a line's end wraps
  - ` + "`<name>`" + ` Q      a blank in the command, and its question
    - (x) ` + "`value`" + `   select one, or press / and type your own
    - [ ] ` + "`value`" + `   select many; [x] starts selected

When a command can be used, for every command in a file, in front matter
at the very top:

  ---
  needs: docker     programs on your PATH
  when: .git        a file here or in a folder above (./x: this folder only)
  ---

For one command, after its code block's language: ` + "```sh when=./Dockerfile" + `

Every pack has one _pack.md next to its modules. It names the pack, says
which section of the menu it's in, and picks one of clingon's logos:

  ---
  section: infrastructure
  logo: terminal
  ---
  # Deploy tools

Every _pack.md has a section: ai, code, command-line, databases,
infrastructure, system, other, or a name of your own, like work-tools.

What you type goes into your shell as written. A blank whose name has
secret, token or password in it is typed hidden and kept out of your history.

  clingon pack create         start a pack from an example, as a form
  clingon check               find mistakes, with the file and line
`

// guide prints how to write a pack.
func guide() error {
	if !isTerm() {
		_, err := fmt.Fprint(os.Stdout, guideTop+"\n"+guideExample+"\n"+guideRest)
		return err
	}
	indent := func(s string) string {
		lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
		for i, l := range lines {
			lines[i] = "  " + l
		}
		return strings.Join(lines, "\n")
	}
	sayBoxed("How packs work", guideTop+"\n"+indent(micro.Markdown(guideExample))+"\n\n"+guideRest)
	return nil
}
