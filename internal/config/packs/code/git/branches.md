---
when: .git
---
# Branches
See recent branches, merge and rebase, and delete branches locally or on the remote

## Delete a remote branch
> [!CAUTION]
> Deletes a branch on origin, for everyone who uses it.

```sh
git push origin --delete <branch>
```

- `<branch>` Which branch

## Recent branches
Lists your 15 most recently worked-on branches, newest first.

```sh
git branch --sort=-committerdate --format='%(committerdate:relative)%09%(refname:short)' | head -n 15
```

## Delete a branch
> [!WARNING]
> Deletes a local branch. Git refuses if it has commits that aren't merged anywhere.

```sh
git branch -d <branch>
```

- `<branch>` Which branch

## Merge a branch
> [!WARNING]
> Brings another branch's commits into this one.

```sh
git merge <branch>
```

- `<branch>` Which branch

## Rebase onto a branch
> [!WARNING]
> Replays this branch's commits on top of another branch, as if you'd started from there.

```sh
git rebase <branch>
```

- `<branch>` Which branch
