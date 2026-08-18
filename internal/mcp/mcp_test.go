package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leetoffice/internal/chat"
	"leetoffice/internal/config"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

func newTestServer(t *testing.T) (*Server, *store.Store, *leetSync.Repo) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	repo, err := leetSync.Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return NewServer(s, repo, keywordStub, "agent:hermes"), s, repo
}

func keywordStub(query, typ string, tags []string, limit int) ([]Hit, error) {
	return []Hit{{DocID: "d1", BlockID: "b1", Slug: "spec", Title: "Spec", Snippet: "match", Score: 1}}, nil
}

// call drives one JSON-RPC request through ServeStdio and returns the
// unmarshalled result (tools/call results have their content text extracted).
func call(t *testing.T, srv *Server, method string, params any) map[string]any {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	raw, _ := json.Marshal(req)
	var out bytes.Buffer
	// one-shot reader: single request then EOF
	if err := srv.ServeStdio(bytes.NewReader(append(raw, '\n')), &out); err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}
	var resp struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("bad response: %s", out.String())
	}
	if resp.Error != nil {
		t.Fatalf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result
}

// toolResult extracts the JSON payload from a tools/call text content blob.
func toolResult(t *testing.T, res map[string]any) any {
	t.Helper()
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content in tools/call result: %#v", res)
	}
	c0 := content[0].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("tool error: %v", c0["text"])
	}
	var out any
	if err := json.Unmarshal([]byte(c0["text"].(string)), &out); err != nil {
		t.Fatalf("tool payload not JSON: %v", c0["text"])
	}
	return out
}

func callTool(t *testing.T, srv *Server, name string, args map[string]any) any {
	t.Helper()
	return toolResult(t, call(t, srv, "tools/call", map[string]any{"name": name, "arguments": args}))
}

func TestHandshakeAndToolsList(t *testing.T) {
	srv, _, _ := newTestServer(t)

	init := call(t, srv, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}})
	if init["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion = %v", init["protocolVersion"])
	}
	list := call(t, srv, "tools/list", nil)
	tools, _ := list["tools"].([]any)
	if len(tools) != 12 {
		t.Fatalf("expected 12 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"search", "read_doc", "write_doc", "create_task", "link", "audit_query", "diff", "list_channels", "send_message", "subscribe", "inbox", "mark_read"} {
		if !names[want] {
			t.Fatalf("tool %q missing", want)
		}
	}
}

