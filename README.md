# clingon

**A menu of the commands you can never remember, next to a real shell that runs them.**

<!-- ![clingon demo](demo.gif): add back once `vhs demo.tape` has made it -->

Type `clingon`. Your commands open on the left, your normal shell on the right. Pick a command, fill in the blanks, and it's typed into the shell and run — as if you'd typed it yourself.

The menu stays on screen, asks for the blanks one at a time, and checks with you before anything risky runs. And because clingon types into your actual shell, not a subshell, `cd`, `export AWS_PROFILE=prod`, `source venv/bin/activate` and friends just work:

- `cd` stays changed, exports stay exported
- your aliases, functions and prompt all work
- every command lands in the shell's history, so `↑` brings it back

## Install

```sh
brew install con-dog/tap/clingon
```

clingon runs on macOS only, and needs zsh or bash as your login shell.

That's the only way to install it, and it sets up tab completion for zsh and bash too (`clingon completion zsh` prints the script, if you need it elsewhere). Homebrew brings everything clingon needs along with it: tmux, [micro](https://micro-editor.github.io) (the pack editor).

## Use

```sh
clingon
```

| Key | |
|---|---|
| type | filter — at the top level it searches every command |
| `↑` `↓` | move |
| `→` / `enter` | forward: open, next step, and finally run |
| `←` | back a step |
| `space` | tick an option in a checklist |
| `esc` | clear the filter or back a step; at the top, close clingon and return to your terminal |
| `ctrl-c` | cancel what you're doing (never closes clingon) |
| `tab` | in the menu: hop right to the shell |
| `S-Tab` | at the shell prompt: hop left to the menu (inside vim, ssh etc. it passes through) |
| `[` `]` | jump to the previous or next heading |
| `?` | every key, on one screen |

Clicking a pane works too, and selecting text with the mouse copies it to your clipboard (double-click a word, triple-click a line). When a command you picked from the menu finishes, focus comes back to the menu by itself; interactive ones (`vim`, `ssh`, `docker exec -it`) come back when you exit them. Commands you type in the shell yourself leave you in the shell.

The menu never types into a shell that isn't ready for it. While something runs there (vim, ssh, a REPL, the command you just picked) or the shell is scrolled back, commands are greyed out and the bottom bar says why; they come back by themselves when it's done. If you've typed something at the prompt and not run it, picking a command asks first: go to the shell, or clear it and run.

Commands marked ⚠ ask No/Yes before they run. Commands marked ☠, in red, can't be undone (dropping a database, deleting a remote branch, a hard reset): you type a random 4-letter code shown with the command first, then it asks No/Yes.

Commands that only make sense in certain folders are greyed out elsewhere, saying why: Git's need a repository, Compose's a compose file, Go's a `go.mod`. Go back to the menu after a `cd` and it catches up.

## Project packs

Put a `.clingon.yaml` in a repository (same format as any pack) and its commands appear at the top of the menu, under **This project**, whenever your shell is in that repository. Commit it, and everyone on the team gets the repo's runbook commands.

Because a project pack's commands come from whoever wrote the repo, clingon asks first: it shows every command the file would run and uses them only once you say yes. That's remembered for that exact file; if it changes, clingon asks again. `clingon check` checks the one in the current folder.

`clingon` always opens in a new, maximized window of your terminal app (not macOS full screen), in clingon's colours and its own font, Clingon Mono: [JetBrains Mono](https://www.jetbrains.com/lp/mono/) plus the logos packs show (Git, Docker, AWS…), bundled and installed for your user the first time. That window only: your terminal's default profile and other windows stay as they are. It works with Terminal, iTerm2, Ghostty, kitty and Alacritty (Terminal and iTerm2 get a "clingon" profile of their own; macOS asks once whether clingon may control them, only to open and style the window; if you said no, allow it in System Settings › Privacy & Security › Automation). Terminal can only start a command by typing it, so its new window's shell gets one line, the same every time: ` /bin/sh ~/Library/Caches/clingon/open-window`. It stays out of your history if you set `hist_ignore_space` (zsh) or `HISTCONTROL=ignorespace` (bash), and running it again from there does nothing. clingon doesn't run over SSH, because there's no window to open: `clingon print` is all you get there.

