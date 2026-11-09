---
when: ./*.csproj, ./*.fsproj
---
# User secrets
Start using Dotnet user secrets, and list, set and remove them

## List secrets
Shows the project's development secrets.

```sh
dotnet user-secrets list
```

## Remove a secret
> [!WARNING]
> Deletes one development secret.

```sh
dotnet user-secrets remove <key>
```

- `<key>` Which key

## Set a secret
Saves a development secret outside the project, so it never gets committed. The value is a secret: it never shows on screen or in your history.

```sh
dotnet user-secrets set <key> <secret-value>
```

- `<key>` Which key
- `<secret-value>` Value

## Start using secrets
Gives the project a UserSecretsId so it can keep development secrets.

```sh
dotnet user-secrets init
```
