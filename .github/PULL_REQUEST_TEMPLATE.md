## What

<!-- What does this change, in one or two sentences? -->

## Why

<!-- What problem does this solve, or what roadmap item does it advance? -->

## Checklist

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` is clean
- [ ] `staticcheck ./...` is clean
- [ ] `go test -race ./...` passes
- [ ] `make coverage` passes (per-package floor in `check_coverage.py` — see [AGENTS.md](../AGENTS.md)'s 95%+ hard rule)
- [ ] New logic has tests written TDD-style (failing test first)
- [ ] Cross-compiles on Windows and Linux if touching OS-specific code:
      `GOOS=windows GOARCH=amd64 go build ./...` / `GOOS=linux GOARCH=amd64 go build ./...`

## Roadmap

<!-- If this relates to a docs/roadmap/items/ entry, link it here. Otherwise delete this section. -->