Want the menu without tmux? `clingon print` picks a command and prints it, e.g. `eval "$(clingon print)"`.

## Packs

clingon ships over 400 commands in packs. Out of the box you get the ones that only need tools every Mac already has; add the rest for the tools you use:

```sh
clingon pack available         # what you can install
clingon pack add git docker    # install
clingon pack list              # every pack you have, installed and your own
clingon pack remove docker     # works for either kind
clingon pack update            # newer versions of installed packs
```

Installed packs are files in `packs/remote/`, your own in `packs/local/`; the split only exists so names never clash. If you have both a `remote` and a `local` pack called `docker`, say which: `clingon pack remove local/docker`.

| Pack | | |
|---|---|---|
| `files` | get around, look at, find, archive, copy and clean up files | installed |
| `network` | ports, HTTP requests, serving a folder, ping, DNS and port checks | installed |
| `package-managers` | search, install, remove and upgrade with Homebrew | installed |
| `ssh` | hosts from `~/.ssh/config`, tunnels, keys and first-time setup | installed |
| `system` | processes, shell history and env vars, disk space, JSON, and generators (secrets, UUIDs, timestamps, base64) | installed |
| `aws` | first-time setup, profiles and SSO, S3, EC2/SSM sessions, ECS, EKS, CloudWatch logs | |
| `claude-code` | start, continue and resume Claude Code, one-off questions | |
| `codex` | start, resume and script OpenAI Codex, model and sandbox, sign-in and MCP servers | |
| `docker` | first-time setup, containers, images, volumes, networks and Compose | |
| `dotnet` | .NET: new, build, test and publish, NuGet packages, EF Core migrations, user secrets and SDKs | |
| `git` | first-time setup, everyday Git, branches, remotes, tags, worktrees, bisect, undo and stash | |
| `github` | sign-in, repos, pull requests, issues, releases, gists and CI runs with the GitHub CLI | |
| `go` | modules, building, testing and tools | |
| `macos` | power, Wi-Fi, DNS, Finder, clipboard and software updates | |
| `node` | npm, pnpm, yarn and bun: each one only works where its lockfile is | |
| `playwright` | run, debug and record tests, reports and traces, and installing browsers | |
| `postgres` | PostgreSQL: connect, query, inspect, dump, restore and drop | |
| `python` | virtualenvs, pip, pytest and uv | |
| `redis` | connect, find and read keys, watch the server, snapshot and flush | |
| `security` | TLS certificates, JWTs and checksums | |

The same commands live in the menu under **Clingon › Packs**, at the bottom. The menu picks up changes when you come back to it. New packs and pack versions come with new releases of clingon; the menu tells you when `clingon pack update` has something to install.

## Keeping it healthy

```sh
clingon doctor          # tmux, PATH, your packs, and each pack's tools
clingon doctor docker   # just one pack
clingon crash           # why clingon last crashed, if it has
clingon update          # brew upgrade con-dog/tap/clingon
```

These are in the menu too, under **Health check** and **Update**.

If clingon ever crashes, the window closes, so it saves what happened to `crash.log` in its config folder. The next welcome and the status bar say so until you've looked. `clingon crash` shows it in full; `clingon crash | pbcopy` copies it plain, for a bug report.

The menu's bottom line checks as you go: open a section and it says whether that pack's tools are there. It stays grey while all's well, and turns red when one isn't: `✗ Needs aws (not installed)`, or `✗ Needs go (not on PATH: it's in ~/go/bin)` when it's installed somewhere your PATH leaves out. On the top level it shows news: a new release or pack updates in amber, packs worth adding in purple.

Each time clingon starts, the shell side opens with a short welcome: the banner plus anything worth doing, like a newer release, packs for tools you already have (`docker` on your PATH but no `docker` pack), or packs whose tools are missing. Run it any time with `clingon welcome`. The release check never slows startup: it reads a cached answer and refreshes it in the background at most once a day.

## Your own commands

