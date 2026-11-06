package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentAccessPathMustStayOutsideStoreEvenThroughSymlink(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "store")
	identityDir := filepath.Join(root, ".identity")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentAccessPath(storeDir, identityDir); err != nil {
		t.Fatalf("external local identity path rejected: %v", err)
	}
	if err := validateAgentAccessPath(storeDir, filepath.Join(storeDir, ".identity")); err == nil {
		t.Fatal("identity path inside synchronized store accepted")
	}
	alias := filepath.Join(root, "identity-link")
	if err := os.Symlink(storeDir, alias); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentAccessPath(storeDir, filepath.Join(alias, "identity")); err == nil {
		t.Fatal("symlinked identity path into synchronized store accepted")
	}
}
