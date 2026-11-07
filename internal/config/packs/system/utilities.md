# Utilities
Generate secrets and UUIDs, print the time, and encode or decode Base64

## Random secret
Prints a random secret, good for API keys, tokens and passwords.

```sh needs=openssl
openssl rand -base64 <bytes> | tr -d '\n'; echo
```

- `<bytes>` Bytes
  - (x) `32`
  - ( ) `16`
  - ( ) `64`

## New UUID
Prints a new random UUID.

```sh needs=uuidgen
uuidgen
```

## Time now (ISO)
Prints the current time in UTC, in ISO 8601 format.

```sh
date -u +%Y-%m-%dT%H:%M:%SZ
```

## Time now (Unix)
Prints the current Unix time: seconds since 1 January 1970.

```sh
date +%s
```

## Base64 encode
Encodes some text as base64.

```sh
printf %s "<text>" | base64
```

- `<text>` Text

## Base64 decode
Decodes base64 back into text.

```sh
printf %s "<text>" | base64 --decode; echo
```

- `<text>` Text
