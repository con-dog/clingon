---
when: compose.yaml, compose.yml, docker-compose.yml, docker-compose.yaml
---
# Compose
Start, stop and rebuild Compose services, follow their logs and shell into one

## Up (detached)
> [!WARNING]
> Starts your Compose services in the background, building or pulling images if needed.

```sh
docker compose up -d <services>
```

- `<services>` Services

## Follow service logs
Streams one Compose service's logs live, starting from the last 100 lines.

```sh
docker compose logs -f --tail 100 <service>
```

- `<service>` Which service

## Down
> [!WARNING]
> Stops your Compose services and removes their containers and networks.

```sh
docker compose down <flags>
```

- `<flags>` Extra options
  - [ ] `--remove-orphans` remove orphan containers

## Down and delete volumes
> [!CAUTION]
> Stops your Compose services and deletes their containers, networks and volumes: any data in those volumes, like a database's, is gone.

```sh
docker compose down -v
```

## Status
Shows your Compose services' containers and whether they're running.

```sh
docker compose ps
```

## Rebuild and start
> [!WARNING]
> Rebuilds your Compose images from source, then starts the services in the background.

```sh
docker compose up -d --build <services>
```

- `<services>` Services

## Restart a service
> [!WARNING]
> Restarts one Compose service's containers without rebuilding them.

```sh
docker compose restart <service>
```

- `<service>` Which service

## Shell into a service
Opens a shell inside a running Compose service.

```sh
docker compose exec <service> sh
```

- `<service>` Which service
