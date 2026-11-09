# Releasing

Releases are fully automated: push a version tag and GitHub Actions builds
everything with GoReleaser (`.goreleaser.yaml`).

## One-time setup

1. Create an empty **public** repo `con-dog/homebrew-tap`. This is where the formula goes.
2. Create a fine-grained personal access token with **Contents: read and write**
   on `con-dog/homebrew-tap` only.
3. In `con-dog/clingon` → Settings → Secrets and variables → Actions, add it as
   `HOMEBREW_TAP_TOKEN`.

## Each release

```sh
git tag v0.1.0
git push origin v0.1.0
```

That produces:

- a GitHub release with macOS amd64/arm64 tarballs and checksums (the formula downloads these)
- `Formula/clingon.rb` in the tap, so `brew install con-dog/tap/clingon` works on macOS (the only platform clingon supports)

Homebrew is the only way clingon is distributed. The formula depends on everything clingon needs (tmux), so a brew install is never missing anything. If clingon starts running a new program, add it to `brews.dependencies` in `.goreleaser.yaml`.

Try a release locally without publishing: `goreleaser release --snapshot --clean --skip=publish` (output in `dist/`; check the formula with `brew style dist/homebrew/Formula/clingon.rb`).

## Notes

- `goreleaser check` warns that `brews` is deprecated in favour of casks. This is deliberate.
  Unsigned binaries in casks get quarantined by Gatekeeper.
- Once the project is popular enough for homebrew-core (roughly 75+ stars and some
  outside users), submitting it there makes it a plain `brew install clingon`.
