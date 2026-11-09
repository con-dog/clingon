# Certificates
Read a site's or a file's certificate, check when it expires and make a self-signed one

## Site certificate
Shows who a site's TLS certificate is for, who issued it, and when it's valid.

```sh
openssl s_client -connect <host>:<port> -servername <host> </dev/null 2>/dev/null | openssl x509 -noout -subject -issuer -dates -text | grep -E '^(subject|issuer|notBefore|notAfter)|DNS:'
```

- `<host>` Which host
- `<port>` Which port
  - (x) `443`

## Read a certificate file
Prints everything in a certificate file.

```sh
openssl x509 -in <file> -noout -text
```

- `<file>` Which file

## Self signed certificate
> [!WARNING]
> Makes a certificate and key for local HTTPS. Files with the same names are replaced.

```sh
openssl req -x509 -newkey rsa:2048 -nodes -keyout <name>.key -out <name>.crt -days <days> -subj '/CN='<host> -addext 'subjectAltName=DNS:'<host>
```

- `<host>` Which host
  - (x) `localhost`
- `<name>` Name
  - (x) `localhost`
- `<days>` Days
  - (x) `365`

## When does it expire
Shows the date a site's TLS certificate runs out.

```sh
openssl s_client -connect <host>:443 -servername <host> </dev/null 2>/dev/null | openssl x509 -noout -enddate
```

- `<host>` Which host
