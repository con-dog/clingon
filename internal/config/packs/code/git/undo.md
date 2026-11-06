---
when: .git
---
# Undo
Unstage files, undo or amend the last commit, discard changes, delete untracked files and hard reset

## Unstage file
Takes a file out of the next commit but keeps your changes to it.

```sh
git restore --staged <file>
```

- `<file>` Which file

## Undo last commit (keep changes)
> [!WARNING]
> Undoes the last commit but keeps its changes, staged and ready to commit again.

```sh
git reset --soft HEAD~1
```

## Discard changes to file
> [!CAUTION]
> Throws away your uncommitted changes to a file.

```sh
git restore <file>
```

- `<file>` Which file

## Hard reset to origin
> [!CAUTION]
> Makes your branch exactly match the remote, throwing away local commits and changes.

```sh
git fetch origin && git reset --hard origin/<branch>
```

- `<branch>` Which branch

## Add to last commit
> [!WARNING]
> Folds your staged changes into the last commit, keeping its message. Don't use it on commits you've pushed and shared.

```sh
git commit --amend --no-edit
```

## Change last commit message
> [!WARNING]
> Rewrites the last commit's message. Don't use it on commits you've pushed and shared.

```sh
git commit --amend -m "<message>"
```

- `<message>` Message

## Delete untracked files
> [!CAUTION]
> Deletes files git isn't tracking, after showing you the list and asking what to remove.

```sh
git clean -di
```
