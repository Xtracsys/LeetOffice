# LeetOffice — Build Specification (BUILD_SPEC)

> **Companion to:** `REQUIREMENTS.md` (the what/why). This file is the *how* — precise, buildable contracts.
> **Companion to:** `RUNBOOK.md` (a prompt any AI can consume to build this from scratch).
> **Version:** 0.1 · **Status:** implemented (v1 — see README.md; phase gates verified by `go test ./...`)
> **License target:** Apache-2.0

## 1. Purpose

Turn the LeetOffice requirements into **concrete, implementable contracts** so that any developer — or any coding AI given `RUNBOOK.md` — can build a working, compatible implementation. This spec fixes:

- The **tech stack** and repository layout.
- The **store schema** (block-aware JSON inside tabbed HTML + Markdown index).
- The **MCP tool contracts** (the agent surface).
- The **node protocol** (discovery, enrollment, mTLS, sync).
- The **sync/reconciliation/audit rules**.
- The **memory, RAG, and registry** internals.
- **Acceptance criteria** (what "done" means, verifiably).

## 2. Tech Stack

| Layer | Choice | Rationale |
|---|---|---|
| Core daemon (`leetd`) | **Go** (single binary, `net/http`) | Always-on headless service; fast compiles; one toolchain; static binary per platform |
| Git sync | **`go-git`** (in-process) | Ships the on-prem git server **in the binary** — no system git / external git needed (M4) |
| mTLS + certs | **`crypto/tls`** + local CA (`crypto/x509`) | Mutual TLS, self-contained, no cloud |
| mDNS discovery | **`hashicorp/mdns`** (Go) | Zero-config node discovery on LAN |
| MCP server | **MCP over stdio + HTTP** (spec v1.x) | Any MCP client (Hermes, Claude Code, Codex) |
| Desktop app (`leetoffice`) | **Electron + React + TypeScript** | Pins its own Chromium → immune to system-browser updates |
| Bundled engine | **Electron** | Version-locked renderer (D14 requirement) |
| Embeddings / LLM | **Ollama** (local) | Fully local RAG; model `nomic-embed-text` |
| Metadata / vector index | **SQLite** via `modernc.org/sqlite` (pure Go, no cgo) + FTS5 | Local index alongside git-backed files |
| Field-level encryption | **`crypto/aes` + GCM** (Go stdlib) | Sensitive JSON fields encrypted at rest |

**Why not CRDT?** Store is file-based (D1). Realtime co-editing can be added later via Yjs if needed; v1 uses short-cadence git sync (D5).

**No cgo / native build deps.** The Go stack uses pure-Go libraries (go-git, modernc sqlite), so the build needs **no cmake, no pkg-config** — just the Go toolchain.

## 3. Repository Layout

```
leetoffice/
├── go.mod                       # single Go module
├── cmd/
│   └── leetd/main.go            # the daemon entrypoint
├── internal/
│   ├── store/                   # store, schema, block model, links, audit
│   ├── sync/                    # go-git, merge driver, monitor, auto-rejoin
│   ├── net/                     # mDNS, mTLS CA, enrollment, protocol
│   ├── mcp/                     # MCP server + tool implementations
│   ├── memory/                  # synthesis, digest, hygiene
│   ├── rag/                     # embeddings, index, semantic search
│   └── registry/                # tools/skills registry + stability lifecycle
├── app/                         # Electron + React frontend (leetoffice)
│   ├── src/
│   └── package.json
├── tools/                       # bundled tools (registry)
├── skills/                      # bundled skills (registry)
├── docs/                        # this spec, runbook, examples
├── LICENSE                      # Apache-2.0
└── NOTICE                       # third-party attributions
```

## 4. Store Schema

### 4.1 On-disk layout

Each workspace document is a **self-contained tabbed HTML file** with an **embedded JSON payload**. The store root is a git repository.

```
<main-share>/
├── .git/
├── INDEX.md                       # Markdown index (discoverability layer)
├── MEMORY.md                      # team memory (M15)
├── docs/
│   ├── <slug>.html                # tabbed HTML doc + embedded JSON
│   └── ...
├── tasks/
├── contacts/
└── _audit/                        # per-entity audit logs (human-readable)
```

