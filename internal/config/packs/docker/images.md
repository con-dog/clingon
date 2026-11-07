# Images
Build, pull, list and run images, check disk usage and prune unused data

## Run image
Starts a throwaway container from an image and drops you into it. It's deleted when you exit.

```sh
docker run --rm -it <flags> <image> <command>
```

- `<image>` Which image
- `<flags>` Extra options
  - [ ] `-v "$PWD":/work -w /work` mount current dir
  - [ ] `--network host` host network
  - [ ] `--entrypoint sh` override entrypoint
- `<command>` Command

## Build image
Builds an image from the Dockerfile in this folder.

```sh when=./Dockerfile
docker build -t <tag> .
```

- `<tag>` Which tag

## Prune unused data
> [!CAUTION]
> Deletes every stopped container, unused image, network and build cache to free disk space.

```sh
docker system prune -af <volumes>
```

- `<volumes>` Volumes
  - [ ] `--volumes` anonymous volumes too

## Disk usage
Shows how much disk space images, containers, volumes and build cache take up.

```sh
docker system df
```

## Pull image
Downloads an image from its registry.

```sh
docker pull <image>
```

- `<image>` Which image

## List images
Lists the images on this machine with their tags and sizes.

```sh
docker images
```
