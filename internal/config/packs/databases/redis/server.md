# Server
See memory use, server info, live stats, latency, clients and slow queries, and watch every command

## Memory use
Shows how much memory Redis is using.

```sh
redis-cli info memory
```

## Server info
Shows the server's version, uptime, clients, persistence and stats.

```sh
redis-cli info <part>
```

- `<part>` Part
  - (x) `server`
  - ( ) `clients`
  - ( ) `persistence`
  - ( ) `stats`
  - ( ) `replication`
  - ( ) `keyspace`
  - ( ) `all`

## Live stats
Prints keys, memory, clients and requests every second until you press Ctrl-C.

```sh
redis-cli --stat
```

## Latency
Measures how long the server takes to answer, live, until you press Ctrl-C.

```sh
redis-cli --latency
```

## Watch every command
Prints every command the server runs as it happens, until you press Ctrl-C. It slows a busy server down.

```sh
redis-cli monitor
```

## Connected clients
Lists every client connected to the server, with its address and last command.

```sh
redis-cli client list
```

## Slow queries
Shows recent commands that took longer than the server's slow-log limit.

```sh
redis-cli slowlog get <count>
```

- `<count>` How many
  - (x) `10`
