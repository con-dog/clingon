---
when: ./*.csproj, ./*.fsproj, ./*.sln, ./*.slnx
---
# Build and run
Restore, build, run, watch, format, clean and publish a .NET project

## Build
Restores packages and compiles the project or solution.

```sh
dotnet build
```

## Clean
> [!WARNING]
> Deletes what the last build made.

```sh
dotnet clean
```

## Format
> [!WARNING]
> Reformats the code to match the project's .editorconfig style.

```sh
dotnet format
```

## Publish
> [!WARNING]
> Builds the app ready to deploy, into bin/<config>/…/publish.

```sh
dotnet publish -c <config> -r <rid> <flags>
```

- `<config>` Config
  - (x) `Release`
  - ( ) `Debug`
- `<rid>` Rid
  - ( ) `linux-x64`
  - ( ) `linux-arm64`
  - ( ) `linux-musl-x64`
  - ( ) `osx-arm64`
  - ( ) `osx-x64`
  - ( ) `win-x64`
  - ( ) `win-arm64`
- `<flags>` Extra options
  - [ ] `--self-contained` include the .NET runtime
  - [ ] `-p:PublishSingleFile=true` single file

## Restore
Downloads the NuGet packages the project needs.

```sh
dotnet restore
```

## Run
Builds and runs the project in this folder.

```sh
dotnet run
```

## Watch and run
Runs the project and rebuilds or hot-reloads it whenever you save a file.

```sh
dotnet watch run
```
