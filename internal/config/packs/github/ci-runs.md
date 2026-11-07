---
when: .git
---
# CI runs
Watch a CI run and rerun failed jobs

## Rerun failed jobs
> [!WARNING]
> Runs only the failed jobs of a CI run again.

```sh
gh run rerun <run> --failed
```

- `<run>` Run

## Watch CI run
Follows a CI run live until it finishes.

```sh
gh run watch <run>
```

- `<run>` Run
