---
when: .git
---
# Stash
Save work to a stash, list stashes, and apply, pop or drop one

## Drop a stash
> [!CAUTION]
> Deletes one stash entry. Its changes are gone unless you've applied them somewhere.

```sh
git stash drop <stash>
```

- `<stash>` Which stash

## List stashes
Lists everything you've stashed, newest first.

```sh
git stash list --format='%gd  %cr  %s'
```

## Stash with message
Puts all uncommitted changes, new files included, aside in the stash.

```sh
git stash push -u -m "<message>"
```

- `<message>` Message

## Apply stash
Brings changes from the stash back into your files, keeping them in the stash too.

```sh
git stash apply <stash>
```

- `<stash>` Which stash

## Pop stash
Brings changes from the stash back into your files and removes them from the stash.

```sh
git stash pop <stash>
```

- `<stash>` Which stash
