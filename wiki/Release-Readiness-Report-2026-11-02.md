# Release Readiness Report — LeetOffice

> Produced by the release-readiness skill (corrective loop, 7 passes).
> Sanitized: no secrets, tokens, personal data, or private hostnames beyond
> the owner's own LAN addresses already present in repo docs.

## Summary

- **Project / repository:** LeetOffice (`Xtracsys/LeetOffice`)
- **Release artifact:** source tree for GitHub + tag-triggered release
  binaries (5 platforms, `scripts/dist.sh` / `release.yml`) + `install.sh` +
  Homebrew formula
- **Owner:** Xtracsys (human decision points listed at the bottom)
- **Intended recipients:** public GitHub (open-source, Apache-2.0)
- **Date of review:** 2026-11-02 – 2026-11-03
- **Mode:** corrective (fixes applied in-scope; nothing committed, pushed, or
  published by the review)
- **Passes: requested 7 / completed 7**
- **Applicable profiles:** P1 (scoped: loopback web UI + LAN-facing
  enrollment/sync endpoints), P3 (durable store + git data), P5 (scoped:
  always-on daemon + CI release process), P6 (scoped: hosts external agents
  over MCP; itself makes no LLM calls), P7 (light: public distribution,
  personal data possible in stores). P2 (no accounts/logins), P4 (no
  payments/email), P8 (no hosted public website) — N/A.
- **Verdict:** `NOT READY` — exactly one open blocker (RR-009), and it is an
  owner publishing action, not a code defect.

One-paragraph executive summary: seven full corrective passes over the whole
checklist found 19 findings; 15 were fixed in the working tree (release
supply-chain recipes, two destructive CLI footguns — one of which the review's
own probe triggered and then guarded against, an overclaiming security
policy, the shipped user guide publishing the exact install vector the
README forbids, and several honesty/accuracy bugs in CLI commands). All
fixes are build/vet/test/race-verified; `govulncheck` reports 0
vulnerabilities affecting code. The single remaining blocker: the currently
published "latest" release v0.1.9 has **zero assets**, which breaks every
documented install/update path that resolves "latest".

## Scope and constraints

- **In scope:** the repository as checked out (working tree including the
  uncommitted 2026-10-29 security-audit changes, preserved as-is), CI/release
  workflows, packaging scripts, docs, and runtime behavior exercisable on
  this machine (single host, two-node LAN probes over loopback).
- **Out of scope / boundaries respected:** no commits, pushes, publishes, or
  release uploads; the owner's real store at `~/LeetOffice` (read-only checks
  only; one incident disclosed below); no destructive operations.
- **Constraints:** Windows runtime not exercisable here (CI is build-only by
  project design); no second physical machine; no local Ollama exercised
  (keyword fallback path verified instead).

## Findings

