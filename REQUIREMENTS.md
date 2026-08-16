# LeetOffice — Local-First Multi-Agent + Multi-Human Workspace Framework

> **Working title:** LeetOffice
> **Status:** Requirements in progress (living document)
> **Project folder:** `/Volumes/XSdrive/Projects/leetoffice`
> **Last updated:** 2026-08-15

## 1. Purpose & Vision

A self-contained, **100% local** workspace framework that lets **multiple humans and multiple AI agents** (Hermes desktop, Hermes TUI, Claude Code, Codex, any MCP-capable client) **work and communicate together** in one shared store — with unified team memory, full audit, and no cloud, no egress.

Why: today a small team is held together by stitching Slack + Linear + Notion + HubSpot + Superhuman and gluing them with MCP/Zapier. LeetOffice is a complete redesign of that as a single local system where documents, tasks, and links live in one graph that both people and agents operate on.

Primary goals:
1. **Boost Xtracsys productivity** — one local workspace for docs, tasks, notes, and agents.
2. **Reusable** across the day job and other projects.
3. **Street credibility** in the builder community — a cleanly scoped, open, local-first agent workspace.

**Strategic note:** The *framework* is open and generic. Anything tied to the Xtracsys strategic plan stays **private** and is never part of the open-source tool surface.

## 2. Non-Negotiable Principles

| # | Principle | Meaning |
|---|---|---|
| P1 | **100% local** | Everything runs on our machines. No cloud, no external service, no egress. |
| P2 | **Open formats** | Documents are openable files (HTML/JSON/Markdown), not proprietary blobs. |
| P3 | **Local git only** | Git is a first-class module. No external git service until the project is finished and packaged for GitHub. |
| P4 | **Audit on everything** | Every change is attributed (human or agent ID) and time-stamped. |
| P5 | **Single app** | One installable app; each node can be a client or be promoted to server. |
| P6 | **Trusted membership** | Nodes discover each other on the LAN but must authenticate (mTLS) to join. |
| P7 | **Offline-first** | Every node works fully offline and auto-rejoins + syncs on reconnect. |

## 3. Acronym Decoder

- **MCP — Model Context Protocol.** Open standard letting an AI agent connect to external tools/data through a server exposing "tools" the agent can call.
- **CRDT — Conflict-free Replicated Data Type.** Data structure allowing many editors to edit concurrently and converge automatically.
- **OT — Operational Transformation.** Older realtime co-editing algorithm (Google Docs lineage).
- **RAG — Retrieval-Augmented Generation.** Grounding an AI answer by retrieving relevant notes first, then feeding them to the model as context.
- **Vault / Store —** the folder of files holding the workspace content.
- **mDNS / Bonjour —** local-network service discovery (how nodes announce and find each other).
- **mTLS — Mutual Transport Layer Security.** Encryption where both sides verify each other's certificates.
- **Embeddings —** numerical vector representations of text enabling semantic search.
- **CLI / TUI / Desktop —** Command-Line Interface / Text User Interface / graphical app. Hermes ships as all three.

## 4. Architecture & Topology

**Model: Hybrid, single application, role-capable.**

- One installable app (`leetoffice`).
- **Default role: fat client** — full local copy of the store, its own local git, its own local MCP server, fully offline-capable.
- **Promotable role: server / coordinator** — runs the git service (bare repo), the Consistency Monitor, nightly Memory Synthesis, and backup. Any node can be promoted if the primary is unavailable (failover).
- **Main share:** an authoritative store whose **location is configurable** (default: coordinator node; could be a folder or disk).

```
                 ┌──────────────────────────────────────┐
                 │  COORDINATOR NODE  (Ubuntu box)       │
                 │  · git service (bare repo)            │
                 │  · Consistency Monitor (watchdog)     │
                 │  · Memory Synthesis                   │
                 │  · Backup (→ XSdrive)                 │
                 └───────────────▲──────────────────────┘
                       encrypted │ mTLS / TLS (LAN)
        ┌────────────────────────┼────────────────────────┐
        │                        │                        │
┌───────┴──────┐        ┌────────┴───────┐        ┌───────┴──────┐
│  FAT CLIENT  │        │  FAT CLIENT    │        │  FAT CLIENT  │
│  Mac (dev)   │        │  laptop        │        │  any TUI     │
│  local store │        │  local store   │        │  local store │
│  local git   │        │  local git     │        │  local git   │
│  local MCP   │        │  local MCP     │        │  local MCP   │
└──────────────┘        └────────────────┘        └──────────────┘
```

