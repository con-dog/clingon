# Connect
Connect to Redis locally, to a server or with a URL, and check that it's up

## Connect
Opens a prompt on the local Redis server.

```sh
redis-cli
```

## Connect to a server
Opens a prompt on any Redis server.

```sh
redis-cli -h <host> -p <port> <flags>
```

- `<host>` Which host
  - (x) `127.0.0.1`
- `<port>` Which port
  - (x) `6379`
- `<flags>` Extra options
  - [ ] `--askpass` ask for the password
  - [ ] `--tls` connect over TLS

## Connect with a URL
Opens a prompt on any Redis server, from a redis:// URL. The URL is a secret: it never shows on screen or in your history.

```sh
redis-cli --no-auth-warning -u <secret-url>
```

- `<secret-url>` Url

## Is it up
Answers PONG if the Redis server is running.

```sh
redis-cli ping
```
