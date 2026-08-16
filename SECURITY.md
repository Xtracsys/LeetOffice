# Security policy

## Reporting a vulnerability

Email: security@leetoffice.dev (or open a private security advisory on GitHub).
Please include reproduction steps and affected versions (`leetd version`).
We aim to respond within 72 hours.

## Model (short version)

- Transport: mTLS on all node traffic; membership via one-time enrollment
  secret → CA-signed node certificates. Rogue nodes are rejected at the
  handshake.
- At rest: field-level AES-256-GCM for sensitive values; volume encryption
  (Cryptomator/VeraCrypt) is the operator's layer and recommended.
- Audit: every change is an attributed, timestamped git commit.
- **100% local by design** — the daemon makes no outbound connections except
  to nodes you enrolled with and (optionally) a local Ollama.

## Scope

The daemon, store format, sync/merge, and clients are in scope. Reports about
the mDNS announcement being visible on your LAN are expected behavior
(discovery is unauthenticated by protocol; enrollment is not).
