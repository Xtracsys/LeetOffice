package agentaccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialVerifierPersistsWithoutToken(t *testing.T) {
	dir := t.TempDir()
	manager, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	agent, token, err := manager.Create("Build agent", []string{"read_doc", "search"})
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID == "" || token == "" || agent.DisplayName != "Build agent" {
		t.Fatalf("create returned incomplete identity: %+v", agent)
	}
	actor, display, scopes, ok := manager.Authenticate(token)
	if !ok || actor != "agent:"+agent.ID || display != agent.DisplayName {
		t.Fatalf("authenticate = %q, %q, %v, %v", actor, display, scopes, ok)
	}
	if len(scopes) != 2 || scopes[0] != "read_doc" || scopes[1] != "search" {
		t.Fatalf("scopes = %v", scopes)
	}

	path := filepath.Join(dir, "agents.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("plaintext credential was persisted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential store permissions = %o, want 600", info.Mode().Perm())
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	actor, display, scopes, ok = reopened.Authenticate(token)
	if !ok || actor != "agent:"+agent.ID || display != agent.DisplayName || len(scopes) != 2 {
		t.Fatalf("persisted credential failed after reopen: %q, %q, %v, %v", actor, display, scopes, ok)
	}
}

func TestCredentialRotationDisableAndRevoke(t *testing.T) {
	dir := t.TempDir()
	manager, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, first, err := manager.Create("Release bot", []string{"write_doc"})
	if err != nil {
		t.Fatal(err)
	}
	rotated, second, err := manager.Rotate(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.ID != agent.ID || first == second {
		t.Fatalf("rotation changed identity or reused token: %+v", rotated)
	}
	if _, _, _, ok := manager.Authenticate(first); ok {
		t.Fatal("rotated credential remained valid")
	}
	if _, _, _, ok := manager.Authenticate(second); !ok {
		t.Fatal("new credential was not valid")
	}
	if _, err := manager.Disable(agent.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := manager.Authenticate(second); ok {
		t.Fatal("disabled credential remained valid")
	}
	if _, err := manager.Enable(agent.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := manager.Authenticate(second); !ok {
		t.Fatal("enabled credential did not become valid")
	}
	if _, err := manager.Revoke(agent.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := manager.Authenticate(second); ok {
		t.Fatal("revoked credential remained valid")
	}
	if _, _, err := manager.Rotate(agent.ID); err == nil {
		t.Fatal("revoked identity was rotated")
	}

	agents, events := manager.List()
	if len(agents) != 1 || !agents[0].Revoked || !agents[0].Disabled {
		t.Fatalf("revoked identity not retained in admin list: %+v", agents)
	}
	if len(events) != 5 {
		t.Fatalf("audit events = %d, want create/rotate/disable/enable/revoke", len(events))
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := reopened.Authenticate(second); ok {
		t.Fatal("revoked credential became valid after restart")
	}
	persistedAgents, persistedEvents := reopened.List()
	if len(persistedAgents) != 1 || !persistedAgents[0].Revoked || len(persistedEvents) != 5 {
		t.Fatalf("revocation/audit did not persist: agents=%+v events=%+v", persistedAgents, persistedEvents)
	}
}

func TestScopesMustNameRealMCPTools(t *testing.T) {
	manager, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create("Bad scope", []string{"not_a_tool"}); err == nil {
		t.Fatal("unknown MCP tool scope accepted")
	}
	agent, token, err := manager.Create("Reader", []string{"read_doc"})
	if err != nil {
		t.Fatal(err)
	}
	actor, _, scopes, ok := manager.Authenticate(token)
	if !ok || actor != "agent:"+agent.ID || len(scopes) != 1 || scopes[0] != "read_doc" {
		t.Fatalf("scope-bound identity = %q %v %v", actor, scopes, ok)
	}
}
