# LeetOffice — Build Runbook (RUNBOOK.md)

> **Purpose:** a self-contained brief you can hand to **any coding AI** (Claude Code, Codex, Hermes, OpenCode, etc.) — or a human developer — to build a working, compatible **LeetOffice** from scratch in their own environment.
>
> **How to use:** give your AI this file plus the two companion docs (`REQUIREMENTS.md`, `BUILD_SPEC.md`), or paste this file alone. Then say: *"Build this, and verify against the acceptance criteria."*
>
> **Clean-room friendly:** everything needed is in this repo's docs. No prior conversation required.

---

## 0. MISSION (state this to the AI)

Build **LeetOffice**: a **100% local, open-source, single-application framework** that lets **multiple humans and multiple AI agents** work together in one shared store — with block-level bidirectional links, unified team memory, full audit, and encrypted node-to-node sync. No cloud, no egress. One installable app; each node runs as a fat client by default and can be promoted to a server/coordinator.

## 1. BUILD ENVIRONMENT PREREQUISITES

Ensure the build machine has:
- **Go toolchain** (1.22+)
- **Node.js 20+** and a package manager (npm/pnpm) for the Electron app
- *(Optional, for RAG)* **Ollama** running locally with the `nomic-embed-text` embedding model
- macOS, Linux, or Windows

> **No cmake, no pkg-config, no cgo, no system-git requirement.** The Go stack is pure-Go (go-git, hashicorp/mdns), so `go build` needs nothing beyond the Go toolchain. Git server capability ships in-process via go-git (the on-prem git solution). There is no SQLite/`modernc.org/sqlite` in v0.1 — RAG is in-memory.

## 2. SOURCE OF TRUTH — READ THESE FIRST

Read these two files in full before writing any code:
1. `REQUIREMENTS.md` — the what/why, 18 decisions (D1–D18), 24-module map.
2. `BUILD_SPEC.md` — the precise technical contracts: tech stack, store schema, MCP tool schemas, node protocol, sync/audit rules, memory/RAG/registry internals, acceptance criteria.

> **Non-negotiable invariants** (from the spec) — do not violate these:
> - **100% local.** No cloud, no external service, no data egress.
> - **Single app.** One installable binary/app; client and server are roles of the same app.
> - **Headless daemon, no browser dependency.** The always-on node (`leetd`) never depends on a browser.
> - **Git is the audit trail.** Every change attributed (`human:<id>` / `agent:<id>`) + time-stamped.
> - **No silent overwrite.** Concurrent same-block edits keep BOTH versions and flag a conflict.
> - **mTLS membership.** Nodes must authenticate (enrollment secret → certificate) to join.
> - **Open formats.** Store is tabbed HTML + embedded JSON (canonical) + Markdown index.
> - **Strategic-content boundary.** Keep all tooling generic; never assume or embed private business content.

## 3. BUILD ORDER (phased, each phase verifiable)

Implement in this order. After each phase, run its gate before proceeding.

### Phase 1 — Core store (`leet-core`)
- Implement the store on-disk layout and the **block-aware JSON schema** (BUILD_SPEC §4).
- Embedded JSON is the single source of truth; HTML is a rendering; `INDEX.md` is derived.
- Implement **block-level bidirectional links** (create backlink atomically).
- Implement **field-level encryption** wrapper (AES-256-GCM) for sensitive fields.
- **Gate:** a doc round-trips (write JSON → parse → render → rewrite) losslessly; links resolve both ways.

### Phase 2 — Git sync & audit (`leet-sync`)
- Wrap git as the version/audit layer.
- Implement the **block-level merge driver** (`leet-merge`): merge at block level; same-block edits on both branches → keep both + flag conflict; never silently overwrite.
- Implement **auto-rejoin**: on reconnect, pull → merge → push → verify (BUILD_SPEC §6.5).
- **Gate:** two nodes editing different blocks merge cleanly; same-block edit flags a conflict, never loses data; `git log` shows attribution.

### Phase 3 — Node network & trust (`leet-net`)
- **mDNS discovery** (`_leetoffice._tcp`).
- **Enrollment:** new node presents one-time secret → coordinator issues **mTLS cert** from a local CA.
- **mTLS** on all node-to-node connections. Wrong secret / no cert → rejected.
- **Sync transport:** `leet://` over mTLS (go-git custom transport). Not SSH.
- **Role promotion / failover:** any client can be promoted to coordinator; store stays consistent.
- **Gate:** two nodes discover + sync over mTLS; a rogue node is rejected; promotion works.

