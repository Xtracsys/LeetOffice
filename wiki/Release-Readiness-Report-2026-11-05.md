# Release Readiness Report — LeetOffice (loop 2)

> Second release-readiness loop (corrective, 7 passes). Loop 1:
> [Release-Readiness-Report-2026-11-02](Release-Readiness-Report-2026-11-02.md) —
> scope, profiles, and standing decisions carry over; this page records what
> changed since.

## Summary

- **Project / repository:** LeetOffice (`Xtracsys/LeetOffice`)
- **Release artifact:** source tree + tag-triggered release binaries
- **Owner:** Xtracsys
- **Date of review:** 2026-11-05
- **Mode:** corrective — **passes: requested 7 / completed 7**
- **Applicable profiles:** P1/P3/P5/P6/P7 scoped, P2/P4/P8 N/A (unchanged from loop 1)
- **Verdict:** code-level `READY` within the stated scope; **release-level
  still gated on the same human step as loop 1** — commit/push the reviewed
  working tree and cut the next release from the fixed workflow.

Executive summary: the loop re-validated the whole checklist against the
current tree (which is unchanged since loop 1 — no commits landed) and found
**two critical defects, both fixed with live/runtime proof**: the updater's
naming contract was broken by the v0.1.9 hand-cut (every install ≤0.1.8 had
a dead update path), and sync could silently discard in-flight writes
(248 of 250 lost in a stress probe). A third, timing-dependent loss was
observed once in a live two-node probe and not reproduced — the complete fix
is proposed as FR-032. Final state: 14/14 packages, full `-race ./...` clean,
`govulncheck` 0 affecting, live updater e2e proven against real GitHub.

## Findings (loop 2; IDs continue loop-1 numbering)

| ID | Severity | Check(s) | Observed/Suspected | Location | Blocks release? | Status |
|---|---|---|---|---|---|---|
| RR-030 | critical | B11, S14, B15 | Observed (live, reproduced) | `internal/update/update.go` vs published v0.1.9 assets | Yes | Resolved (pass 2, FR-030) |
| RR-031 | critical | D7, S8, D6 | Observed (reproduced) | `internal/sync/git.go` | Yes | Resolved (pass 3, FR-031) |
| RR-032 | informational | B15, B16 | Observed | `docs/user-guide.html` | No | Resolved (pass 5) |
| RR-033 | medium | D7 | Observed once, unreproduced | two-node probe, sync burst | No* | Open → FR-032 (proposed) |

### RR-030 — updater vs the v0.1.9 hand-cut
The v0.1.9 re-cut uploaded assets as `leetd-v0.1.9-*` (tag verbatim); the
updater resolves `leetd-0.1.9-*`. Reproduced live: `leetd update --apply`
from a 0.1.8 build failed ("no leetd-0.1.9-darwin-arm64 in v0.1.9"). The repo
workflow cannot produce v-prefixed names — the re-cut bypassed it (its binary
reports commit `4c944cc`, not on local main). **Fix (FR-030):** `Latest`
accepts both names; regression test added; **live e2e re-run: 0.1.8 copy →
downloaded v0.1.9, SHA-256 verified, replaced itself, reports
`leetoffice v0.1.9 (4c944cc)`.**

### RR-031 — sync discarded in-flight writes
`Store.Save` writes without the repo lock; `Sync`'s fast-forward hard reset
and merge file writes silently discarded saves that landed during a sync
cycle. Stress probe: **248 of 250 writes lost** (3/3 rounds) — the writer's
`CommitAll` blocks on the mutex while sync holds it, exposing the unguarded
save through the entire fetch+reset. **Fix (FR-031):** `snapshotInFlight`
commits uncommitted state under the lock before every destructive worktree
operation; the fast-forward branch re-checks and takes the merge path when a
save landed mid-sync; `LEET_SYNC_TRACE=1` ops hook added during diagnosis.
Post-fix probe: **0 of 60 lost**; permanent regression test
(`TestSyncDoesNotDiscardInFlightSaves`, skipped in `-short`); full suite and
`-race` clean.

