---
when: go.mod
---
# Build and test
Build, run, test, benchmark, format and vet Go code

## Benchmarks
Runs the benchmarks in every package, skipping the ordinary tests.

```sh
go test -run '^$' -bench . ./...
```

## Build
Compiles every package to check it builds. With a single main package here, it also writes that program's binary.

```sh
go build ./...
```

## Format
> [!WARNING]
> Formats every Go file here the standard way, listing the ones it changed.

```sh
gofmt -l -w .
```

## Run
Builds and runs the program in this folder.

```sh
go run .
```

## Test
Runs the tests in every package.

```sh
go test <flags> ./...
```

- `<flags>` Extra options
  - [ ] `-count=1` don't use cached results
  - [ ] `-cover` coverage
  - [ ] `-race` race detector
  - [ ] `-v` verbose

## Vet
Checks the code for likely mistakes the compiler doesn't catch.

```sh
go vet ./...
```
