// Package store implements the LeetOffice store: the block-aware document
// schema (BUILD_SPEC §4), bidirectional block-level links (D15), field-level
// encryption (D2), and JSON serialization. The embedded JSON is the single
// source of truth; the tabbed HTML renders from it and INDEX.md derives from it.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SchemaURL identifies the canonical document schema version.
const SchemaURL = "https://leetoffice.dev/schema/doc.json"

// DocType enumerates the document kinds.
type DocType string

const (
	TypeDoc     DocType = "doc"
	TypeTask    DocType = "task"
	TypeContact DocType = "contact"
	TypeChannel DocType = "channel"
	TypeCompany DocType = "company"
	TypeEmail   DocType = "email"
	TypeMemory  DocType = "memory"
)

// BlockType enumerates block kinds.
type BlockType string

const (
	BlockParagraph BlockType = "paragraph"
	BlockHeading   BlockType = "heading"
	BlockTaskItem  BlockType = "task-item"
	BlockListItem  BlockType = "list-item"
	BlockCode      BlockType = "code"
	BlockField     BlockType = "field"
	BlockDivider   BlockType = "divider"
)

// Link is a block-level edge. dir is "out" on the source block and "in" on the
// backlink written to the target block's block.
type Link struct {
	ID          string `json:"id"`
	TargetDoc   string `json:"target_doc"`
	TargetBlock string `json:"target_block"`
	Label       string `json:"label,omitempty"`
	Dir         string `json:"dir"`
}

// Block is the unit of content and the unit of linking (D15).
type Block struct {
	ID      string         `json:"id"`
	Type    BlockType      `json:"type"`
	Content string         `json:"content"`
	Level   int            `json:"level,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
	Links   []Link         `json:"links,omitempty"`
}

// Audit records the last editor and commit for attribution (D7).
type Audit struct {
	LastEditor string `json:"last_editor"`
	LastCommit string `json:"last_commit,omitempty"`
}

// Doc is the canonical embedded-JSON payload (BUILD_SPEC §4.2).
type Doc struct {
	Schema     string         `json:"$schema"`
	ID         string         `json:"id"`
	Type       DocType        `json:"type"`
	Slug       string         `json:"slug"`
	Title      string         `json:"title"`
	Version    int            `json:"version"`
	Updated    string         `json:"updated"`
	Tags       []string       `json:"tags,omitempty"`
	Blocks     []Block        `json:"blocks"`
	Properties map[string]any `json:"properties,omitempty"`
	Audit      Audit          `json:"audit"`
}

// NewDoc creates a Doc with a fresh ID and timestamp.
func NewDoc(t DocType, slug, title string) *Doc {
	return &Doc{
		Schema:  SchemaURL,
		ID:      NewID(),
		Type:    t,
		Slug:    slug,
		Title:   title,
		Version: 1,
		Updated: time.Now().UTC().Format(time.RFC3339),
		Blocks:  []Block{},
	}
}

// AddBlock appends a block and returns it.
func (d *Doc) AddBlock(b Block) *Block {
	b.ID = NewID()
	if b.ID == "" {
		b.ID = NewID()
	}
	d.Blocks = append(d.Blocks, b)
	d.Bump()
	return &d.Blocks[len(d.Blocks)-1]
}

// AddParagraph is a convenience for appending a paragraph block.
func (d *Doc) AddParagraph(content string) *Block {
	return d.AddBlock(Block{Type: BlockParagraph, Content: content})
}

// Block returns the block with the given ID, or nil.
func (d *Doc) Block(id string) *Block {
	for i := range d.Blocks {
		if d.Blocks[i].ID == id {
			return &d.Blocks[i]
		}
	}
	return nil
}

// Bump increments the version and refreshes the timestamp.
func (d *Doc) Bump() {
	d.Version++
	d.Updated = time.Now().UTC().Format(time.RFC3339)
}

// Bytes serializes the canonical JSON payload.
func (d *Doc) Bytes() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

// ParseDoc decodes a canonical JSON payload.
func ParseDoc(data []byte) (*Doc, error) {
	var d Doc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse doc: %w", err)
	}
	if d.Schema != SchemaURL {
		return nil, errors.New("unknown doc schema: " + d.Schema)
	}
	return &d, nil
}
