# Archives and copy
Make and extract archives, copy with progress, move, duplicate and make shortcuts

## Make tarball
> [!WARNING]
> Packs a file or folder into a compressed .tar.gz archive.

```sh
tar -czf <name>.tar.gz <path>
```

- `<path>` Which path
- `<name>` Name

## Extract archive
> [!WARNING]
> Unpacks an archive into this folder.

```sh
tar -xf <archive>
```

- `<archive>` Archive

## Copy with progress
> [!WARNING]
> Copies files or folders with a progress bar. Run it again and it only copies what changed.

```sh
rsync -ah --progress <src> <dest>
```

- `<src>` Copy from
- `<dest>` Copy to

## Move or rename
Moves or renames a file or folder, asking before it overwrites anything.

```sh
mv -i <src> <dest>
```

- `<src>` Copy from
- `<dest>` Copy to

## Duplicate
Copies a file or folder to a new name, asking before it overwrites anything.

```sh
cp -Ri <src> <dest>
```

- `<src>` Copy from
- `<dest>` Copy to

## Make a shortcut
Creates a symbolic link: a shortcut that points at another file or folder.

```sh
ln -s <target> <link>
```

- `<target>` points at
- `<link>` shortcut name