## 5. Module Map

| ID | Layer | Module | Status |
|----|-------|--------|--------|
| M1 | Store | **Workspace Store** — tabbed HTML files + embedded JSON + Markdown index | ✅ Decided |
| M2 | Store | **Content Model & Bidirectional Links** — notes/tasks/docs/contacts + graph | ✅ Decided |
| M3 | Store | **Encryption at rest** — volume-level + field-level | ✅ Decided |
| M4 | Sync | **Git service module** (central bare repo) | ✅ Decided |
| M5 | Sync | **Local git** (per fat client) | ✅ Decided |
| M6 | Sync | **Short-cadence encrypted sync** (near-continuous) | ✅ Decided |
| M7 | Sync | **Reconciliation / conflict resolution** | ✅ Decided |
| M8 | Sync | **Auto Rejoin & Sync-on-Reconnect** | ✅ Decided |
| M9 | Sync | **Consistency Monitor / Watchdog** | ✅ Decided |
| M10 | Access | **Local MCP server** (per node) | ✅ Decided |
| M11 | Access | **Tool surface** (search, read, write, task, link, audit, diff) | 🟡 Open |
| M12 | Access | **Agent identity & audit attribution** | ✅ Decided |
| M13 | Trust | **Node authentication & team membership (mTLS)** | ✅ Decided |
| M14 | Trust | **Role & promotion / failover** | ✅ Decided |
| M15 | Memory | **Unified team memory** — near-continuous synthesis → MEMORY.md | ✅ Decided |
| M16 | Memory | **Local semantic search / RAG** — whole store, memory-boosted (Ollama) | ✅ Decided |
| M17 | Clients | **Human client** — bundled desktop app (pinned engine) + browser read-only fallback | ✅ Decided |
| M18 | Clients | **Agent clients** — any MCP client (Hermes desktop/TUI, Claude Code, Codex) + CLI | ✅ Decided |
| M19 | Automation | **Automations** — memory synthesis, daily digest, doc hygiene | ✅ Decided |
| M20 | Automation | **Integrations** (GitHub/PRs, email, external links) | 🟡 Open |
| M21 | Security | **Local-only guarantee, data-at-rest** | ✅ Decided |
| M22 | Packaging | **Profiles / config, installer, docs, GitHub release** | 🟡 Open |
| M23 | Extensibility | **Skills & Tools Registry** — import/export, versioning, stability lifecycle | ✅ Decided |
| M24 | Packaging | **Build Specification & Runbook** — buildable spec + AI build prompt | ✅ Decided |
| M25 | Communication | **Team chat** — humans + agents converse in shared channels; messages are blocks | ✅ Decided |

## 6. Decisions Log

### D1 — Store format (M1)
Tabbed **HTML files** with **embedded JSON** and a **Markdown index**.
- HTML = self-contained, rich, openable in any browser.
- **Embedded JSON = the single source of truth** for each document's structured content. The tabbed HTML is a *rendering* of the JSON; the Markdown index is the *discoverability* layer. JSON canonical → tabs render from it → index reads from it → no drift between three copies.
- Reconciliation diffs on JSON + MD index (clean) rather than raw HTML (noisy).

### D2 — Encryption at rest (M3)
Layered approach (avoids breaking git diffing / indexing):
- **Volume-level:** Cryptomator (or VeraCrypt) over the project folder — files plain *while mounted*, encrypted *at rest*.
- **Field-level:** sensitive values (credentials, personal data) encrypted even when mounted.
- XSdrive is a backup disk → encrypting the project there is the right instinct.

### D3 — Sync & git (M4/M5)
**Local git only.** Git is a module. **No external git service until packaged for GitHub.**
- Central bare repo on the coordinator = the **git service module**.
- Each fat client runs its own **local git**.
- Git provides durable version history = the **audit trail**.

