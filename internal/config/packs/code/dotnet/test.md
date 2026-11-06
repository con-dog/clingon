---
when: ./*.csproj, ./*.fsproj, ./*.sln, ./*.slnx
---
# Test
Run .NET tests, all of them or just those matching a filter, once or on every change

## Test
Builds and runs every test project.

```sh
dotnet test
```

## Test matching
Runs only the tests that match a filter.

```sh
dotnet test --filter <filter>
```

- `<filter>` Filter

## Watch tests
Runs the tests again whenever you save a file.

```sh
dotnet watch test
```
