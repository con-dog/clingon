# Processes
See the top CPU and memory users, uptime and load, and stop a running program

## Top memory users
Lists the 15 processes using the most memory.

```sh
ps aux | sort -rnk 4 | head -n 15
```

## Top CPU users
Lists the 15 processes using the most CPU.

```sh
ps aux | sort -rnk 3 | head -n 15
```

## Stop a running program
> [!WARNING]
> Stops every running copy of a program.

```sh
pkill -x <name> || echo "No running program called "<name>"."
```

- `<name>` Name

## Uptime and load
Shows how long this machine has been up and how busy it is.

```sh
uptime
```
