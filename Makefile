# go-notifykit is a library: there is no binary to build or release. The Go
# module lives in src/, so every go command names it (a bare `go test ./...`
# at the root finds no packages and passes).

GO ?= go

.PHONY: check fmt fmt-check vet test race lint helper helper-check try

check: fmt-check vet test helper-check

fmt:
	gofmt -w src

fmt-check:
	@out=$$(gofmt -l src); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) -C src vet ./...

test:
	$(GO) -C src test ./... -count=1 -timeout 120s

race:
	$(GO) -C src test ./... -race -count=1 -timeout 180s

lint:
	cd src && golangci-lint run ./...

# helper rebuilds the embedded macOS notifier from src/macos/notifier.swift as
# one universal binary, and records the source hash that `make helper-check`
# and TestHelperIsBuiltFromSource compare. Needs Xcode or the Command Line
# Tools; macOS only. Intermediates go to .helper-build/ (git-ignored).
HELPER_MIN := 11.0
HELPER_TMP := .helper-build
helper:
	@test "$$(uname -s)" = Darwin || { echo "make helper needs macOS"; exit 1; }
	@mkdir -p $(HELPER_TMP)
	xcrun swiftc -O -target arm64-apple-macos$(HELPER_MIN)  src/macos/notifier.swift -o $(HELPER_TMP)/notifier-arm64
	xcrun swiftc -O -target x86_64-apple-macos$(HELPER_MIN) src/macos/notifier.swift -o $(HELPER_TMP)/notifier-x86_64
	xcrun lipo -create $(HELPER_TMP)/notifier-arm64 $(HELPER_TMP)/notifier-x86_64 -output $(HELPER_TMP)/notifier
	xcrun strip -x $(HELPER_TMP)/notifier
	mv $(HELPER_TMP)/notifier src/macos/notifier
	rm -f $(HELPER_TMP)/notifier-arm64 $(HELPER_TMP)/notifier-x86_64
	@shasum -a 256 src/macos/notifier.swift | cut -d' ' -f1 > src/macos/notifier.swift.sha256
	@echo "rebuilt src/macos/notifier ($$(wc -c < src/macos/notifier | tr -d ' ') bytes)"

# helper-check fails when notifier.swift changed without `make helper`.
helper-check:
	@want=$$(cat src/macos/notifier.swift.sha256); have=$$(shasum -a 256 src/macos/notifier.swift | cut -d' ' -f1); \
	  if [ "$$want" != "$$have" ]; then echo "src/macos/notifier.swift changed: run make helper"; exit 1; fi

# try posts one real notification from a throwaway "notifykit-try" app, so you
# can see the banner, its name and its icon. macOS asks once to allow it.
try:
	NOTIFYKIT_DESKTOP=1 $(GO) -C src run ./internal/try $(ARGS)
