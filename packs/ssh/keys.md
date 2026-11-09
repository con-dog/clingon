# Keys
Make a new SSH key, add it to the agent, show or copy your public key and copy it to a host

## Copy my key to host
> [!WARNING]
> Installs your public key on a host so you can log in without a password.

```sh
ssh-copy-id <host>
```

- `<host>` Which host

## New SSH key
Creates a new ed25519 SSH key pair in ~/.ssh.

```sh
ssh-keygen -t ed25519 -C <comment>
```

- `<comment>` Comment

## Add key to agent
Unlocks a key once and saves its passphrase in your keychain, so you aren't asked again.

```sh needs=ipconfig
ssh-add --apple-use-keychain <key>
```

- `<key>` Which key

## Copy my public key
Copies a public key to the clipboard, ready to paste into GitHub or a server's settings.

```sh needs=pbcopy
pbcopy < <key>
```

- `<key>` Which key

## Show my public key
Prints a public key, the half you give to GitHub and servers.

```sh
cat <key>
```

- `<key>` Which key
