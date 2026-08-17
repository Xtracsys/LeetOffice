// Package registry implements the Skills & Tools Registry (M23, BUILD_SPEC
// §9): local import/export of tool & skill folders, versioning through git
// commits, and the promoted-on-proof stability lifecycle (D12): experimental
// entries auto-promote to stable after N clean uses; a failed use resets.
package registry

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"leetoffice/internal/sync"
)

// DefaultThreshold is the auto-promotion threshold when a manifest omits it (D12).
const DefaultThreshold = 10

// Stability lifecycle values (D11).
type Stability string

const (
	Experimental Stability = "experimental"
	Stable       Stability = "stable"
	Deprecated   Stability = "deprecated"
)

// Manifest is the registry entry descriptor (BUILD_SPEC §9.1).
type Manifest struct {
	Name        string    `json:"name"`
	Kind        string    `json:"kind"` // "skill" | "tool"
	Version     string    `json:"version"`
	Stability   Stability `json:"stability"`
	Tools       []string  `json:"tools,omitempty"`
	CleanUses   int       `json:"clean_uses"`
	Threshold   int       `json:"threshold,omitempty"`
	Author      string    `json:"author,omitempty"`
	Description string    `json:"description,omitempty"`
}

// Entry is a loaded registry item.
type Entry struct {
	Manifest Manifest
	Dir      string // folder inside the registry root
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Root is the directory Load/Import/RecordUse scan for skills/ and tools/.
// It is the store worktree so a wizard store at ~/LeetOffice does not
// scan ~/skills (outside git, not the team's registry).
func Root(storeDir string) string {
	return storeDir
}

// Load scans <root>/skills and <root>/tools for manifest.json entries.
func Load(root string) ([]*Entry, error) {
	var out []*Entry
	for _, kind := range []string{"skills", "tools"} {
		dir := filepath.Join(root, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // no such registry section yet
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			manifestPath := filepath.Join(dir, e.Name(), "manifest.json")
			raw, err := os.ReadFile(manifestPath)
			if err != nil {
				continue // not a registry folder; hygiene's job to flag
			}
			var m Manifest
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
			}
			out = append(out, &Entry{Manifest: m, Dir: filepath.Join(kind, e.Name())})
		}
	}
	return out, nil
}

// Find loads a single named entry.
func Find(root, name string) (*Entry, error) {
	entries, err := Load(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Manifest.Name == name {
			return e, nil
		}
	}
	return nil, fmt.Errorf("registry entry %q not found", name)
}

func validate(m Manifest) error {
	if !nameRe.MatchString(m.Name) {
		return fmt.Errorf("invalid registry name %q", m.Name)
	}
	if m.Kind != "skill" && m.Kind != "tool" {
		return fmt.Errorf("kind must be skill or tool, got %q", m.Kind)
	}
	if !strings.HasPrefix(m.Version, "v") && !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(m.Version) {
		return fmt.Errorf("version %q is not semver", m.Version)
	}
	return nil
}

// Import copies a tool/skill folder into the registry and commits it (D11).
// repo may be nil to skip the git commit.
func Import(root, srcDir, actor string, repo *sync.Repo) (*Entry, error) {
	raw, err := os.ReadFile(filepath.Join(srcDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("source has no manifest.json: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse source manifest: %w", err)
	}
	if err := validate(m); err != nil {
		return nil, err
	}
	dest := filepath.Join(root, kindDir(m.Kind), m.Name)
	if _, err := os.Stat(filepath.Join(dest, "manifest.json")); err == nil {
		return nil, fmt.Errorf("%s %q already imported", m.Kind, m.Name)
	}
	if err := copyDir(srcDir, dest); err != nil {
		return nil, err
	}
	if repo != nil {
		_, err := repo.CommitAll(actor, fmt.Sprintf("registry: import %s@%s", m.Name, m.Version))
		if err != nil && !errors.Is(err, sync.ErrNoChanges) {
			return nil, err
		}
	}
	return &Entry{Manifest: m, Dir: filepath.Join(kindDir(m.Kind), m.Name)}, nil
}

func kindDir(kind string) string {
	if kind == "tool" {
		return "tools"
	}
	return "skills"
}

// Export zips the entry folder to <dest>/<name>-<version>.zip (D11).
func Export(e *Entry, root, dest string) (string, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(dest, fmt.Sprintf("%s-%s.zip", e.Manifest.Name, e.Manifest.Version))
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	src := filepath.Join(root, e.Dir)
	base := filepath.Base(e.Dir)
	err = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		zf, err := w.Create(filepath.Join(base, rel))
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = zf.Write(raw)
		return err
	})
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	return out, nil
}

// Save persists the entry's manifest.
func (e *Entry) Save(root string) error {
	raw, err := json.MarshalIndent(e.Manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, e.Dir, "manifest.json"), append(raw, '\n'), 0o644)
}

// RecordUse applies the promoted-on-proof rule (D12): a successful use
// increments clean_uses and auto-promotes experimental → stable at the
// threshold (default 10); a failed use resets clean_uses to zero.
func RecordUse(root, name string, success bool, actor string, repo *sync.Repo) (*Entry, error) {
	e, err := Find(root, name)
	if err != nil {
		return nil, err
	}
	threshold := e.Manifest.Threshold
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	if !success {
		e.Manifest.CleanUses = 0
		if err := e.Save(root); err != nil {
			return nil, err
		}
		if repo != nil {
			_, _ = repo.CommitAll(actor, fmt.Sprintf("registry: reset %s uses", name))
		}
		return e, nil
	}
	e.Manifest.CleanUses++
	promoted := false
	if e.Manifest.Stability == Experimental && e.Manifest.CleanUses >= threshold {
		e.Manifest.Stability = Stable
		promoted = true
	}
	if err := e.Save(root); err != nil {
		return nil, err
	}
	if repo != nil {
		msg := fmt.Sprintf("registry: use %s (%d/%d)", name, e.Manifest.CleanUses, threshold)
		if promoted {
			msg = fmt.Sprintf("registry: promote %s to stable", name)
		}
		_, _ = repo.CommitAll(actor, msg)
	}
	return e, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // skip links — registry entries are plain files
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
