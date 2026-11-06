package agentaccess

import "testing"

func TestLongLivedManagerObservesRotationsAndRevocations(t *testing.T) {
	dir := t.TempDir()
	admin, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mcpProcess, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent, first, err := admin.Create("Long session", []string{"read_doc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := mcpProcess.Authenticate(first); !ok {
		t.Fatal("already-running MCP manager did not observe newly issued credential")
	}
	_, second, err := admin.Rotate(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := mcpProcess.Authenticate(first); ok {
		t.Fatal("already-running MCP manager accepted rotated credential")
	}
	if _, _, _, ok := mcpProcess.Authenticate(second); !ok {
		t.Fatal("already-running MCP manager did not observe rotated credential")
	}
	if _, err := admin.Revoke(agent.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := mcpProcess.Authenticate(second); ok {
		t.Fatal("already-running MCP manager accepted revoked credential")
	}
}
