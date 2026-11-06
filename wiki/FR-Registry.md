# FR Registry — Corrective Change Records

Functional-requirement records for corrective changes made during
release-readiness review (started 2026-11-02). Convention: `FR-NNN`
sequential, allocated once, never reused or renumbered. Each entry links
its release-readiness finding ID (`RR-NNN`, see the dated release-readiness
report), the pass that made the change, affected files, rationale, actual
build/test evidence, and any remaining issues.

| ID | Title | Finding | Pass | Status |
|---|---|---|---|---|
| [FR-001](#fr-001--release-workflow-publishes-pinned-install-recipe-and-attaches-installsh) | Release workflow: pinned install recipe, install.sh attached + digested | RR-001, RR-006 | 1 | Done |
| [FR-002](#fr-002--leetd-init-refuses-to-overwrite-an-existing-config) | `leetd init` refuses to overwrite an existing config | RR-002 | 1 | Done |
| [FR-003](#fr-003--leetd-check-verifies-the-real-store) | `leetd check` verifies the real configured store | RR-003 | 1 | Done |
| [FR-004](#fr-004--leetd-audit---doc-maps-every-doc-type) | `leetd audit --doc` maps every doc type | RR-004 | 1 | Done |
| [FR-005](#fr-005--usage-text-matches-implemented-doc-subcommands) | Usage text matches implemented `doc` subcommands | RR-005 | 1 | Done |
| [FR-006](#fr-006--doc-type-to-folder-mapping-consolidated) | Doc type→folder mapping consolidated on `store.DirForType` | RR-011 | 2 | Done |
| [FR-007](#fr-007--notice-covers-all-bundled-go-modules) | NOTICE covers all bundled Go modules honestly | RR-010 | 2 | Done |
| [FR-008](#fr-008--readme-install-paths-point-at-real-release-assets) | README install paths point at real release assets | RR-008, RR-012 | 2 | Done |
| [FR-009](#fr-009--leetd-enroll-bootstraps-fresh-machines-and-refuses-to-hijack-existing-ones) | `leetd enroll` bootstraps fresh machines, refuses to hijack existing ones | RR-013, RR-015 | 3 | Done |
| [FR-010](#fr-010--serve-banner-prints-the-real-listen-address) | `leetd serve` banner prints the real listen address | RR-014 | 3 | Done |
| [FR-011](#fr-011--security-policy-and-user-guide-match-actual-behavior) | SECURITY.md + user guide match actual behavior | RR-018, RR-019 | 6 | Done |
| [FR-012](#fr-012--hermetic-test-home-guardrail) | Hermetic test home so tests can never touch real user data | RR-002, RR-015 | — | **Done 2026-11-04** |
| [FR-013](#fr-013--leetd-status-and-healthz-with-degraded-mode-surfacing) | `leetd status` + `/healthz`, degraded-mode surfaced in the UI | RR-014 | — | **Proposed** |
| [FR-014](#fr-014--leetd-check-validates-every-doc-and-fails-on-corruption) | `leetd check` parses every doc, exits non-zero on corruption | RR-003 | — | **Done 2026-11-04** |
| [FR-015](#fr-015--bounded-incremental-audit-log) | Bounded, incrementally cached audit-log walk | RR-017 | — | **Proposed** |
| [FR-016](#fr-016--sync-commit-interlock-for-in-flight-writes) | Sync/commit interlock closes the hard-reset window | RR-015 | — | **Proposed** |
| [FR-017](#fr-017--automatic-identity-backup-before-any-overwrite) | Automatic identity backup before any `--force` overwrite | RR-015 | — | **Done 2026-11-04** |
| [FR-018](#fr-018--enrollment-rate-limiting) | Enrollment rate limiting + lockout | RR-001 | — | **Proposed** |
| [FR-019](#fr-019--ca-fingerprint-pinning-in-the-join-flow) | CA fingerprint pinning surfaced in the join flow | RR-015 | — | **Proposed** |
| [FR-020](#fr-020--secrets-out-of-argv) | Secrets accepted via env/prompt, not argv | RR-002 | — | **Done 2026-11-04** |
| [FR-021](#fr-021--security-headers-on-the-http-ui) | Security headers (CSP and friends) on the HTTP UI | RR-018 | — | **Proposed** |
| [FR-022](#fr-022--single-instance-lock) | Single-instance lockfile (no launchd flap) | RR-009 | — | **Proposed** |
| [FR-023](#fr-023--rotating-logs-for-the-always-on-service) | Rotating, size-bounded logs for the always-on service | RR-007 | — | **Proposed** |
| [FR-024](#fr-024--leetd-doc-addedit-for-scriptability) | `leetd doc add|edit` for scriptability | RR-005 | — | **Proposed** |
| [FR-025](#fr-025--generated-license-inventory-in-ci) | Generated third-party license inventory in CI | RR-010 | — | **Proposed** |
| [FR-026](#fr-026--fsnotify-store-watching) | fsnotify-driven store watching for automations | RR-017 | — | **Proposed** |
| [FR-027](#fr-027--markdown-importexport-round-trip) | Markdown import/export round-trip | RR-011 | — | **Proposed** |
| [FR-028](#fr-028--config-schema-version-and-migration-hook) | Config schema version field + migration hook | RR-015 | — | **Proposed** |
| [FR-029](#fr-029--cleanrepopath-substring-false-positive) | `cleanRepoPath` stops rejecting benign `..`-substring names | RR-016 | — | **Done 2026-11-04** |
| [FR-030](#fr-030--updater-accepts-tag-prefixed-asset-names) | Updater accepts tag-prefixed release asset names | RR-030 | L2 | Done 2026-11-05 |
| [FR-031](#fr-031--sync-snapshots-in-flight-writes) | Sync snapshots in-flight writes instead of discarding them | RR-031 | L3 | Done 2026-11-05 |
| [FR-032](#fr-032--atomic-write-transactions) | `Repo.WithStoreWrite` atomic write transactions | RR-031 | — | **Proposed** |
| [FR-030](#fr-030--updater-accepts-tag-prefixed-asset-names) | Updater accepts tag-prefixed release asset names | RR-030 | L2 | Done 2026-11-05 |
| [FR-031](#fr-031--sync-snapshots-in-flight-writes) | Sync snapshots in-flight writes instead of discarding them | RR-031 | L3 | Done 2026-11-05 |
| [FR-032](#fr-032--atomic-write-transactions) | `Repo.WithStoreWrite` atomic write transactions | RR-031 | — | **Proposed** |

---

### FR-001 — Release workflow publishes pinned install recipe and attaches install.sh
- **Finding:** RR-001 (high — release notes published the exact
  `curl …/main/install.sh | sh` vector the README's supply-chain guard
  forbids) and RR-006 (high — README's pinned install URL pointed at
  `releases/download/<tag>/install.sh`, which the workflow never attached:
  404 on the documented install path).
- **Pass:** 1 (2026-11-02).
- **Files:** `.github/workflows/release.yml`, `README.md` (both install blocks).
- **Rationale:** the project's own install-script supply-chain guard (README
  bottom) requires pinned tags + published digests. The release notes are the
  most-copied snippet in the repo and must model the guarded recipe; the
  documented URL must actually exist.
- **Change:** release body now downloads `install.sh` + `checksums-<ver>.txt`
  from the pinned tag, verifies via `shasum -a 256 -c`, then runs
  `sh install.sh --version <tag>`; workflow attaches `install.sh` as a release
  asset and appends its SHA-256 to the published checksums file (which already
  covers all five binaries). README install blocks updated to the same recipe
  (placeholder `<PUBLISH_RELEASE_SHA256>` removed).
- **Evidence:** `go build ./...`, `go vet ./...`, `go test ./... -count=1`
  → 14/14 packages ok, 0 FAIL (workflow is YAML-only but repo-wide checks run
  on every pass). YAML structure re-read after edit; `files:` list includes
  `install.sh`.
- **Remaining:** first release cut with the new workflow should be spot-checked
  on the GitHub Releases page (asset present, checksums file covers 6 lines:
  5 binaries + install.sh).
- **Spot-check (2026-11-04):** v0.1.9 was re-cut by the owner from the
  pre-fix workflow — binaries + checksums verified good (RR-009 closed), but
  it ships no install.sh/guide, as expected; the fix takes effect on the
  next cut from the reviewed tree.

### FR-002 — `leetd init` refuses to overwrite an existing config
- **Finding:** RR-002 (high, **observed in this review session**: running
  `leetd init` for a runtime probe silently replaced the operator's real
  `~/.config/leetoffice/node.json`, discarding team membership). `cmdInit`
  wrote `config.DefaultPath()` unconditionally, with no `--config` override
  and no overwrite protection — the CLI twin of a wizard bug fixed earlier
  (see [Debug Log](Debug-Log-2026-10-29.md) Bug 3).
- **Pass:** 1 (2026-11-02).
- **Files:** `cmd/leetd/main.go` (`cmdInit`, usage text).
- **Rationale:** an existing node.json IS the node's team membership (main
  share, identity dir, subscription cursors). Project ground rule 2 is
  "never silently overwrite". Re-running a documented command must not be
  able to destroy it.
- **Change:** `cmdInit` now takes `--config FILE` and refuses when the target
  config exists unless `--force` is passed; the refusal message explains the
  stakes and the escapes. Usage text updated.
- **Evidence:** runtime — `leetd init --store …` against the real config
  returns
  `error: config /Users/config/.config/leetoffice/node.json already exists — re-running init would discard team membership; use --force to overwrite or --config FILE for a different node`
  and the real file is untouched (verified by re-read: node_id/actor/main_share
  intact); `leetd init --config /tmp/.../n3.json …` succeeds;
  `go build ./... && go vet ./...` clean; full suite 14/14 ok.
- **Remaining:** none.

### FR-003 — `leetd check` verifies the real store
- **Finding:** RR-003 (low) — the usage text and README say "store self-test",
  but the command only round-tripped a synthetic in-memory doc and never
  opened the configured store.
- **Pass:** 1 (2026-11-02).
- **Files:** `cmd/leetd/main.go` (`runCheck`).
- **Rationale:** an operator running `leetd check` expects the documented
  check to touch the store; a schema round-trip alone gives false
  confidence (e.g. a corrupt `docs/*.html` would still report OK).
- **Change:** after the round-trip, `runCheck` loads the default config and —
  when the store dir exists — opens it and `List()`s every doc, reporting the
  count; any unreadable doc is skipped by `List` (hygiene's domain) but open
  failures now fail the command. Fresh machines with no config keep the old
  behavior.
- **Evidence:** runtime — `leetd check` prints
  `store: /Users/config/LeetOffice — 4 docs readable` then `store: OK`;
  with no config the command still exits 0 (round-trip only). Full suite
  14/14 ok.
- **Remaining:** enhancement candidate — per-doc parse validation and
  non-zero exit on corrupt docs (folded into Enhancements #8 `leetd doctor`).

### FR-004 — `leetd audit --doc` maps every doc type
- **Finding:** RR-004 (low) — the doc filter hardcoded `docs/` and `tasks/`,
  so auditing a contacts/channels/companies/emails/memory doc silently
  returned an empty history.
- **Pass:** 1 (2026-11-02).
- **Files:** `cmd/leetd/main.go` (`cmdAudit`), `internal/store/fs.go`
  (new exported `DirForType`; `typeDir` now delegates to it).
- **Change:** the audit path is built from `store.DirForType(d.Type)`, the
  same mapping `Store.Path` uses on write.
- **Evidence:** `go build ./...`, `go vet ./...` clean; full suite 14/14 ok
  (includes `internal/store` and audit-path consumers). Runtime UI audit
  check on a live node shows chat commits (`chat: #general`) listed.
- **Remaining:** none.

### FR-005 — Usage text matches implemented `doc` subcommands
- **Finding:** RR-005 (informational) — usage advertised `leetd doc
  add|edit`, which return "not implemented"; the error message points to the
  UI/MCP but the help text misleads first.
- **Pass:** 1 (2026-11-02).
- **Files:** `cmd/leetd/main.go` (usage).
- **Change:** usage now reads `leetd doc show|list ... (add/edit via UI or
  MCP)`.
- **Evidence:** `go build ./...` clean; help output re-read.
- **Remaining:** implement `doc add`/`doc edit` for scriptability
  (enhancement candidate; not scheduled).

---

### FR-006 — Doc type→folder mapping consolidated
- **Finding:** RR-011 (low, drift risk) — the doc-type→on-disk-folder mapping
  existed in three places (`store.typeDir`, `mcp.docPath`, `memory.dirOf` +
  hygiene's hardcoded dir list). A new doc type would silently diverge:
  written to one folder, indexed from another, missed by hygiene.
- **Pass:** 2 (2026-11-02).
- **Files:** `internal/mcp/server.go`, `internal/memory/memory.go`,
  `internal/memory/hygiene.go`, `internal/store/fs.go`.
- **Change:** all three now delegate to the exported `store.DirForType`
  (introduced in FR-004); hygiene derives its scan list from the same
  mapping over the seven store types.
- **Evidence:** `go build ./... && go vet ./...` clean;
  `go test ./internal/memory ./internal/mcp ./internal/store -count=1` all
  ok; full suite 14/14 ok on pass close.
- **Remaining:** none.

### FR-007 — NOTICE covers all bundled Go modules honestly
- **Finding:** RR-010 (low, P7-8) — NOTICE named only go-git and mdns while
  the binary compiles in further third-party modules (x/crypto, x/net,
  circl, go-crypto, dns, …).
- **Pass:** 2 (2026-11-02).
- **Files:** `NOTICE`.
- **Change:** NOTICE now names the additional compiled-in module set and
  designates `go.mod` / `go list -m all` as the authoritative versioned
  list and each module's LICENSE file as the authoritative text — without
  asserting per-module license labels beyond the permissive families
  (BSD/MIT/Apache-2.0), consistent with the checklist's "flag items needing
  professional review" rule.
- **Evidence:** file re-read after edit; no code change; full suite 14/14 ok.
- **Remaining:** owner decision — a generated third-party-license dump
  (e.g. `go-licenses`) in CI would make this mechanical (Enhancements #8
  territory).

### FR-008 — README install paths point at real release assets
- **Finding:** RR-008 (medium, observed) — both README install examples
  pinned `LEET_VER=v1.0.0`, a tag that does not exist (live releases are
  v0.1.0–v0.1.9; latest v0.1.9). Copy-pasting the flagship snippet 404s.
  RR-012 (low) — a bare `shasum -a 256 -c checksums-*.txt` fails whenever
  only one of the five listed binaries was downloaded; the verify line must
  grep its own entry. Also observed: `install.sh` is not attached to any
  existing release (fixed for future releases in FR-001), so the installer
  recipe is documented as "from the next release onward" and the
  always-works binary path is now primary.
- **Pass:** 2 (2026-11-02).
- **Files:** `README.md` (hero block + Install section).
- **Change:** version pinned to v0.1.8 (the newest release with published
  assets at review time); binary download + per-entry checksum verify made
  the primary path; installer recipe kept with the release-asset caveat.
- **Evidence:** live GitHub API — v0.1.9 assets list is empty (see RR-009);
  v0.1.8 assets: 5 binaries + `checksums-0.1.8.txt` + guide; commands
  re-read for correctness (grep+shasum pattern verified against the
  published checksums file format).
- **Remaining:** owner action on RR-009; when a post-FR-001 release is cut,
  switch the README's `LEET_VER` to it.

---

### FR-009 — `leetd enroll` bootstraps fresh machines and refuses to hijack existing ones
- **Finding:** RR-013 (low, observed) — on a machine with no config, `leetd
  enroll` died with `open …/node.json: no such file or directory`, although
  enroll *is* the joiner's init. RR-015 (**high, observed — this review's own
  live probe triggered it**): the bootstrap path defaulted to the operator's
  real `~/LeetOffice` store and the shared `~/.leetoffice-identity`, silently
  **overwriting the existing node identity** (node.key/node.crt/ca.crt) and
  repointing the store's origin — destroying a live team membership with no
  warning. Direct violation of ground rule 2 (never silently overwrite).
- **Pass:** 3 (2026-11-02).
- **Files:** `cmd/leetd/main.go` (`cmdEnroll`).
- **Change:** (1) missing config → bootstrap a default config (saved only
  after a successful enroll); (2) new `--store` flag so the joined node's
  store/identity dir are explicit; (3) hard guard: when `node.key` already
  exists in the identity dir, enroll refuses with an explanation unless
  `--force` is passed — checked **before** any network round-trip.
- **Evidence (runtime):** guard refusal message printed when run on this
  machine with the real identity in place; fresh-machine enroll (clean
  `--store /tmp/rr-client/client-store`) succeeded end-to-end against a live
  coordinator; full two-node round-trip re-verified (client chat → mTLS sync
  → coordinator `/api/state` shows the message; client git shows the
  attributed commit); `go build ./... && go vet ./...` clean; full suite
  14/14 ok.
- **Remaining (owner action, incident disclosure):** the RR-015 probe run
  before the guard existed **overwrote this machine's real Aug-16 node
  identity** — the original team membership (coordinator 192.168.1.98) must
  be restored by re-enrolling with a fresh invite from that coordinator. The
  real store also carries probe-era commits (2026-11-03, authors
  `human:Hivemind.local`/`human:coord`); origin was restored to
  `leet://192.168.1.98:7666/main.git` and history left append-only. See the
  release-readiness report, "Incident disclosure".

### FR-010 — `leetd serve` banner prints the real listen address
- **Finding:** RR-014 (low, observed) — the startup banner hardcoded
  `http://127.0.0.1:7667` regardless of the configured listener.
- **Pass:** 3 (2026-11-02).
- **Files:** `cmd/leetd/main.go` (`cmdServe`), `internal/daemon/setup.go`
  (exported `HTTPListenAddr`, sharing the daemon's own resolution logic).
- **Evidence:** `go build ./... && go vet ./...` clean; full suite 14/14 ok
  (daemon package covers listen-address resolution); banner code re-read.
- **Remaining:** none.

---

### FR-011 — SECURITY.md and user guide match actual behavior
- **Finding:** RR-018 (medium, P7-4) — SECURITY.md claimed "field-level
  AES-256-GCM for sensitive values" at rest, but D2 field encryption is not
  wired into the store (docs are plaintext HTML on disk; the `Keyring` is
  dead code). RR-019 (high, supply-chain guard) — `docs/user-guide.html`,
  which ships as the `leetoffice-guide.html` release asset, contained the
  forbidden `curl …/main/install.sh | sh` pattern **twice** (visible HTML and
  the canonical embedded JSON payload), and claimed "Latest is v0.1.1".
- **Pass:** 6 (2026-11-02/03).
- **Files:** `SECURITY.md`, `docs/user-guide.html`.
- **Change:** SECURITY.md now states the honest at-rest posture (plaintext
  HTML today, D2 implemented-but-unwired, volume encryption recommended,
  0600/0700 permissions noted). The guide's install section uses the pinned
  binary + per-entry checksum recipe; both embedded and visible copies of
  the pipe-to-shell command removed.
- **Evidence:** `grep -c "curl -fsSL https://raw.githubusercontent.com"
  docs/user-guide.html` → 0; the guide's embedded JSON re-parsed after the
  edit (14 blocks, schema URL intact); `go build ./... && go vet ./...`
  clean; full suite 14/14 ok.
- **Remaining:** ship a release built from the fixed workflow (FR-001) so the
  published guide asset picks this up.

---

## Backlog — from the 2026-11 release-readiness passes

FR-012, FR-014, FR-017, FR-020, and FR-029 were implemented and verified on
2026-11-04 (evidence appended to each entry below); the rest remain
proposed. Allocation stays final either way.

Status `Proposed` = not yet implemented; allocation is final (no reuse or
renumbering). Grounding references point at review findings (RR-NNN in the
[release-readiness report](Release-Readiness-Report-2026-11-02.md)), completed
FRs, and checklist areas. These complement the product-level
[Enhancements](Enhancements.md) backlog — items there are features; items here
are engineering improvements to what already exists.

### FR-012 — Hermetic test home guardrail
- **Problem:** two separate incidents (the wizard test writing into the real
  `~/LeetOffice`, the `leetd init` probe clobbering the real `node.json` —
  RR-002, RR-015) share one root cause: Go tests and probes defaulting to
  `os.UserHomeDir()`. Per-fix guards exist (FR-002, FR-009), but nothing
  *structurally* prevents the next default-path consumer from repeating it.
- **Proposal:** package-level `TestMain` in every test package that sets
  `HOME`, `USERPROFILE`, `XDG_CONFIG_HOME`, and `os.UserHomeDir`'s backing
  env to a per-run temp dir; plus a canary test that asserts
  `config.DefaultPath()`/`DefaultStoreDir()` resolve inside it.
- **Acceptance:** deliberately calling `DefaultStoreDir()` in a test yields a
  temp path; the whole suite passes with `$HOME` pointing at an empty dir;
  a test that writes the real home fails loudly.
- **Effort:** S · **Refs:** RR-002, RR-015 (both incident classes)

- **Implementation (2026-11-04):** `internal/testenv` (`Isolate`), a
  `TestMain` in all 14 test packages (`zz_hermetic_test.go`), and the canary
  `TestDefaultsStayInsideHermeticHome` in `internal/config`.
- **Evidence:** full suite 14/14 ok **with the hermetic home active** (would
  fail loudly if any test touched the real home); canary asserts
  `DefaultPath`/`DefaultStoreDir`/`IdentityDirFor` resolve inside the
  isolated home; `go vet` clean.
- **Status: Done 2026-11-04.**

### FR-013 — `leetd status` and `/healthz`, degraded mode surfaced
- **Problem:** the operator cannot see node health without reading logs.
  `becomeNode` deliberately continues "in degraded mode" when networking
  fails (git/enroll port taken), but nothing in the UI reveals it — the 2026-10
  mDNS socket-leak incident showed a wedged daemon looking perfectly healthy
  to launchd (KeepAlive only sees process death).
- **Proposal:** `GET /healthz` returning JSON (store ok, git remote
  reachable, last sync result, listener state, degraded reasons, cert
  expiry); `leetd status` renders the same; Settings and the chat rail show
  a persistent degraded banner when any loop failed.
- **Acceptance:** with the git port occupied, Settings shows the degraded
  reason instead of silent normality; `leetd status` exits non-zero when
  unhealthy.
- **Effort:** M · **Refs:** RR-014, daemon `becomeNode`, P5-10/P5-13

### FR-014 — `leetd check` validates every doc, fails on corruption
- **Problem:** FR-003 made `check` open and list the store, but `Store.List`
  *skips* unparseable docs (by design, hygiene reports them) — so a corrupt
  doc still yields "store: OK".
- **Proposal:** in `check`, parse every `.html` under the type dirs via
  `ExtractDoc`; report per-doc failures and exit non-zero; keep the
  round-trip test. Optionally `--fix` to re-render recoverable docs.
- **Acceptance:** hand-corrupting a doc's embedded JSON makes `leetd check`
  exit 1 naming the file; clean store exits 0.
- **Effort:** S · **Refs:** RR-003 remaining, FR-003, Enhancements #8 (doctor)

- **Implementation (2026-11-04):** `runCheck` walks every type dir and
  `ExtractDoc`s each `.html`; failures are named (first 5) and the command
  exits 1. Output now reads "N docs, all readable".
- **Evidence (runtime):** a store with one good and one corrupt doc →
  `error: store /tmp/...: 1 of 2 docs unreadable, first: docs/broken.html`,
  exit 1; clean store → exit 0. Suite 14/14 ok.
- **Status: Done 2026-11-04.**

### FR-015 — Bounded, incrementally cached audit log
- **Problem:** `Repo.AuditLog` walks the *entire* history and computes a
  per-commit tree diff (`changedFiles`) for every commit — O(history ×
  files) on every `/audit` page load and `audit_query` MCP call. Observed in
  the pass-1 review; cost grows without bound on long-lived stores.
- **Proposal:** cache (commit → changed files) keyed by commit hash under
  the store's `_audit/` or an in-memory LRU; walk with the existing `limit`
  early-stop (already present) plus a max-depth guard; optionally persist
  the map so cold starts are fast.
- **Acceptance:** `/audit` latency stays flat as history grows 10×; identical
  output to the current walker on a fixture store.
- **Effort:** M · **Refs:** RR-017, P3-11, sync/git.go `AuditLog`

### FR-016 — Sync/commit interlock for in-flight writes
- **Problem:** suspected race from the pass-1 sync review: `Store.Save`
  writes files without the repo lock, `CommitAll` takes it a moment later;
  if a sync fast-forward (`wt.Reset(HardReset)`) lands in that window, the
  just-written doc content is discarded. Narrow, but it is exactly the
  "silently overwrite" class the project forbids.
- **Proposal:** route store mutations through a shared store-level mutex the
  sync path also takes during `Reset`/`mergeInto` file writes (or: commit
  synchronously inside `Save` under the repo lock).
- **Acceptance:** a stress test interleaving saves and forced fast-forwards
  never loses a write (`-race -count` clean).
- **Effort:** M · **Refs:** RR-015 class, D6, sync/git.go `Sync`

### FR-017 — Automatic identity backup before any overwrite
- **Problem:** even with the FR-009 guard, `--force` (or a future code path)
  still destroys the old identity irrecoverably — the RR-015 incident made
  re-enrollment the only recovery.
- **Proposal:** `Identity.Save` and the enroll/init `--force` paths first
  copy the existing identity dir to `<dir>.bak-<timestamp>` (keep last 3).
- **Acceptance:** enrolling with `--force` over an existing identity leaves a
  restorable backup; guard messaging points at it.
- **Effort:** S · **Refs:** RR-015, FR-009

- **Implementation (2026-11-04):** `backupIdentityBeforeSave` in
  `internal/net/ca.go` — whole-tree copy to `<dir>.bak-<stamp>` before any
  overwrite, newest 3 kept, and `Save` **fails closed** if the backup cannot
  be made (an identity is never overwritten un-preserved).
- **Evidence:** `TestIdentitySaveBacksUpPreviousIdentity` (backup holds the
  previous cert, identity actually replaced, fresh-create makes no backup)
  and `TestIdentitySavePrunesOldBackups` pass; `-race` clean on
  `internal/net`; suite 14/14 ok.
- **Status: Done 2026-11-04.**

### FR-018 — Enrollment rate limiting
- **Problem:** the enrollment endpoint answers unlimited 403s; the 12-char
  secret is huge, but nothing throttles a LAN brute-forcer (audit residual,
  deliberately deferred during the security audit).
- **Proposal:** per-source-IP token bucket (e.g. 5 attempts/min, exponential
  backoff, optional temporary lockout) in `EnrollmentServer`; log strikes.
- **Acceptance:** a probe hammering `/enroll` sees 429s; legitimate joins are
  unaffected; state survives secret rotation.
- **Effort:** S · **Refs:** RR-001 (hardening family), P1-10

### FR-019 — CA fingerprint pinning surfaced in the join flow
- **Problem:** `JoinTeam`/`leetd enroll` pass an empty fingerprint — first
  join is trust-on-first-use; a LAN MITM during the first enrollment wins
  (audit residual 5). The mDNS TXT already carries the coordinator's CA
  fingerprint; the code just never asks the human to compare it.
- **Proposal:** the join wizard shows the discovered coordinator's
  fingerprint (short form) for out-of-band comparison; `leetd enroll` gains
  `--expect-fingerprint`; `Enroll` already enforces the pin when non-empty.
- **Acceptance:** mismatched pin aborts with a clear error; matching pin
  succeeds; wizard displays the value.
- **Effort:** S · **Refs:** RR-015 family, D8, net/enroll.go

### FR-020 — Secrets out of argv
- **Problem:** `leetd enroll --secret S` exposes the invite code in `ps`
  output to every local process (observed in the pass-1 CLI review).
- **Proposal:** accept `--secret-env NAME` and interactive prompt (terminal
  echo off); keep `--secret` working but deprecate it in help text.
- **Acceptance:** enroll completes via env and prompt paths; the secret never
  appears in `ps` or shell history in those paths.
- **Effort:** S · **Refs:** RR-002, S12, S6

- **Implementation (2026-11-04):** `cmdEnroll` accepts exactly one secret
  source: `--secret` (deprecated in help), `--secret-env NAME`, or
  `--secret-stdin` (first line); usage text updated. Zero new dependencies
  (no `x/term` — stdin read suffices for scripted use).
- **Evidence (runtime):** no source → named error; two sources → named
  error; empty env → named error; env-resolved and stdin-resolved runs both
  pass resolution and reach the identity guard (proof the secret was
  accepted). Suite 14/14 ok.
- **Status: Done 2026-11-04.**

### FR-021 — Security headers on the HTTP UI
- **Problem:** the loopback UI sends no CSP, `X-Content-Type-Options`, or
  frame-options headers (pass-2 P1-9 note). XSS defenses are escaping plus
  the Origin guard; headers are cheap defense-in-depth, and the Electron
  shell loads the same origin.
- **Proposal:** middleware setting a strict-but-workable CSP
  (`default-src 'self'`; inline styles/scripts currently used — either nonce
  them or allow `'unsafe-inline'` deliberately with a comment), plus
  `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: no-referrer`.
- **Acceptance:** headers present on every UI response; chat/docs/settings
  render unchanged in a browser and in the Electron shell.
- **Effort:** S · **Refs:** RR-018, P1-9, P1-8

### FR-022 — Single-instance lock
- **Problem:** a second `leetd serve` on the same config fails the HTTP bind
  and exits, and launchd `KeepAlive` + `ThrottleInterval=2` turns that into
  periodic flapping with log noise (observed structure in
  `internal/daemon/service.go`; the exit message says "another leetd is
  probably already running").
- **Proposal:** a lockfile (`<identity dir>/leetd.lock` with pid + stale
  detection) or an IPC probe of the existing UI port: second instance exits
  0 immediately with "already running (pid N)" so KeepAlive never flaps.
- **Acceptance:** double-start exits fast with a clear message and no
  launchd restart loop; stale lockfile after SIGKILL does not wedge startup.
- **Effort:** S · **Refs:** RR-009 (ops family), P5-6

### FR-023 — Rotating logs for the always-on service
- **Problem:** the launchd plist (and systemd equivalent) points both stdout
  and stderr at one file with no rotation — an always-on daemon's log grows
  unbounded on the operator's disk (P5-11).
- **Proposal:** size-bounded rotation in `service.go`'s supported launchers
  (newsyslog config drop on macOS, `logrotate` snippet on Linux), or simple
  internal rotation when the file exceeds N MB.
- **Acceptance:** log file cannot exceed the configured bound across a
  week-long run; old segments retained (e.g. 3×5 MB).
- **Effort:** S · **Refs:** RR-007 (ops hygiene), P5-10/P5-11

### FR-024 — `leetd doc add|edit` for scriptability
- **Problem:** usage advertised `doc add|edit` since v0.1.x but they return
  "not implemented" (RR-005); agents and shell scripts have no CLI write
  path short of MCP.
- **Proposal:** implement `doc add <slug> <title> [--type T] [--body …]` and
  `doc edit <slug> [--append|--replace-block ID]`, mirroring the MCP
  `write_doc` semantics (same commit messages) so the audit trail is
  uniform.
- **Acceptance:** add/edit via CLI produce the same doc JSON and commit
  shapes as the MCP tools; usage text updated back.
- **Effort:** S · **Refs:** RR-005 remaining, FR-005

### FR-025 — Generated license inventory in CI
- **Problem:** FR-007 fixed NOTICE by hand with an honest pointer to go.mod;
  manual lists drift again on the next dependency change.
- **Proposal:** CI step generating a `THIRD-PARTY-LICENSES.md` from
  `go list -m all` + each module's LICENSE (e.g. `go-licenses` or a small
  script), attached to releases alongside the checksums file; diff-checked
  so a new dependency with an incompatible license fails the build.
- **Acceptance:** adding a GPL dependency (hypothetically) fails CI with a
  named module; the release assets include the generated file.
- **Effort:** S · **Refs:** RR-010 remaining, FR-007, B8/P7-8

### FR-026 — fsnotify-driven store watching
- **Problem:** memory synthesis debounces via a 60 s fingerprint poll
  (`storeFingerprint` walks and hashes every doc listing); digests/hygiene
  poll on 10 min/1 h tickers. Latency and pointless CPU on idle stores.
- **Proposal:** watch the store dirs (fsnotify is the obvious dependency;
  pure-Go polling fallback for unsupported platforms) to trigger synthesis
  immediately on change; keep tickers as fallback.
- **Acceptance:** a doc edit synthesizes MEMORY.md within ~2 s without the
  poll firing on an idle store; CPU ~0 when idle.
- **Effort:** M · **Refs:** RR-017, D16/M19, B22

### FR-027 — Markdown import/export round-trip
- **Problem:** the store format is open (tabbed HTML + embedded JSON) but
  the only human-editable interchange is copy-paste; P3-9 round-tripping is
  untested territory for anything outside the store itself.
- **Proposal:** `leetd doc export <slug> --format md` and `--import`
  mapping markdown headings/lists/code/tasks onto the existing block types;
  lossless for supported blocks, documented lossy for the rest.
- **Acceptance:** export → import produces the same embedded JSON for all
  seven block types with a documented exception list; git audit records the
  import like any write.
- **Effort:** M · **Refs:** RR-011, P3-9, D1

### FR-028 — Config schema version and migration hook
- **Problem:** `node.json` has no version field; every config evolution
  (the LAN block being the live example) is "new keys, old binaries ignore
  them" — there is no place to put breaking migrations, and old configs
  can't be detected (RR-015's root cause family: defaults silently applied).
- **Proposal:** `"schema": 1` written by `Save`; `Load` rejects-or-migrates
  unknown major versions with a clear message; migrations as small
  functions.
- **Acceptance:** a config with `"schema": 99` loads nowhere with an
  actionable error; v0 configs (all current files) load unchanged and are
  re-saved with `"schema": 1`.
- **Effort:** S · **Refs:** RR-015, FR-002/009 family, P3-2

### FR-029 — `cleanRepoPath` stops rejecting benign `..` substrings
- **Problem:** the git server's path validator rejects any path *containing*
  `..` as a substring (a repo literally named `data..git` or `v1..v2.git`
  cannot be served) — a false positive left as cosmetic debt in the security
  audit; `path.Clean` already removes traversal elements.
- **Proposal:** validate on path *segments* (`elem == ".."`) after `Clean`
  instead of substring search; keep the existing tests plus a
  `data..git` case.
- **Acceptance:** `/data..git` serves; `/../etc` still rejected; net tests
  pass.
- **Effort:** S · **Refs:** RR-016 (audit residual 6), S8

- **Implementation (2026-11-04):** per-segment validation after
  `path.Clean` in `cleanRepoPath` (the segment check is unreachable
  backstop, kept deliberately); benign `..`-substring names now serve.
- **Evidence:** new `TestCleanRepoPath` (accepts `/data..git`,
  `/v1..v2.git`, `/a/b/main.git`; rejects non-absolute, `/`, empty;
  documents `/../etc` → `/etc` staying inside the root) passes; suite 14/14
  ok; `govulncheck` 0 affecting.
- **Status: Done 2026-11-04.**

### FR-032 — `Repo.WithStoreWrite` atomic write transactions
- **Problem:** FR-031's snapshot shrinks the in-flight-write window to
  microseconds but cannot eliminate it: `Store.Save` writes outside the repo
  lock, so a save can still land between a dirty-check and a destructive
  write inside `Sync`. **Observed once in the loop-2 live two-node probe
  (RR-033): a chat message was committed (dangling commit recovered via
  `fsck`) and then orphaned by a fast-forward during the enrollment-time
  sync burst; the clean rerun converged correctly.** Timing-dependent,
  unreproduced on retry — which is exactly why the complete fix matters.
- **Proposal:** `Repo.WithStoreWrite(fn func(s *store.Store) error) error`
  — run the mutation and its commit atomically under the repo lock; migrate
  the ~10 Save+CommitAll call sites (httpui, mcp, chat, memory, daemon) to
  it. Saves then physically cannot interleave with sync worktree writes.
- **Acceptance:** the FR-031 stress probe reports **zero** losses over many
  rounds with the transaction path; all call sites migrated; suite green.
- **Effort:** M · **Refs:** RR-031, FR-031, D6
