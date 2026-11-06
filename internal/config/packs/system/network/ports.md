# Ports
See what's listening, find who's on a port and kill it

## What is listening
Lists your programs listening for network connections, and on which port. Other users' programs only show with sudo.

```sh
lsof -nP -iTCP -sTCP:LISTEN
```

## Who is on a port
Shows which of your programs is listening on a port. Other users' programs only show with sudo.

```sh
lsof -nP -iTCP:<port> -sTCP:LISTEN || echo "Nothing of yours is listening on port <port>."
```

- `<port>` Which port

## Kill whatever is on port
> [!WARNING]
> Stops whichever of your programs is listening on a port.

```sh
pids=$(lsof -t -iTCP:<port> -sTCP:LISTEN); if [ -n "$pids" ]; then echo "$pids" | xargs kill && echo "Stopped what was on port <port>."; else echo "Nothing of yours is listening on port <port>."; fi
```

- `<port>` Which port