### Phase 4 — MCP server & tools (`leet-mcp`)
- Implement the **9 MCP tools** with the exact contracts (BUILD_SPEC §5): `search`, `read_doc`, `write_doc`, `create_task`, `link`, `audit_query`, `diff`, `list_channels`, `send_message`.
- Every write is **attributed** (inject `actor` from the authenticated connection) + committed.
- Expose over stdio and HTTP so Hermes / Claude Code / Codex can connect.
- **Gate:** all 9 tools work end-to-end against the store; agent writes appear attributed in the audit trail.

### Phase 5 — Memory, digest, hygiene (`leet-memory`)
- **Memory synthesis** → `MEMORY.md`, near-continuous (debounced on change).
- **Daily digest** from the audit trail (`_audit/DIGEST-YYYY-MM-DD.md`).
- **Doc hygiene:** flag broken block-links, stale docs, unindexed files.
- **Gate:** `MEMORY.md` updates on change; digest produced; a deliberately broken link is flagged.

### Phase 6 — RAG / semantic search (`leet-rag`)
- Embed every block via **Ollama** when it is up; keep the index **in memory** and rebuild it per query. When Ollama is down, **keyword fallback** must still satisfy the `search` contract.
- **Memory-boosted ranking:** `MEMORY.md` + domain-summary docs rank higher.
- Exclude encrypted fields from the index.
- **Gate:** `search` returns relevant blocks with memory boosted to top (keyword path works without Ollama).

### Phase 7 — Registry (`leet-registry`)
- Implement the tool/skill **package format** (manifest.json) and **import/export**.
- **Stability lifecycle:** `experimental → stable` via clean-use counting, auto-promote at threshold (default 10); reset on failure/revert.
- **Gate:** a skill imports, is used N times, and auto-promotes to stable.

### Phase 8 — Human client (`app/` + `internal/httpui`)
- **Electron** thin wrapper (`app/main.js`) pins its Chromium. There is no React app under `app/src/`.
- Daemon `internal/httpui` serves the editor UI over localhost; Electron loads that URL; editing updates embedded JSON → audited commit.
- **Read-only browser fallback:** the tabbed HTML opens in any browser with no write path.
- **Gate:** edit works in the bundled app; browser shows read-only content; no reliance on the system browser.

### Phase 9 — Packaging & license
- Single installable artifact; default role = client, promotable to server.
- Add **Apache-2.0** `LICENSE` and a `NOTICE` file.
- Wire the **runbook** into the repo so the build is reproducible.
- **Gate:** a clean install of the app on a fresh machine starts a node that discovers and syncs.

## 4. VERIFICATION — RUN THE ACCEPTANCE CRITERIA

Run **BUILD_SPEC §12** acceptance criteria (A–G) and **§13** test plan. Do not call the build done until:
- Store round-trips; links resolve bidirectionally.
- Concurrent same-block edits flag a conflict (both retained) — never silent overwrite.
- A node taken offline then rejoined catches up fully with zero manual steps.
- Rogue node rejected; mTLS mutual auth works.
- All 9 MCP tools pass; agent writes attributed.
- Memory synthesizes near-continuously; digest + hygiene run.
- Registry auto-promotes at the threshold.
- Bundled app edits; browser fallback is read-only.

Report results per criterion. If any gate fails, fix the implementation before moving on — do not mark the phase done.

## 5. DELIVERABLES

A working v1:
1. **`leetd`** — Go daemon (store, sync, net, MCP, memory, rag, registry), single static binary.
2. **`leetoffice`** — Electron desktop app (human client).
3. **Bundled tools/skills** — the starter registry.
4. **Docs** — `README.md` (quickstart), this runbook, the spec.
5. **License** — Apache-2.0 + NOTICE.

## 6. NOTES FOR THE BUILD AI

- **Work incrementally and test as you go.** Do not attempt the whole thing at once.
- Prefer the simplest correct implementation for each phase; refine later.
- If a dependency (e.g. Ollama) is unavailable in the build environment, stub it cleanly and note it — but `search` must still return results (e.g. keyword fallback) so the tool contract holds.
- Keep everything **local and offline-capable**. No cloud keys, no hosted services.
- Keep the **strategic-content boundary**: build generic tooling only; never embed private business data.
