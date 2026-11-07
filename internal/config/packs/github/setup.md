# Setup
Sign in to GitHub, check that you are, add an SSH key and use gh for git passwords

## Add SSH key to GitHub
> [!WARNING]
> Uploads your public SSH key so you can push and pull over SSH.

```sh
gh ssh-key add <key> --title "$(hostname)"
```

- `<key>` Which key

## Am I signed in
Shows which GitHub accounts you're signed in to and how.

```sh
gh auth status
```

## Sign in
Signs you in to GitHub through your browser, asking a few questions first.

```sh
gh auth login
```

## Use gh for git passwords
Lets git push and pull over HTTPS using your gh sign-in, so it never asks for a password.

```sh
gh auth setup-git
```
