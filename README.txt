██╗      ██████╗ ██╗  ██╗████████╗
██║     ██╔═══██╗██║ ██╔╝╚══██╔══╝
██║     ██║   ██║█████╔╝    ██║
██║     ██║   ██║██╔═██╗    ██║
███████╗╚██████╔╝██║  ██╗   ██║
╚══════╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝

LOKT — SIMPLE LOCKS FOR RISKY CLI OPS
====================================

Lokt is a tiny CLI that serializes high-risk operations (deploys, migrations,
terraform apply, release cuts) across multiple terminals/agents on the same
machine — without trusting the LLM to "remember".

It uses atomic lockfiles under your repo's shared git worktree directory.

INSTALL
-------

  curl -fsSL https://raw.githubusercontent.com/nikolasavic/lokt/main/scripts/install.sh | sh

Or specify a version:

  LOKT_VERSION=v0.3.0 curl -fsSL .../install.sh | sh

Supported platforms: macOS (amd64, arm64), Linux (amd64, arm64)

CORE COMMANDS
-------------

  lokt lock <name> [--ttl 30m] [--wait] [--timeout 10m]
      Acquire a named lock.

  lokt unlock <name> [--break-stale] [--force]
      Release a lock (owner-checked by default).

  lokt status [<name>] [--json] [--prune-expired]
      Show held locks + metadata. --prune-expired removes expired locks
      while listing.

  lokt why <name> [--json]
      Explain why a lock cannot be acquired. Shows holder info, freeze
      status, and staleness details.

  lokt guard <name> [--ttl 30m] [--wait] [--timeout 10m] -- <cmd...>
      Acquire lock, run command, release lock (even on failure).
      With --ttl, a background heartbeat auto-renews the lock at TTL/2
      intervals so long-running commands don't lose their lock.

FREEZE SWITCH
-------------

Temporarily block an operation for a fixed window (e.g. during an incident
or maintenance). Guard commands for a frozen lock are rejected immediately.

  lokt freeze <name> --ttl 15m
      Create a time-limited freeze. TTL is required.

  lokt unfreeze <name> [--force]
      Remove the freeze early. Owner-checked by default.

Guard auto-prunes expired freezes, so a forgotten freeze won't block forever.
Status shows [FROZEN] labels on frozen locks.

AUDIT LOG
---------

All lock operations emit events to an append-only JSONL file at
<root>/audit.log. Each lifecycle (acquire through release) shares a
lock_id for easy correlation and filtering.

  lokt audit [--since 1h] [--name deploy] [--tail]
      Query the audit log. --since accepts Go durations (1h, 30m) or
      RFC3339 timestamps. --tail follows in real-time (like tail -f).

Event types: acquire, deny, release, force-break, stale-break,
auto-prune, corrupt-break, renew, freeze, unfreeze, force-unfreeze,
freeze-deny.

HEALTH CHECK
------------

  lokt doctor [--json]
      Pre-flight validation of your lokt setup. Checks:
      - Directory writability
      - Network filesystem detection (NFS/CIFS warning)
      - Clock sanity
      Exit code 1 on critical failure, 0 otherwise.

DEMO
----

Generate and run a visual concurrency demo. The demo spawns workers that
race to build a hex wall — with and without locking — so you can see what
happens when concurrent processes share state without coordination.

  lokt demo
      Generate the hexwall demo script in the current directory.

  ./lokt-hexwall-demo.sh              # with locking (clean)
  ./lokt-hexwall-demo.sh --no-lock    # without locking (chaos)
  ./lokt-hexwall-demo.sh --help       # all options

See DEMO.md for a detailed walkthrough of how the demo works.

VERSION
-------

  lokt version
      Print version, git commit, and build date.

RELIABILITY
-----------

Lokt includes several self-healing behaviors that require no user action:

  Heartbeat renewal
      When guard runs with --ttl, a background goroutine renews the lock
      at TTL/2 intervals. Long builds won't lose their lock.

  Auto-prune dead PIDs
      If a lock is held by a process that has crashed (dead PID on same
      host), it is silently removed on the next acquire attempt.

  Corrupted lock handling
      Malformed JSON in lock files is treated as stale. Corrupted files
      are auto-removed during acquire, and removable via --break-stale
      or --force during release.

EXAMPLES
--------

  # serialize deploys
  lokt guard deploy --ttl 30m -- ./scripts/deploy.sh

  # serialize terraform apply
  lokt guard tf-apply --ttl 60m -- terraform apply -auto-approve

  # wait for a busy lock (with 10 minute timeout)
  lokt guard release --ttl 30m --wait --timeout 10m -- ./cut_release.sh

  # stop deploys for 15 minutes (incident / focus window)
  lokt freeze deploy --ttl 15m

  # check who has the lock
  lokt status deploy

  # review recent lock activity
  lokt audit --since 1h

  # validate setup before first use
  lokt doctor

ENVIRONMENT VARIABLES
---------------------

  LOKT_ROOT         Override lock root directory (default: auto-discovered
                    from git common dir or .lokt/ in cwd)
  LOKT_OWNER        Override owner identity in lock files (default: OS user)
  LOKT_VERSION      Pin install script to specific version (default: latest)
  LOKT_INSTALL_DIR  Override install directory (default: ~/.local/bin)

EXIT CODES
----------

  0   Success
  1   General error
  2   Lock held by another owner (or frozen)
  3   Lock not found
  4   Not lock owner

PHILOSOPHY
----------

- shared state lives outside worktrees (git common dir) so all terminals agree
- locks are enforceable (wrappers/scripts), not "social"
- keep it boring, reliable, and fast
