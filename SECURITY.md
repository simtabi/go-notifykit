# Security Policy

Where this file is silent, the [Simtabi security policy](https://github.com/simtabi/.github/blob/HEAD/SECURITY.md) applies.

## Reporting a vulnerability

Report it privately through GitHub's
[private vulnerability reporting](https://github.com/simtabi/go-notifykit/security/advisories/new)
for this repository. If you cannot use GitHub, email **security@simtabi.com**.
Do not open a public issue.

Include what you found and its impact, steps to reproduce, and the version and
platform. We aim to acknowledge reports within a few business days and will keep
you informed until a fix ships. We are glad to credit you unless you prefer not.

## Scope

Of most interest:

- the ping channel (`Listen`, `Ping`): reaching it as another user, bypassing the
  token, or making it do more than show a notification;
- any way notification text could run a command, change a script, or escape its
  field in a backend;
- the macOS helper app: its assembly, signing and registration.

How these are meant to be protected is described in [docs/security.md](docs/security.md).

## Supported versions

Security fixes target the latest released minor version.
