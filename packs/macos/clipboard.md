# Clipboard
Show the clipboard, copy a file into it and paste it into a file

## Copy file contents
Puts a file's text on the clipboard.

```sh
pbcopy < <file>
```

- `<file>` Which file

## Paste into a file
> [!CAUTION]
> Writes what's on the clipboard to a file, replacing what's in it.

```sh
pbpaste > <file>
```

- `<file>` Which file

## Show the clipboard
Prints what's on the clipboard.

```sh
pbpaste
```
