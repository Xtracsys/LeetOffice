# Contributing to LeetOffice

Thanks for helping build a workspace that stays on your machines.

## Ground rules (from the spec)

- **100% local, no egress (P1).** A PR that phones home — analytics, remote
  fonts, update pings — will be declined. Exceptions must be explicitly
  user-enabled and documented.
- **The store format is a contract (D1).** Tabbed HTML + embedded canonical
  JSON + Markdown index. Changes to the schema need a BUILD_SPEC update.
- **Never silently overwrite (D6).** Concurrent edits keep both versions.
- **Git is the audit trail (D3).** Every write is attributed and timestamped.
- **No cgo, no system deps.** Pure Go only; the binary builds anywhere.

## Workflow

1. `go test ./...` must pass (`-short` if your machine blocks multicast).
2. Match the existing code style: doc comments cite spec decisions (D-numbers,
   §-sections), minimal exported surface, errors wrapped with context.
3. New user-visible surfaces should render through the shared theme and work
   without a terminal.
4. PRs: describe what + why; link the spec section you're implementing.

## Repo map

`internal/store` schema · `internal/sync` git+audit+merge · `internal/net`
mTLS/mDNS/transport · `internal/mcp` agent surface · `internal/chat` team chat ·
`internal/memory`+`internal/rag` automations/search · `internal/httpui` GUI ·
`internal/daemon` composition · `cmd/leetd` CLI. The wiki-style file-by-file
tour lives in the LeetOfficeWiki companion.