func TestWriteReadAuditDiffFlow(t *testing.T) {
	srv, st, repo := newTestServer(t)

	// 1. write_doc creates a doc
	res := callTool(t, srv, "write_doc", map[string]any{
		"id_or_slug": "onboarding", "content": "Boot the node from the local binary."}).(map[string]any)
	if res["commit_sha"] == "" || res["version"].(float64) < 1 {
		t.Fatalf("write_doc result: %#v", res)
	}
	d, err := st.Load("onboarding")
	if err != nil || len(d.Blocks) != 1 {
		t.Fatalf("store.Load: %v blocks=%d", err, len(d.Blocks))
	}

	// 2. write_doc appends a block
	callTool(t, srv, "write_doc", map[string]any{
		"id_or_slug": "onboarding", "content": "Second paragraph."})
	d, _ = st.Load("onboarding")
	if len(d.Blocks) != 2 {
		t.Fatalf("append failed: %d blocks", len(d.Blocks))
	}

	// 3. attribution in the audit trail
	entries := callTool(t, srv, "audit_query", map[string]any{}).([]any)
	foundActor := false
	for _, e := range entries {
		if e.(map[string]any)["actor"] == "agent:hermes" {
			foundActor = true
		}
	}
	if !foundActor {
		t.Fatalf("agent:hermes not attributed: %#v", entries)
	}
	_ = repo

	// 4. read_doc
	read := callTool(t, srv, "read_doc", map[string]any{"id_or_slug": "onboarding"}).(map[string]any)
	if read["doc"] == nil || !strings.Contains(read["text"].(string), "Boot the node") {
		t.Fatalf("read_doc: %#v", read)
	}
	// single-block read
	read1 := callTool(t, srv, "read_doc", map[string]any{
		"id_or_slug": "onboarding", "block_id": d.Blocks[1].ID}).(map[string]any)
	if strings.Contains(read1["text"].(string), "Boot the node") {
		t.Fatal("block filter not applied")
	}

	// 5. diff sees the added block
	diff := callTool(t, srv, "diff", map[string]any{"id_or_slug": "onboarding"}).(map[string]any)
	if diff["blocks_added"].(float64) < 1 {
		t.Fatalf("diff: %#v", diff)
	}

	// 6. search
	hits := callTool(t, srv, "search", map[string]any{"query": "boot"}).([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["slug"] != "spec" {
		t.Fatalf("search hits: %#v", hits)
	}
}

// TestWriteDocReplaceJSONBoolean: MCP clients send replace as a JSON
// boolean. Treating it as a string meant only the literal "true" replaced
// and true appended a new block.
func TestWriteDocReplaceJSONBoolean(t *testing.T) {
	srv, st, _ := newTestServer(t)
	callTool(t, srv, "write_doc", map[string]any{
		"id_or_slug": "notes", "content": "original"})
	d, err := st.Load("notes")
	if err != nil || len(d.Blocks) != 1 {
		t.Fatalf("seed: %v blocks=%d", err, len(d.Blocks))
	}
	id := d.Blocks[0].ID

	callTool(t, srv, "write_doc", map[string]any{
		"id_or_slug": "notes", "block_id": id, "content": "replaced", "replace": true})
	d, _ = st.Load("notes")
	if len(d.Blocks) != 1 || d.Blocks[0].Content != "replaced" {
		t.Fatalf("JSON true did not replace: %+v", d.Blocks)
	}

	callTool(t, srv, "write_doc", map[string]any{
		"id_or_slug": "notes", "block_id": id, "content": "from-string", "replace": "true"})
	d, _ = st.Load("notes")
	if len(d.Blocks) != 1 || d.Blocks[0].Content != "from-string" {
		t.Fatalf("string true did not replace: %+v", d.Blocks)
	}

	callTool(t, srv, "write_doc", map[string]any{
		"id_or_slug": "notes", "block_id": id, "content": "appended"})
	d, _ = st.Load("notes")
	if len(d.Blocks) != 2 {
		t.Fatalf("omit replace should append: %d blocks", len(d.Blocks))
	}
}

// TestDiffHonorsVersions: from_version/to_version select by doc.Version.
// The tool used to ignore them and always diff current vs HEAD~1.
func TestDiffHonorsVersions(t *testing.T) {
	srv, st, _ := newTestServer(t)
	callTool(t, srv, "write_doc", map[string]any{"id_or_slug": "hist", "content": "one"})
	callTool(t, srv, "write_doc", map[string]any{"id_or_slug": "hist", "content": "two"})
	callTool(t, srv, "write_doc", map[string]any{"id_or_slug": "hist", "content": "three"})
	d, err := st.Load("hist")
	if err != nil || len(d.Blocks) != 3 {
		t.Fatalf("seed: %v blocks=%d", err, len(d.Blocks))
	}
	// first write is NewDoc v1 + AddParagraph → v2
	from, to := 2, d.Version
	if to < from+2 {
		t.Fatalf("expected version to grow by 2, got %d", to)
	}
	res := callTool(t, srv, "diff", map[string]any{
		"id_or_slug": "hist", "from_version": from, "to_version": to,
	}).(map[string]any)
	if res["from_version"].(float64) != float64(from) || res["to_version"].(float64) != float64(to) {
		t.Fatalf("reported versions: %#v", res)
	}
	if res["blocks_added"].(float64) != 2 {
		t.Fatalf("from v%d to v%d should add 2 blocks: %#v", from, to, res)
	}
	if strings.Contains(res["note"].(string), "HEAD~1") {
		t.Fatalf("versioned diff still used HEAD~1 default: %#v", res)
	}

	def := callTool(t, srv, "diff", map[string]any{"id_or_slug": "hist"}).(map[string]any)
	if def["blocks_added"].(float64) != 1 {
		t.Fatalf("default current vs HEAD~1 should add 1: %#v", def)
	}
}

func TestCreateTaskAndLink(t *testing.T) {
	srv, st, _ := newTestServer(t)

	callTool(t, srv, "write_doc", map[string]any{"id_or_slug": "notes", "content": "first note"})

	res := callTool(t, srv, "create_task", map[string]any{
		"title": "Verify imaging", "assignee": "human:josh",
		"links": []any{map[string]any{"doc": "notes", "label": "relates to"}},
	}).(map[string]any)
	if !strings.HasPrefix(res["url"].(string), "doc://tasks/") || res["task_id"] == "" {
		t.Fatalf("create_task result: %#v", res)
	}
	task, err := st.Load("verify-imaging")
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if len(task.Blocks) == 0 || task.Blocks[0].Meta["done"] != false || task.Blocks[0].Meta["assignee"] != "human:josh" {
		t.Fatalf("task block: %#v", task.Blocks[0])
	}
	// bidirectional link landed
	if len(task.Blocks[0].Links) != 1 || task.Blocks[0].Links[0].Dir != "out" {
		t.Fatalf("task link missing: %#v", task.Blocks[0].Links)
	}
	note, _ := st.Load("notes")
	if len(note.Blocks[0].Links) != 1 || note.Blocks[0].Links[0].Dir != "in" {
		t.Fatalf("backlink missing: %#v", note.Blocks[0].Links)
	}

	// the link tool between two docs
	edge := callTool(t, srv, "link", map[string]any{
		"from_doc": "notes", "from_block": note.Blocks[0].ID,
		"to_doc": "verify-imaging", "label": "tracks",
	}).(map[string]any)
	if edge["edge_id"] == "" {
		t.Fatalf("edge_id missing: %#v", edge)
	}
	note2, _ := st.Load("notes")
	if len(note2.Blocks[0].Links) != 2 {
		t.Fatalf("second link not added: %#v", note2.Blocks[0].Links)
	}
}

func TestUnknownToolAndMethod(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res := call(t, srv, "tools/call", map[string]any{"name": "nope", "arguments": map[string]any{}})
	if res["isError"] != true {
		t.Fatalf("unknown tool should be isError: %#v", res)
	}

	// unknown method → JSON-RPC error
	raw := []byte(`{"jsonrpc":"2.0","id":7,"method":"bogus/method"}`)
	var out bytes.Buffer
	_ = srv.ServeStdio(bytes.NewReader(raw), &out)
	if !strings.Contains(out.String(), "-32601") {
		t.Fatalf("expected -32601, got %s", out.String())
	}
}

func TestNotificationsProduceNoResponse(t *testing.T) {
	srv, _, _ := newTestServer(t)
	raw := []byte("{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n")
	var out bytes.Buffer
	if err := srv.ServeStdio(bytes.NewReader(raw), &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("notification produced a response: %s", out.String())
	}
}

func TestHTTPHandler(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Handler()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var res struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Result.Tools) != 12 {
		t.Fatalf("HTTP tools/list: %d tools", len(res.Result.Tools))
	}

	// a tools/call through HTTP writes to the store
	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"write_doc","arguments":{"id_or_slug":"http-doc","content":"via http"}}}`
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/", strings.NewReader(callBody))
	h.ServeHTTP(rec2, req2)
	if _, err := srv.store.Load("http-doc"); err != nil {
		t.Fatalf("http write_doc did not persist: %v", err)
	}
	_ = io.Discard
}

var _ = filepath.Join

func TestAgentChat(t *testing.T) {
	srv, st, repo := newTestServer(t)

	// agent joins the conversation
	res := callTool(t, srv, "send_message", map[string]any{
		"channel": "#ops", "text": "imaging run complete"}).(map[string]any)
	if res["commit_sha"] == "" || res["channel"] != "ops" {
		t.Fatalf("send_message: %#v", res)
	}
	chans := callTool(t, srv, "list_channels", map[string]any{}).([]any)
	if len(chans) != 1 || chans[0].(map[string]any)["messages"].(float64) != 1 {
		t.Fatalf("list_channels: %#v", chans)
	}

	// the message lives in the store, attributed, and committed
	d, err := st.Load("ops")
	if err != nil || d.Type != store.TypeChannel {
		t.Fatalf("channel doc: %v", err)
	}
	msgs := chat.Messages(d)
	if len(msgs) != 1 || msgs[0].Author != "agent:hermes" || msgs[0].Text != "imaging run complete" {
		t.Fatalf("messages: %#v", msgs)
	}
	entries, _ := repo.AuditLog("channels/ops.html", time.Time{}, "agent:hermes", 5)
	if len(entries) == 0 {
		t.Fatal("chat message not attributed in audit log")
	}

	// empty messages are rejected
	res2 := call(t, srv, "tools/call", map[string]any{
		"name": "send_message", "arguments": map[string]any{"channel": "ops", "text": "  "}})
	if res2["isError"] != true {
		t.Fatalf("empty message accepted: %#v", res2)
	}
}

func TestAgentInbox(t *testing.T) {
	srv, st, repo := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "node.json")
	cfg := config.Default(t.TempDir(), "agent:hermes")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	srv.BindConfig(cfg, cfgPath)

	sub := callTool(t, srv, "subscribe", map[string]any{"channels": []string{}}).(map[string]any)
	if sub["ok"] != true {
		t.Fatalf("subscribe: %#v", sub)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil || len(loaded.AgentSubscriptions) != 1 || loaded.AgentSubscriptions[0].Actor != "agent:hermes" {
		t.Fatalf("subscribe did not persist: %v %#v", err, loaded)
	}

	if _, _, err := chat.Send(st, repo, "human:josh", "general", "standup, no one tagged"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := chat.Send(st, repo, "human:josh", "general", "need @agent:hermes on the join bug"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := chat.Send(st, repo, "human:josh", "ops", "@agent:codex only"); err != nil {
		t.Fatal(err)
	}

	box := callTool(t, srv, "inbox", map[string]any{}).(map[string]any)
	if box["count"].(float64) != 1 {
		t.Fatalf("inbox should be mention-only: %#v", box)
	}
	items := box["items"].([]any)
	item := items[0].(map[string]any)
	if item["channel"] != "general" || item["mentioned"] != true || item["author"] != "human:josh" {
		t.Fatalf("item: %#v", item)
	}
	if !strings.Contains(item["content"].(string), "@agent:hermes") {
		t.Fatalf("content: %#v", item)
	}

	ts := item["ts"].(string)
	marked := callTool(t, srv, "mark_read", map[string]any{"channel": "general", "ts": ts}).(map[string]any)
	if marked["ok"] != true {
		t.Fatalf("mark_read: %#v", marked)
	}
	after := callTool(t, srv, "inbox", map[string]any{}).(map[string]any)
	if after["count"].(float64) != 0 {
		t.Fatalf("cursor should hide seen mention: %#v", after)
	}

	// attribution on send_message unchanged
	sent := callTool(t, srv, "send_message", map[string]any{
		"channel": "general", "text": "on it"}).(map[string]any)
	if sent["commit_sha"] == "" || sent["channel"] != "general" {
		t.Fatalf("send_message: %#v", sent)
	}
	d, _ := st.Load("general")
	msgs := chat.Messages(d)
	last := msgs[len(msgs)-1]
	if last.Author != "agent:hermes" {
		t.Fatalf("attribution: %#v", last)
	}

	// cannot read another actor's inbox
	bad := call(t, srv, "tools/call", map[string]any{
		"name": "inbox", "arguments": map[string]any{"actor": "agent:codex"}})
	if bad["isError"] != true {
		t.Fatalf("cross-actor inbox allowed: %#v", bad)
	}

	// restart: reload node.json
	reloaded, err := config.Load(cfgPath)
	if err != nil || reloaded.AgentSubscriptions[0].Cursor["general"] != ts {
		t.Fatalf("cursor not durable: %v %#v", err, reloaded)
	}
}
