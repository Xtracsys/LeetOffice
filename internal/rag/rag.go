// Package rag implements retrieval over the store (BUILD_SPEC §8.3, D17):
// an always-offline keyword search with memory-boosted ranking, plus an
// optional Ollama embeddings path that degrades to the keyword search when
// the local model server is unavailable. The v1 index is kept in memory and
// rebuilt per query from the canonical embedded JSON; encrypted fields are
// never indexed (D2/D17).
package rag

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"leetoffice/internal/store"
)

// defaultLimit is the result cap when limit <= 0 (MCP search contract, §5).
const defaultLimit = 10

// snippetLen is the approximate snippet window around the first match.
const snippetLen = 160

// MemoryBoost is the configurable score multiplier applied to hits from
// MEMORY.md and from summary/decision docs, so curated context outranks raw
// blocks (D17).
var MemoryBoost = 1.5

// Hit is one search result — the shape the MCP search tool returns (§5).
type Hit struct {
	DocID   string  `json:"doc_id"`
	BlockID string  `json:"block_id"`
	Slug    string  `json:"slug"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

// Search is the offline keyword fallback: it tokenizes the query (lowercase,
// split on non-alphanumerics), scores each block as the sum of query-term
// frequencies in its content (plus a small boost for title/tag matches),
// applies the memory boost, and returns the top hits. typ filters by doc type
// when non-empty; tags filters to docs carrying all the given tags.
func Search(s *store.Store, query, typ string, tags []string, limit int) ([]Hit, error) {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	cands, err := collectCandidates(s, typ, tags)
	if err != nil {
		return nil, err
	}
	var hits []Hit
	for _, c := range cands {
		score := scoreCandidate(c, terms)
		if score <= 0 {
			continue
		}
		if c.boosted {
			score *= MemoryBoost
		}
		hits = append(hits, Hit{
			DocID:   c.docID,
			BlockID: c.blockID,
			Slug:    c.slug,
			Title:   c.title,
			Snippet: snippet(c.text, terms),
			Score:   score,
		})
	}
	hits = sortHits(hits, limit)
	return hits, nil
}

// candidate is one indexable text unit: a store block, or a MEMORY.md line.
type candidate struct {
	docID, blockID, slug, title, text string
	tags                              []string
	boosted                           bool
}

// collectCandidates gathers every indexable block, plus MEMORY.md lines as
// pseudo-blocks (Slug "MEMORY"). Doc properties are never indexed; blocks
// flagged encrypted (meta enc, or content that is an EncryptedValue) are
// excluded (D2/D17). MEMORY lines only participate in unfiltered searches.
func collectCandidates(s *store.Store, typ string, tags []string) ([]candidate, error) {
	docs, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []candidate
	for _, d := range docs {
		if typ != "" && !strings.EqualFold(string(d.Type), typ) {
			continue
		}
		if !hasAllTags(d.Tags, tags) {
			continue
		}
		boost := curated(d)
		for i := range d.Blocks {
			b := &d.Blocks[i]
			if !indexable(b) {
				continue
			}
			out = append(out, candidate{
				docID: d.ID, blockID: b.ID, slug: d.Slug,
				title: d.Title, tags: d.Tags, text: b.Content, boosted: boost,
			})
		}
	}
	if typ == "" && len(tags) == 0 {
		out = append(out, memoryCandidates(s)...)
	}
	return out, nil
}

// memoryCandidates parses the root MEMORY.md as plain markdown, treating each
// heading and bullet line as a boosted pseudo-block (D17).
func memoryCandidates(s *store.Store) []candidate {
	raw, err := os.ReadFile(filepath.Join(s.Root, "MEMORY.md"))
	if err != nil {
		return nil
	}
	title := "Team Memory"
	var out []candidate
	for i, line := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(line)
		var content string
		switch {
		case strings.HasPrefix(t, "#"):
			content = strings.TrimSpace(strings.TrimLeft(t, "# "))
			if title == "Team Memory" && strings.HasPrefix(t, "# ") {
				title = content
			}
		case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "):
			content = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, "- "), "* "))
		default:
			continue
		}
		if content == "" {
			continue
		}
		out = append(out, candidate{
			docID: "MEMORY", blockID: fmt.Sprintf("line-%d", i+1), slug: "MEMORY",
			title: title, text: content, boosted: true,
		})
	}
	return out
}

// curated reports whether a doc is a summary/decision doc (by tag or title) —
// those rank above raw blocks (D17).
func curated(d *store.Doc) bool {
	for _, tag := range d.Tags {
		if lt := strings.ToLower(tag); lt == "summary" || lt == "decision" {
			return true
		}
	}
	title := strings.ToLower(d.Title)
	return strings.Contains(title, "summary") || strings.Contains(title, "decision")
}

// indexable reports whether a block may enter the index: encrypted blocks
// (meta enc, or content wrapping an EncryptedValue) are excluded (D2/D17).
func indexable(b *store.Block) bool {
	if truthy(b.Meta["enc"]) {
		return false
	}
	var ev store.EncryptedValue
	if err := json.Unmarshal([]byte(b.Content), &ev); err == nil && ev.Enc {
		return false
	}
	return true
}

// truthy interprets a meta value loosely (JSON decodes to bool/string/float).
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1" || t == "yes"
	case float64:
		return t != 0
	default:
		return false
	}
}

// hasAllTags reports whether docTags contains every requested tag.
func hasAllTags(docTags []string, want []string) bool {
	for _, w := range want {
		found := false
		for _, t := range docTags {
			if strings.EqualFold(t, w) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// scoreCandidate sums query-term frequencies in the candidate text, with a
// small boost when a term also matches the doc title or tags.
func scoreCandidate(c candidate, terms []string) float64 {
	freq := termFreq(tokenize(c.text))
	titleTerms := termSet(c.title)
	tagTerms := termSet(strings.Join(c.tags, " "))
	var score float64
	for _, t := range terms {
		score += float64(freq[t])
		if titleTerms[t] {
			score += 0.5
		}
		if tagTerms[t] {
			score += 0.25
		}
	}
	return score
}

// termFreq counts word occurrences.
func termFreq(tokens []string) map[string]int {
	m := make(map[string]int, len(tokens))
	for _, t := range tokens {
		m[t]++
	}
	return m
}

// termSet returns the set of distinct tokens.
func termSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, t := range tokenize(s) {
		m[t] = true
	}
	return m
}

// tokenize lowercases and splits on non-alphanumeric runes.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// snippet trims content to ~snippetLen chars around the first query match.
func snippet(content string, terms []string) string {
	c := strings.TrimSpace(content)
	if len(c) <= snippetLen {
		return c
	}
	lower := strings.ToLower(c)
	best := -1
	for _, t := range terms {
		if i := strings.Index(lower, t); i >= 0 && (best == -1 || i < best) {
			best = i
		}
	}
	start := 0
	if best > 40 {
		start = best - 40
	}
	end := start + snippetLen
	if end > len(c) {
		end = len(c)
		start = max(0, end-snippetLen)
	}
	out := c[start:end]
	if start > 0 {
		out = "..." + out
	}
	if end < len(c) {
		out += "..."
	}
	return out
}

// sortHits orders by score (stable, so equal scores keep store order) and
// truncates to limit.
func sortHits(hits []Hit, limit int) []Hit {
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}