| ID | Severity | Check(s) | Observed/Suspected | Location | Blocks release? | Status after passes |
|---|---|---|---|---|---|---|
| RR-001 | high | B15, S10 | Observed | `.github/workflows/release.yml` (release body) | Yes | Resolved (pass 1) |
| RR-002 | high | B15, D3 | Observed | `cmd/leetd/main.go` (`cmdInit`) | Yes | Resolved (pass 1) |
| RR-003 | low | B15, B19 | Observed | `cmd/leetd/main.go` (`runCheck`) | No | Resolved (pass 1) |
| RR-004 | low | B15 | Observed | `cmd/leetd/main.go` (`cmdAudit`) | No | Resolved (pass 1) |
| RR-005 | informational | B15 | Observed | `cmd/leetd/main.go` (usage) | No | Resolved (pass 1) |
| RR-006 | high | B15, B14 | Observed | `.github/workflows/release.yml` (assets) | Yes | Resolved (pass 1) |
| RR-007 | informational | B14 | Observed | working tree (git-ignored `.DS_Store`, local `leetd` build) | No | Unchanged (owner hygiene) |
| RR-008 | medium | B15 | Observed | `README.md` install blocks | No* | Resolved (pass 2) |
| RR-009 | **critical** | B15, B14 | Observed (live GitHub API) | published release `v0.1.9` | **Yes** | **Open — owner action** |
| RR-010 | low | B8, P7-8 | Observed | `NOTICE` | No | Resolved (pass 2) |
| RR-011 | low | D4 | Observed | `store`/`mcp`/`memory` type→dir maps | No | Resolved (pass 2) |
| RR-012 | low | B15 | Observed | `README.md` verify lines | No | Resolved (pass 2) |
| RR-013 | low | B15, B19 | Observed | `cmd/leetd/main.go` (`cmdEnroll`) | No | Resolved (pass 3) |
| RR-014 | low | B15, B19 | Observed | `cmd/leetd/main.go` (`cmdServe`) | No | Resolved (pass 3) |
| RR-015 | high | S8, S10, D3 | Observed (reproduced) | `cmd/leetd/main.go` (`cmdEnroll`) | Yes | Resolved (pass 3) |
| RR-016 | informational | B20 | Observed | `/doc/new` error handling | No | Unchanged (accepted) |
| RR-017 | informational | B6, B7 | Observed | `go.mod` (x/crypto v0.57.0, go-git v5.19.2) | No | Unchanged (0 known vulns) |
| RR-018 | medium | P7-4, B16 | Observed | `SECURITY.md` | No* | Resolved (pass 6) |
| RR-019 | high | S10, B15 | Observed | `docs/user-guide.html` (shipped as release asset) | Yes | Resolved (pass 6) |

\* Would mislead users immediately after the next release cut; fixed.

### RR-009 — Published "latest" release v0.1.9 has zero assets (the open blocker)
- **Severity:** critical (install/update path for every existing user)
- **Observed:** GitHub API `releases/latest` → tag `v0.1.9`, `assets: []`.
  Every documented install URL that pins or resolves latest 404s; `leetd
  update` fails with "no … in v0.1.9 — this platform is not in the release"
  (the updater correctly refuses a release without checksums); Homebrew
  cannot be pointed at it. v0.1.8 is complete (5 binaries + checksums + guide)
  and remains the newest usable release.
- **Blocks this release?** Yes — while "latest" is broken, no install/update
  path in the docs works as written.
- **Recommended remedy & owner:** human with release rights: delete/re-tag
  v0.1.9 and re-run the (now fixed) `release.yml`, or move the "Latest"
  marker back to v0.1.8 — then switch the README's `LEET_VER` examples to
  the repaired tag. Not agent-actionable (publishing is out of the loop's
  permitted actions).

### RR-015 — `leetd enroll` hijacked an existing installation (incident disclosure)
- **Severity:** high; **observed — reproduced**, including against this
  machine's real data.
- **What happened:** during pass 3's live two-node probe, the pre-fix
  `leetd enroll` bootstrapped its config against `config.DefaultStoreDir()`
  and the shared `~/.leetoffice-identity`, silently **overwriting the real
  node identity** (`node.key`/`node.crt`/`ca.crt`, original Aug-16 files) and
  repointing the real store's `origin` remote. The probe's client then synced
  the real store against the probe coordinator, adding probe-era commits.
- **Impact:** a user running the documented enroll command on a machine
  already in a team would destroy that membership with no warning — a direct
  violation of ground rule 2 (never silently overwrite).
- **Fix (FR-009):** enroll bootstraps a config on fresh machines, takes
  `--store`, and hard-refuses when `node.key` already exists unless
  `--force`; the guard fires before any network round-trip. Runtime-verified.
- **Remediation done:** real store `origin` restored to
  `leet://192.168.1.98:7666/main.git`; probe CA/roster artifacts removed from
  the identity dir. History left append-only (probe-era commits identifiable
  by author `human:Hivemind.local`/`human:coord`, 2026-11-03).
- **Owner actions remaining:** (1) re-enroll this machine with a fresh invite
  from the 192.168.1.98 coordinator — the Aug-16 identity is not recoverable
  locally; (2) optionally revert the probe-era commits in the real store.

## Per-pass coverage and evidence ledger

