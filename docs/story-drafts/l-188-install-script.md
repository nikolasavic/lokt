# L-188: Install Script / Distribution

status: draft
created: 2026-01-27
backlog-ref: docs/backlog.md

## Verification
- Level: optional
- Environments: sandbox

---

## Problem

Users who want to try lokt must currently clone the repo and build from source. This friction prevents adoption. A simple `curl | sh` install script would let users get started in seconds, leveraging the existing GitHub Releases infrastructure (goreleaser already builds darwin/linux for amd64/arm64).

## Users

- **CLI user**: Wants to install lokt quickly without Go toolchain. Expects familiar `curl | sh` pattern.
- **CI/automation**: Needs scriptable install for pipelines. Expects exit codes and quiet mode.

## Requirements

1. Single install script hosted in repo that downloads the correct binary for OS/arch
2. Detect darwin/linux and amd64/arm64; fail clearly on unsupported platforms
3. Download from GitHub Releases (latest or specified version)
4. Verify checksum against `checksums.txt` from release
5. Install to user-writable location (default: `~/.local/bin` or `/usr/local/bin` with sudo)
6. Support `LOKT_INSTALL_DIR` env var override
7. Support `LOKT_VERSION` env var for pinned version installs

## Non-Goals

- Homebrew formula: future work, not this story
- Windows support: lokt is Unix-focused (file locking semantics differ)
- Automatic PATH modification: inform user, don't modify shell configs

## Acceptance Criteria

- [ ] **Basic install**: Given macOS arm64, when `curl -fsSL .../install.sh | sh`, then lokt binary appears in install dir and `lokt --version` works
- [ ] **Linux support**: Given Linux amd64, when install script runs, then correct binary downloaded
- [ ] **Version pin**: Given `LOKT_VERSION=v1.0.0`, when install script runs, then that specific version installed
- [ ] **Checksum verify**: Given download completes, when checksum mismatches, then script exits non-zero with clear error
- [ ] **Unsupported platform**: Given Windows or unsupported arch, when script runs, then clear error message and exit 1
- [ ] **Idempotent**: Given lokt already installed, when script runs again, then binary updated without errors

## Edge Cases

- GitHub rate limiting — detect 403/429 and suggest `GITHUB_TOKEN` or retry later
- Partial download — wrap in function to prevent truncated execution
- No curl/wget — check for download tool availability first
- No write permission — detect and suggest sudo or different install dir
- Checksum tool missing — try sha256sum, shasum -a 256, or openssl dgst

## Constraints

- Must work in POSIX sh (not just bash) for maximum compatibility
- Script should be auditable — keep it simple, no obfuscation
- GitHub releases URL pattern: `https://github.com/nikolasavic/lokt/releases/download/{tag}/lokt_{version}_{os}_{arch}.tar.gz`

---

## Notes

### Reference patterns
- Homebrew installer: function wrapping, OS detection, retry logic
- goreleaser naming: `lokt_{version}_{os}_{arch}.tar.gz`
- Checksums available at: `checksums.txt` in each release

### Security considerations
- Always use HTTPS
- Verify checksums
- Wrap script in function to prevent partial execution attacks
- Don't require sudo by default

### Install location precedence
1. `$LOKT_INSTALL_DIR` if set
2. `$HOME/.local/bin` if writable and in PATH
3. `/usr/local/bin` with sudo prompt

---

**Next:** Run `/kickoff L-188` to promote to Beads execution layer.
