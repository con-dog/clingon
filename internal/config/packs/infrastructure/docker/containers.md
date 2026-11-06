# Containers
List, inspect, restart, stop and remove containers, follow logs, shell in and copy files

## Running containers
Lists running containers with their image, status and ports.

```sh
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
```

## Shell into container
Opens an interactive shell inside a running container.

```sh
docker exec -it <container> <shell>
```

- `<container>` Which container
- `<shell>` Which shell
  - (x) `sh`
  - ( ) `bash`
  - ( ) `zsh`

## Follow logs
Streams a container's logs live as they're written.

```sh
docker logs -f --tail <lines> <container>
```

- `<container>` Which container
- `<lines>` How many lines
  - (x) `100`
  - ( ) `1000`
  - ( ) `all`

## Restart container
> [!WARNING]
> Stops a container and starts it again, keeping its configuration.

```sh
docker restart <container>
```

- `<container>` Which container

## Remove container
> [!CAUTION]
> Deletes a container for good.

```sh
docker rm <flags> <container>
```

- `<container>` Which container
- `<flags>` Extra options
  - [ ] `-f` force (stop if running)
  - [ ] `-v` also remove anonymous volumes

## Stop container
> [!WARNING]
> Stops a running container. It keeps its data and can be started again.

```sh
docker stop <container>
```

- `<container>` Which container

## Copy a file out
> [!WARNING]
> Copies a file or folder out of a container. Anything already at the destination is replaced.

```sh
docker cp <container>:<path> <dest>
```

- `<container>` Which container
- `<path>` Which path
- `<dest>` Copy to
  - (x) `.` this folder

## Copy a file in
> [!WARNING]
> Copies a file into a container. Anything already at that path is replaced.

```sh
docker cp <file> <container>:<path>
```

- `<file>` Which file
- `<container>` Which container
- `<path>` Which path

## Inspect container
Shows everything Docker knows about a container: its config, mounts, network and state.

```sh
docker inspect <container>
```

- `<container>` Which container

## Container stats
Shows each running container's CPU, memory, network and disk use right now.

```sh
docker stats --no-stream
```
