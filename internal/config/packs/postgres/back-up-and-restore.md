# Back up and restore
Dump one or every database as data, SQL or schema, and restore a dump

## Dump a database
> [!WARNING]
> Writes a whole PostgreSQL database to one file that pg_restore can load. The file holds all its data, so keep it safe.

```sh
pg_dump -Fc <database> -f <file>
```

- `<database>` Which database
- `<file>` Which file

## Dump as SQL
> [!WARNING]
> Writes a whole PostgreSQL database out as plain SQL that psql can run. The file holds all its data, so keep it safe.

```sh
pg_dump <database> -f <file>
```

- `<database>` Which database
- `<file>` Which file

## Dump the schema only
> [!WARNING]
> Writes a database's tables, indexes and functions out as SQL, without any rows.

```sh
pg_dump --schema-only <database> -f <file>
```

- `<database>` Which database
- `<file>` Which file

## Dump every database
> [!WARNING]
> Writes every database on the server, plus its users, out as SQL. The file holds all its data, so keep it safe.

```sh
pg_dumpall -f <file>
```

- `<file>` Which file

## Restore a dump
> [!CAUTION]
> Loads a pg_dump file into a database, dropping and replacing whatever is in the dump.

```sh
pg_restore --clean --if-exists -d <database> <file>
```

- `<database>` Which database
- `<file>` Which file

## Restore as a new database
> [!CAUTION]
> Creates the database a pg_dump file came from, under its old name, and loads it.

```sh
pg_restore --create --exit-on-error -d postgres <file>
```

- `<file>` Which file
