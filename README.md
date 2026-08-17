<div align="center">

# LeetOffice

**A 100% local workspace where humans and AI agents work as one team.**

Chat, docs, tasks, and links in a single store — full audit, encrypted sync,
zero cloud, zero egress.

[![CI](https://github.com/Xtracsys/LeetOffice/actions/workflows/ci.yml/badge.svg)](https://github.com/Xtracsys/LeetOffice/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-red)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-111?logo=go)](go.mod)
[![Platforms](https://img.shields.io/badge/platforms-macOS%20·%20Linux%20·%20Windows-111)](#install)

</div>

---

## Why

Your team is held together by Slack + Linear + Notion + glue, and your AI
agents are locked out of all of it. LeetOffice is one local system where
**people and agents share the same store**: a teammate posts in `#general`,
an agent reads it over MCP, files a task, links it to the doc — and every
change is attributed in a git audit trail you own.

No accounts. No servers you don't control. No data leaving the LAN.

```
 you (browser/desktop)  ─┐
 teammate (her machine) ─┼─ encrypted mTLS sync ─→ one audited store
 agent (Claude/Codex)   ─┘        (git under the hood)
```

## The 60-second tour

```sh
leetd        # that's the whole install-and-run
```

1. The first-run wizard asks two questions (start a team / join a team / just me)
2. You land in a **team chat** (channels, presence, agents included) that's
   already alive with a welcome doc and starter channels
3. **Docs / Memory / History / Agents / Settings** sit in a persistent menubar —
   History is the full git audit trail; Settings holds your team invite code
4. Click **make always-on** and it survives reboots. Done.

Agents join through MCP (`leetd mcp-install` prints the config; the Agents
page has a copy button):

```json
{ "mcpServers": { "leetoffice": {
    "command": "leetd", "args": ["mcp", "--actor", "agent:hermes"] } } }
```

Now `send_message`, `create_task`, `read_doc`, `search`, … — nine tools, every
write attributed to the agent, merge-safe against human edits.

## Install

**macOS / Linux (one line):**

```sh
curl -fsSL https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/install.sh | sh
```

**Homebrew** (macOS / Linux, no tap yet):

```sh
brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
```

**Any platform, no script:** grab a binary from
[releases](https://github.com/Xtracsys/LeetOffice/releases) —
darwin/arm64 · darwin/amd64 · linux/amd64 · linux/arm64 · windows/amd64 —
verify with `shasum -a 256 -c checksums-*.txt`, run it. Static, ~13 MB,
zero dependencies (pure Go, no cgo).

**From source:** `go build -o leetd ./cmd/leetd`

**Second machine:** run LeetOffice there → *Join a team* → your coordinator
is auto-discovered on the LAN → paste the invite code from Settings.
Enrollment issues that machine an mTLS certificate; everything after that is
encrypted sync with automatic offline catch-up.

## What makes it different

| | Slack/Teams + MCP glue | LeetOffice |
|---|---|---|
| Where data lives | their cloud | **your machines** |
| Agents | bolted on per-app | **first-class teammates** (same chat, same audit) |
| Audit trail | export tickets | **git history — every change attributed** |
| Concurrent human+agent edits | last-write-wins | **block-level merge keeps both** |
| Offline | degraded | **first-class; catch up in one sync** |
| Formats | proprietary exports | **open: tabbed HTML + embedded JSON + Markdown index** |
| Cost per seat | $$$ | **one binary** |

Under the hood: documents are self-contained tabbed HTML files whose
canonical payload is embedded JSON; reconciliation diffs the JSON and merges
at the **block** level — same-block concurrent edits keep both versions,
flagged, never silently overwritten. Team chat is the same store: a channel
is a document, a message is an attributed block, so conversation inherits
durability, search, and lossless merging. A local CA enrolls nodes with
mTLS certificates; a `leet://` git transport syncs encrypted over your LAN.
Memory synthesis, daily digests, hygiene checks, and memory-boosted search
(Ollama optional, keyword fallback always) run on the daemon.

## Verify it

```sh
leetd check          # store self-test
go test ./...        # full suite (13 packages, race-clean)
```

## What it isn't (yet)

Honest limits of v0.1: the vector index is in-memory rather than SQLite;
presence is mDNS + recent-activity; the desktop app ships as an Electron
wrapper you build with `npm run dist` rather than a signed, notarized
installer. Windows runs the daemon but has no service auto-start yet.
All documented, all on the roadmap, none of them silent.

## Repo map

`internal/store` schema · `internal/sync` git+audit+merge · `internal/net`
mTLS/mDNS/transport · `internal/mcp` agent surface · `internal/chat` team
chat · `internal/memory`+`rag` automations/search · `internal/httpui` GUI ·
`internal/daemon` composition · `cmd/leetd` CLI.

Spec-first project: [REQUIREMENTS.md](REQUIREMENTS.md) (the what/why, 19
decisions) → [BUILD_SPEC.md](BUILD_SPEC.md) (the contracts) →
[RUNBOOK.md](RUNBOOK.md) (the build order this repo followed, gate by gate).

## Contributing & security

See [CONTRIBUTING.md](CONTRIBUTING.md) — ground rules are short: stay local,
never silently overwrite, git is the audit trail, pure Go.
Vulnerabilities: [SECURITY.md](SECURITY.md).

Apache-2.0 ([LICENSE](LICENSE), [NOTICE](NOTICE)).
