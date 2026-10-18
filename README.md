<div align="center">

# LeetOffice

**A 100% local workspace where humans and AI agents work as one team.**

Chat, docs, tasks, and links in a single store — full audit, encrypted sync,
zero cloud, zero egress.

**Install:** download your platform binary and the matching `checksums-*.txt`
from [GitHub Releases](https://github.com/Xtracsys/LeetOffice/releases). Verify
the binary against its entry in that checksum file before running it; see
[Install](#install).

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
   History is the full git audit trail; coordinators see the team invite code
   and CA fingerprint in Settings for out-of-band verification, plus a **check
   for update** button (GitHub is contacted only when you click)
4. Click **make always-on** and it survives reboots. Done.

Connect an agent through MCP (`leetd mcp-install` prints the config; the Agents
page has a copy button). First create a local identity in **Settings → Agent
access** and grant only the tools it needs. No tools are granted by default.
Credentials and grants stay on this node, outside the synchronized store. The
available tool scopes are `search`, `read_doc`, `write_doc`, `create_task`,
`link`, `audit_query`, `diff`, `list_channels`, `send_message`, `subscribe`,
`inbox`, and `mark_read`.

```json
{
  "mcpServers": {
    "leetoffice": {
      "command": "leetd",
      "args": ["mcp"],
      "env": { "LEETOFFICE_AGENT_TOKEN": "PASTE_ONE_TIME_TOKEN" }
    }
  }
}
```

Replace the obvious placeholder with the one-time credential shown when you
create it, and keep the MCP client configuration private. `leetd mcp` reads
`LEETOFFICE_AGENT_TOKEN`; the server derives the immutable `agent:<id>` identity
from that credential, not a caller-supplied actor or name. Only the granted
tools are available. Writes are attributed to that identity in the git audit
trail. Agents can `subscribe` and poll `inbox` for `@agent:<id>` mentions;
`mark_read` keeps the cursor in `node.json`.

`leetd mcp-install --client claude --write` creates/replaces `.mcp.json` with
mode `0600` via a same-directory temporary file, and refuses an existing
symlink or non-regular destination. Run it in a trusted project directory.
Never commit a real one-time token.

## Install

**GitHub Releases (all platforms):**

1. Open [releases](https://github.com/Xtracsys/LeetOffice/releases).
2. Download your platform binary and the `checksums-*.txt` file from the same
   release.
3. Verify the selected binary against its matching filename entry in the
   checksum file using your platform's SHA-256 utility before running it.
4. On macOS/Linux, make the binary executable and place it on your `PATH`;
   Windows users can run the `.exe` directly.

Available targets: darwin/arm64 · darwin/amd64 · linux/amd64 · linux/arm64 ·
windows/amd64. Pure Go, built without cgo, with no system runtime dependency.

**From source:** `go build -o leetd ./cmd/leetd`

**Second machine:** run LeetOffice there → *Join a team* → select the
auto-discovered coordinator. Before entering the invite code, independently
compare its displayed SHA-256 CA fingerprint with the value received from the
coordinator through a trusted out-of-band channel; do not trust LAN discovery
alone. Only after the fingerprints match, enter the invite code. Enrollment
issues that machine an mTLS certificate; everything after that is encrypted
sync with automatic offline catch-up.

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
go test ./...        # full suite
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
`internal/update` user-initiated GitHub updater · `internal/daemon`
composition · `cmd/leetd` CLI.

Spec-first project: [REQUIREMENTS.md](REQUIREMENTS.md) (the what/why, 19
decisions) → [BUILD_SPEC.md](BUILD_SPEC.md) (the contracts) →
[RUNBOOK.md](RUNBOOK.md) (the build order this repo followed, gate by gate).

## Contributing & security

See [CONTRIBUTING.md](CONTRIBUTING.md) — ground rules are short: stay local,
never silently overwrite, git is the audit trail, pure Go.
Vulnerabilities: [SECURITY.md](SECURITY.md).

### Install-script supply-chain guard

LeetOffice install docs are written for **AI coding agents and humans alike** and
are read by tools that blindly follow `llms.txt` / README instructions. Therefore:

- **Never** publish `curl .../main/... | sh` or `npx <unclaimed-name>` patterns. They
  are the exact vector behind the 2026-08 `llms.txt` supply-chain attacks (Ars
  Technica: *Claude, Codex, and Hermes installed unowned code inside corporate
  networks*).
- Publish a one-line installer only when the release workflow attaches a
  versioned copy of the script and publishes its SHA-256 digest. Until then,
  direct users to release binaries and the matching checksum manifest; do not
  claim a pinned installer that is not a published asset.
- Any package/domain referenced in docs must be **owned and registered** by
  Xtracsys before shipping. If a name is unclaimed, that is a security bug, not a
  TODO.

Apache-2.0 ([LICENSE](LICENSE), [NOTICE](NOTICE)).
