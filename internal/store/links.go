package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewID returns a random 128-bit hex id (uuid-equivalent, no external dep).
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("rand: %v", err))
	}
	return hex.EncodeToString(b)
}

// AddLink creates a block-level bidirectional link (D15). It writes an "out"
// link onto src.Block and an "in" backlink onto dst.Block, so the graph is
// navigable in both directions. Both docs are returned so the caller can
// persist and commit both (the single-write atomicity is the caller's job).
func AddLink(src *Doc, srcBlockID string, dst *Doc, dstBlockID string, label string) error {
	srcB := src.Block(srcBlockID)
	if srcB == nil {
		return fmt.Errorf("source block %q not found", srcBlockID)
	}
	dstB := dst.Block(dstBlockID)
	if dstB == nil {
		return fmt.Errorf("target block %q not found in %q", dstBlockID, dst.Slug)
	}
	id := NewID()
	srcB.Links = append(srcB.Links, Link{
		ID:          id,
		TargetDoc:   dst.ID,
		TargetBlock: dstBlockID,
		Label:       label,
		Dir:         "out",
	})
	dstB.Links = append(dstB.Links, Link{
		ID:          id,
		TargetDoc:   src.ID,
		TargetBlock: srcBlockID,
		Label:       label,
		Dir:         "in",
	})
	src.Bump()
	dst.Bump()
	return nil
}

// BrokenLinks reports links whose target no longer resolves, for doc hygiene
// (D16). target is a function that returns the doc with the given id.
func BrokenLinks(d *Doc, lookup func(id string) *Doc) []Link {
	var broken []Link
	for i := range d.Blocks {
		for _, l := range d.Blocks[i].Links {
			if l.Dir != "out" {
				continue
			}
			td := lookup(l.TargetDoc)
			if td == nil || td.Block(l.TargetBlock) == nil {
				broken = append(broken, l)
			}
		}
	}
	return broken
}
