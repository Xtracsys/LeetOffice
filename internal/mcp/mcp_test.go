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
	if len(tools) != 9 {
		t.Fatalf("expected 9 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"search", "read_doc", "write_doc", "create_task", "link", "audit_query", "diff", "list_channels", "send_message"} {
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
	if len(res.Result.Tools) != 9 {
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
