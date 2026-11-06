# Find
Find files by name and search for text inside them

## Find files by name
Finds files and folders whose name contains some text, skipping node_modules and .git.

```sh
find <dir> -iname '*'"<pattern>"'*' -not -path '*/node_modules/*' -not -path '*/.git/*'
```

- `<pattern>` What to look for
- `<dir>` Which folder
  - (x) `.` this folder

## Search text in files
Searches inside files for some text and shows every match with its line number.

```sh
grep -rn <flags> -- "<pattern>" <dir> || echo "No matches."
```

- `<pattern>` What to look for
- `<flags>` Extra options
  - [ ] `-i` ignore case
  - [ ] `-w` whole words
  - [ ] `-l` file names only
  - [ ] `--exclude-dir=node_modules --exclude-dir=.git` skip node_modules and .git
- `<dir>` Which folder
  - (x) `.` this folder