### RR-033 — one message lost in the live probe (open, gated on FR-032)
The first live two-node probe lost the client's chat message: it was
committed (recovered as a dangling commit via `git fsck`) and then orphaned
by a fast-forward during the enrollment-time sync burst. The clean rerun
converged correctly (both messages on both nodes). Labeled
*observed once, unreproduced* per the checklist's honesty rule; the residual
micro-window and the complete fix are documented as proposed FR-032.

### RR-032 — guide drift
`docs/user-guide.html` still carried a paragraph describing the removed
installer script plus three stale `v0.1.1` mentions (badge, example output,
troubleshooting pins). Fixed: paragraph rewritten to the binary+checksum
recipe, badge/example bumped to v0.1.8, troubleshooting pins made
version-floors ("v0.1.1 or newer"). Embedded JSON re-parsed after each edit.

## Per-pass evidence ledger

| Pass | Coverage highlights | Build & tests | New findings | Resolved |
|---|---|---|---|---|
| 1 | Discovery delta (no new commits; 51 files uncommitted; identity not re-enrolled), full baseline, B1/B2 scans, live release check | build/vet/gofmt clean; 14/14; 0 FAIL | — | — |
| 2 | Live updater e2e against real GitHub; release-asset audit | e2e failed → RR-030 | RR-030 | RR-030 |
| 3 | FR-016/FR-031 stress probe (3×250 iters) | pre-fix 248/250 lost → fix → 0/60; suite 14/14; race clean | RR-031 | RR-031 |
| 4 | Live two-node conflict-path e2e (enroll, concurrent chat, convergence) | first run lost one message (RR-033); clean rerun converged | RR-033 | — (FR-032 proposed) |
| 5 | Docs/claims sweep (README, SECURITY.md, guide) | guide JSON re-parsed; suite green | RR-032 | RR-032 |
| 6 | Full `-race ./...`; D2/D6 sweeps; gofmt | 14/14 race-clean; sweeps clean | — | — |
| 7 | Final regression (build/vet/fmt/suite/govulncheck) | 14/14; govulncheck 0 affecting | — | final verification of pass-5/6 changes |

## Changes made (loop 2)

| Pass | Change | Files | FR ID | Verified by |
|---|---|---|---|---|
| 2 | Updater accepts tag-prefixed asset names | `internal/update/update.go`, `vnamed_test.go` | FR-030 | unit test + live e2e |
| 3 | Sync snapshots in-flight writes; fast-forward re-check; `LEET_SYNC_TRACE` | `internal/sync/git.go`, `race_probe_test.go` | FR-031 | stress probe 248/250 → 0/60; suite; race |
| 5 | Guide paragraph + version stamps corrected | `docs/user-guide.html` | (extends FR-011) | grep + JSON re-parse |

## Remaining human decisions (unchanged from loop 1, still pending)

1. Commit & push the working tree (now 55 files); cut the next release from
   the fixed workflow; move README `LEET_VER` pins to it.
2. Re-enroll this machine to the 192.168.1.98 team (identity still the
   Nov-2 probe certs).
3. Decide on the probe-era commits in the real store.

## Untested scope and limitations

- Same as loop 1 (not a penetration test; Windows runtime unexercised; no
  second physical machine; Ollama path not exercised; Electron not launched).
- RR-033 is observed-once/unreproduced; its complete fix (FR-032) is
  proposed, not implemented.

## Verdict rationale

- Review workflow: **complete, 7/7 passes.**
- Application code: `READY` within scope — both critical findings fixed with
  live proof; no unresolved code blockers known.
- Release: pending the owner's commit/push + next cut (identical to loop 1's
  addendum). Until then, v0.1.9's binaries are installable and — with FR-030
  shipped in the *next* release — updatable.
