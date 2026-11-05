# clingon

**A menu of the commands you can never remember, next to a real shell that runs them.**

<!-- ![clingon demo](demo.gif): add back once `vhs demo.tape` has made it -->

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
3. Press `r` to see the recipe, then `enter` on **Run recipe**.

clingon offers to add the recipe before it runs, unless you've already added those commands. Your recipes are listed under **All recipes**, at **Clingon › Recipes**, where you can run, describe or remove them.

**Positions work like this:**

- A command can be in a recipe more than once, like `git status`, `git pull`, `git status`. Its row shows each number: `1 3`.
- Pressing a command's own number removes it from that position, and later ones close up.
- Putting a new command at a position clears every position after it.
- Recipes have up to 9 commands, with no gaps. Number keys don't work while you're filtering.
- Press `x` to clear the recipe.

**Blanks are asked for when each command is reached**, so earlier commands have already had their effect. Answers aren't kept: a recipe asks again every run. Risky commands still ask before they run.

**Run from anywhere.** Press `enter` on a command in the recipe's list to run the recipe from there, leaving out the ones before it. If a command stops the recipe, the cursor waits on it when you press `r`: press `enter` to run it again and carry on, or move down a row to skip it, say if you've fixed things by hand in the shell. **Run recipe** always starts from the top.

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

A pack is a folder, with a file for each submenu. `aws` is all of AWS and `aws/ecs` is just its ECS commands. `clingon pack update` only updates what you have: it never adds a part you didn't choose, and tells you what else there is.

All of this is also in the menu, under **Clingon › Packs**.

## Your own commands

```sh
clingon pack create work-tools   # opens a commented example in micro
clingon pack edit work-tools
clingon check                    # find mistakes
```

Your packs are folders in `~/.clingon/packs/mine/`. All a pack needs is a file for each submenu:

```
~/.clingon/packs/mine/work-tools/
  _pack.yaml        # optional: how it looks in the menu
  kubernetes.yaml   # a submenu
```

A `<blank>` in a command gets asked for each time it runs:

```yaml
# kubernetes.yaml
name: Kubernetes
commands:
  - name: Tail logs
    cmd: kubectl logs -f deploy/<app>
    desc: Streams an app's logs.
    args:
      - name: app          # type a value
        quote: true

  - name: Restart app
    cmd: kubectl rollout restart deploy/<app> -n <namespace>
    risk: warn             # asks first; danger makes you type a code too
    when: [k8s]            # greyed out unless this is here or in a folder above
    args:
      - name: app
        quote: true
      - name: namespace    # pick one
        options: [staging, prod]
```

Without a `_pack.yaml`, a pack is called after its folder and a submenu after its file. Add one to change that:

```yaml
# _pack.yaml
name: Work tools
description: What we run at work
category: Infrastructure   # AI, Code, Command Line, Databases, Infrastructure, System or Other
icon: ✶
```

Args can also be `type: flags` (tick any) or `type: secret` (typed hidden, and kept out of your shell history). [schema.json](schema.json) and [pack.schema.json](pack.schema.json) have every option, and editors with the YAML language server use them to autocomplete and check your packs.

Give a command a `risk`:

- `risk: warn` (⚠, asks No/Yes) if it changes or stops things but can be put right: merges, rebases, pushes, stopping or restarting, uninstalling, overwriting files you can make again, changing shared state, installing things, dumping a database (it only reads, but the file holds all the data).
- `risk: danger` (☠ in red; you type a random code, then No/Yes) if it destroys data or shared state for good, or can take down something live: stopping or redeploying a cloud service, deleting files or uncommitted work, hard resets, deleting remote branches, removing containers or volumes, overwriting a bucket, dropping or restoring databases.

Every built-in pack follows those rules, and a test keeps it that way.

**Project packs:** put packs in `.clingon/packs/` in a repository, like `.clingon/packs/deploy/prod.yaml`, and their commands appear under **This project** whenever your shell is in that repository. clingon shows you every command and asks before using them, and asks again if any file changes.

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
go build -o clingon . && ./clingon
go test ./...
```

## License

MIT