### 4.2 Embedded JSON — the single source of truth (D1)

The JSON lives in a `<script type="application/json" id="leet-doc">` inside each HTML file. **It is canonical**; the HTML tabs render from it; the MD index reads from it. All reconciliation diffs the JSON, not the HTML.

```json
{
  "$schema": "https://leetoffice.dev/schema/doc.json",
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "type": "doc | task | contact | channel | company | email | memory",
  "slug": "imaging-runbook",
  "title": "XtracBox Imaging Runbook",
  "version": 128,
  "updated": "2026-08-15T12:34:56Z",
  "tags": ["imaging", "xtracbox"],
  "blocks": [
    {
      "id": "blk_1a2b3c4d",
      "type": "paragraph | heading | task-item | list-item | code | field | divider",
      "content": "Boot the target from the Ventoy USB…",
      "level": 0,
      "meta": {},
      "links": [
        { "id": "lnk_x", "target_doc": "uuid", "target_block": "blk_..." ,
          "label": "master USB", "dir": "out" }
      ]
    }
  ],
  "properties": {},
  "audit": {
    "last_editor": "human:josh | agent:hermes-uuid",
    "last_commit": "a1b2c3d4e5f6…"
  }
}
```

### 4.3 Block model & block-level links (D15)

- Every block has a stable `id`. Links attach at the **block** level.
- A link `{target_doc, target_block, dir}` in block A **creates a backlink** in block B (`dir:"in"`). The graph is bidirectional and reconstructed by scanning links (or cached in SQLite).
- The `link` MCP tool (see §5) creates both directions atomically.
- **Hygiene** (M19) reports any link whose `target_doc`/`target_block` no longer resolves.

### 4.4 Markdown index (`INDEX.md`)

Updated by the daemon (and by doc hygiene). One row per document — the human/agent-readable map:

```markdown
| slug | type | title | updated | tags | link-count |
|------|------|-------|---------|------|-----------|
| imaging-runbook | doc | XtracBox Imaging Runbook | 2026-08-15 | imaging | 12 |
| prd-v1 | doc | PRD v1 | 2026-08-14 | product | 34 |
```

### 4.5 Field-level encryption (D2)

A JSON field whose value must be encrypted at rest is wrapped:

```json
"credentials": { "enc": true, "alg": "AES-256-GCM", "data": "<base64 ciphertext>" }
```

Key material is provided by the node's keyring (or the Cryptomator volume if used). Encrypted fields are never indexed by RAG.

## 5. MCP Tool Contracts

The MCP server (`leet-mcp`) exposes these tools. All are **attributed + audited** (D7): every write tags the acting `agent_id`/`human_id` and produces a git commit.

| Tool | Input | Output |
|---|---|---|
| `search` | `{query, type?, tags?, limit?=10}` | `[{doc_id, block_id, snippet, score}]` |
| `read_doc` | `{id \| slug, block_id?}` | `{doc: DocJson, text?: rendered}` |
| `write_doc` | `{id, block_id?, content, replace?}` | `{version, commit_sha}` |
| `create_task` | `{title, body?, assignee?, links?[]}` | `{task_id, url}` |
| `link` | `{from_doc, from_block, to_doc, to_block?, label?}` | `{edge_id}` (bidirectional) |
| `audit_query` | `{doc_id?, since?, actor?}` | `[{change, actor, when, commit_sha}]` |
| `diff` | `{id, from_version, to_version}` | `{unified_diff, blocks_added, blocks_removed}` |
| `list_channels` | `{}` | `[{channel, messages}]` — team chat channels + activity (M25) |
| `send_message` | `{channel, text}` | `{message_id, channel}` — auto-creates the channel (M25) |

**Tool identity:** each tool call carries an `actor` context (`agent:<id>` or `human:<id>`) injected by the daemon from the authenticated connection. This is what powers audit attribution.

**Communication (M25):** team chat is a first-class surface. **Channels are documents** (type `channel`); **messages are blocks** within them. Because blocks are the merge unit (D6), concurrent messages merge cleanly and are git-audited + attributed like any content. Agents participate bidirectionally: `list_channels` + `send_message`, and read via `read_doc`/`search`. See `internal/chat`.

