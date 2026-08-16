# LeetOffice

A **100% local** workspace framework where **multiple humans and multiple AI
agents** work together in one shared store — documents, tasks, notes, and
links in a single graph, with block-level bidirectional links, unified team
memory, full audit attribution, and encrypted node-to-node sync.
**No cloud, no egress.**

One installable app; every node is a fat client by default and any node can be
promoted to coordinator (server). The daemon (`leetd`) is headless and
always-on; humans get a bundled desktop app (or any browser, read-only);
agents get MCP.

- Design docs: [`REQUIREMENTS.md`](REQUIREMENTS.md) (the what/why),
  [`BUILD_SPEC.md`](BUILD_SPEC.md) (the contracts),
  [`RUNBOOK.md`](RUNBOOK.md) (the build order — this repo is that build).
- License: Apache-2.0 ([LICENSE](LICENSE), [NOTICE](NOTICE)).

## What's implemented

| Phase | Module | Status |
|---|---|---|
| 1 | Store: tabbed HTML + embedded JSON (canonical) + `INDEX.md`, block-level links, field-level AES-256-GCM | ✅ |
| 2 | Git sync & audit: go-git, block-level merge (`leet-merge`), conflicts keep both versions, auto-rejoin | ✅ |
| 3 | Node network & trust: local CA, enrollment secret → mTLS certs, mDNS discovery, git-over-mTLS `leet://` transport | ✅ |
| 4 | MCP server: `search`, `read_doc`, `write_doc`, `create_task`, `link`, `audit_query`, `diff` over stdio + HTTP | ✅ |
| 5 | Memory synthesis (`MEMORY.md`), daily digest, doc hygiene + monitor notices | ✅ |
| 6 | RAG: keyword fallback (always on) + Ollama embeddings (optional), memory-boosted ranking, encrypted fields excluded | ✅ |
| 7 | Skills & tools registry: manifest format, import/export, auto-promote at N clean uses | ✅ |
| 8 | Human client: daemon-served editor UI + Electron wrapper (`app/`), browser read-only fallback | ✅ |
| 9 | Packaging: single binary, Apache-2.0, runbook-in-repo | ✅ |

v1 simplifications (documented, by design — see RUNBOOK §6 notes): the vector
index is in-memory rather than SQLite, and the Electron app is a thin wrapper
you run with `npm install && npm start` rather than a signed installer.

## Quickstart

Build (pure Go, no cgo, no system git needed):

```sh
go build -o leetd ./cmd/leetd
```

**Start a coordinator** (creates the main share, prints a one-time enrollment
secret):

```sh
./leetd init --coordinator --store ~/leetoffice/store --actor human:josh
./leetd serve
```

**Join from another machine** (or the same one, for testing):

```sh
./leetd enroll --coordinator <host>:7443 --secret <the-secret>
./leetd init --store ~/leetoffice/store --actor human:maya \
            --share leet://<host>:7418/main.git
./leetd serve
```

Nodes discover each other via mDNS (`_leetoffice._tcp`), sync every 5 s over
mutually authenticated TLS, and auto-rejoin after any offline period: pull →
block-merge → push, zero manual steps.

**Humans:** open `http://127.0.0.1:7667` (or run the Electron app in `app/`).
Editing a block updates the canonical embedded JSON and lands as a git commit
attributed to your actor id. Opening `docs/<slug>.html` in any browser is the
read-only fallback.

**Agents** (Claude Code, Codex, Hermes, any MCP client):

```json
{ "mcpServers": { "leetoffice": { "command": "/path/to/leetd", "args": ["mcp", "--actor", "agent:hermes"] } } }
```

or point an MCP HTTP client at `http://127.0.0.1:7667/mcp`.

## Everyday commands

```sh
leetd doc list                       # what's in the store
leetd doc show <slug>                # canonical JSON of a doc
leetd audit [--doc <slug>] [--actor human:josh]   # what changed, by whom
leetd sync                           # one fetch → merge → push cycle
leetd memory | digest | hygiene      # run the automations on demand
leetd registry list                  # skills & tools, stability state
leetd registry use hello-leetoffice  # record a clean use (auto-promotes at threshold)
leetd check                          # store self-test
```

## Store format (D1)

Each document is a self-contained **tabbed HTML file** with an **embedded JSON
payload** — the JSON is canonical; the tabs are a rendering; `INDEX.md` is
derived. Reconciliation diffs the JSON and merges at the **block** level:
same-block concurrent edits keep **both** versions and flag a conflict — never
a silent overwrite. Git history **is** the audit trail (every commit authored
`human:<id>` or `agent:<id>`).

```
<store>/
├── INDEX.md  MEMORY.md  NOTICE.md
├── docs/<slug>.html  tasks/  contacts/  channels/  companies/  emails/  memory/
└── _audit/DIGEST-YYYY-MM-DD.md
```

## Security model

| Concern | Mechanism |
|---|---|
| Transport | mTLS on all node traffic; hostname matching replaced by CA-membership verification |
| Membership | enrollment secret → node certificate from the team CA; rogue nodes rejected at handshake |
| At rest | Cryptomator/VeraCrypt volume (your side) + field-level AES-256-GCM (`{"enc":true,...}` wraps) |
| Audit | git history; attributed, timestamped; conflicts and hygiene issues surface in `NOTICE.md` |
| Boundary | tooling is generic; private strategic content never ships in the open-source surface |

## Verify

```sh
go test ./...          # full suite (store, sync, net, mcp, memory, rag, registry, e2e)
go test -short ./...   # CI-safe (skips real multicast + network e2e)
```

The e2e package walks the definition of done: two nodes, a human UI edit, an
agent MCP write, attribution in `git log`, same-block conflict retention,
memory synthesis, and registry auto-promotion — plus a full mTLS sync cycle
with a rogue-node rejection.
