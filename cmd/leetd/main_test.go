package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPInstallTightensExistingConfigPermissions(t *testing.T) {
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	configPath := filepath.Join(workDir, ".mcp.json")
	if err := os.WriteFile(configPath, []byte(`{"old":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configPath, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdMCPInstall([]string{"--client", "claude", "--write"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf(".mcp.json permissions = %04o, want 0600", got)
	}
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"mcpServers"`) {
		t.Fatalf(".mcp.json was not replaced with MCP configuration: %s", contents)
	}
}

func TestMCPInstallRefusesSymlinkConfig(t *testing.T) {
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	targetPath := filepath.Join(workDir, "target.json")
	configPath := filepath.Join(workDir, ".mcp.json")
	original := []byte(`{"keep":true}`)
	if err := os.WriteFile(targetPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, configPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	if err := cmdMCPInstall([]string{"--client", "claude", "--write"}); err == nil {
		t.Fatal("mcp-install unexpectedly followed a .mcp.json symlink")
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("symlink target content changed: %s", got)
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("symlink target permissions = %04o, want 0644", got)
	}
}
