# Connect
Connect to PostgreSQL locally, to a server or with a URL, and check that it's up

## Connect
Opens a psql prompt on a local database. \q leaves it.

```sh
psql <database>
```

- `<database>` Which database

## Connect to a server
Opens a psql prompt on any PostgreSQL server, asking for the password if it needs one.

```sh
psql -h <host> -p <port> -U <user> -d <database>
```

- `<host>` Which host
  - (x) `localhost`
- `<port>` Which port
  - (x) `5432`
- `<user>` Which user
  - (x) `postgres`
- `<database>` Which database
  - (x) `postgres`

## Connect with a URL
Opens a psql prompt on any database, from a postgres:// URL. The URL is a secret: it never shows on screen or in your history.

```sh
psql <secret-url>
```

- `<secret-url>` Url

## Is it up
Says whether a PostgreSQL server is accepting connections.

```sh
pg_isready -h <host> -p <port>
```

- `<host>` Which host
  - (x) `localhost`
- `<port>` Which port
  - (x) `5432`
