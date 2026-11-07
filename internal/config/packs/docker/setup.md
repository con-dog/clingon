# Setup
Start Docker, check that it works and log in to a registry

## Check it works
Runs Docker's tiny test image, which prints a message if everything is set up.

```sh
docker run --rm hello-world
```

## Log in to a registry
Signs you in to a registry so you can push images and pull private ones.

```sh
docker login <registry>
```

- `<registry>` Which registry

## Start Docker
Starts Docker Desktop. docker commands work once its whale icon stops animating.

```sh needs=open,ipconfig
open -a Docker
```
