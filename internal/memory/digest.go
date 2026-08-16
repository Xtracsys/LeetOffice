package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"leetoffice/internal/store"
	"leetoffice/internal/sync"
)

// DailyDigest writes <root>/_audit/DIGEST-YYYY-MM-DD.md summarizing the git
// audit trail over day's calendar day (UTC): what changed (per doc path and
// commit count), by whom (per actor), new tasks (audit files under tasks/),
// and notable links (commits whose message starts "link:") — §8.2, D16. The
// digest file itself is committed attributed to actor. Returns the file path.
func DailyDigest(s *store.Store, repo *sync.Repo, day time.Time, actor string) (string, error) {
	utc := day.UTC()
	start := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	entries, err := repo.AuditLog("", start, "", 0)
	if err != nil {
		return "", err
	}

	pathCounts := map[string]int{}
	actorCounts := map[string]int{}
	seenTask := map[string]bool{}
	var newTasks, links []string
	for _, e := range entries {
		if !e.When.Before(end) {
			continue // past the calendar day
		}
		actorCounts[e.Actor]++
		if msg := strings.TrimSpace(e.Msg); strings.HasPrefix(msg, "link:") {
			links = append(links, fmt.Sprintf("- %s (%s, %s)", msg, e.Actor, shortSha(e.Commit)))
		}
		for _, f := range e.Files {
			if strings.HasSuffix(f, ".html") {
				pathCounts[f]++
			}
			if strings.HasPrefix(f, "tasks/") && !seenTask[f] {
				seenTask[f] = true
				newTasks = append(newTasks, f)
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Daily Digest %s\n\n", start.Format("2006-01-02"))
	b.WriteString("## What changed\n")
	if len(pathCounts) == 0 {
		b.WriteString("- _none_\n")
	}
	for _, p := range sortedKeys(pathCounts) {
		fmt.Fprintf(&b, "- `%s` — %d commit(s)\n", p, pathCounts[p])
	}
	b.WriteString("\n## By whom\n")
	if len(actorCounts) == 0 {
		b.WriteString("- _none_\n")
	}
	for _, a := range sortedKeys(actorCounts) {
		fmt.Fprintf(&b, "- `%s` — %d commit(s)\n", a, actorCounts[a])
	}
	b.WriteString("\n## New tasks\n")
	if len(newTasks) == 0 {
		b.WriteString("- _none_\n")
	}
	for _, t := range newTasks {
		fmt.Fprintf(&b, "- `%s`\n", t)
	}
	b.WriteString("\n## Notable links\n")
	if len(links) == 0 {
		b.WriteString("- _none_\n")
	}
	for _, l := range links {
		b.WriteString(l + "\n")
	}

	dir := filepath.Join(s.Root, "_audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "DIGEST-"+start.Format("2006-01-02")+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	if _, err := repo.CommitAll(actor, "digest: "+start.Format("2006-01-02")); err != nil && !errors.Is(err, sync.ErrNoChanges) {
		return "", err
	}
	return path, nil
}

// sortedKeys returns the map's keys in sorted order (deterministic digest).
func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// shortSha abbreviates a commit sha for human-readable output.
func shortSha(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
