# Disk
See free space, folder sizes and the biggest things here

## Free space
Shows how full each disk is.

```sh
df -h
```

## Biggest things here
Lists the biggest files and folders here, largest first.

```sh
find . -mindepth 1 -maxdepth 1 -exec du -sh {} + 2>/dev/null | sort -rh | head -n <count>
```

- `<count>` How many
  - (x) `15`

## Folder size
Shows how much space a folder takes up, everything inside it included.

```sh
du -sh <dir>
```

- `<dir>` Which folder
