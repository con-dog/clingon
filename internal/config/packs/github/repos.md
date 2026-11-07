# Repos
Clone your repos, open one in the browser and publish this folder

## Clone one of my repos
Downloads one of your repositories into a new folder here.

```sh
gh repo clone <repo>
```

- `<repo>` Which repo

## Open repo in browser
Opens this repository on GitHub in your browser.

```sh when=.git
gh browse
```

## Publish this folder
> [!WARNING]
> Creates a GitHub repository named after this folder and pushes your commits to it. Commit something first.

```sh when=.git
gh repo create --source . --remote origin --push <visibility>
```

- `<visibility>` Visibility
  - (x) `--private` private
  - ( ) `--public` public
