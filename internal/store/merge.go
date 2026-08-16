package store

import (
	"fmt"
	"strings"
)

// Conflict describes a same-block concurrent edit resolved by keeping both
// versions (BUILD_SPEC §7.2 — never silently overwrite).
type Conflict struct {
	BlockID  string `json:"block_id"`
	Slug     string `json:"slug"`
	Ours     string `json:"ours"`
	Theirs   string `json:"theirs"`
	Resolved string `json:"resolved"`
}

// MergeDocs performs a three-way, block-level merge of ours and theirs against
// base (all the same document). Rules (leet-merge, §7.2):
//   - block added in one branch only → kept
//   - block edited in one branch only → that version wins
//   - block edited in both branches → BOTH versions kept; the later one is
//     marked meta.conflict=true so the monitor flags it
//   - block deleted in one branch and unedited in the other → deleted
func MergeDocs(base, ours, theirs *Doc) (*Doc, []Conflict) {
	merged := *ours // start from ours; copy-of-struct, Blocks re-sliced below
	merged.Blocks = nil
	var conflicts []Conflict

	theirsByID := blockMap(theirs)
	oursByID := blockMap(ours)

	// Pass 1: all blocks from ours, in order.
	for i := range ours.Blocks {
		ob := ours.Blocks[i]
		bb, inBase := blockMap(base)[ob.ID]
		tb, inTheirs := theirsByID[ob.ID]

		switch {
		case !inTheirs && (inBase && blockEqual(bb, ob)): // deleted on theirs, unedited here
			// drop (deleted on their branch)
		case !inTheirs:
			// deleted on their branch but edited here → keep ours
			merged.Blocks = append(merged.Blocks, ob)
		case blockEqual(ob, tb):
			merged.Blocks = append(merged.Blocks, ob) // identical
		case inBase && blockEqual(bb, ob) && !blockEqual(bb, tb):
			merged.Blocks = append(merged.Blocks, tb) // only theirs edited
		case inBase && !blockEqual(bb, ob) && blockEqual(bb, tb):
			merged.Blocks = append(merged.Blocks, ob) // only ours edited
		default:
			// both edited (or no base): keep both; the later arrival (ours,
			// the branch being merged) gets the conflict flag
			fl := ob
			fl.Meta = copyMeta(fl.Meta)
			fl.Meta["conflict"] = true
			merged.Blocks = append(merged.Blocks, tb, fl)
			conflicts = append(conflicts, Conflict{
				BlockID: ob.ID, Slug: ours.Slug,
				Ours: ob.Content, Theirs: tb.Content, Resolved: "both-retained",
			})
		}
	}

	// Pass 2: blocks added only on theirs.
	for i := range theirs.Blocks {
		tb := theirs.Blocks[i]
		if _, ok := oursByID[tb.ID]; ok {
			continue
		}
		if _, inBase := blockMap(base)[tb.ID]; inBase {
			continue // deleted on ours, unedited there → stays deleted
		}
		merged.Blocks = append(merged.Blocks, tb)
	}

	merged.Version = max(ours.Version, theirs.Version) + 1
	merged.Audit = ours.Audit
	merged.Audit.LastEditor = mergeAttribution(ours.Audit.LastEditor, theirs.Audit.LastEditor)
	return &merged, conflicts
}

func mergeAttribution(a, b string) string {
	if a == b || b == "" {
		return a
	}
	if a == "" {
		return b
	}
	return fmt.Sprintf("%s + %s", a, b)
}

func blockMap(d *Doc) map[string]Block {
	m := make(map[string]Block, len(d.Blocks))
	for _, b := range d.Blocks {
		m[b.ID] = b
	}
	return m
}

func blockEqual(a, b Block) bool {
	if a.Type != b.Type || a.Content != b.Content || a.Level != b.Level {
		return false
	}
	return fmt.Sprint(a.Meta) == fmt.Sprint(b.Meta)
}

func copyMeta(m map[string]any) map[string]any {
	c := make(map[string]any, len(m)+1)
	for k, v := range m {
		c[k] = v
	}
	return c
}

// UnifiedBlockDiff renders a simple line diff between two versions of a doc
// at block granularity (used by the MCP diff tool).
func UnifiedBlockDiff(from, to *Doc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s v%d\n+++ %s v%d\n", from.Slug, from.Version, to.Slug, to.Version)
	fm := blockMapIndex(from)
	tm := blockMapIndex(to)
	for i := range from.Blocks {
		blk := from.Blocks[i]
		if _, ok := tm[blk.ID]; !ok {
			fmt.Fprintf(&b, "-[%s %s] %s\n", blk.ID, blk.Type, blk.Content)
		}
	}
	for i := range to.Blocks {
		blk := to.Blocks[i]
		if _, ok := fm[blk.ID]; !ok {
			fmt.Fprintf(&b, "+[%s %s] %s\n", blk.ID, blk.Type, blk.Content)
		}
	}
	for i := range to.Blocks {
		blk := to.Blocks[i]
		if fb, ok := fm[blk.ID]; ok && !blockEqual(fb, blk) {
			fmt.Fprintf(&b, "-[%s %s] %s\n", blk.ID, fb.Type, fb.Content)
			fmt.Fprintf(&b, "+[%s %s] %s\n", blk.ID, blk.Type, blk.Content)
		}
	}
	return b.String()
}

func blockMapIndex(d *Doc) map[string]Block { return blockMap(d) }
