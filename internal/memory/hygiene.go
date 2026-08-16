package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"leetoffice/internal/store"
)

// Issue kinds reported by Hygiene.
const (
	KindBrokenLink    = "broken-link"
	KindStaleDoc      = "stale-doc"
	KindUnindexedFile = "unindexed-file"
)

// defaultStaleAfter is the staleness threshold used when staleAfter <= 0.
const defaultStaleAfter = 30 * 24 * time.Hour

// Issue is one doc-hygiene finding (D16, BUILD_SPEC §7.3).
type Issue struct {
	Kind   string // "broken-link" | "stale-doc" | "unindexed-file"
	Detail string
}

// Hygiene scans the store for broken block-links (via store.BrokenLinks over
// all docs), stale docs (Updated older than staleAfter), and unindexed files
// (.html files under the type dirs whose embedded JSON cannot be parsed), then
// reindexes the store (INDEX.md is derived). staleAfter <= 0 means 30 days.
func Hygiene(s *store.Store, staleAfter time.Duration) ([]Issue, error) {
	if staleAfter <= 0 {
		staleAfter = defaultStaleAfter
	}
	docs, err := s.List()
	if err != nil {
		return nil, err
	}
	var issues []Issue
	lookup := s.Lookup()
	cutoff := time.Now().UTC().Add(-staleAfter)
	for _, d := range docs {
		for _, l := range store.BrokenLinks(d, lookup) {
			issues = append(issues, Issue{
				Kind:   KindBrokenLink,
				Detail: fmt.Sprintf("doc %s block %s -> doc %s block %s", d.Slug, blockIDOfLink(d, l.ID), l.TargetDoc, l.TargetBlock),
			})
		}
		if t, err := time.Parse(time.RFC3339, d.Updated); err == nil && t.Before(cutoff) {
			issues = append(issues, Issue{
				Kind:   KindStaleDoc,
				Detail: fmt.Sprintf("doc %s (%s) updated %s", d.Slug, d.Type, d.Updated),
			})
		}
	}
	issues = append(issues, unindexedFiles(s)...)
	if err := s.Reindex(); err != nil {
		return issues, err
	}
	return issues, nil
}

// blockIDOfLink finds the id of the block holding the link with the given id.
func blockIDOfLink(d *store.Doc, linkID string) string {
	for i := range d.Blocks {
		for _, l := range d.Blocks[i].Links {
			if l.ID == linkID {
				return d.Blocks[i].ID
			}
		}
	}
	return "?"
}

// unindexedFiles walks the type dirs for .html files whose embedded JSON does
// not parse (missing dirs are skipped).
func unindexedFiles(s *store.Store) []Issue {
	var issues []Issue
	for _, dir := range []string{"docs", "tasks", "contacts", "channels", "companies", "emails", "memory"} {
		entries, err := os.ReadDir(filepath.Join(s.Root, dir))
		if err != nil {
			continue // missing dir is fine
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(s.Root, dir, e.Name()))
			if err != nil {
				continue
			}
			if _, err := store.ExtractDoc(raw); err != nil {
				issues = append(issues, Issue{
					Kind:   KindUnindexedFile,
					Detail: fmt.Sprintf("%s/%s: %v", dir, e.Name(), err),
				})
			}
		}
	}
	return issues
}
