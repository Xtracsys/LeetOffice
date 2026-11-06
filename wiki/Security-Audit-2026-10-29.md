# Security Audit — 2026-10-29

Scope: full manual review of the network/trust layer (`internal/net`),
store (`internal/store`), human UI (`internal/httpui`), MCP surface
(`internal/mcp`), updater (`internal/update`), daemon composition,
`install.sh`, plus `govulncheck` over all dependencies. Every finding
below was **fixed in the same session** unless explicitly listed under
"Residual risks".

Verdict after remediation: **`go vet` clean, 14/14 packages pass,
`go test -race` clean on touched packages, `govulncheck`: 0 vulnerabilities
affecting code.**

## Fixed findings

### F1 — Stored XSS in rendered doc HTML (Medium) — *fixed*
`d.Type` and `d.Updated` are attacker-influenceable strings (they arrive
inside a peer's sync payload / via MCP `write_doc` / the UI form, and
`ParseDoc` validates only the `$schema` field). They were interpolated
**unescaped** in:
- `internal/store/html.go` (`RenderDoc` meta line),
- `internal/httpui/httpui.go` (`handleHome` doc table, `renderDoc` meta),
- plus `blk.Type` / `blk.ID` (peer-controlled block IDs) in the editor form.

A teammate (or a compromised/buggy agent) could inject `<script>` into the
doc type or updated timestamp and execute it when a human opens the doc.

**Fix:** escape all of them (`html.EscapeString`) at every render site;
the embedded JSON payload was already safe (Go's `encoding/json` escapes
`<`/`>`/`&`, so `</script>` cannot break out of `#leet-doc`).

