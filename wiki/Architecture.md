# Architecture

Verified against the code as of 2026-10-29 (v0.1.8 line, go 1.27.3).

## Topology

- **Coordinator** — holds the team CA (`<identity_dir>/ca/`), serves the
  bare share over `leet://` (git pack protocol inside mTLS), serves the
  enrollment endpoint, announces itself via mDNS (`_leetoffice._tcp`).
- **Client** — enrolls with the invite code, receives an mTLS certificate,
  syncs its store clone against `leet://<coordinator>:7666/main.git`.
- **Single-player** — no share, no networking; sync/jobs loops still run
  locally (digest, hygiene, memory synthesis).

The HTTP listener (UI + MCP-over-HTTP) is **loopback-only**. A non-loopback
bind is rejected by `config.Validate` unless `lan.enabled=true` is set, and
`daemon.StartAtPath` then refuses to start with
`LAN mode is not available in this build` — fail-closed ahead of the M28
authenticated-LAN work (REQUIREMENTS D20–D24).

## Package map

| Package | Owns | Key facts |
|---|---|---|
| `internal/store` | The workspace on disk | Docs are tabbed HTML + embedded JSON (`<script id="leet-doc">`); `ParseDoc` enforces the schema URL; `MergeDocs` is the three-way block-level merge (never silently overwrite, D6); slugs are sanitized into file names in `Path` |
| `internal/sync` | git + audit | Every write is a commit attributed to an actor (D3/D7); `leet://` transport; push/pull/merge cycle with conflict report |
| `internal/net` | mTLS, enrollment, mDNS, git server | Team CA (ed25519, 10-year certs) in `ca.go`; enrollment = one-time secret → CSR → node cert (constant-time compare, `D8`); `GitServer` speaks git pack inside TLS 1.3, repo paths stay inside the bare root; mDNS TXT carries node_id/role/fingerprint |
| `internal/mcp` | The agent surface | 12 JSON-RPC tools over stdio and HTTP; actor injected at construction (D7); `mark_read`/`subscribe` cursors persist in node.json |
| `internal/chat` | Team chat | Channels are docs, messages are blocks; inbox = mentions of `@agent:<id>` per subscription |
| `internal/memory` | Automations (M19) | Memory synthesis on store change (fingerprint-keyed, not HEAD-keyed), daily digest (`_audit/DIGEST-<day>.md`, committed), hourly hygiene + `NOTICE.md` |
| `internal/rag` | Search | Ollama semantic when reachable, keyword fallback always (D17) |
| `internal/httpui` | Human UI | Loopback web UI (chat shell, docs, audit, settings); `GuardMutations` blocks cross-site writes (Origin/Host match); updates are user-initiated only (P1) |
| `internal/update` | Updater | GitHub Releases only (host allowlist), SHA-256 checksums mandatory + GitHub asset digest cross-check, 80 MiB cap, atomic binary replace |
| `internal/daemon` | Composition | `ListenAndServe` → first-run wizard or `StartAtPath`; loops = sync ticker + jobs; `WaitLoops` drains background work after cancel |
| `internal/config` | node.json | Runtime-mutated fields (Actor, SyncEverySec, EnrollmentSecret, HiddenActors, subscriptions) are guarded by an internal RWMutex — use the accessors, not the fields |
| `internal/hostsign` | macOS ad-hoc signing | launchd kills unsigned/copied binaries; `Ensure` re-signs after update/install |
| `cmd/leetd` | CLI | `serve`, `mcp`, `enroll`, `sync`, `check`, `doc`, `audit`, `update`, `mcp-install`, `service` |

## Data flow of one edit

1. Human types in the chat shell or doc editor → HTTP POST →
   `store.Save` renders the doc HTML + refreshes `INDEX.md`.
2. `Repo.CommitAll(actor, msg)` — the git commit IS the audit entry (D3).
3. `KickSync` (chat/MCP writes) or the 5 s ticker → `Repo.Sync`:
   fetch → `MergeDocs` (block-level, conflicts flagged) → push.
4. Other nodes pull the same way; offline catch-up is one sync.
5. Jobs loop notices the fingerprint change → re-synthesizes MEMORY.md,
   writes the daily digest, runs hygiene.

## Trust model (see [Security Audit](Security-Audit-2026-10-29.md) for detail)

- **Loopback HTTP**: the UI and `/mcp` assume the local user; there is no
  request authentication yet. Cross-site browser writes are refused via
  Origin/Host checking; remote access is deliberately unavailable until
  D21–D24 (agent tokens + human accounts + managed certs) land.
- **LAN sync**: only CA-signed peers get past the mTLS handshake; the
  enrollment endpoint requires the one-time invite code; an empty secret
  can never open enrollment.
- **Updates**: only `api.github.com`/`github.com`/`*.githubusercontent.com`,
  checksums required, user-initiated only.
- **Known gap**: field-level encryption at rest (D2, `store/encrypt.go`)
  is implemented as a primitive but not wired into the store — docs on
  disk are plaintext HTML. Tracked in [Enhancements](Enhancements.md) #7.