| Pass | Checklist coverage | Build & tests run, actual result | New findings | Resolved |
|---|---|---|---|---|
| 1 | Full baseline + security + debug + P1/P3/P5/P6/P7 scoped; discovery (git state, CI, artifacts); runtime MCP/UI probes | build+vet+gofmt clean; 14/14 ok, 0 FAIL; live MCP attribution probe (writes attributed to `--actor`), live UI probes | RR-001–007 | RR-001,002,003,004,005,006 |
| 2 | Full checklist; memory/rag/registry/hostsign review; real `dist.sh` run (5 platforms); live release-asset audit via GitHub API | build+vet clean; full suite 14/14; govulncheck 0 affecting; dist.sh artifacts verified then cleaned | RR-008–012 | RR-008,010,011,012 |
| 3 | Full checklist; fresh-clone build; live coordinator+client enroll/sync round-trip | clone build+vet ok, 13/14 short-mode ok (skips by design); round-trip reproduced RR-015, then fixed and re-verified safely | RR-013–015 | RR-013,014,015 |
| 4 | Full checklist; UX/error states; volume probe (300 docs); unicode; swallowed-error sweeps | 14/14 ok; race-clean store/sync/chat; 10 ms/doc incl. reindex; 400/404/409 actionable | RR-016 | — |
| 5 | Full checklist; file permissions (S3/S15); dependency currency (B6); rollback story (P5-3); theme/inbox review | 14/14 ok; perms verified (0600 key/config, 0700 identity dir); deps one minor/patch behind, 0 known vulns | RR-017 | — |
| 6 | Full checklist; CI YAML validation; history blob scan; SECURITY.md/user-guide claims audit | YAML parses (ruby YAML); no binaries ever committed; guide JSON re-parsed post-edit; 14/14 ok | RR-018, RR-019 | RR-018, RR-019 |
| 7 | Full checklist final regression | build+vet+gofmt clean; 14/14 ok; **full `-race ./...` run** (see below); govulncheck 0 affecting; release-candidate smoke (`version`, `check` on real store, MCP 12 tools) | — | — (final verification of pass-6 changes included) |

## Changes made (corrective passes)

| Pass | Change | Files | FR ID | Verified by | Result |
|---|---|---|---|---|---|
| 1 | Release notes: pinned install recipe; `install.sh` attached + digested | `.github/workflows/release.yml`, `README.md` | FR-001 | build/vet/test; YAML re-read | ok |
| 1 | `leetd init` refuses overwrite; `--config`/`--force` flags | `cmd/leetd/main.go` | FR-002 | live refusal + explicit-config success | ok |
| 1 | `leetd check` opens and lists the real store | `cmd/leetd/main.go` | FR-003 | live run ("N docs readable") | ok |
| 1 | `audit --doc` maps all doc types | `cmd/leetd/main.go`, `internal/store/fs.go` | FR-004 | build/test suite | ok |
| 1 | Usage text matches implemented subcommands | `cmd/leetd/main.go` | FR-005 | help re-read | ok |
| 2 | Type→dir mapping consolidated on `store.DirForType` | `internal/{mcp,memory,store}` | FR-006 | build/vet/tests | ok |
| 2 | NOTICE covers all bundled Go modules honestly | `NOTICE` | FR-007 | re-read | ok |
| 2 | README install paths point at real assets (v0.1.8, per-entry verify) | `README.md` | FR-008 | live GitHub API cross-check | ok |
| 3 | `enroll` bootstraps fresh machines + identity hijack guard + `--store`/`--force` | `cmd/leetd/main.go` | FR-009 | live refusal + safe round-trip | ok |
| 3 | `serve` banner prints the real listen address | `cmd/leetd/main.go`, `internal/daemon/setup.go` | FR-010 | build/tests | ok |
| 6 | SECURITY.md at-rest claim corrected; guide pipe-to-shell removed (×2) | `SECURITY.md`, `docs/user-guide.html` | FR-011 | grep-clean; JSON re-parse; suite | ok |

## Build, test, and runtime verification

- `go build ./...`, `go vet ./...`, `gofmt -l` — clean on every pass.
- `go test ./... -count=1` — 14/14 packages ok, 0 FAIL (every pass).
- `go test -race` — targeted packages (passes 1–4) and a full `./...` race
  run in pass 7; no data races reported.
