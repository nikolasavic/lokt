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
machine — without trusting the LLM to “remember”.

It uses atomic lockfiles under your repo's shared git worktree directory.

INSTALL
-------

  curl -fsSL https://raw.githubusercontent.com/nikolasavic/lokt/main/scripts/install.sh | sh

Or specify a version:

  LOKT_VERSION=v1.0.0 curl -fsSL .../install.sh | sh

Environment variables:
  LOKT_VERSION      Pin to specific version (default: latest)
  LOKT_INSTALL_DIR  Override install directory (default: ~/.local/bin)

Supported platforms: macOS (amd64, arm64), Linux (amd64, arm64)

CORE COMMANDS
-------------

  lokt guard <name> -- <cmd...>
      Acquire lock, run command, release lock (even on failure).

  lokt lock <name> [--ttl 30m] [--wait] [--timeout 10m]
      Acquire a named lock.

  lokt unlock <name> [--break-stale] [--force]
      Release a lock (owner-checked by default).

  lokt status [<name>] [--json]
      Show held locks + metadata.

FREEZE SWITCH
-------------

  lokt freeze <name> [--ttl 15m]
      Temporarily block an operation (e.g. deploy) for a fixed window.

  lokt unfreeze <name> [--force]
      Remove the freeze early.

EXAMPLES
--------

  # serialize deploys
  lokt guard deploy --ttl 30m -- ./scripts/deploy.sh

  # serialize terraform apply
  lokt guard tf-apply --ttl 60m -- terraform apply -auto-approve

  # cut releases one-at-a-time
  lokt guard release --ttl 30m -- ./scripts/cut_release.sh

  # stop deploys for 15 minutes (incident / focus window)
  lokt freeze deploy --ttl 15m

PHILOSOPHY
----------

- shared state lives outside worktrees (git common dir) so all terminals agree
- locks are enforceable (wrappers/scripts), not “social”
- keep it boring, reliable, and fast

