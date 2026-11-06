# Manage
Create databases and users, disconnect everyone, and drop tables, databases and users

## New database
> [!WARNING]
> Creates an empty PostgreSQL database.

```sh
createdb <name>
```

- `<name>` Name

## New user
> [!WARNING]
> Creates a PostgreSQL user that can log in, asking for its password.

```sh
createuser --pwprompt <name>
```

- `<name>` Name

## Disconnect everyone
> [!CAUTION]
> Ends every session connected to a database, so it can be dropped or renamed.

```sh
psql -d postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '"<database>"' AND pid <> pg_backend_pid()"
```

- `<database>` Which database

## Drop a table
> [!CAUTION]
> Deletes a table and every row in it.

```sh
psql -d <database> -c 'DROP TABLE '<table>
```

- `<database>` Which database
- `<table>` Which table

## Drop a database
> [!CAUTION]
> Deletes a PostgreSQL database and everything in it.

```sh
dropdb <flags> <database>
```

- `<database>` Which database
- `<flags>` Extra options
  - [ ] `--force` disconnect anyone using it first (PostgreSQL 13+)

## Drop a user
> [!CAUTION]
> Deletes a PostgreSQL user. It fails while the user still owns anything.

```sh
dropuser <name>
```

- `<name>` Name
