---
needs: jq
---
# JSON
Pretty-print a JSON file and pick out a field

## Pick out a field
Prints one value from a JSON file.

```sh
jq -r <path> <file>
```

- `<file>` Which file
- `<path>` Which path

## Pretty print a file
Shows a JSON file indented and coloured.

```sh
jq -C . <file> | less -R
```

- `<file>` Which file
