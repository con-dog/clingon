# clingon

**A menu of the commands you can never remember, next to a real shell that runs them.**

Type `clingon`. Commands open on the left and your shell on the right. Pick a command, fill in the blanks, and it's typed into your shell and run, as if you'd typed it yourself. So `cd` stays changed, exports stay exported, your aliases work, and `↑` brings the command back.

## Install

```sh
brew install con-dog/tap/clingon
```

Needs MacOs, with zsh or bash as your login shell. Homebrew brings everything else, including tab completion.

## Use

```sh
clingon
```

It opens in its own window of your terminal (Terminal, iTerm2, Ghostty, kitty or Alacritty).

| Key | |
|---|---|
| `↑` `↓` | move |
| `→` or `enter` | open, next blank, run |
| `←` | back |
| `/` | filter; at the top level it searches every command |
| `space` | tick an option in a checklist |
| `1`-`9` | put the command at that position in a recipe |
| `r` | go to the current recipe |
| `x` | clear the recipe |
| `tab` | from the menu to the shell |
| `shift-tab` | from the shell prompt back to the menu |
| `esc` | back; at the top, quit |
| `?` | every key |

**It won't get in your way.** Running a command leaves you in the menu, so your keys always go where you think they're going. If a command needs you (vim, ssh, a password), the status bar says so: press `tab` to go to the shell. You come back to the menu when it finishes. While something is running, the menu's commands are greyed out. Commands that don't fit your folder are greyed out too, like Git's outside a repository.

**It checks before risky commands.** ⚠ commands ask No/Yes first. ☠ commands can't be undone, so you type a short code first, then answer No/Yes.

## Recipes

A recipe runs several commands in order, and stops if one fails or clingon can't tell whether it worked.

**Build one.** On any command, press a number to put it at that position:

1. Open a section and press `1` on the first command.
2. Press `2` on the next, from the same section or any other.
3. Press `r` to see the recipe, then `enter` on **Run**.

The recipe's own list shows its numbered steps, each with its pack, under a heading that says how many there are, how many are risky and whether it's saved. Under the steps: **Run**, **Save as…**, **Describe** and **Clear**.

**Clingon › Recipes** lists the current recipe and your **Saved recipes**, each with how many steps it has and a mark if any step is risky. Open a saved one to see its steps, then **Run**, **Describe**, **Edit** or **Remove** it. **Edit** makes it the current recipe: change it with the number keys, then **Save as…** under the same name to save over it.

Each saved recipe is a Markdown file in `~/.clingon/recipes/`, next to your packs: a name, an optional description, and a numbered list of links to each command's heading in its pack's file. You can edit one by hand, or add a description:

```md
# Deploy check
Pull, then see what changed.

1. [Status](../packs/remote/code/git/everyday.md#status)
2. [Pull](../packs/remote/code/git/everyday.md#pull)
```

**Positions work like this:**

- A command can be in a recipe more than once, like `git status`, `git pull`, `git status`. Its row shows each number: `1 3`.
- Pressing a command's own number removes it from that position, and later ones close up.
- Putting a new command at a position clears every position after it.
- Recipes have up to 9 commands, with no gaps. Number keys don't work while you're filtering.
- Press `x` to clear the recipe.

**Blanks are asked for when each command is reached**, so earlier commands have already had their effect. Answers aren't kept: a recipe asks again every run. Risky commands still ask before they run.

**Run from anywhere.** Press `enter` on a command in the recipe's list to run the recipe from there, leaving out the ones before it. If a command stops the recipe, the cursor waits on it when you press `r`: press `enter` to run it again and carry on, or move down a row to skip it, say if you've fixed things by hand in the shell. **Run** always starts from the top.

**Need a command that isn't in a pack?** Number **Command Line › Commands › Type a command** like any other command. It asks what to run each time the recipe gets there. If you run the same line every time, [add it to a pack of your own](#your-own-commands).

From the command line:

```sh
clingon recipe list
clingon recipe describe "My flow"   # what each command runs; runs nothing
clingon recipe remove "My flow"
```

## Packs

Commands come in packs. These are added to begin with: `files`, `network`, `package-managers`, `ssh`, `system`.

Add the rest as you need them: `aws`, `claude-code`, `codex`, `docker`, `dotnet`, `git`, `github`, `go`, `macos`, `node`, `playwright`, `postgres`, `python`, `redis`, `security`.

```sh
clingon pack available                      # what you can add
clingon pack add git docker
clingon pack add aws/ecs                    # just part of a pack
clingon pack list
clingon pack remove docker
clingon pack update
clingon pack describe docker                # print a pack's files
```

A pack is a folder, with a file for each module. `aws` is all of AWS and `aws/ecs` is just its ECS commands. `clingon pack update` only updates what you have: it never adds a module you didn't choose, and tells you what else there is.

All of this is also in the menu, under **Clingon › Packs**.

## Your own commands

```sh
clingon pack guide               # how packs work, with an example to copy
clingon pack create              # starts one from a working example, as a form
clingon pack edit work-tools
clingon check                    # find mistakes
```

A pack is a folder of Markdown files, one for each module. They read as documentation on GitHub, and clingon runs them. Your packs go in `~/.clingon/packs/mine/`, a folder each:

