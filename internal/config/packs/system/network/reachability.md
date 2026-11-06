# Reachability
Ping a host, look up DNS, test a port, trace a route and find your local IP

## Ping a host
Checks that a host answers, and how quickly, with four pings.

```sh
ping -c 4 <host>
```

- `<host>` Which host

## DNS lookup
Looks up the IP addresses a domain name points to.

```sh needs=dig
dig +short <domain>
```

- `<domain>` Domain

## Is a port open
Checks whether you can connect to a port on another machine.

```sh needs=nc,ipconfig
nc -z -v -G 5 <host> <port>
```

- `<host>` Which host
- `<port>` Which port

## Trace the route
Shows each network hop between you and a host, and how long each takes.

```sh needs=traceroute
traceroute <host>
```

- `<host>` Which host

## My local IP
Shows this Mac's IP address on your local network.

```sh needs=ipconfig
ipconfig getifaddr "$(route -n get default | awk '/interface:/ { print $2 }')"
```
