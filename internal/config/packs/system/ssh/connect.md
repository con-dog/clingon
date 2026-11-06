# Connect
Connect to a host and tunnel a port

## Connect to host
Opens a shell on a remote machine from your SSH config.

```sh
ssh <host>
```

- `<host>` Which host

## Tunnel a port
Makes a port on a remote machine reachable on your own localhost, until you press Ctrl-C.

```sh
ssh -N -L <local>:localhost:<remote> <host>
```

- `<host>` Which host
- `<local>` Local
- `<remote>` Which remote
