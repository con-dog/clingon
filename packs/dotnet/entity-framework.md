---
when: ./*.csproj, ./*.fsproj, ./*.sln, ./*.slnx
---
# Entity Framework
Add, list, apply and remove EF Core migrations, make a SQL script and drop the database

## Add a migration
Writes a migration for the model changes since the last one.

```sh
dotnet ef migrations add <name>
```

- `<name>` Name

## Drop the database
> [!CAUTION]
> Deletes the database and all its data.

```sh
dotnet ef database drop
```

## List migrations
Lists the migrations, marking those not applied to the database yet.

```sh
dotnet ef migrations list
```

## Migrate to a migration
> [!CAUTION]
> Moves the database to a given migration, undoing any after it, which can delete columns and their data.

```sh
dotnet ef database update <migration>
```

- `<migration>` Migration

## Remove the last migration
> [!WARNING]
> Deletes the newest migration's files, if it isn't applied to the database yet.

```sh
dotnet ef migrations remove
```

## SQL script
Prints the SQL for every migration, safe to run on a database at any version.

```sh
dotnet ef migrations script --idempotent
```

## Update the database
> [!WARNING]
> Applies every migration the database doesn't have yet.

```sh
dotnet ef database update
```
