package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leetoffice/internal/agentaccess"
	"leetoffice/internal/chat"
	"leetoffice/internal/config"
	"leetoffice/internal/mcp"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

func newAuthenticatedServer(t *testing.T, access *agentaccess.Manager, initialActor string) (*mcp.Server, *store.Store, *leetSync.Repo) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := leetSync.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := mcp.NewServer(st, repo, nil, initialActor)
	srv.SetAuthenticator(access)
	return srv, st, repo
}

func TestHTTPBearerAuthenticationAndToolScopeEnforcement(t *testing.T) {
	access, err := agentaccess.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := access.Create("Reader", []string{"read_doc"})
	if err != nil {
		t.Fatal(err)
	}
	srv, st, _ := newAuthenticatedServer(t, access, "human:untrusted")
	handler := srv.Handler()

	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated MCP status = %d", unauth.Code)
	}

	listReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	var listResponse struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResponse); err != nil {
		t.Fatal(err)
	}
	if listRec.Code != http.StatusOK || len(listResponse.Result.Tools) != 1 || listResponse.Result.Tools[0].Name != "read_doc" {
		t.Fatalf("scope-filtered tools/list: status=%d response=%s", listRec.Code, listRec.Body.String())
	}

	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"write_doc","arguments":{"id_or_slug":"denied-doc","content":"should not persist"}}}`
	callReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(callBody))
	callReq.Header.Set("Authorization", "Bearer "+token)
	callRec := httptest.NewRecorder()
	handler.ServeHTTP(callRec, callReq)
	var callResponse struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(callRec.Body.Bytes(), &callResponse); err != nil {
		t.Fatal(err)
	}
	if !callResponse.Result.IsError {
		t.Fatalf("write_doc without scope was not denied: %s", callRec.Body.String())
	}
	if _, err := st.Load("denied-doc"); err == nil {
		t.Fatal("scope-denied tool changed the store")
	}
}

func TestAuthenticatedStdioBindsActorAndRejectsSpoof(t *testing.T) {
	identityDir := t.TempDir()
	access, err := agentaccess.Open(identityDir)
	if err != nil {
		t.Fatal(err)
	}
	agent, token, err := access.Create("Trusted writer", []string{"send_message", "subscribe"})
	if err != nil {
		t.Fatal(err)
	}
	srv, st, repo := newAuthenticatedServer(t, access, "human:caller-supplied")
	cfgPath := filepath.Join(t.TempDir(), "node.json")
	cfg := config.Default(t.TempDir(), "human:local-config")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	srv.BindConfig(cfg, cfgPath)

	if err := srv.ServeStdio(bytes.NewReader(nil), &bytes.Buffer{}); err == nil {
		t.Fatal("unauthenticated stdio path remained enabled after credential setup")
	}
	requests := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
			"name": "send_message", "arguments": map[string]any{"channel": "ops", "text": "credential-bound write", "actor": "human:forged"},
		}},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{
			"name": "subscribe", "arguments": map[string]any{"actor": "agent:someone-else", "channels": []string{"ops"}},
		}},
	}
	var input bytes.Buffer
	for _, request := range requests {
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(raw)
		input.WriteByte('\n')
	}
	var output bytes.Buffer
	if err := srv.ServeStdioAuthenticated(token, &input, &output); err != nil {
		t.Fatalf("authenticated stdio: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("response count = %d, output=%s", len(lines), output.String())
	}
	var spoofResponse struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &spoofResponse); err != nil {
		t.Fatal(err)
	}
	if !spoofResponse.Result.IsError {
		t.Fatalf("caller-selected actor was accepted by subscribe: %s", lines[1])
	}
	channel, err := st.Load("ops")
	if err != nil {
		t.Fatal(err)
	}
	messages := chat.Messages(channel)
	if len(messages) != 1 || messages[0].Author != "agent:"+agent.ID {
		t.Fatalf("write was not attributed to authenticated identity %s: %+v", agent.ID, messages)
	}
	entries, err := repo.AuditLog("channels/ops.html", time.Time{}, "agent:"+agent.ID, 5)
	if err != nil || len(entries) == 0 {
		t.Fatalf("authenticated actor missing from git audit history: %v %+v", err, entries)
	}
	if _, ok := cfg.Subscription("agent:" + agent.ID); ok {
		t.Fatal("spoofed subscribe changed the credential-bound actor's subscription")
	}
}
