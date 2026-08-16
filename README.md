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

## Team chat

The workspace home (`http://127.0.0.1:7667`) is a team chat in the shape of
Teams/ZCode: a channel rail with people & agent presence, a message stream,
and a composer. Under the hood a channel is just a `channel`-type document and
each message an attributed, timestamped block — so chat inherits everything
the store guarantees: git-durable history, per-message attribution
(`human:<id>` / `agent:<id>`), full-text search, offline-first catch-up on
rejoin, and lossless concurrent sends (block-level merge keeps both).

Agents are teammates in the same channels: `send_message` and `list_channels`
are MCP tools, so Claude Code / Codex / Hermes converse where the humans do.
Docs stay one click away (`docs` in the top bar); share a doc by mentioning
its slug.

## Installing on other systems

- **Any machine, no toolchain:** copy the right binary from `dist/` (built by
  `./scripts/dist.sh` for macOS arm64/amd64, Linux amd64/arm64, Windows amd64),
  run it once, and the first-run wizard does the rest — team coordinators are
  auto-discovered on the LAN, joining needs one invite code.
- **Desktop app:** `cd app && npm run dist` builds a `.dmg` / `AppImage` /
  Windows installer that bundles `leetd` and auto-starts it — double-click and
  you're in.
- **From source:** `go build -o leetd ./cmd/leetd` (pure Go, no cgo).
- **macOS/Linux service:** the UI's "make always-on" button (or `leetd install`)
  registers a launchd agent / systemd user unit.

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
| 8 | Human client: Teams-style chat shell + editor UI + Electron wrapper (`app/`), browser read-only fallback | ✅ |
| 9 | Packaging: single binary, Apache-2.0, runbook-in-repo | ✅ |

v1 simplifications (documented, by design — see RUNBOOK §6 notes): the vector
index is in-memory rather than SQLite, and the Electron app is a thin wrapper
you run with `npm install && npm start` rather than a signed installer.

## Quickstart — the easy way

Build once (pure Go, no cgo, no system git needed), then never touch a terminal again:

```sh
go build -o leetd ./cmd/leetd
./leetd            # that's it
```

The daemon opens a first-run wizard at `http://127.0.0.1:7667`:

- **Start a new team** — this machine becomes the coordinator. The wizard shows
  your one-time **invite code** to share out of band.
- **Join a team** — coordinators on your network are auto-discovered via mDNS;
  pick one (or type host:port), paste the invite code, done. Enrollment issues
  your mTLS certificate and configures encrypted sync automatically.
- **Just me** — a private local workspace; you can join a team later.

Answer two questions and you land in the workspace. Then click **make
always-on** on the home page (or run `leetd install`): LeetOffice registers as
a login service (launchd on macOS, systemd on Linux) and survives reboots —
no terminal, no manual startup, ever.

**Agents:** the **agents** page in the UI shows your MCP configuration with a
copy button (`leetd mcp-install` prints it; `leetd mcp-install --client claude
--write` drops a `.mcp.json` in the current project). Claude Code, Codex,
Hermes, or any MCP HTTP client against `http://127.0.0.1:7667/mcp`.

## Quickstart — the CLI way (power users)

```sh
./leetd init --coordinator --store ~/leetoffice/store --actor human:josh && ./leetd serve
# second machine:
./leetd enroll --coordinator <host>:7443 --secret <invite-code>
./leetd init --store ~/leetoffice/store --actor human:maya --share leet://<host>:7418/main.git
./leetd serve
```

Nodes sync every 5 s over mutually authenticated TLS and auto-rejoin after any
offline period: pull → block-merge → push, zero manual steps.

**Humans:** `http://127.0.0.1:7667` (or the Electron app in `app/`). Editing a
block updates the canonical embedded JSON and lands as a git commit attributed
to your actor id. Opening `docs/<slug>.html` in any browser is the read-only
fallback.

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
