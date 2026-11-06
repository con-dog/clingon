# HTTP
Check headers and status, time requests, download files, POST JSON, serve a folder and find your public IP

## Response headers
Fetches just a URL's response headers: status, redirects, caching and so on.

```sh
curl -sSI <url>
```

- `<url>` Url

## Status code only
Requests a URL and prints only the HTTP status code.

```sh
curl -o /dev/null -sS -w '%{http_code}\n' <url>
```

- `<url>` Url

## Time a request
Times a request: DNS lookup, connecting, first byte and total.

```sh
curl -o /dev/null -sS -w 'dns %{time_namelookup}s  connect %{time_connect}s  ttfb %{time_starttransfer}s  total %{time_total}s\n' <url>
```

- `<url>` Url

## Download a file
> [!WARNING]
> Downloads a file into this folder, following redirects.

```sh
curl -fLO <url>
```

- `<url>` Url

## POST some JSON
> [!WARNING]
> Sends JSON to a URL in a POST request and prints the response.

```sh
curl -sS -X POST -H 'Content-Type: application/json' -d <json> <url>
```

- `<url>` Url
- `<json>` Json
  - (x) `{}`

## Serve this folder
> [!WARNING]
> Serves the files in this folder over HTTP, to this machine only, until you press Ctrl-C.

```sh needs=python3
python3 -I -m http.server --bind 127.0.0.1 <port>
```

- `<port>` Which port
  - (x) `8000`

## Serve this folder on your network
> [!WARNING]
> Serves the files in this folder over HTTP until you press Ctrl-C. Anyone on your network can read them while it runs.

```sh needs=python3
python3 -I -m http.server <port>
```

- `<port>` Which port
  - (x) `8000`

## My public IP
Shows the public IP address the internet sees you coming from.

```sh
curl -s https://ifconfig.me && echo
```
