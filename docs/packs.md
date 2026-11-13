# Packs

Commands come in packs. These are added to begin with: `files`, `network`, `package-managers`, `ssh`, `system`.

Add the rest as you need them: `aws`, `claude-code`, `codex`, `docker`, `dotnet`, `git`, `github`, `go`, `macos`, `node`, `playwright`, `postgres`, `python`, `redis`, `security`.

```sh
clingon pack available                      # what you can add
clingon pack add git docker
clingon pack add aws/ecs                    # just one module
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
clingon pack create              # starts one as a form, with greyed hints to fill in
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

