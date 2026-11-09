# Cleanup and permissions
Delete files matching a pattern and make files executable

## Delete files matching
> [!CAUTION]
> Permanently deletes every file matching a pattern, listing each one as it goes.

```sh
find <dir> -type f -name "<pattern>" -print -delete
```

- `<pattern>` What to look for
- `<dir>` Which folder
  - (x) `.` this folder

## Make executable
Makes a file runnable as a program.

```sh
chmod +x <file>
```

- `<file>` Which file
