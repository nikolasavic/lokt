# L-186/L-187: GitHub Release Pipeline

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md (L-186, L-187)

## Verification
- Level: required
- Environments: sandbox (test with pre-release tag)

---

## Problem

Currently, releasing lokt requires manual steps:
1. Run `./scripts/build.sh` locally
2. Copy binary to `/usr/local/bin/`
3. No cross-platform binaries (only builds for current OS)
4. No GitHub releases for users to download

Developers who want to use lokt must clone and build from source, which is friction that reduces adoption.

## Users

- **End users**: Need downloadable binaries for their platform (macOS, Linux) without building from source
- **Maintainers**: Want automated releases triggered by git tags, no manual build/upload steps
- **CI systems**: Need to pull lokt binary for use in pipelines

## Requirements

1. **CI workflow** for building and testing on push/PR (Linux + macOS)
2. **Release workflow** triggered by version tags (`v*`)
3. **Multi-platform binaries** via goreleaser (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64)
4. **GitHub Release** with binaries attached and auto-generated changelog
5. **Checksum file** for binary verification

## Non-Goals

- **Homebrew formula**: Future work, not this story
- **Windows support**: Not prioritized (no Windows stale detection anyway)
- **Docker image**: Overkill for a CLI tool
- **Signed binaries**: Nice to have, defer to future

## Acceptance Criteria

- [ ] **CI runs on PR**: Given a PR is opened, when CI runs, then tests pass on ubuntu-latest and macos-latest
- [ ] **CI runs on push to main**: Given code is pushed to main, when CI runs, then build + test succeeds
- [ ] **Release on tag**: Given a `v*` tag is pushed, when release workflow runs, then GitHub Release is created with binaries
- [ ] **Binaries work**: Given binaries are downloaded, when user runs `lokt version`, then correct version/commit/date shown
- [ ] **Checksums present**: Given a release exists, when user downloads checksums.txt, then SHA256 sums match binaries

## Edge Cases

- **Tag without v prefix** — Ignored, no release triggered
- **Failed tests on release** — Release aborted, no partial release
- **Concurrent releases** — goreleaser handles atomically

## Constraints

- **GitHub Actions**: Use GitHub-hosted runners (free for public repos)
- **goreleaser**: Industry standard, handles cross-compilation and release creation
- **Existing build.sh**: goreleaser should use same ldflags pattern for version injection

---

## Implementation Notes

### Files to create

```
.github/
  workflows/
    ci.yml          # Build + test on PR/push
    release.yml     # goreleaser on tag push
.goreleaser.yml     # goreleaser config
```

### goreleaser config sketch

```yaml
builds:
  - main: ./cmd/lokt
    binary: lokt
    ldflags:
      - -X main.version={{.Version}}
      - -X main.commit={{.ShortCommit}}
      - -X main.date={{.Date}}
    goos: [darwin, linux]
    goarch: [amd64, arm64]

archives:
  - format: tar.gz
    name_template: "lokt_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

release:
  github:
    owner: nikolasavic
    name: lokt
```

### CI workflow sketch

```yaml
name: CI
on: [push, pull_request]
jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'
      - run: go test ./...
      - run: go build ./cmd/lokt
```

---

**Next:** Run `/kickoff L-186` to promote to Beads execution layer.
