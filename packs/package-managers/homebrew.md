---
needs: brew
---
# Homebrew
Search, install, uninstall, upgrade and clean up packages with Homebrew

## Search
Searches Homebrew for formulae and apps by name.

```sh
brew search "<query>"
```

- `<query>` Query

## Install
> [!WARNING]
> Installs a formula or app with Homebrew.

```sh
brew install <name>
```

- `<name>` Name

## Uninstall
> [!WARNING]
> Removes something you installed with Homebrew.

```sh
brew uninstall <name>
```

- `<name>` Name

## Info
Shows a package's version, dependencies and where it's installed.

```sh
brew info <name>
```

- `<name>` Name

## What is outdated
Refreshes Homebrew's package list and shows what has newer versions.

```sh
brew update && brew outdated
```

## Upgrade everything
> [!WARNING]
> Refreshes Homebrew and upgrades everything that's out of date.

```sh
brew update && brew upgrade
```

## Upgrade one
> [!WARNING]
> Upgrades one package to its latest version.

```sh
brew upgrade <name>
```

- `<name>` Name

## Installed (top level)
Lists the packages you installed yourself, leaving out ones that came in as dependencies.

```sh
brew leaves -r
```

## Clean up old versions
> [!WARNING]
> Deletes old versions and cached downloads to free disk space.

```sh
brew cleanup
```

## Check for problems
Checks your Homebrew setup for problems and suggests fixes.

```sh
brew doctor
```