### D4 — Topology (M4/M5/M14)
**Hybrid, single app, role-capable.** One installable app; fat client by default, any node promotable to server. Rejected: pure central-server/thin-client (offline dies) and pure peer-to-peer mesh (no single audit/consistency anchor).

### D5 — Update transport (M6)
**Short-cadence encrypted git sync** (near-continuous) over **mTLS/TLS**, not a live realtime event stream. Rationale: better offline/rejoin fallback — a node can be offline any length of time and catch up on reconnect.

### D6 — Reconciliation (M7/M8/M9)
- **Audit trail on everything** (attributed + time-stamped).
- **Notes are merged**, not overwritten.
- A **Consistency Monitor / Watchdog** watches the store + git for drift/conflicts and flags "additional changes."
- **Auto Rejoin:** on reconnect, a node re-authenticates with its existing mTLS cert, pulls, merges, pushes, and the monitor verifies — **zero manual steps**.

### D7 — Agent access model (M10/M11/M12)
**Read-write with full attribution + audit.** Agents write freely; every change tagged with agent ID + timestamp (git history). No approval queue (considered and rejected as too much friction). Safety comes from audit + monitor.

### D8 — Node authentication & membership (M13)
Nodes discover each other over the LAN (mDNS). To **join the team**, a node presents a **team enrollment secret** (one-time pairing code) and receives an **mTLS certificate**. Thereafter every connection is mutually authenticated + encrypted. Rogue machines can neither join nor read traffic. **All local — no cloud auth provider.**

### D9 — Unified team memory (M15)
**Near-continuous synthesis** into a living `MEMORY.md` capturing current state: what's in flight, key decisions, open tasks, owners, context. Agents read it for full team context. Hybrid: local memory on each node + one configurable main share.

### D10 — Main share (M4)
The authoritative store's location is **configurable** (default: coordinator node; can point to any folder/disk).

### D11 — Skills & Tools Registry (M23)
The framework ships a **local registry** for agent **tools** and **skills**:
- **Import/export** — each tool/skill bundles into one unit (folder/single file) that can be moved between nodes, backed up, or ported to another project.
- **Versioning** — every tool/skill carries a version; upgrades are git-committed, audited, and reversible.
- **Stability lifecycle** — `experimental` → `stable` (→ `deprecated`). New tools/skills start experimental; promotion to stable happens only after successful real-world use. Agents rely on stable; experimental is flagged.
- Purpose: the framework **grows from real use** — import a tool, dogfood it on Xtracsys work, promote it to stable once proven, then it's shared team-wide.

### D12 — Stability gate (M23)
**Promoted-on-proof.** Experimental tools/skills are fully usable, and the framework **auto-promotes them to stable after N clean uses** (configurable, default ~10) with no error or revert. Leans on the audit + monitor to catch failures. The promotion threshold is configurable per tool/skill.

### D13 — Build specification & runbook (M24)
Before building, produce a **complete, unambiguous build spec** so anyone can rebuild the tool in their own environment. Deliverables:
- **REQUIREMENTS.md** (this file) — the what/why, decisions, module map.
- **BUILD_SPEC.md** — the precise, buildable technical spec: store schema, MCP tool contracts, node protocol, sync/reconciliation rules, module interfaces.
- **RUNBOOK.md / BUILD_PROMPT.md (AGENTS.md)** — a self-contained document someone can hand to their **own AI** (Claude Code, Codex, Hermes, OpenCode) to build the tool from scratch in their environment.
Goal: fully reproducible, community-credible — a stranger with only these docs can prompt their AI and get a working build.

### D14 — Human & agent clients (M17/M18)
**Bundled desktop app + browser fallback.** The human interface is a **bundled desktop app that ships its own pinned rendering engine** (not the system browser), so browser/OS updates can't break it. The tabbed HTML files remain openable in any browser as a **read-only fallback**. Agent clients are **any MCP-capable client** (Hermes desktop/TUI, Claude Code, Codex) plus a CLI. The **node daemon is headless** — no browser dependency — and runs always-on as a system service (launchd/systemd).

