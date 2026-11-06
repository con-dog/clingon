# Volumes
List and inspect Docker volumes, and remove one or all unused ones

## List volumes
Lists Docker's volumes, where containers keep data that outlives them.

```sh
docker volume ls
```

## Remove a volume
> [!CAUTION]
> Deletes a volume and every file in it.

```sh
docker volume rm <volume>
```

- `<volume>` Which volume

## Remove unused volumes
> [!CAUTION]
> Deletes every volume no container is using, and every file in them.

```sh
docker volume prune -af
```

## Volume details
Shows where a volume lives and how it was made.

```sh
docker volume inspect <volume>
```

- `<volume>` Which volume
