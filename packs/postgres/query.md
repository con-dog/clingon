# Query
Run a query or SQL file, and save a query as CSV

## Run a query
> [!WARNING]
> Runs one SQL statement and prints the result.

```sh
psql -d <database> -c "<sql>"
```

- `<database>` Which database
- `<sql>` Sql

## Save a query as CSV
> [!WARNING]
> Runs a query and writes the result to a CSV file with a header row.

```sh
psql -d <database> --csv -c "<sql>" -o <file>
```

- `<database>` Which database
- `<sql>` Sql
- `<file>` Which file

## Run a SQL file
> [!CAUTION]
> Runs every statement in a SQL file, such as a plain pg_dump, stopping at the first error.

```sh
psql -d <database> -v ON_ERROR_STOP=1 -f <file>
```

- `<database>` Which database
- `<file>` Which file
