//go:build !darwin

package notifykit

// helperBinary is empty off macOS, so other builds carry no notifier.
var helperBinary []byte
