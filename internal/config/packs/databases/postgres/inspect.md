# Inspect
List databases, tables and users, describe a table, and see sizes and running queries

## List databases
Lists the databases on the local PostgreSQL server.

```sh
psql -l
```

## Database sizes
Lists every database with how much disk it uses, biggest first.

```sh
psql -d postgres -c 'SELECT datname, pg_size_pretty(pg_database_size(datname)) AS size FROM pg_database ORDER BY pg_database_size(datname) DESC'
```

## List tables
Lists the tables in a database with their sizes.

```sh
psql -d <database> -c '\dt+'
```

- `<database>` Which database

## Describe a table
Shows a table's columns, types, indexes and constraints.

```sh
psql -d <database> -c '\d+ '<table>
```

- `<database>` Which database
- `<table>` Which table

## Running queries
Lists the queries running right now, longest first, with the pid to cancel them by.

```sh
psql -d postgres -c "SELECT pid, usename, datname, state, now() - query_start AS running_for, query FROM pg_stat_activity WHERE NOT (state = 'idle' OR pid = pg_backend_pid()) ORDER BY running_for DESC"
```

## List users
Lists the users (roles) on the server and what they can do.

```sh
psql -d postgres -c '\du'
```
