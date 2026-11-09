# SDKs and tools
See installed SDKs and environment info, manage global tools and trust the dev certificate

## Check for updates
Shows which installed SDKs and runtimes are out of date or out of support.

```sh
dotnet sdk check
```

## Info
Shows which SDK is in use here, and every SDK and runtime installed.

```sh
dotnet --info
```

## Install a global tool
> [!WARNING]
> Installs a .NET tool for every project.

```sh
dotnet tool install --global <tool>
```

- `<tool>` Tool

## Install the EF Core tool
> [!WARNING]
> Installs the dotnet ef command that Entity Framework migrations need.

```sh
dotnet tool install --global dotnet-ef
```

## Installed SDKs
Lists every .NET SDK installed and where.

```sh
dotnet --list-sdks
```

## Trust the dev certificate
> [!WARNING]
> Makes a local HTTPS certificate and trusts it, so https://localhost works without warnings.

```sh
dotnet dev-certs https --trust
```

## Update a global tool
> [!WARNING]
> Updates a global .NET tool to its latest version.

```sh
dotnet tool update --global <tool>
```

- `<tool>` Tool
