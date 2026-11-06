# Look around
List files, see a folder tree, read, follow and compare files, and count lines of code

## List files
Lists everything in a folder, hidden files included, with sizes, dates and permissions.

```sh
ls -lah <flags> <dir>
```

- `<flags>` Extra options
  - [ ] `-t` newest first
  - [ ] `-S` biggest first
  - [ ] `-R` subfolders too
- `<dir>` Which folder
  - (x) `.` this folder

## Folder tree
Shows the folders and files below a folder as an indented tree, skipping hidden ones and node_modules.

```sh
find <dir> -maxdepth <depth> -not -path '*/.*' -not -path '*/node_modules*' | sort | sed 's|[^/]*/|  |g'
```

- `<dir>` Which folder
  - (x) `.` this folder
- `<depth>` Depth
  - (x) `2`
  - ( ) `3`
  - ( ) `1`

## Recently changed files
Lists files changed recently, handy for finding what a build or a tool just wrote.

```sh
find <dir> -type f -mmin -<minutes> -not -path '*/.git/*' -not -path '*/node_modules/*'
```

- `<minutes>` Minutes
  - (x) `30` in the last half hour
  - ( ) `60` in the last hour
  - ( ) `1440` in the last day
- `<dir>` Which folder
  - (x) `.` this folder

## Peek at a file
Prints the first lines of a file.

```sh
head -n <lines> <file>
```

- `<file>` Which file
- `<lines>` How many lines
  - (x) `40`

## Read a file
Opens a file to scroll and search through. Press q to quit, / to search.

```sh
less <file>
```

- `<file>` Which file

## Follow a file
Prints new lines as they're added to a file, like a log. Ctrl-C stops.

```sh
tail -f <file>
```

- `<file>` Which file

## Compare two files
Shows the lines that differ between two files.

```sh
diff -u <a> <b>
```

- `<a>` first file
- `<b>` second file

## Count lines of code
Counts lines in every file of one type and lists the 20 biggest, with the total.

```sh
find . -type f -name '*.'<ext> -not -path '*/node_modules/*' -not -path '*/.git/*' -print0 | xargs -0 wc -l | sort -n | tail -n 20
```

- `<ext>` file type
