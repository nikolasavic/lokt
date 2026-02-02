# L-202: `lokt doctor` health check

status: draft
created: 2026-01-28
backlog-ref: docs/backlog.md

## Verification
- Level: optional
- Environments: sandbox

---

## Problem

Users need a way to verify their lokt setup is correct before relying on it for critical lock coordination. Common issues include:
- Lock directory not writable (permissions, disk full)
- Network filesystems that don't support atomic `O_EXCL` (NFS, CIFS)
- Clock skew that would break TTL-based staleness detection
- Missing git context when expecting `.git/lokt/` discovery

Without pre-flight validation, users discover these issues only when locks fail at critical moments (e.g., during a build that corrupts state).

## Users

- **DevOps engineer**: Needs to verify lokt will work correctly in CI/CD pipelines before deploying. Wants a single command that returns 0 if safe, non-zero if not.
- **Developer**: Wants to understand why lokt isn't working as expected. Needs diagnostic output explaining what's wrong and how to fix it.
- **CI system**: Needs machine-readable output to integrate health checks into pipeline validation.

## Requirements

1. **Check directory writability**: Verify the lock directory exists (or can be created) and is writable. Test with actual file creation/deletion.
2. **Detect network filesystems**: Warn if the lock directory is on NFS, CIFS, or other network filesystems where atomic operations aren't guaranteed.
3. **Check clock sanity**: Verify system clock is reasonable (not obviously wrong - e.g., year < 2020 or far in future). TTL-based staleness depends on accurate timestamps.
4. **Report root discovery**: Show which root discovery method was used (LOKT_ROOT, git, .lokt/) and the resolved path.
5. **Return appropriate exit code**: Exit 0 if all critical checks pass, non-zero if any fail. Warnings (like clock skew) may not be fatal.
6. **Support --json output**: Machine-readable format for CI integration.

## Non-Goals

- **Fix problems**: `doctor` is diagnostic only. It reports issues but doesn't attempt remediation.
- **Network filesystem blocking**: L-204 handles refusing/warning on network FS. `doctor` should detect and report, not enforce policy.
- **Cross-host clock sync**: Detecting clock skew across multiple hosts is out of scope. Only local sanity check.
- **Performance benchmarking**: Not measuring lock acquisition speed or contention.

## Acceptance Criteria

- [ ] **Writability check**: Given a valid root directory, when `lokt doctor` runs, then it creates a test file, writes to it, and removes it successfully, reporting "OK" for writability.
- [ ] **Network FS detection**: Given a lock directory on NFS/CIFS, when `lokt doctor` runs, then it reports a warning about network filesystem with explanation of why this is problematic.
- [ ] **Clock sanity**: Given a system with reasonable clock, when `lokt doctor` runs, then it reports "OK" for clock check. Given year < 2020 or > 2100, it reports warning.
- [ ] **Root discovery reporting**: Given various root discovery scenarios (LOKT_ROOT env, git repo, .lokt/ fallback), when `lokt doctor` runs, then it reports which method was used and the resolved path.
- [ ] **Exit codes**: Given all checks pass, exit 0. Given any critical failure (e.g., not writable), exit 1. Given warnings only, exit 0 with warnings printed.
- [ ] **JSON output**: Given `lokt doctor --json`, when run, then output is valid JSON with check names, status, and messages.

## Edge Cases

- **No root found**: If neither LOKT_ROOT, git, nor .lokt/ provides a valid root, doctor should report this clearly (the directory doesn't need to exist yet, but the discovery method should be reported).
- **Permission denied on parent**: If user can't create .lokt/ due to parent permissions, writability check should fail gracefully with clear message.
- **Disk full**: Writability check should handle disk full errors distinctly from permission errors.
- **Symlinked directories**: If lock directory is a symlink to NFS mount, detection should follow symlinks.
- **Docker/container**: Clock skew checks should work in containers where system time might differ from host.

## Constraints

- **No new dependencies**: Use stdlib only for filesystem detection. May use `stat` syscall or `/proc/mounts` parsing on Linux.
- **Cross-platform**: Must work on macOS, Linux, and Windows. Network FS detection may vary by platform.
- **Idempotent**: Running doctor multiple times should not change system state (clean up test files).
- **Fast**: Should complete in under 1 second for local filesystems.

---

## Notes

### Related tickets
- L-203: Handle corrupted lock files gracefully (defensive parsing)
- L-204: Warn/refuse on network filesystems (enforcement, whereas doctor is detection)

### Network FS detection approaches
- Linux: Parse `/proc/mounts` or use `statfs` syscall to check `f_type`
- macOS: Use `statfs` or check `f_fstypename`
- Windows: Check drive type with GetDriveType or similar

### Clock sanity thresholds
- Warning if year < 2020 (lokt didn't exist)
- Warning if year > 2100 (likely misconfigured)
- Could also check if system time is way off from file mtime on freshly created test file

### Example output (text mode)
```
lokt doctor

Root:        .git/lokt (via git common dir)
Path:        /home/user/project/.git/lokt

Checks:
  [OK] Directory writable
  [WARN] Network filesystem detected (NFS)
        Atomic file operations may not be reliable
  [OK] Clock sanity

Result: PASS with warnings
```

### Example output (JSON mode)
```json
{
  "root_method": "git",
  "root_path": "/home/user/project/.git/lokt",
  "checks": [
    {"name": "writable", "status": "ok"},
    {"name": "network_fs", "status": "warn", "message": "NFS detected"},
    {"name": "clock", "status": "ok"}
  ],
  "overall": "warn"
}
```

---

**Next:** Run `/kickoff L-202` to promote to Beads execution layer.
