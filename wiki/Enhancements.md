# Enhancement Backlog — Top 10 Feature Requests

Proposed 2026-10-29 after the security audit and debug session. Each item
is grounded in a spec decision (REQUIREMENTS D-numbers), an audit finding,
or a documented honest limit in the README ("What it isn't (yet)").

Priority legend: 🔥 now · ⏭ next · 💤 later. Effort: S ≤1 day, M ~1 week,
L multi-week.

---

## 1. 🔥 Authenticated LAN access (M28, D21–D24)

**Problem.** LAN mode is fail-closed today (`StartAtPath` refuses
`lan.enabled`) because the loopback UI and `/mcp` have no request
authentication. Remote teammates must run a full node to see anything.

**Proposal.** Implement the decided model end-to-end: per-agent random API
tokens over HTTPS (token ⇒ actor, callers cannot self-label — D21/D22),
individual local human accounts (D23), and a fully managed local
certificate authority for HTTPS with zero manual certificate steps (D24).
Then lift the fail-closed gate behind this transport.

**Acceptance.** A browser on another machine signs in with a local account
and sees the workspace; an agent on an enrolled device presents its token
and is attributed correctly; revoking a token blocks that agent
immediately; `lan.enabled` starts a real node.

**Effort:** L · **Refs:** REQUIREMENTS O7, D20–D24 · [audit F4]

## 2. 🔥 Member revocation & certificate lifecycle

**Problem.** Node certificates are valid 10 years and there is no
revocation: Settings lists accepted members but offers no remove. A lost
laptop is a permanent team member; the only reset is re-creating the team
CA. Enrollment also has no rate limiting, and first-join relies on TOFU
(no fingerprint pin).

**Proposal.** Coordinator-side revocation list checked at the mTLS layer;
"remove member" in Settings (cert expires ≤24 h); enrollment rate
limiting + lockout; optional CA-fingerprint pin surfaced in the join
wizard; cert rotation flow.

**Acceptance.** A removed member's sync attempts fail at the handshake;
revocation propagates to all nodes within one sync cadence; Settings shows
cert expiry and revocation state.

**Effort:** M · **Refs:** [audit residual 2, 3, 5]

## 3. ⏭ Field-level encryption at rest (D2)

**Problem.** `store/encrypt.go` implements AES-256-GCM correctly — and
nothing calls it. Documents sit as plaintext HTML in `~/LeetOffice`; disk
encryption is the only protection today, and the "encrypted" story is
transport-only.

**Proposal.** Mark sensitive doc types/properties (contacts, emails,
task assignees) for field encryption; derive the key from the node
identity into the OS keychain (macOS Keychain / libsecret / DPAPI);
encrypted fields stay unindexed by RAG (per D17) and render after
unlock. Encrypt-at-rest must not break block-level merge — encrypt
values, merge structure.

**Acceptance.** With the keychain locked, `strings` over the store files
leaks nothing sensitive; merge and search behave as before for plaintext
docs; upgrade path encrypts on next write.

**Effort:** M · **Refs:** D2, D17

## 4. ⏭ Structured collections & saved views (M27, D20)

**Problem.** Docs/tasks are flat lists. Teams need a lightweight database:
custom properties per collection, filter/sort/group, saved views — the
largest gap vs. the Notion-style vision, and the spec's own lean (O8) for
what ships first.

**Proposal.** A collection = a doc type with a property schema (typed:
text/number/date/select/actor/doc-link); properties stored in block
`meta`/doc `properties` so the store format, sync, and merge stay
untouched; views (table/board) rendered by the UI over `/api/state`.
Agents get `create_task`-style tools per collection via MCP.

**Acceptance.** Define a collection, add rows with custom properties,
save a filtered view; two nodes edit different rows concurrently and merge
cleanly; agents can create/query rows via MCP.

**Effort:** L · **Refs:** M27, D20, O8

## 5. ⏭ Richer block editor & page hierarchy (M26, D20)

**Problem.** The doc editor is one `<textarea>` per block; no nesting,
drag-reorder, or markdown shortcuts; pages have no parent/child
hierarchy.

**Proposal.** Client-side block editor (contenteditable or a small
library) writing back through the same `/doc/<slug>` POST → same store
format; block nesting via parent IDs in `meta`; page hierarchy = parent
doc link + breadcrumb UI. Markdown import/export stays lossless via the
embedded JSON.

**Acceptance.** Reorder/nest blocks without page reload; hierarchy shows
in Docs list; concurrent edits of *different* blocks still merge cleanly
(D6 holds).

**Effort:** L · **Refs:** M26, D20

## 6. ⏭ SQLite-backed vector index & hybrid search

**Problem.** The RAG index is in-memory (README honest limit): rebuilt on
start, no persistence, Ollama-only embeddings; keyword fallback is
scanning. On big stores, search latency and memory grow unbounded.

