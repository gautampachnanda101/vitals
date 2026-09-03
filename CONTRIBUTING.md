# Contributing to vitals

Thanks for considering a contribution. This file is the quick-start;
[AGENTS.md](AGENTS.md) is the full, authoritative guide (originally
written for AI coding agents, but it's the same rules for everyone —
read it before making non-trivial changes).

## Quick start

```sh
git clone https://github.com/gautampachnanda101/vitals.git
cd vitals
make hooks-install   # runs gofmt/vet/staticcheck/race-test/coverage before every commit
make build           # ./vitals
make race            # go test -race ./...
```

Go 1.x per [go.mod](go.mod); no other tooling required. One runtime
dependency in the whole project (`gopsutil`) — see AGENTS.md's "Don't add
a dependency" before proposing a new one.

## Before opening a PR

- **TDD**: write the failing test first, confirm it fails for the right
  reason, then implement. This repo's whole test suite was built this
  way.
- **95%+ coverage of a package's pure/testable logic is a hard rule**,
  not a target — see AGENTS.md's "Testing conventions" for what counts
  as genuinely exempt (blocking loops, subprocess exec, OS-level reads)
  versus what should be extracted and tested. `check_coverage.py`
  enforces a floor per package; it only ratchets up.
- Run the full local gate before pushing:
  ```sh
  gofmt -l .              # must print nothing
  go vet ./...
  staticcheck ./...
  go test -race ./...
  make coverage
  ```
  `make hooks-install` wires all of this into `git commit` automatically.
- Cross-compile check for the two platforms macOS development won't
  catch bugs on:
  ```sh
  GOOS=windows GOARCH=amd64 go build ./...
  GOOS=linux   GOARCH=amd64 go build ./...
  ```
- Keep commits small and working — one tested increment per commit, not
  a whole feature bundled into one.

## Reporting bugs / requesting features

Open a [GitHub issue](https://github.com/gautampachnanda101/vitals/issues/new/choose).
For a security vulnerability, see [SECURITY.md](SECURITY.md) instead —
please don't file it as a public issue.

## Code of Conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
