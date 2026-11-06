---
when: .git
---
# Everyday
Status, diff, log, commit, pull and push, switch or create branches and go to the repo root

## Status
Shows your branch and which files are changed, staged or untracked.

```sh
git status -sb
```

## Switch branch
Switches to another branch.

```sh
git switch <branch>
```

- `<branch>` Which branch

## New branch
Creates a new branch from where you are and switches to it.

```sh
git switch -c <name>
```

- `<name>` Name

## Commit everything
> [!WARNING]
> Stages every change, new and deleted files included, and commits it. New files means all of them, a .env you forgot to ignore too: check Status first.

```sh
git add -A && git commit -m "<message>"
```

- `<message>` Message

## Pretty log
Shows recent commits as a compact graph with branch and tag names.

```sh
git log --oneline --graph --decorate -n <count> <flags>
```

- `<count>` How many
  - (x) `20`
- `<flags>` Extra options
  - [ ] `--all` all branches
  - [ ] `--first-parent` first parent only
  - [ ] `--stat` files changed

## Pull (rebase)
> [!WARNING]
> Fetches new commits and replays yours on top, tucking uncommitted work away while it does.

```sh
git pull --rebase --autostash
```

## Diff
Shows your changes that aren't staged yet, line by line.

```sh
git diff
```

## Diff staged
Shows what's staged: exactly what your next commit will contain.

```sh
git diff --staged
```

## Push this branch
> [!WARNING]
> Pushes this branch to origin, creating it there and tracking it if it's new.

```sh
git push -u origin HEAD
```

## Fetch everything
Downloads new commits and branches from every remote, and forgets branches deleted there. Your files don't change.

```sh
git fetch --all --prune
```

## Go to repo root
Moves your shell to the top folder of this repository.

```sh
root=$(git rev-parse --show-toplevel) && cd "$root"
```
