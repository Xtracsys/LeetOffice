# LeetOffice Wiki

**A 100% local workspace where humans and AI agents work as one team.**
Chat, docs, tasks, and links in a single store — full git audit, encrypted
mTLS sync, zero cloud, zero egress.

This wiki is the living documentation next to the spec-first docs in the
repo root:

| Doc | Role |
|---|---|
| [REQUIREMENTS.md](../REQUIREMENTS.md) | The what/why — modules M1–M28, decisions D1–D24, open questions |
| [BUILD_SPEC.md](../BUILD_SPEC.md) | The contracts — schema, tool surface, protocol |
| [RUNBOOK.md](../RUNBOOK.md) | The build order the repo followed, gate by gate |
| [SECURITY.md](../SECURITY.md) | How to report vulnerabilities |
| **This wiki** | Architecture, audits, debug logs, enhancement backlog |

## Pages

- **[Architecture](Architecture.md)** — how the node fits together: store,
  sync, net, MCP, daemon, UI. Read this first.
- **[FR Registry](FR-Registry.md)** — corrective-change records
  (FR-001…) from release-readiness review passes, with finding IDs and
  verification evidence.
- **[Security Audit — 2026-10-29](Security-Audit-2026-10-29.md)** — full
  audit of the network, store, UI, updater, and supply chain. All findings
  fixed in the same session; verdict and residual risks documented.
- **[Debug Log — 2026-10-29](Debug-Log-2026-10-29.md)** — the failing-test
  session: two failing tests, a test that wrote into a **real** user store,
  a config data race, and a self-deadlocking log filter. All root-caused
  and fixed.
- **[Enhancements](Enhancements.md)** — the top-10 feature-request backlog,
  each grounded in a spec decision or an audit finding.
- **[Release-Readiness Report — 2026-11-02](Release-Readiness-Report-2026-11-02.md)** —
  the first 7-pass pre-release review: findings, per-pass evidence, changes,
  and the readiness verdict.
- **[Release-Readiness Report — 2026-11-05](Release-Readiness-Report-2026-11-05.md)** —
  second 7-pass loop: updater/release-name drift and the sync lost-write
  race found and fixed with live proof; one timing-dependent loss documented
  (FR-032).

## The 60-second mental model

```
 you (browser/Electron)  ─┐
 teammate (her machine)  ┼─ encrypted mTLS sync ─→ one audited store
 agent (Claude/Codex)    ┘        (git under the hood)
```

- One binary (`leetd`), one store (`~/LeetOffice`), one config
  (`~/.config/leetoffice/node.json`).
- A **document** is a self-contained tabbed HTML file whose canonical
  payload is embedded JSON (`<script id="leet-doc">`).
- A **channel** is a document; a **message** is an attributed block — chat
  inherits durability, search, and lossless block-level merging.
- **Every write is a git commit** attributed to a human or agent actor;
  `History` in the UI is `git log`.
- **Nodes authenticate with mTLS certificates** from a local team CA; the
  enrollment endpoint trades a one-time invite code for a certificate.
- **Agents join through MCP** (12 tools over stdio or HTTP) — identity is
  injected by the server (D7), never self-reported.

## Ground rules (from CONTRIBUTING.md)

1. Stay local — nothing egresses except the user-initiated update check.
2. Never silently overwrite — concurrent same-block edits keep both
   versions, flagged (`meta.conflict`).
3. Git is the audit trail — every mutation is attributed.
4. Pure Go — no cgo, static binary, ~13 MB, zero dependencies at runtime.

## Maintenance notes for wiki editors

- The wiki pages under `2026-10-29` are point-in-time audit/debug records:
  keep them as written (they cite line-level evidence), and add new
  dated pages rather than editing history.
- `Enhancements.md` is a living backlog — move items to "Shipped" with a
  link to the PR/commit instead of deleting them.
