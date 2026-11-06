# Keys
Find, count and read keys, set expiries, delete keys, and find the biggest ones

## Find keys
Lists keys matching a pattern, without blocking the server like KEYS does.

```sh
redis-cli --scan --pattern "<pattern>"
```

- `<pattern>` What to look for
  - (x) `*`

## Count keys
Says how many keys are in the current database.

```sh
redis-cli dbsize
```

## Get a value
Prints the value of a string key.

```sh
redis-cli get <key>
```

- `<key>` Which key

## Key type and expiry
Says what type a key is, then how many seconds it has left (-1 means it never expires).

```sh
redis-cli type <key> && redis-cli ttl <key>
```

- `<key>` Which key

## Set an expiry
> [!WARNING]
> Makes a key delete itself after a number of seconds.

```sh
redis-cli expire <key> <seconds>
```

- `<key>` Which key
- `<seconds>` Seconds
  - (x) `3600`

## Delete a key
> [!CAUTION]
> Deletes a key and its value for good.

```sh
redis-cli unlink <key>
```

- `<key>` Which key

## Biggest keys
Scans every key and reports the biggest of each type, without blocking the server.

```sh
redis-cli --bigkeys
```

## Keys using most memory
Scans every key and reports the ones using the most memory.

```sh
redis-cli --memkeys
```
