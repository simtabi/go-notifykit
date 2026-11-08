//go:build darwin

package notifykit

import _ "embed"

// helperBinary is the universal (arm64 and x86_64) notifier built from
// macos/notifier.swift by `make helper`. Only macOS builds carry it.
//
//go:embed macos/notifier
var helperBinary []byte
