---
when: .git
---
# Releases
List and download releases, and publish a new one

## Download a release
> [!WARNING]
> Downloads a release's files into this folder.

```sh
gh release download <tag>
```

- `<tag>` Which tag

## List releases
Lists this repository's releases, newest first.

```sh
gh release list
```

## New release
> [!WARNING]
> Publishes a release on GitHub for a tag, with notes written from the pull requests since the last one.

```sh
gh release create <tag> --generate-notes <flags>
```

- `<tag>` Which tag
- `<flags>` Extra options
  - [ ] `--draft` draft
  - [ ] `--prerelease` pre-release