## 6. Node Protocol

### 6.1 Roles
- **Client node:** local store + local git + local MCP; offline-capable; syncs to the main share.
- **Coordinator node:** hosts the git **bare repo** (the main share), runs the Consistency Monitor, Memory Synthesis, backup. Any client can be **promoted** (D4).

### 6.2 Discovery (mDNS)
Nodes announce `_leetoffice._tcp`, advertising `{node_id, host, port, role, cert_fingerprint}`. Clients resolve the coordinator without config.

### 6.3 Enrollment (trust gate, D8)
1. New node generates a keypair and presents a **one-time enrollment secret** (the "team password") to the coordinator over a TLS connection.
2. Coordinator verifies the secret, issues an **mTLS certificate** signed by the team CA (fingerprinted from the coordinator).
3. Node stores its cert + key; thereafter every connection is **mutually authenticated** via mTLS.
4. Rogue nodes (no valid cert / wrong secret) are rejected; their traffic is unreadable.

### 6.4 Sync transport
- **git over SSH**, authenticating with the node's mTLS identity (SSH key issued at enrollment), or a minimal custom transport over the mTLS channel. SSH is the v1 default (battle-tested).
- **Cadence:** every 5 seconds when online (D5). Immediate on rejoin.

### 6.5 Auto-rejoin (D6/M8)
On detecting the coordinator (mDNS) after being offline:
1. mTLS handshake with existing cert (no re-enrollment).
2. `git pull --rebase` (or `fetch` + block-merge).
3. Apply local changes → commit → `git push`.
4. Monitor verifies no drift; conflicts routed to reconciliation (§7).

## 7. Sync, Reconciliation & Audit

### 7.1 Git is the audit trail (D3)
Every change is a commit with `author = human:<id> | agent:<id>` and a timestamp. `git log` IS the audit log; `audit_query` reads it.

### 7.2 Block-level merge driver (D6)
Raw git can't merge concurrent HTML edits. LeetOffice ships a **git merge driver** (`leet-merge`) for the embedded JSON:

- Merge at the **block level**, not line level.
- Blocks edited in only one branch → take that version.
- Blocks edited in both branches → keep **both** and mark the later one with a `"conflict": true` flag + a monitor alert (do **not** silently overwrite a human edit).
- `INDEX.md`/`MEMORY.md` regenerate post-merge.

