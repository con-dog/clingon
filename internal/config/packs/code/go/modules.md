# Modules
Start a module, add, update and tidy dependencies, see why one is needed and install tools

## Add a dependency
Adds a module to go.mod, or changes the version you use.

```sh when=go.mod
go get <module>
```

- `<module>` Which module

## Install a tool
Builds a command and puts it in $(go env GOPATH)/bin.

```sh
go install <package>
```

- `<package>` Which package

## Start a module
Creates a go.mod here, making this folder a Go module.

```sh
go mod init <module>
```

- `<module>` Which module

## Tidy
Adds missing requirements to go.mod and removes unused ones.

```sh when=go.mod
go mod tidy
```

## Update dependencies
> [!WARNING]
> Updates every dependency to its newest minor or patch version.

```sh when=go.mod
go get -u ./... && go mod tidy
```

## Why is this a dependency
Shows which of your packages pulls a module in.

```sh when=go.mod
go mod why -m <module>
```

- `<module>` Which module
