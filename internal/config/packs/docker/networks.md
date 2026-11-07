# Networks
List and inspect Docker networks, and remove unused ones

## Inspect a network
Shows a network's settings and which containers are on it.

```sh
docker network inspect <network>
```

- `<network>` Which network

## List networks
Lists Docker's networks.

```sh
docker network ls
```

## Remove unused networks
> [!WARNING]
> Deletes every network no container is using.

```sh
docker network prune -f
```
