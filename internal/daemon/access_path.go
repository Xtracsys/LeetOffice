package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateAgentAccessPath prevents local credentials from entering the
// synchronized worktree, including when a path traverses a symlink.
func validateAgentAccessPath(storeDir, identityDir string) error {
	storePath, err := resolvedPath(storeDir)
	if err != nil {
		return fmt.Errorf("resolve store path: %w", err)
	}
	identityPath, err := resolvedPath(identityDir)
	if err != nil {
		return fmt.Errorf("resolve agent identity path: %w", err)
	}
	rel, err := filepath.Rel(storePath, identityPath)
	if err != nil {
		return fmt.Errorf("compare store and agent identity paths: %w", err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return fmt.Errorf("agent identity directory must be outside the synchronized store")
	}
	return nil
}

// resolvedPath evaluates any existing symlink prefixes and appends missing
// path components without creating them.
func resolvedPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", err
		}
		suffix = append(suffix, filepath.Base(abs))
		abs = parent
	}
}
