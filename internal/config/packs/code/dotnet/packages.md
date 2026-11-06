---
when: ./*.csproj, ./*.fsproj, ./*.sln, ./*.slnx
---
# Packages
Add and remove NuGet packages, and find outdated or vulnerable ones

## Add a package
Adds a NuGet package to the project, at its latest version.

```sh
dotnet add package <package>
```

- `<package>` Which package

## Outdated packages
Lists the packages that have newer versions.

```sh
dotnet list package --outdated
```

## Remove a package
> [!WARNING]
> Takes a NuGet package out of the project.

```sh
dotnet remove package <package>
```

- `<package>` Which package

## Vulnerable packages
Lists the packages, direct or not, that have known security holes.

```sh
dotnet list package --vulnerable --include-transitive
```
