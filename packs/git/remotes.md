---
when: .git
---
# Remotes
Show remotes, add one and change a remote's URL

## Add a remote
Connects this repository to another copy of it somewhere else.

```sh
git remote add <name> <url>
```

- `<name>` Name
  - (x) `origin`
- `<url>` Url

## Change a remote URL
Points a remote somewhere new, for example after a repository moves or to switch from HTTPS to SSH.

```sh
git remote set-url <remote> <url>
```

- `<remote>` Which remote
- `<url>` Url

## Show remotes
Lists the remotes this repository pushes to and pulls from.

```sh
git remote -v
```
