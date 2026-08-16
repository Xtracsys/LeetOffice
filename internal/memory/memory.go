// Package memory implements the v1 automations (BUILD_SPEC §8.1–8.2, D16):
// team-memory synthesis into MEMORY.md, the daily digest from the git audit
// trail, and doc hygiene (broken links, stale docs, unindexed files).
//
// MEMORY.md is a derived root artifact, not a store doc: it lives at the store
// root (never under docs/), so it never appears in store.List or INDEX.md.
package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"leetoffice/internal/store"
	"leetoffice/internal/sync"
)

// inFlightWindow is how recently a doc must have been updated to count as
// "in flight" (§8.1).
const inFlightWindow = 48 * time.Hour

// Synthesize reads the whole store and (re)writes <root>/MEMORY.md in the
// §8.1 shape (In flight / Decisions / Open tasks / Owners), then commits it
// attributed to actor ("memory: synthesize"). A no-op re-run is not an error
// (sync.ErrNoChanges is tolerated). The daemon debounces re-runs on store
// change (near-continuous synthesis, D16).
func Synthesize(s *store.Store, repo *sync.Repo, actor string) error {
	docs, err := s.List()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var b strings.Builder
	b.WriteString("# Team Memory\n")
	fmt.Fprintf(&b, "_updated %s_\n", now.Format(time.RFC3339))
	writeSection(&b, "## In flight", inFlightBullets(docs, now))
	writeSection(&b, "## Decisions", decisionBullets(docs))
	writeSection(&b, "## Open tasks", openTaskBullets(docs))
	writeSection(&b, "## Owners / context", ownerBullets(docs))

	if err := os.WriteFile(filepath.Join(s.Root, "MEMORY.md"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	if _, err := repo.CommitAll(actor, "memory: synthesize"); err != nil && !errors.Is(err, sync.ErrNoChanges) {
		return err
	}
	return nil
}

// writeSection emits a section heading plus bullets, or a placeholder line
// when empty (§8.1 keeps all four sections present).
func writeSection(b *strings.Builder, heading string, bullets []string) {
	fmt.Fprintf(b, "\n%s\n", heading)
	if len(bullets) == 0 {
		b.WriteString("- _none_\n")
		return
	}
	for _, x := range bullets {
		b.WriteString(x)
		b.WriteByte('\n')
	}
}

// inFlightBullets lists docs updated within the in-flight window.
func inFlightBullets(docs []*store.Doc, now time.Time) []string {
	cutoff := now.Add(-inFlightWindow)
	var out []string
	for _, d := range docs {
		t, err := time.Parse(time.RFC3339, d.Updated)
		if err != nil || !t.After(cutoff) {
			continue
		}
		out = append(out, docBullet(d))
	}
	return out
}

// decisionBullets lists docs tagged "decision" — the tag only, per §8.1
// (a "D"-prefixed title is deliberately NOT treated as a decision).
func decisionBullets(docs []*store.Doc) []string {
	var out []string
	for _, d := range docs {
		for _, tag := range d.Tags {
			if strings.EqualFold(tag, "decision") {
				out = append(out, docBullet(d))
				break
			}
		}
	}
	return out
}

// openTaskBullets lists task docs holding at least one open task-item block
// (Meta["done"] != true), one bullet each.
func openTaskBullets(docs []*store.Doc) []string {
	var out []string
	for _, d := range docs {
		if d.Type != store.TypeTask {
			continue
		}
		open := false
		for i := range d.Blocks {
			if d.Blocks[i].Type == store.BlockTaskItem && d.Blocks[i].Meta["done"] != true {
				open = true
				break
			}
		}
		if open {
			out = append(out, fmt.Sprintf("- [ ] %s (tasks/%s) — updated %s", d.Title, d.Slug, dateOf(d.Updated)))
		}
	}
	return out
}

// ownerBullets lists docs carrying an owner — in doc properties, or in a
// block's meta as fallback.
func ownerBullets(docs []*store.Doc) []string {
	var out []string
	for _, d := range docs {
		owner, ok := d.Properties["owner"].(string)
		if !ok || owner == "" {
			for i := range d.Blocks {
				if o, isStr := d.Blocks[i].Meta["owner"].(string); isStr && o != "" {
					owner, ok = o, true
					break
				}
			}
		}
		if ok {
			out = append(out, fmt.Sprintf("- %s — %s (%s/%s)", owner, d.Title, dirOf(d.Type), d.Slug))
		}
	}
	return out
}

// docBullet renders "- <title> (<dir>/<slug>) — updated <date>".
func docBullet(d *store.Doc) string {
	return fmt.Sprintf("- %s (%s/%s) — updated %s", d.Title, dirOf(d.Type), d.Slug, dateOf(d.Updated))
}

// dateOf extracts the YYYY-MM-DD part of an RFC3339 timestamp.
func dateOf(updated string) string {
	return strings.SplitN(updated, "T", 2)[0]
}

// dirOf maps a doc type to its on-disk folder, mirroring the store layout.
func dirOf(t store.DocType) string {
	switch t {
	case store.TypeTask:
		return "tasks"
	case store.TypeContact:
		return "contacts"
	case store.TypeChannel:
		return "channels"
	case store.TypeCompany:
		return "companies"
	case store.TypeEmail:
		return "emails"
	case store.TypeMemory:
		return "memory"
	default:
		return "docs"
	}
}
