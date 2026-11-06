# Security policy

## Reporting a vulnerability

Open a **private security advisory** on GitHub (Security → Report a vulnerability).
Please include reproduction steps and affected versions (`leetd version`).
We aim to respond within 72 hours.

## Model (short version)

- Transport: mTLS on all node traffic; membership via one-time enrollment
  secret → CA-signed node certificates. Rogue nodes are rejected at the
  handshake.
- At rest: documents are stored as readable HTML on disk today. Field-level
  AES-256-GCM (D2) is implemented in the codebase but not yet applied to the
  store; until it ships, **volume encryption (Cryptomator/VeraCrypt/FileVault)
  is the operator's layer and strongly recommended**. The store directory and
  node identity files are permission-restricted (`0600`/`0700`).
- Audit: every change is an attributed, timestamped git commit.
- **100% local by design** — the daemon makes no outbound connections except
  to nodes you enrolled with and (optionally) a local Ollama.
  The one documented exception is a **user-initiated update**: Settings →
  **check for update** / **install**, or `leetd update [--apply]`. Those
  clicks talk to GitHub Releases, verify the SHA-256 in `checksums-*.txt`,
  and (on install) replace this binary. There is no startup ping, no
  timer, and no background check.

## Scope

The daemon, store format, sync/merge, and clients are in scope. Reports about
the mDNS announcement being visible on your LAN are expected behavior
(discovery is unauthenticated by protocol; enrollment is not).
