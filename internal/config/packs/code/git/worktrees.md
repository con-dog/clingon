---
when: .git
---
# Worktrees
List, create and remove worktrees

## List worktrees
Lists the folders this repository is checked out in.

```sh
git worktree list
```

## New worktree
Checks a branch out in a second folder, so you can work on two branches at once.

```sh
git worktree add <path> <branch>
```

- `<branch>` Which branch
- `<path>` Which path

## Remove a worktree
> [!WARNING]
> Deletes a worktree's folder. Git refuses if it has changes you haven't committed.

```sh
git worktree remove <worktree>
```

- `<worktree>` Which worktree