### F2 — Path traversal in slug → file path (Medium) — *fixed*
`store.Path(t, slug)` joined the slug unsanitized. A peer-controlled doc
whose `Slug` contained `/` or `..\` could make `store.Save` write
`<root>/docs/../../evil.html` — outside the store — during a pull/merge.

**Fix:** `safeSlug` in `internal/store/fs.go` replaces path separators and
NUL before joining. (Traversal requires a separator; a bare `..` joined
with the mandatory `.html` suffix is an inert file name.)

### F3 — Cross-site request forgery against the loopback UI (Medium) — *fixed*
The UI has no auth token, and every mutating endpoint accepted plain form
POSTs (`/doc/new`, `/doc/<slug>`, `/settings`, `/settings/invite`,
`/settings/hide`, `/service/*`, `/sync`, `/settings/update/apply`). Any web
page open in the operator's browser could silently: rename the actor,
regenerate the team invite, edit docs, or **download and replace the leetd
binary** via the update endpoint. (JSON endpoints like `/api/send` were
already protected by CORS preflight; form endpoints were not.)

**Fix:** `httpui.GuardMutations` — a middleware on both the node mux
(incl. `/mcp`, `/service/*`) and the first-run wizard: non-GET requests
with an `Origin` header that does not match the request `Host` are refused
(403). Same-origin pages and the Electron shell pass; non-browser clients
(curl, MCP stdio) send no Origin and pass; browsers always send Origin on
cross-site POSTs, so they are blocked.

### F4 — LAN exposure fail-closed gate missing (Medium, guard for in-flight work) — *implemented*
The uncommitted work-in-progress added `config.Validate` (rejects
non-loopback binds unless `lan.enabled=true`), but nothing enforced the
next step — a config could enable `lan.enabled` and the daemon would start
exposing the unauthenticated UI/MCP over the LAN. REQUIREMENTS D20 is
explicit: *"Do not expose the current HTTP listener to the LAN merely by
widening its bind address."*

**Fix:** `daemon.StartAtPath` now fails closed **before the store is even
opened** with `LAN mode is not available in this build` (the wizard cannot
boot such a node either). LAN access stays impossible until D21–D24
(per-agent tokens, human accounts, managed certificates) are implemented.

### F5 — Enrollment could be opened by an empty secret (Low, defense in depth) — *fixed*
`subtle.ConstantTimeCompare("", "") == 1`: if an `EnrollmentServer` were
ever created with an empty secret, any LAN peer could enroll. Production
code never does this today (`serveCoordinator` only starts enrollment when
the secret is non-empty), but the endpoint itself now rejects an empty
current secret with 403.

### F6 — Roster write traversal via CSR node id (Low) — *fixed*
`RecordIssued` sanitized `/`, `\`, NUL but not `..`; a CSR CN of
`../../x` (attacker needs the invite code) could write `x.crt` outside the
issued-members directory. Now `..` sequences are rewritten to `-`.

### F7 — Known-vulnerable dependencies (High at scan time, no exploit path shown) — *fixed*
`govulncheck` reported 12 vulnerabilities affecting called code:
- `golang.org/x/crypto` v0.53.0 → **v0.57.0** (2 × SSH channel DoS)
- `golang.org/x/net` v0.56.0 → **v0.60.0** (6 × HTTP/2 issues)
- Go stdlib go1.27.1 → **toolchain go1.27.3** in `go.mod`
  (crypto/x509, crypto/tls, net/http, net/textproto fixes)

Re-scan: **0 vulnerabilities affecting code** (3 unreachable findings
remain in required modules).

### F8 — `install.sh` header documented the forbidden supply-chain pattern (Low, docs) — *fixed*
The file's own usage comment showed `curl …/main/install.sh | sh` — the
exact vector the README's guard forbids (2026-08 `llms.txt`-style
attacks). Replaced with the pinned-release + SHA-256-verify recipe. The
script body itself was already correct (release tag + checksum verification
before install).

## Reviewed and found sound (no action)

- **Team CA & mTLS** (`net/ca.go`): ed25519, TLS 1.3 minimum, client certs
  required on sync, membership verification replaces hostname checks (D8)
  as documented; `Enroll` verifies the coordinator chains to the returned
  CA, checks the fingerprint pin when provided, and confirms the issued
  cert binds the freshly generated key.
- **Git server** (`net/gitserver.go`): handshake timeout + per-conn
  deadline; repo paths normalized and pinned inside the bare root; rogue
  nodes die at the TLS handshake.
- **Updater** (`update.go`): host allowlist (GitHub only), checksums file
  mandatory, GitHub asset digest cross-check, size cap, redirect limit,
  binary replace is atomic-on-Unix with rollback.
- **HTML rendering** (`store/html.go`): block content, titles, tags, link
  labels escaped; embedded JSON cannot break out of its script tag.
- **Chat shell** (`chatui.go`): client-side rendering escapes all
  user-supplied text via `textContent`.
- **mDNS** (`discover.go`): discovery data is only a hint — actual auth is
  mTLS; discover uses bounded contexts and a single in-flight query.
- **Field crypto** (`store/encrypt.go`): AES-256-GCM with random nonce —
  correct as a primitive (but see Residual risk #4).

## Residual risks / accepted for now

1. **No request authentication on loopback HTTP.** Any local process can
   read/write the store as the node actor via the UI or `/mcp`. This is
   the documented v0.x posture; M28 (D21–D24) is the fix. The CSRF guard
   only protects against *remote* web pages, not local processes.
2. **No certificate revocation.** Node certs live 10 years; removing a
   member means rotating the whole team CA. Tracked as
   [Enhancement](Enhancements.md) #3.
3. **Enrollment has no rate limiting.** The 12-char invite secret has
   ~3×10²¹ combinations, but a future brute-force limiter is cheap
   insurance (part of Enhancement #3's hardening).
4. **Field-level encryption at rest (D2) is not wired in** — `Keyring`
   exists, nothing calls it; docs are plaintext HTML on disk. Tracked as
   Enhancement #7.
5. **TOFU on first enrollment**: `JoinTeam` passes an empty CA fingerprint
   pin. A LAN MITM during the first join could hijack enrollment; the
   invite code (verified against the real coordinator) is the current
   mitigation. Fingerprint pinning in the wizard is a cheap future win
   (folded into Enhancement #3).
6. **`cleanRepoPath` rejects any path containing `..` as a substring**
   (e.g. a repo literally named `data..git`) — a false positive, never a
   traversal. Cosmetic.

## Verification commands

```sh
go build ./... && go vet ./...
go test ./... -count=1
go test -race -count=2 ./internal/daemon ./internal/httpui ./internal/config ./internal/mcp
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```
