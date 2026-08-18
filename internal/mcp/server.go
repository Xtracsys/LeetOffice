// Package mcp implements the LeetOffice MCP server (M10/M11, BUILD_SPEC §5):
// the MCP agent surface spoken as JSON-RPC 2.0 over stdio and HTTP, per the
// Model Context Protocol. Every write is attributed to the actor injected at
// server construction (D7 — agents never self-report identity) and lands in
// the git audit trail.
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"leetoffice/internal/config"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

// Hit is one search result (rag provides a matching shape; kept here so the
// mcp package does not depend on rag).
type Hit struct {
	DocID   string  `json:"doc_id"`
	BlockID string  `json:"block_id"`
	Slug    string  `json:"slug"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

// SearchFunc is the pluggable search backend (rag.Search satisfies it).
type SearchFunc func(query string, typ string, tags []string, limit int) ([]Hit, error)

// Server is an MCP server bound to one store, repo, and actor.
type Server struct {
	store   *store.Store
	repo    *leetSync.Repo
	search  SearchFunc
	actor   string
	cfg     *config.Config
	cfgPath string
	// OnWrite is invoked after a successful send_message so the daemon
	// can push immediately instead of waiting for the sync ticker.
	OnWrite func()
}

// NewServer builds a Server. actor is e.g. "agent:hermes" or "human:josh".
func NewServer(s *store.Store, repo *leetSync.Repo, search SearchFunc, actor string) *Server {
	return &Server{store: s, repo: repo, search: search, actor: actor}
}

// BindConfig attaches node.json so subscribe / mark_read persist.
func (s *Server) BindConfig(cfg *config.Config, path string) {
	if s == nil {
		return
	}
	s.cfg = cfg
	s.cfgPath = path
}

// --- JSON-RPC plumbing -----------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const (
	errParse     = -32700
	errMethod    = -32601
	errInvalid   = -32602
	errInternal  = -32603
	protocolCode = "2025-06-18"
)

// ServeStdio serves newline-delimited JSON-RPC until the reader closes.
func (s *Server) ServeStdio(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: errParse, Message: "parse error"}})
			continue
		}
		resp := s.dispatch(req)
		if resp == nil { // notification — no response
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

// Handler exposes the same JSON-RPC surface over HTTP POST.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: errParse, Message: "parse error"}})
			return
		}
		resp := s.dispatch(req)
		if resp == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, *resp)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func isNotification(req rpcRequest) bool {
	return len(req.ID) == 0 || string(req.ID) == "null"
}

func (s *Server) dispatch(req rpcRequest) *rpcResponse {
	switch req.Method {
	case "initialize":
		return result(req, map[string]any{
			"protocolVersion": protocolCode,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "leetoffice", "version": "0.1.0"},
		})
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		return result(req, map[string]any{})
	case "tools/list":
		return result(req, map[string]any{"tools": toolDescriptors()})
	case "tools/call":
		return s.callTool(req)
	default:
		return errResp(req, errMethod, "method not found: "+req.Method)
	}
}

func result(req rpcRequest, v any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: v}
}

func errResp(req rpcRequest, code int, msg string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
}

func (s *Server) callTool(req rpcRequest) *rpcResponse {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req, errInvalid, "invalid tools/call params")
	}
	args := map[string]any{}
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return errResp(req, errInvalid, "invalid arguments")
		}
	}
	out, err := s.invoke(p.Name, args)
	if err != nil {
		return result(req, map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		})
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	return result(req, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(raw)}},
		"isError": false,
	})
}

func (s *Server) invoke(name string, args map[string]any) (any, error) {
	switch name {
	case "search":
		return s.toolSearch(args)
	case "read_doc":
		return s.toolReadDoc(args)
	case "write_doc":
		return s.toolWriteDoc(args)
	case "create_task":
		return s.toolCreateTask(args)
	case "link":
		return s.toolLink(args)
	case "audit_query":
		return s.toolAuditQuery(args)
	case "diff":
		return s.toolDiff(args)
	case "list_channels":
		return s.toolListChannels(args)
	case "send_message":
		return s.toolSendMessage(args)
	case "subscribe":
		return s.toolSubscribe(args)
	case "inbox":
		return s.toolInbox(args)
	case "mark_read":
		return s.toolMarkRead(args)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

// --- Tool descriptors ------------------------------------------------------

func prop(names ...string) map[string]any {
	props := map[string]any{}
	for _, n := range names {
		props[n] = map[string]any{"type": "string"}
	}
	return props
}

func toolDescriptors() []map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return []map[string]any{
		{"name": "search", "description": "Find notes/tasks/links across the store",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"query": str("search text"), "type": str("doc type filter"),
				"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"limit": map[string]any{"type": "integer", "default": 10}}}},
		{"name": "read_doc", "description": "Read a note or document (by slug or id)",
			"inputSchema": map[string]any{"type": "object", "properties": prop("id_or_slug", "block_id"), "required": []string{"id_or_slug"}}},
		{"name": "write_doc", "description": "Create/edit a note (audited, attributed)",
			"inputSchema": map[string]any{"type": "object", "properties": prop("id_or_slug", "block_id", "content", "replace"), "required": []string{"id_or_slug", "content"}}},
		{"name": "create_task", "description": "Create a task, optionally linked to notes",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"title": str("task title"), "body": str("task body"),
				"assignee": str("owner"),
				"links": map[string]any{"type": "array", "items": map[string]any{
					"type": "object", "properties": prop("doc", "block", "label")}}},
				"required": []string{"title"}}},
		{"name": "link", "description": "Create a bidirectional block-level link",
			"inputSchema": map[string]any{"type": "object", "properties": prop("from_doc", "from_block", "to_doc", "to_block", "label"), "required": []string{"from_doc", "from_block", "to_doc"}}},
		{"name": "audit_query", "description": "What changed, when, and by whom",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"doc_id": str("filter by doc"), "since": str("RFC3339 timestamp"),
				"actor": str("human:<id> or agent:<id>"),
				"limit": map[string]any{"type": "integer"}}}},
		{"name": "list_channels", "description": "List team chat channels with activity",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
		{"name": "send_message", "description": "Send a message to a team channel (auto-creates it)",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"channel": str("channel name, e.g. general or ops"), "text": str("message body")},
				"required": []string{"channel", "text"}}},
		{"name": "diff", "description": "Show the difference between two versions",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"id_or_slug": str("document"), "from_version": map[string]any{"type": "integer"},
				"to_version": map[string]any{"type": "integer"}}}},
		{"name": "subscribe", "description": "Watch channel(s) for @mentions of this actor",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"actor":    str("defaults to the MCP session actor"),
				"channels": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "empty = all channels"}}}},
		{"name": "inbox", "description": "New @mentions for this actor since a timestamp or read cursor",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"actor":    str("defaults to the MCP session actor"),
				"since_ts": str("RFC3339; omit to use the stored cursor"),
				"channel":  str("limit to one channel"),
				"limit":    map[string]any{"type": "integer", "default": 50}}}},
		{"name": "mark_read", "description": "Advance this actor's inbox cursor for a channel",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"actor":   str("defaults to the MCP session actor"),
				"channel": str("channel slug"),
				"ts":      str("RFC3339 timestamp of the last item seen")},
				"required": []string{"channel", "ts"}}},
	}
}

// --- Helpers ---------------------------------------------------------------

func docPath(d *store.Doc) string {
	dir := "docs"
	switch d.Type {
	case store.TypeTask:
		dir = "tasks"
	case store.TypeContact:
		dir = "contacts"
	case store.TypeChannel:
		dir = "channels"
	case store.TypeCompany:
		dir = "companies"
	case store.TypeEmail:
		dir = "emails"
	case store.TypeMemory:
		dir = "memory"
	}
	return dir + "/" + d.Slug + ".html"
}

func argStr(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func argInt(args map[string]any, key string, def int) int {
	if n, ok := argIntOK(args, key); ok {
		return n
	}
	return def
}

func argIntOK(args map[string]any, key string) (int, bool) {
	switch v := args[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

// argBool accepts a JSON boolean or the strings "true"/"1" (MCP clients
// disagree on whether replace is typed). A JSON true used to be ignored
// because write_doc only compared the string "true".
func argBool(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	default:
		return false
	}
}

// commit saves the doc and commits it attributed to the server actor (D7).
func (s *Server) commit(d *store.Doc, msg string) (string, error) {
	if err := s.store.Save(d, s.actor); err != nil {
		return "", err
	}
	sha, err := s.repo.CommitAll(s.actor, msg)
	if errors.Is(err, leetSync.ErrNoChanges) {
		return "", nil
	}
	return sha.String(), err
}

func renderText(d *store.Doc, blockID string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s · %s · v%d · updated %s\n",
		d.Title, d.Type, d.Slug, d.Version, d.Updated)
	for i := range d.Blocks {
		blk := &d.Blocks[i]
		if blockID != "" && blk.ID != blockID {
			continue
		}
		switch blk.Type {
		case store.BlockHeading:
			fmt.Fprintf(&b, "\n%s %s\n", strings.Repeat("#", max(blk.Level, 1)+1), blk.Content)
		case store.BlockDivider:
			b.WriteString("\n---\n")
		default:
			fmt.Fprintf(&b, "- %s\n", blk.Content)
		}
	}
	return b.String()
}
