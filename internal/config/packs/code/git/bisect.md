---
when: .git
---
# Bisect
Track down the commit that broke something by marking commits good or bad

## Start bisecting
Finds the commit that broke something by binary search: this commit is marked bad, and you pick one that was still good.

```sh
git bisect start && git bisect bad && git bisect good <commit>
```

- `<commit>` Which commit

## This commit is bad
Marks the commit you're on as broken, and checks out the next one to test.

```sh
git bisect bad
```

## This commit is good
Marks the commit you're on as working, and checks out the next one to test.

```sh
git bisect good
```

## Stop bisecting
Ends the search and takes you back to where you started.

```sh
git bisect reset
```
