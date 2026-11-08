# Contributing

Where this file is silent, the [Simtabi contributing guide](https://github.com/simtabi/.github/blob/HEAD/CONTRIBUTING.md) applies.

## Development

```bash
git clone https://github.com/simtabi/go-notifykit ~/Artisan/projects/opensource/simtabi/packages/go-notifykit
cd ~/Artisan/projects/opensource/simtabi/packages/go-notifykit
make check     # gofmt, go vet, tests, and the macOS helper's source hash
make lint      # golangci-lint
make try       # on a Mac: post one real banner from a throwaway app
```

The module lives in `src/`; every `go` command names it (`go -C src test ./...`).
The ping tests open a Unix socket, so run them where binding a socket is allowed.

## The macOS helper

`src/macos/notifier` is built from `src/macos/notifier.swift`. After changing the
Swift source, run `make helper` on a Mac with Xcode or the Command Line Tools and
commit the binary, the source and `notifier.swift.sha256` together.
`TestHelperIsBuiltFromSource` fails otherwise.

## Conventions

- `gofmt`, `go vet` and golangci-lint clean; errors wrapped with `%w`.
- Commands are built by pure functions and tested by their exact arguments;
  user text always travels as data (argv, stdin or environment), never as script.
- Tests never show a real banner: inject a `Recorder` or the package's seams.
- A guard lands with the behaviour it guards, and is shown failing once.

## Pull requests

- Subject of 72 characters or fewer, imperative mood; explain *why* in the body.
- Add or update tests, and add a line under `## [Unreleased]` in `CHANGELOG.md`.