**Proposal.** Persist an embedding index (SQLite + sqlite-vec or a pure-Go
ANN store — keep the zero-cgo rule!) under `~/.leetoffice-identity/` or
the store; incremental updates keyed on the existing store fingerprint;
hybrid ranking (keyword + vector); pluggable embedder (Ollama today,
local ONNX later).

**Acceptance.** Restart does not rebuild from scratch; search returns in
<50 ms on a 10k-block store; `go build` still produces a static,
cgo-free binary.

**Effort:** M · **Refs:** D17, README limits

## 7. ⏭ Live updates: SSE for humans, push for agents

**Problem.** The chat shell polls `/api/state` every 2.5 s and agents poll
`inbox`; there is no push channel. Latency is fine locally but noisy
(mDNS/`cachedPeople` churn, battery) and slow for multi-node chat.

**Proposal.** Server-Sent Events endpoint (`/api/events`) fanned from
store commits (the daemon already knows when `CommitAll`/sync lands);
UI switches from polling to SSE with poll fallback; optional webhook
registration for agents (`subscribe` gains a `webhook_url`) — still
loopback/LAN-only behind #1's auth when remote.

**Acceptance.** A message from a teammate appears in <500 ms of sync
landing, without polling traffic; offline clients catch up via the
existing inbox cursor.

**Effort:** M · **Refs:** M17, M25, D19

## 8. 💤 Backup, restore & `leetd doctor`

**Problem.** The team CA prints "keep it backed up" — and that's it. No
export/import, no scheduled backup of store + identity + config, no
health check beyond `leetd check`'s store self-test. The audit/debug
session showed how test runs can mutate a real store; a good backup story
is the safety net for humans too.

**Proposal.** `leetd backup` (tar of store + identity + config, optional
passphrase via the D2 keyring), `leetd restore`, scheduled local backups
(user-chosen dir — still zero egress), and `leetd doctor`: config
validation, cert expiry report, remote reachability, git fsck, store
integrity, disk space.

**Acceptance.** Backup → wipe `~/LeetOffice` → restore → node rejoins its
team with the same identity; doctor flags an expiring cert and a broken
remote before they hurt.

**Effort:** M · **Refs:** D8, §7.3

## 9. 💤 Signed, notarized desktop distribution + Windows auto-start

**Problem.** The Electron wrapper is "build it yourself with
`npm run dist`" and Windows has no service auto-start (README honest
limits). Non-technical adopters on both platforms hit friction and
Gatekeeper/SmartScreen warnings.

**Proposal.** Sign + notarize the macOS app in CI (Developer ID + stapled
ticket), produce an MSI/NSIS installer with a Windows service
(`svc`-style helper, mirroring launchd/systemd in `internal/daemon/
service.go`), and publish both next to the binaries with the same
checksums-file pattern.

**Acceptance.** macOS app launches without right-click-bypass; Windows
installer registers the auto-start service; release artifacts carry
checksums + signatures.

**Effort:** M · **Refs:** D14, README limits

## 10. 💤 Roles & per-channel permissions

**Problem.** Every enrolled member is omnipotent: full read/write on the
single main share, every channel, every doc. Teams (and especially agent
fleets) need least privilege — an agent that can post to `#ops` should
not necessarily read `#hr`.

**Proposal.** Role model (admin / member / agent) with per-channel and
per-collection ACLs stored in a system doc (so it syncs and is audited
like everything else); enforced at the MCP tool layer and the UI API —
the git layer stays per-node (trust boundary is the cert, per D8), so
ACLs are honored-merge rather than crypto-enforced until #1 ships;
agent tokens (D22) carry their role.

**Acceptance.** An agent restricted to `#ops` gets denied `send_message`
elsewhere with a clear MCP error; an admin sees ACL changes in History
with attribution.

**Effort:** L · **Refs:** D8, D22, M25

---

## Shipped (moved from backlog)

- *2026-10-29* — LAN fail-closed gate, CSRF guard for the loopback UI,
  slug sanitization, XSS escapes, enrollment hardening, dependency
  upgrades (see [Security Audit](Security-Audit-2026-10-29.md)). These
  were audit remediations rather than backlog items, listed here for the
  record.
---

## Engineering improvements — see also

The 2026-11 release-readiness passes produced a second, engineering-focused
backlog recorded as **proposed FRs** (FR-012…FR-029 — hermetic test homes,
`leetd status`/healthz, deep `check`, bounded audit walks, sync interlock,
identity backups, enrollment throttling, fingerprint pinning, security
headers, single-instance lock, log rotation, CLI write path, license
inventory, fsnotify watching, markdown round-trip, config schema versions,
path-validator precision): see the
[FR Registry](FR-Registry.md#backlog--from-the-2026-11-release-readiness-passes) —
FR-012, FR-014, FR-017, FR-020, and FR-029 were implemented and verified on
2026-11-04.

