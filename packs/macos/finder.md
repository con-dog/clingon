# Finder
Show or hide hidden files, open this folder in Finder and trust a downloaded app

## Hide hidden files
> [!WARNING]
> Stops Finder showing files that start with a dot. Finder restarts to pick it up.

```sh
defaults write com.apple.finder AppleShowAllFiles -bool false && killall Finder
```

## Open this folder in Finder
Shows the shell's folder in a Finder window.

```sh
open .
```

## Show hidden files
> [!WARNING]
> Makes Finder show files that start with a dot. Finder restarts to pick it up.

```sh
defaults write com.apple.finder AppleShowAllFiles -bool true && killall Finder
```

## Trust a downloaded app
> [!WARNING]
> Removes the quarantine mark MacOs puts on downloads, so an app opens without the 'can't be opened' warning. Only do it for apps you trust.

```sh
xattr -dr com.apple.quarantine <app>
```

- `<app>` App