### D15 — Bidirectional link granularity (M2)
**Block-level links.** Links attach to a specific *block* inside a document (a paragraph, checklist item, field) rather than whole documents. This is what makes the graph genuinely powerful — pointing at the exact idea, not the whole page. Requires a careful block-aware JSON schema (defined precisely in BUILD_SPEC).

### D16 — v1 automations (M19)
Ship in v1: **(a) team memory synthesis** (near-continuous → MEMORY.md), **(b) daily digest** ("what changed" note from the audit trail), and **(c) doc hygiene** (scan for broken block-links, stale notes, unindexed files). XtracBox-specific automation ships later through the Skills & Tools Registry.

### D17 — Semantic search / RAG scope (M16)
**Both, memory-boosted.** Index the whole store (all documents + blocks) for semantic retrieval via local Ollama embeddings, but rank `MEMORY.md` and per-domain summary notes higher in results. Agents find deep doc content *and* curated team memory surfaces first. Fully local.

### D18 — License (M22)
**Apache-2.0.** Permissive (use/modify/sell/build on, even in proprietary products) with an **explicit patent grant** protecting adopters, plus GPLv3 compatibility and an enterprise-friendly posture. Attribution via a NOTICE file. Chosen over MIT for the patent protection and robustness.

### D19 — Team chat / communication (M25)
**Core feature.** Humans and agents communicate in the same workspace through **shared channels**. Design:
- **Messages are blocks** in a channel document (type `channel`). Because blocks are the merge unit (D6), concurrent messages merge cleanly, are git-audited and attributed, and sync across nodes like any other content.
- **Agents are first-class participants**: they can `list_channels`, `send_message`, and read messages via `read_doc`/`search` (channels are docs). This is the bidirectional human↔agent loop — the whole point of the project.
- `RecentActors` surfaces who has been active in a workspace.
- Channels can be linked (@-mentioned) into tasks, docs, and other items via the block link graph.

## 7. Open Questions

| # | Module | Question | My lean |
|---|--------|----------|---------|
| ~~O1~~ | M11 | ~~Tool surface — which Xtracsys tools beyond starter set?~~ | ✅ Resolved by D11/D12 (grows via registry + dogfooding) |
| ~~O2~~ | M2 | ~~Link graph granularity — note vs block?~~ | ✅ Resolved → D15 (block-level) |
| ~~O3~~ | M16 | ~~RAG scope — whole store, or per-domain notes?~~ | ✅ Resolved → D17 (whole store, memory-boosted) |
| ~~O4~~ | M17 | ~~Human client — Obsidian or built-in UI?~~ | ✅ Resolved → D14 (bundled app + browser fallback) |
| ~~O5~~ | M19 | ~~Which automations first?~~ | ✅ Resolved → D16 (memory + digest + hygiene) |
| ~~O6~~ | M22 | ~~License for the open-source release?~~ | ✅ Resolved → D18 (Apache-2.0) |

**All open questions resolved — 19 decisions, 25 modules.**

## 8. Tool Surface (v1 starter set — grows via the Registry per D11/D12)

| Tool | Purpose |
|------|---------|
| `search` | Find notes/tasks/links across the store |
| `read_doc` | Read a note or document |
| `write_doc` | Create/edit a note (goes through audit) |
| `create_task` | Create a task, optionally linked to a note/channel |
| `link` | Create a bidirectional link between two items |
| `audit_query` | "What changed, when, and by whom" |
| `diff` | Show difference between two versions |
| `list_channels` | List team chat channels with activity (M25) |
| `send_message` | Send a message to a team channel — auto-creates it (M25) |

## 9. Security Model

- **Transport:** all node-to-node traffic encrypted (mTLS/TLS). Local network only.
- **Membership:** enrollment secret → mTLS certificate; no impersonation, no outsider read/write.
- **At rest:** Cryptomator volume + field-level JSON encryption.
- **Audit:** git history + attributed, time-stamped changes (human or agent ID).
- **Boundary:** framework tools generic; strategic-plan content private, never in the open-source surface.
