# Snapshots and flushing
Save a snapshot, and empty one database or delete everything

## Save a snapshot
> [!WARNING]
> Downloads a snapshot of the whole Redis database to a file. It holds all its data, so keep it safe.

```sh
redis-cli --rdb <file>
```

- `<file>` Which file

## Save on the server
> [!WARNING]
> Tells the server to write its snapshot file to disk now, in the background.

```sh
redis-cli bgsave
```

## Empty a database
> [!CAUTION]
> Deletes every key in one Redis database.

```sh
redis-cli -n <db> flushdb
```

- `<db>` Db
  - (x) `0`

## Delete everything
> [!CAUTION]
> Deletes every key in every database on the Redis server.

```sh
redis-cli flushall
```
