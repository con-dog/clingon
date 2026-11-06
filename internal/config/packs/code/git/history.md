---
when: .git
---
# History
Show commits and file history, see who changed each line, cherry-pick and recover lost commits

## Find a lost commit
Lists where HEAD has been lately, so you can find commits lost to a reset, rebase or deleted branch.

```sh
git reflog -n 50 --date=relative
```

## Get a lost commit back
Makes a new branch at a commit from the reflog, so a lost commit is safe again.

```sh
git branch <name> <commit>
```

- `<commit>` Which commit
- `<name>` Name

## Cherry pick a commit
> [!WARNING]
> Copies one commit from anywhere onto this branch.

```sh
git cherry-pick <commit>
```

- `<commit>` Which commit

## Show a commit
Shows a commit's message and every change it made.

```sh
git show <commit>
```

- `<commit>` Which commit

## File history
Shows every commit that changed a file, with its changes, following renames.

```sh
git log --follow -p -- <file>
```

- `<file>` Which file

## Who changed each line
Shows, for every line of a file, the commit and author that last changed it.

```sh
git blame <file>
```

- `<file>` Which file