Your commands live in packs of your own, in exactly the same format as the built-in ones. In the menu, **Clingon › Packs › Your packs › Create a pack** asks for a name, then opens a working example pack in micro (Ctrl-S saves, Ctrl-Q quits), with a comment on every line saying what it does. Change the example into your own commands; any `<blank>` in a command gets asked for each time you run it. When you quit, clingon tells you if anything's wrong. **Edit a pack** opens it again later.

The same from the command line:

```sh
clingon pack create work-tools  # start a pack from the example and open it in micro
clingon pack edit work-tools    # edit it again
clingon check                   # validate everything

# or add simple commands without opening an editor
clingon command add work-tools Deploy "Tail logs" 'kubectl logs -f deploy/<app>' "Streams an app's logs."
clingon command remove work-tools/Deploy/Tail logs
```

`command add` edits the file for you and keeps your comments; it won't write anything that doesn't validate. For pick-lists, checklists and the rest, edit the YAML: each of your packs is a file in `~/.config/clingon/packs/local/`. A section named like a built-in one (say `AWS`) adds to it. Sections, groups and commands are listed alphabetically. Every section's commands sit under group headings; any you leave out of a group go under **Commands**. A command is a template whose `<placeholders>` get filled in, one at a time, while the command line builds up live:

```yaml
sections:
  - name: Docker
    logo: docker                                    # a brand logo (the names are in schema.json), or
    # icon: ▣                                       # one glyph: a symbol or emoji
    color: "#0087ff"                                # the logo's or icon's colour (grey if left out)
    category: Infrastructure                        # heading on the main menu
    groups:
      - name: Containers
        commands:
          - name: Shell into container
            cmd: docker exec -it <container> <shell>
            desc: Opens an interactive shell inside a running container.
            args:
              - name: container                     # text: type it
                quote: true                         # a name, so it's kept as one word
                desc: Inside <container>.           # added to the description once filled in
              - name: shell                         # choose: pick one
                options: [sh, bash, zsh]

          - name: Remove container
            cmd: docker rm <flags> <container>
            risk: danger                            # ☠ type a code, then No/Yes (warn: ⚠ No/Yes)
            args:
              - name: container
                quote: true
              - name: flags                         # flags: tick any
                type: flags
                options:
                  - { value: -f, label: force }
                  - { value: -v, label: remove volumes }

          - name: Build image
            cmd: docker build -t <tag> .
            when: [Dockerfile]                      # greyed out unless there's one here or above
            args:
              - name: tag                           # text: type it
                placeholder: myapp:dev
```

Give a command a `risk`:

- `risk: warn` (⚠, asks No/Yes) if it changes or stops things but can be put right: merges, rebases, pushes, stopping or restarting, uninstalling, overwriting files you can make again, changing shared state, installing things, dumping a database (it only reads, but the file holds all the data).
- `risk: danger` (☠ in red; you type a random code, then No/Yes) if it destroys data or shared state for good, or can take down something live: stopping or redeploying a cloud service, deleting files or uncommitted work, hard resets, deleting remote branches, removing containers or volumes, overwriting a bucket, dropping or restoring databases.

Every built-in pack follows those rules, and a test keeps it that way.

