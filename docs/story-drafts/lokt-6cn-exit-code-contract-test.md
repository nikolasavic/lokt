# lokt-6cn: Exit Code Contract Test (Table-Driven)

## Problem

Exit codes are a critical part of lokt's CLI contract — other tools and scripts depend on specific codes (0, 1, 2, 3, 4, 64) to distinguish between success, held-by-other, not-found, not-owner, and usage errors. Existing tests cover individual scenarios but there's no single comprehensive test that documents and enforces the full exit code contract across all commands.

## Requirements

1. Create a table-driven test covering every exit code path for: lock, unlock, guard, freeze, unfreeze
2. Each test case specifies: command, args, fixture setup, expected exit code
3. Test serves as living documentation of the exit code contract
4. Catches regressions if CLI error handling is refactored

## Acceptance Criteria

- [ ] Table-driven test covers all 6 exit codes (0, 1, 2, 3, 4, 64)
- [ ] Covers commands: lock, unlock, status, guard, freeze, unfreeze
- [ ] Each scenario has a descriptive name (e.g., "lock/held-by-other")
- [ ] Corrupt lockfile scenario produces ExitError (1)
- [ ] Frozen lock scenario produces ExitLockHeld (2)
- [ ] Not-found scenario produces ExitNotFound (3)
- [ ] Not-owner scenario produces ExitNotOwner (4)
- [ ] Usage error scenarios produce ExitUsage (64)
- [ ] All tests pass: `go test ./cmd/lokt/ -run TestExitCode`

## Edge Cases

- Corrupt JSON in lockfile → ExitError
- Expired lock still returns ExitLockHeld (lock file exists)
- guard with frozen lock → ExitLockHeld
- unlock not-owner without --force → ExitNotOwner

## Constraints

- Use existing test helpers: `setupTestRoot`, `captureCmd`, `writeLockJSON`
- Follow existing test patterns in cmd/lokt/*_test.go
- Single file: `cmd/lokt/exitcode_test.go`

## Verification

- Level: agent
- Environments: local
