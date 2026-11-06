# Setup
Clone or start a repo, and set your name, email, editor, commit signing and recommended settings

## Clone a repo
Downloads a repository and its history into a new folder here.

```sh
git clone <url>
```

- `<url>` Url

## Start a repo here
Turns this folder into a new Git repository.

```sh
git init
```

## Prune on fetch
Fetch forgets branches deleted on the remote, in every repository.

```sh
git config --global fetch.prune true
```

## Start new repos on main
New repositories start on a branch called main.

```sh
git config --global init.defaultBranch main
```

## Rebase on pull
Pull rebases your commits instead of making a merge commit.

```sh
git config --global pull.rebase true
```

## Set up tracking on first push
The first push of a new branch sets up tracking, so plain git push works.

```sh
git config --global push.autoSetupRemote true
```

## Remember conflict fixes
Git remembers how you fixed a conflict and reuses the fix next time.

```sh
git config --global rerere.enabled true
```

## Set your email
Sets the email address recorded on your commits. Use the one your Git host knows you by.

```sh
git config --global user.email <email>
```

- `<email>` Email

## Set your name
Sets the name recorded on your commits.

```sh
git config --global user.name <name>
```

- `<name>` Name

## Set your editor
Sets the editor Git opens for commit messages and rebases.

```sh
git config --global core.editor <editor>
```

- `<editor>` Editor
  - ( ) `'code --wait'` VS Code
  - ( ) `micro`
  - (x) `nano`
  - ( ) `nvim`
  - ( ) `vim`

## Sign commits with SSH key
> [!WARNING]
> Signs every commit with your SSH key, so your Git host can show it as verified. Add the key there as a signing key too.

```sh
git config --global gpg.format ssh && git config --global user.signingkey <key> && git config --global commit.gpgsign true
```

- `<key>` Which key

## Your settings
Lists every global Git setting and the file it's in.

```sh
git config --global --list --show-origin
```
