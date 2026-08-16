// Package e2e walks the v1 "definition of done" (BUILD_SPEC §14) with the
// file:// sync transport: two nodes share a bare main share; a human edits via
// the httpui; an agent writes via MCP; every change is attributed in git;
// MEMORY.md synthesizes; the registry promotes on proof.
package e2e

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leetoffice/internal/config"
	"leetoffice/internal/httpui"
	"leetoffice/internal/mcp"
	"leetoffice/internal/memory"
	"leetoffice/internal/registry"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

func fixture(t *testing.T) (bare string, human, agent *mcp.Server, sHuman, sAgent *store.Store, rHuman, rAgent *leetSync.Repo) {
	t.Helper()
	dir := t.TempDir()
	bare = filepath.Join(dir, "main-share.git")
	if _, err := leetSync.InitBare(bare); err != nil {
		t.Fatal(err)
	}
	shareURL := "file://" + bare

	newNode := func(name, actor string) (*mcp.Server, *store.Store, *leetSync.Repo) {
		s, err := store.OpenStore(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		r, err := leetSync.Init(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := r.AddRemote("origin", shareURL); err != nil {
			t.Fatal(err)
		}
		return mcp.NewServer(s, r, searchStub, actor), s, r
	}
	human, sHuman, rHuman = newNode("node-human", "human:josh")
	agent, sAgent, rAgent = newNode("node-agent", "agent:hermes")

	// seed the share with one doc from the human node
	callTool(t, human, "write_doc", map[string]any{
		"id_or_slug": "quarterly-plan", "content": "Ship the local-first workspace v1."})
	if _, err := rHuman.Sync("origin", "human:josh"); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	// the agent node joins: pull the share (auto-rejoin, §6.5)
	if _, err := rAgent.Sync("origin", "agent:hermes"); err != nil {
		t.Fatalf("agent join sync: %v", err)
	}
	return bare, human, agent, sHuman, sAgent, rHuman, rAgent
}

func searchStub(query, typ string, tags []string, limit int) ([]mcp.Hit, error) {
	return []mcp.Hit{{Slug: "stub", Snippet: query, Score: 1}}, nil
}

func callTool(t *testing.T, srv *mcp.Server, name string, args map[string]any) map[string]any {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args}}
	raw, _ := json.Marshal(req)
	var out bytes.Buffer
	if err := srv.ServeStdio(bytes.NewReader(append(raw, '\n')), &out); err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("bad rpc response: %s", out.String())
	}
	if resp.Result.IsError || len(resp.Result.Content) == 0 {
		t.Fatalf("tool %s failed: %s", name, out.String())
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &payload); err != nil {
		t.Fatalf("tool %s payload: %v", name, err)
	}
	return payload
}

// TestDefinitionOfDone: human edit + agent task on the same store, sync both
// ways, everything attributed; memory synthesizes; registry promotes.
func TestDefinitionOfDone(t *testing.T) {
	_, _, agent, sHuman, sAgent, rHuman, rAgent := fixture(t)

	// agent (via MCP) reads the plan and creates a linked task
	read := callTool(t, agent, "read_doc", map[string]any{"id_or_slug": "quarterly-plan"})
	if read["doc"] == nil {
		t.Fatal("agent could not read the plan")
	}
	task := callTool(t, agent, "create_task", map[string]any{
		"title": "Verify imaging", "assignee": "human:josh",
		"links": []any{map[string]any{"doc": "quarterly-plan", "label": "delivers"}}})
	if task["task_id"] == "" {
		t.Fatalf("create_task: %#v", task)
	}
	if _, err := rAgent.Sync("origin", "agent:hermes"); err != nil {
		t.Fatalf("agent sync: %v", err)
	}

	// human edits a different doc in the UI
	cfg := config.Default(sHuman.Root, "human:josh")
	ui := &httpui.UI{Store: sHuman, Repo: rHuman, Config: cfg}
	h := httptest.NewServer(ui.Handler())
	defer h.Close()
	res, err := h.Client().PostForm(h.URL+"/doc/new", url.Values{
		"type": {"doc"}, "slug": {"field-notes"}, "title": {"Field Notes"}, "body": {"first entry"}})
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("ui create: %v %v", err, res)
	}
	res.Body.Close()
	if _, err := rHuman.Sync("origin", "human:josh"); err != nil {
		t.Fatalf("human sync: %v", err)
	}

	// agent node pulls the human's edit; both see one history
	if _, err := rAgent.Sync("origin", "agent:hermes"); err != nil {
		t.Fatalf("agent pull: %v", err)
	}
	if _, err := sAgent.Load("field-notes"); err != nil {
		t.Fatalf("agent node missing human doc: %v", err)
	}

	// attribution: git log shows both actors
	entries, err := rAgent.AuditLog("", time.Time{}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	actors := map[string]bool{}
	for _, e := range entries {
		actors[e.Actor] = true
	}
	if !actors["human:josh"] || !actors["agent:hermes"] {
		t.Fatalf("attribution missing: %v", actors)
	}

	// memory synthesis picks up the open task
	if err := memory.Synthesize(sAgent, rAgent, "agent:hermes"); err != nil {
		t.Fatal(err)
	}
	raw, err := readFile(filepath.Join(sAgent.Root, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Verify imaging") {
		t.Fatalf("MEMORY.md missing open task:\n%s", raw)
	}

	// registry promotes after clean uses (import the bundled skill, threshold 3)
	root := filepath.Dir(sAgent.Root)
	if _, err := registry.Import(root, "../../skills/hello-leetoffice", "human:josh", rAgent); err != nil {
		t.Fatalf("import skill: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := registry.RecordUse(root, "hello-leetoffice", true, "agent:hermes", rAgent); err != nil {
			t.Fatal(err)
		}
	}
	e, err := registry.Find(root, "hello-leetoffice")
	if err != nil {
		t.Fatal(err)
	}
	if e.Manifest.Stability != registry.Stable {
		t.Fatalf("skill not promoted: %+v", e.Manifest)
	}
}

// TestSameBlockConflictAcrossNodes: human and agent edit the same block on
// different nodes; the merge keeps both and flags the conflict (D6).
func TestSameBlockConflictAcrossNodes(t *testing.T) {
	_, human, agent, sHuman, sAgent, rHuman, rAgent := fixture(t)

	d, err := sHuman.Load("quarterly-plan")
	if err != nil {
		t.Fatal(err)
	}
	blockID := d.Blocks[0].ID

	// human edits block 1, syncs
	callTool(t, human, "write_doc", map[string]any{
		"id_or_slug": "quarterly-plan", "block_id": blockID, "replace": "true",
		"content": "Ship the local-first workspace v1 (human wording)."})
	if _, err := rHuman.Sync("origin", "human:josh"); err != nil {
		t.Fatal(err)
	}

	// agent edits the same block from the older version, syncs → conflict
	callTool(t, agent, "write_doc", map[string]any{
		"id_or_slug": "quarterly-plan", "block_id": blockID, "replace": "true",
		"content": "Ship the local-first workspace v1 (agent wording)."})
	res, err := rAgent.Sync("origin", "agent:hermes")
	if err != nil {
		t.Fatalf("agent sync: %v", err)
	}
	if len(res.Conflicts) == 0 {
		t.Fatal("expected conflict flag")
	}
	merged, _ := sAgent.Load("quarterly-plan")
	var contents []string
	for _, b := range merged.Blocks {
		contents = append(contents, b.Content)
	}
	if !contains(contents, "(human wording)") || !contains(contents, "(agent wording)") {
		t.Fatalf("merge dropped a version: %v", contents)
	}
}

func contains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
