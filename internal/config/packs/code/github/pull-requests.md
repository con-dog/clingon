---
when: .git
---
# Pull requests
Create, check out, open and merge pull requests, and see your PRs and reviews

## Check out pull request
Checks out a pull request's branch locally so you can try it.

```sh
gh pr checkout <pr>
```

- `<pr>` Which pull request

## Create pull request
> [!WARNING]
> Opens a pull request for this branch, using your commits for its title and body.

```sh
gh pr create --fill <flags>
```

- `<flags>` Extra options
  - [ ] `--draft` draft
  - [ ] `--web` finish in the browser

## Merge pull request
> [!WARNING]
> Merges a pull request on GitHub.

```sh
gh pr merge <pr> <how> <flags>
```

- `<pr>` Which pull request
- `<how>` How
  - ( ) `--merge` merge commit
  - ( ) `--rebase` rebase
  - (x) `--squash` squash
- `<flags>` Extra options
  - [ ] `--delete-branch` delete its branch too

## My PRs and reviews
Shows your open pull requests and the ones waiting for your review.

```sh
gh pr status
```

## Open PR in browser
Opens this branch's pull request on GitHub.

```sh
gh pr view --web
```