### 7.3 Consistency Monitor (M9)
A loop on the coordinator watches git state + filesystem:
- Detects **drift** (a node that hasn't pushed / an unexpected change).
- Flags **conflicts** and unmerged blocks.
- Runs **doc hygiene** (broken links, stale docs, unindexed files) on a schedule.
- Emits an alert into the store (a `NOTICE.md`) on anything requiring a human.

## 8. Memory & Intelligence

### 8.1 Memory synthesis (M15/D16)
A daemon job reads the whole store and writes `MEMORY.md`:

```markdown
# Team Memory
_updated 2026-08-15 12:35Z_

## In flight
- …

## Decisions
- …

## Open tasks
- …

## Owners / context
- …
```

**Cadence:** near-continuous — debounced re-run on store change (e.g. 60s quiet), not only nightly.

### 8.2 Daily digest (D16)
A cron job writes `_audit/DIGEST-YYYY-MM-DD.md` from the audit trail: what changed, by whom, new tasks, notable links.

### 8.3 RAG / semantic search (D17)
- **Index:** every block embedded via **Ollama** (`nomic-embed-text`); vectors in SQLite.
- **Boost:** `MEMORY.md` and domain-summary docs get a score bonus so curated context ranks first.
- **Query:** `search` embeds the query, retrieves top-K, applies boost, returns blocks with scores.
- Encrypted fields are excluded from the index.

## 9. Skills & Tools Registry (D11/D12)

### 9.1 Package format
Each tool/skill is a folder under `tools/` or `skills/`:

```
skills/xtracbox-imaging/
├── manifest.json
├── SKILL.md          # the procedural instructions (Hermes-skill compatible)
└── assets/           # optional templates/scripts
```

`manifest.json`:
```json
{
  "name": "xtracbox-imaging",
  "kind": "skill",
  "version": "1.2.0",
  "stability": "experimental | stable | deprecated",
  "tools": ["create_task", "search", "write_doc"],
  "clean_uses": 3,
  "author": "human:josh"
}
```

### 9.2 Import / export (D11)
- **Import:** copy the folder into the registry + git commit. Safe — files, reversible.
- **Export:** package the folder (optionally as a single `.zip`/`.tar.gz`) for sharing/backup.
- Versioning maps to git tags; every upgrade is audited and reversible.

### 9.3 Stability lifecycle (D12 — promoted-on-proof)
- New tools/skills start `experimental` and are usable.
- Each **clean use** (tool call succeeds with no error and no revert) increments `clean_uses`.
- At the configurable threshold (default 10), the daemon auto-promotes to `stable` (a git commit records the promotion).
- A revert or failed use resets `clean_uses`. `deprecated` is manual.

## 10. Security Model

| Concern | Mechanism |
|---|---|
| Transport | mTLS/TLS on all node-to-node traffic (D8) |
| Membership | enrollment secret → mTLS cert; no impersonation |
| At rest | Cryptomator volume + field-level AES-256-GCM (D2) |
| Agent writes | attributed + audited, block-level merge protects humans (D6/D7) |
| Boundary | framework tools generic; strategic content private, never in OSS surface |

## 11. Human Client (D14)

- **Bundled Electron app** pins its Chromium → immune to system-browser updates.
- The daemon serves the **editor UI** over `localhost`; Electron wraps it. Editing updates the embedded JSON → git commit (audited).
- **Read-only browser fallback:** the tabbed HTML opens in any browser with no write path.

## 12. Acceptance Criteria (verifiable "done")

**A. Store**
- [ ] Creating/editing a doc writes canonical JSON; HTML tabs render from it.
- [ ] `INDEX.md` stays in sync after writes.
- [ ] Block-level bidirectional links resolve in both directions.

**B. Sync/audit**
- [ ] Two nodes editing different blocks of the same doc both merge without data loss.
- [ ] Same-block concurrent edits flag a conflict (both retained) + monitor alert — never silent overwrite.
- [ ] `git log` shows every change attributed to a human or agent ID.
- [ ] A node offline then rejoined catches up fully with zero manual steps.

**C. Nodes/trust**
- [ ] Two nodes discover each other via mDNS.
- [ ] A node with a wrong enrollment secret is rejected; a rogue node's traffic is unreadable.
- [ ] Promoting a client node to coordinator (failover) keeps the store consistent.

**D. Agents**
- [ ] All 7 MCP tools (search, read, write, create_task, link, audit_query, diff) work against the store.
- [ ] Agent writes are attributed in the audit trail.

**E. Memory/RAG**
- [ ] `MEMORY.md` updates near-continuously on change.
- [ ] Daily digest is produced from the audit trail.
- [ ] `search` returns relevant blocks, with `MEMORY.md` boosted to top results.
- [ ] Doc hygiene flags a deliberately broken link.

**F. Registry**
- [ ] A tool/skill imports, is used N times, and auto-promotes to stable at the threshold.

**G. Human client**
- [ ] Editing works in the bundled app; opening the HTML in a browser shows read-only content.

## 13. Test Plan

- **Unit:** block-merge logic, link resolution, registry promotion, field encryption.
- **Integration:** two-node sync + conflict; rejoin after offline; MCP tools end-to-end.
- **Security:** wrong-secret rejection, mTLS mutual auth, encrypted-field at rest (bytes unreadable on disk).
- **E2E:** the G acceptance criteria scenarios.

## 14. Definition of Done for v1

A minimal working LeetOffice where: one coordinator + one client node sync over mTLS; a human edits a doc in the bundled app and an agent (via MCP) reads it and creates a task; both changes appear in `git log` attributed; `MEMORY.md` synthesizes; the daily digest runs; and the acceptance criteria above pass. Packaged as a single installable app, Apache-2.0, with `RUNBOOK.md` verified by a clean-room build.