- `govulncheck ./...` — 0 vulnerabilities affecting code (passes 2, 7).
- Runtime: MCP stdio round-trip with actor attribution; two-node
  enroll→mTLS-sync→chat round-trip; UI endpoint probes incl. live CSRF 403;
  300-doc volume probe; `scripts/dist.sh` full 5-platform packaging run;
  fresh-clone build; release-candidate smoke (`version`, `check`, `tools/list`).
- Full command outputs were captured in the session log of each pass; counts
  quoted verbatim above.

## Wiki / FR update status

- Wiki location: `wiki/` in-repo. FR registry added this review:
  `wiki/FR-Registry.md` (FR-001…FR-011, each with finding ID, pass, files,
  rationale, evidence, remaining issues). Home page links all pages.
- Closeout status: **complete** (wiki existed; no creation decision needed).

## Remaining human decisions

1. **RR-009 (release blocker):** re-cut v0.1.9 through the fixed workflow or
   move "Latest" back to v0.1.8; then update README `LEET_VER` examples.
   *Owner with release rights.*
2. **Re-enroll this machine** to the team at 192.168.1.98 (identity
   overwritten during the RR-015 probe; see incident disclosure). *Owner.*
3. **Probe-era commits** in the real store (`~/LeetOffice`, 2026-11-03):
   leave as audit history or revert. *Owner's data, owner's call.*
4. **Commit the working tree:** 20 modified files + `wiki/` + two new LAN
   test files are uncommitted (the review never commits). Review and commit.
5. Optional: dependency minor/patch bumps (RR-017) at the next release;
   generated third-party license dump in CI (FR-007 remaining).

## Untested scope and limitations

- This review is structured inspection plus executed functional tests —
  **not a penetration test**; passing it does not certify the app secure,
  compliant, or bug-free.
- Windows: compile-verified via CI matrix and `dist.sh`; no runtime
  execution (project-documented limitation).
- Multi-physical-machine LAN behavior (real mDNS discovery between two
  hosts) not exercised — probes used loopback; mDNS code paths are
  test-covered in-process.
- Ollama semantic search not exercised (no local server; keyword fallback
  verified). Electron app not launched (packaging config reviewed
  statically; `npm run dist` not run).
- Full git history reviewed by pattern/blob scans only, not line-by-line.

## Verdict rationale

- **Application code:** all observed release-blocking code findings
  (RR-001, 002, 006, 015, 019) are fixed and verified; `READY` at the code
  level within the stated scope.
- **Release as a whole:** `NOT READY` — RR-009 (empty v0.1.9 release) is an
  unresolved blocker that only the owner can clear by publishing. Once it is
  re-cut and the README's version pins moved to it, no known blockers remain.
- Review workflow: **complete, 7/7 requested passes** — application readiness
  and workflow completion are separate statements, and only the former is
  gated on RR-009.

## Addendum — 2026-11-04 verdict update

- **RR-009 (the sole open blocker) is resolved by the owner:** v0.1.9 was
  re-cut and now carries all five platform binaries plus a well-formed
  `checksums-v0.1.9.txt` (verified live via the GitHub API and a raw
  download). `leetd update` and the install docs resolve "latest" again.
- Residuals of the re-cut, expected and already covered by the fixed
  workflow (FR-001): v0.1.9 was built from the pre-fix workflow, so it ships
  **no `install.sh`** and **no guide asset** (v0.1.8 had the latter), and the
  published checksums file does not yet include the installer's digest. The
  README's installer recipe therefore still points at a future tag — the
  binary recipe is the working path for v0.1.9.
- **Code-level verdict: READY within the stated scope** (all observed
  release-blocking code findings fixed and verified). **Release-level: one
  publishing step remains** — commit/push the reviewed working tree and cut
  the next release from the fixed workflow (which attaches `install.sh` +
  guide and digests both), then move the README's `LEET_VER` examples to
  that tag.
- Implemented since the report: FR-012, FR-014, FR-017, FR-020, FR-029
  (see [FR-Registry](FR-Registry.md), Done 2026-11-04, each with evidence).
