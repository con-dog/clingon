# Setup
Add a host to your SSH config and test your GitHub login

## Add a host
> [!WARNING]
> Adds a machine to your SSH config under a short name, which ssh and the host lists here then know.

```sh
mkdir -p ~/.ssh && printf '\nHost %s\n  HostName %s\n  User %s\n' <alias> <hostname> <user> >> ~/.ssh/config
```

- `<alias>` Alias
- `<hostname>` Hostname
- `<user>` Which user

## Test GitHub login
Checks that GitHub accepts your SSH key. It greets you by username if it does.

```sh
ssh -T git@github.com
```
