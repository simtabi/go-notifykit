# Release

go-notifykit is a library: a release is a pair of git tags, and there are no binaries to publish.

## Tags

Each release is tagged twice on the same commit: `vX.Y.Z`, and `src/vX.Y.Z`, which
is the tag Go resolves for a module in `src/`. Tags are final: a public tag is
cached by the Go module proxy for good, so it is never moved, deleted or reused.
Test with `vX.Y.Z-rc.N`; fix a bad release with the next version and a `retract`.

```bash
git tag -a v0.1.0 -m v0.1.0
git tag -a src/v0.1.0 -m src/v0.1.0
git push origin v0.1.0 src/v0.1.0
```

## Before tagging

1. Move `## [Unreleased]` in `CHANGELOG.md` into a dated `## [X.Y.Z]` section.
2. `make check` (format, vet, tests, helper source hash) and `make lint`.
3. On a Mac, `make try` and look at the banner: it should say `notifykit-try` with
   a blue icon.

## Rebuilding the macOS helper

After any change to `src/macos/notifier.swift`:

```bash
make helper    # needs Xcode or the Command Line Tools
```

It rebuilds `src/macos/notifier` as one universal binary and records the source
hash; `make helper-check` and `TestHelperIsBuiltFromSource` fail when the two
disagree. Commit the binary and the hash together with the source change.

---

[← Docs index](../README.md#documentation)