```
~/.clingon/packs/mine/
  k8s/
    _pack.md            # the pack's name, section, logo and what it needs
    pods.md             # a module
```

Folder and file names are lowercase, with no spaces.

Each file starts with a `# Name`, and each `##` heading is a command:

````md
---
when: k8s
---
# Pods
Logs and restarts.

## Tail logs
Streams an app's logs.

```sh
kubectl logs -f deploy/<app>
```

- `<app>` Which app

## Restart app
> [!WARNING]
> Restarts every pod of an app, one at a time.

```sh needs=kubectl
kubectl rollout restart deploy/<app> -n <namespace>
```

- `<app>` Which app
- `<namespace>` Which namespace
  - (x) `staging`
  - ( ) `prod`
````

- **A command** is a `##` heading, a description, and a code block with what's typed into your shell. End a line with `\` to carry a long command on to the next.
- **Risk:** a `> [!WARNING]` callout under the heading makes clingon ask No/Yes first; `> [!CAUTION]` makes you type a short code, then answer No/Yes. The callout's text, with any description next to it, is the command's description.
- **Blanks:** each `<name>` in the command is listed after it, with the question to ask. Under a blank, `( )` choices are select one: one is picked, or you type over it (`(x)` is picked to begin with, so a single `(x)` is a default). `[ ]` choices are select many: any of them can be picked (`[x]` starts selected). Backticks hold what each choice types; a label after them is optional.
- **What you type** goes into your shell exactly as written. Where a value should stay one word, put quotes in the command: `git commit -m "<message>"`. A blank whose name has `secret`, `token` or `password` in it is typed hidden and kept out of your history.
- **When a command fits:** front matter at the top of a file sets `needs:` (programs on your PATH) and `when:` (a file here or in a folder above, like `.git`; `./Dockerfile` means this folder only) for every command in it. After a code block's language, `needs=` and `when=` do the same for one command. Commas in `when` mean any of them will do; every program in `needs` has to be there.
- **Notes:** `<!-- comments -->` are for whoever edits the file.

Names use letters, digits, spaces and `( )`. Packs use only this much Markdown: `-` bullets, no bold or italics, and code blocks in `sh`, `bash`, `shell`, `zsh` or no language. `clingon check` names the line and the fix for anything else.

A `_pack.md` names the pack, says which section of the menu it's in, and can pick one of clingon's logos:

```md
---
menu: infrastructure
logo: terminal
needs: kubectl
---
# Kubernetes
Pods, logs and rollouts.
```

`menu:` is where the menu shows the pack, and every `_pack.md` has one: `ai`, `code`, `command-line`, `databases`, `infrastructure`, `system`, `other`, or a name of your own, like `work-tools`, which gets a heading of its own (Work tools). Menu names are lowercase, with `-` between words.

Give a command a callout:

- `[!WARNING]` (⚠, asks No/Yes) if it changes or stops things but can be put right: merges, rebases, pushes, stopping or restarting, uninstalling, overwriting files you can make again, changing shared state, installing things, dumping a database (it only reads, but the file holds all the data).
- `[!CAUTION]` (☠ in red; you type a random code, then No/Yes) if it destroys data or shared state for good, or can take down something live: stopping or redeploying a cloud service, deleting files or uncommitted work, hard resets, deleting remote branches, removing containers or volumes, overwriting a bucket, dropping or restoring databases.

Every built-in pack follows those rules, and a test keeps it that way.

**Project packs:** put packs in `.clingon/packs/` in a repository, like `.clingon/packs/deploy/prod.md` with its `.clingon/packs/deploy/_pack.md`, and their commands appear under **This project** whenever your shell is in that repository. clingon shows you every command and asks before using them, and asks again if any file changes.

## Good to know

- **Your setup stays yours.** clingon runs a private tmux server and reads your shell config as usual, but never changes it. Its prompt and colours apply only inside its own window.
- **What it adds.** Its own font, and a "clingon" profile in Terminal or iTerm2. MacOs asks once whether clingon may control those two apps, only to open and style the window.
- **Separate history.** Unless your config sets `HISTFILE`, zsh inside clingon keeps its own history, apart from your usual one.
- **Everything else it keeps** is in `~/.clingon`: your packs and recipes at the top, and its own files in `~/.clingon/config`. Set `$CLINGON_CONFIG` in your shell's startup files to move it.
- **Not on SSH.** clingon needs a window to open, so over SSH use `clingon print`, which prints the command you pick: `eval "$(clingon print)"`.
- **Something wrong?** `clingon doctor` checks tmux and your packs' tools, and `clingon crash` shows why it last crashed.
- **Updating:** `clingon update`.

## Removing clingon

```sh
brew uninstall clingon
rm -rf ~/.clingon
rm -f ~/Library/Fonts/ClingonMono-*.otf
rm -f ~/Library/Application\ Support/iTerm2/DynamicProfiles/clingon.json  # iTerm2
rm -rf ~/Library/Caches/clingon                                          # Terminal
```

If you use Terminal, also delete any profiles named "clingon" in Settings › Profiles.

## Hacking on clingon

```sh
go build -o clingon ./cmd/clingon && ./clingon
go test ./...
```

## License

MIT