`when: [.git]` greys a command out unless one of the listed files is in your shell's folder or a folder above it (globs work: `['*.tf']`). Start it with `./` for tools that only look in the current folder: `['./*.tf']`. Put it on a group and it covers every command in the group.

| Arg type | Asks you to | Keys |
|---|---|---|
| `text` | type a value | `default`, `placeholder`, `optional` |
| `choose` | pick one | `options` — strings or `{value, label}` |
| `flags` | tick any number | `options`, `join` (default: space), `default` (ticked to begin with) |
| `secret` | type it hidden | the command reads it from a file that's deleted straight after, so it's never on screen or in your shell history |

Any typed value can have a `pattern` (a regular expression it must match, like `[0-9]+` for a port); clingon won't move on until it does.

A section's `category` is one of `AI`, `Code`, `Databases`, `Infrastructure`, `System`; leave it out and the section goes under **Other**.

`desc` says what a command does. Each arg can add a sentence of its own once it's filled in, with `<name>` standing for the value, so the description grows as you go: *"Opens an interactive shell inside a running container. Inside web. Using bash."* Options without one contribute their labels instead.

`type` is inferred when you leave it out (`options` → choose, else text). Details that make it pleasant:

- Only declared args are placeholders, so `cat < file` and `{{.Names}}` are left alone.
- Keep the placeholder of a `quote: true` arg outside quotes, because clingon quotes the value itself: write `'DROP TABLE '<table>`, not `'DROP TABLE <table>'`. `clingon check` tells you if you've missed one.
- A group or command with `requires: [brew]` only shows up when `brew` is on your PATH, so a pack can cover tools you may not have.

The format has a [JSON schema](schema.json): editors with the YAML language server autocomplete and check it, and so can your AI assistant when you ask it to "add my usual kubectl commands to clingon".

## How it works

clingon starts a **private tmux server** on its own socket with its own config — it never reads `~/.tmux.conf` or touches your other tmux sessions, and it works from inside tmux too. The left pane runs the menu; the right pane runs your `$SHELL`. Picking a command sends its keystrokes to the shell pane with `tmux send-keys` (multi-line commands go in as a bracketed paste). Close either side and the whole thing goes away, along with anything still running in the shell, so clingon won't close from the menu while a command is running there, and asks first if there are jobs in the background or stopped.

clingon has **one look, everywhere**, so every screenshot looks the same. Both panes get the same dark colours, whatever your terminal's theme. The shell pane is your own shell: zsh and bash load your config as usual (aliases, PATH, plugins), and then clingon's prompt goes on top: the folder, the git branch, the AWS profile if `AWS_PROFILE` is set, and a ❯ that turns red when a command fails. Starship, powerlevel10k and oh-my-zsh themes step aside only inside clingon. None of your files change: clingon's live in `~/.config/clingon/shell/`, and your terminal and any other shell are left exactly as they are. clingon needs zsh or bash as your login shell and won't start on anything else, though `clingon print` works in any shell.

It's a single ~6 MB Go binary built on [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss) — the libraries behind gum — so it starts instantly. Homebrew brings everything else it needs.

Everything clingon keeps is in one folder, `~/.config/clingon`. `$CLINGON_CONFIG` moves it; clingon's window starts a fresh login shell, so set it in your shell's startup files, not just for one command.

| Path | |
|---|---|
| `packs/remote/*.yaml` | installed packs, one file each: `pack add` copies one in, `pack remove` deletes it, `pack update` replaces it |
| `packs/local/*.yaml` | your own packs (names: letters, digits and dashes, not starting with a dash) |
| `packs/_available.yaml` | every pack you can install (written by clingon) |
| `micro/` | clingon's settings for micro, the pack editor (your own `~/.config/micro` is left alone) |
| `shell/` | the startup files that give the shell inside clingon its look; each one reads yours first. On macOS, unless your config sets `HISTFILE`, zsh inside clingon keeps its history here too (`shell/zsh/.zsh_history`), apart from your usual one |
| `latest-release` | the once-a-day check for a newer release |
| `trusted-projects` | the project packs you've said yes to, by checksum |
| `run/` | the socket of a running session, and secrets waiting for the command that reads them; empty otherwise |

## Removing clingon

```sh
brew uninstall clingon
rm -rf ~/.config/clingon
rm -f ~/Library/Fonts/ClingonMono-*.otf
rm -f ~/Library/Application\ Support/iTerm2/DynamicProfiles/clingon.json  # if you use iTerm2
rm -rf ~/Library/Caches/clingon                                          # if you use Terminal
```

If you use Terminal, delete any profiles named "clingon" in Settings › Profiles. clingon never deletes a profile itself, so an update that changes its font or colours can leave an older one behind.

## Hacking on clingon

```sh
go build -o clingon . && ./clingon
```

A local build behaves like an installed one: inside its session `clingon` on PATH is that build, so the menu's own commands, the welcome and anything you type use it. `clingon update` rebuilds it from the checkout, `clingon version` shows the commit it was built from, and `go test ./...` runs the tests.

## License

MIT
