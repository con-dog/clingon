---
when: .git
---
# Tags
List, create, push and delete tags

## Delete a tag
> [!WARNING]
> Deletes a tag here. It stays on the remote if you've pushed it.

```sh
git tag -d <tag>
```

- `<tag>` Which tag

## List tags
Lists the 30 newest tags.

```sh
git tag --sort=-creatordate --format='%(creatordate:short)  %(refname:short)' | head -n 30
```

## New tag
Tags the commit you're on, e.g. for a release.

```sh
git tag -a <tag> -m "<message>"
```

- `<tag>` Which tag
- `<message>` Message

## Push tags
> [!WARNING]
> Pushes every tag you have to origin.

```sh
git push origin --tags
```
