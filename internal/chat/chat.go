// Package chat is team conversation on top of the store (M2's channel type):
// a channel is a channel-type document, a message is a block with author/timestamp
// metadata. Because blocks are the merge unit (D6), concurrent messages from
// different nodes both survive sync — chat semantics fall out of the store's
// existing guarantees: attributed, timestamped, durable in git, searchable,
// and offline-first with catch-up on rejoin.
package chat

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

var nameUnwanted = regexp.MustCompile(`[^a-z0-9-]+`)

// Normalize turns "#General Chat", "general chat" etc. into a slug.
func Normalize(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "#")
	name = strings.ReplaceAll(name, " ", "-")
	name = nameUnwanted.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "general"
	}
	return name
}

// EnsureChannel loads a channel by name, creating it on first use. Channels
// start with one empty paragraph so links have an anchor block.
func EnsureChannel(s *store.Store, name string) (*store.Doc, error) {
	slug := Normalize(name)
	if d, err := s.Load(slug); err == nil {
		if d.Type != store.TypeChannel {
			return nil, fmt.Errorf("%q is a %s, not a channel", slug, d.Type)
		}
		return d, nil
	}
	d := store.NewDoc(store.TypeChannel, slug, "#"+slug)
	d.AddParagraph("Channel " + slug)
	return d, nil
}

// Channels lists channel docs, most recently active first.
func Channels(s *store.Store) ([]*store.Doc, error) {
	docs, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []*store.Doc
	for _, d := range docs {
		if d.Type == store.TypeChannel {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out, nil
}

// Message is one chat message extracted from a channel doc.
type Message struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	At     string `json:"at"`
	Text   string `json:"text"`
}

// Messages reads the message blocks of a channel, oldest first (sorted by
// timestamp; block order is per-node append order and interleaves after sync).
func Messages(d *store.Doc) []Message {
	var msgs []Message
	for i := range d.Blocks {
		b := &d.Blocks[i]
		if b.Meta["kind"] != "message" {
			continue
		}
		author, _ := b.Meta["author"].(string)
		at, _ := b.Meta["ts"].(string)
		if author == "" || at == "" {
			continue // not a well-formed message
		}
		msgs = append(msgs, Message{ID: b.ID, Author: author, At: at, Text: b.Content})
	}
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].At < msgs[j].At })
	return msgs
}

// LastActivity returns the newest message timestamp in a channel ("" if none).
func LastActivity(d *store.Doc) string {
	msgs := Messages(d)
	if len(msgs) == 0 {
		return ""
	}
	return msgs[len(msgs)-1].At
}

// Send appends an attributed message to a channel and commits it. The commit
// message doubles as the audit entry, so chat history lands in git like every
// other change (§7.1).
func Send(s *store.Store, repo *leetSync.Repo, actor, channel, text string) (Message, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Message{}, "", fmt.Errorf("empty message")
	}
	d, err := EnsureChannel(s, channel)
	if err != nil {
		return Message{}, "", err
	}
	at := time.Now().UTC().Format(time.RFC3339)
	blk := d.AddBlock(store.Block{
		Type:    store.BlockParagraph,
		Content: text,
		Meta:    map[string]any{"kind": "message", "author": actor, "ts": at},
	})
	msg := Message{ID: blk.ID, Author: actor, At: at, Text: text}
	if err := s.Save(d, actor); err != nil {
		return msg, "", err
	}
	sha, err := repo.CommitAll(actor, fmt.Sprintf("chat: #%s", d.Slug))
	if err != nil && err != leetSync.ErrNoChanges {
		return msg, "", err
	}
	return msg, sha.String(), nil
}

// RecentActors returns who spoke (docs or chat) within the window — the
// presence signal for humans and agents alike. Online-ness of other nodes
// comes from mDNS; this covers "recently active" for everyone else.
func RecentActors(repo *leetSync.Repo, since time.Duration) []string {
	entries, err := repo.AuditLog("", time.Now().UTC().Add(-since), "", 200)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for i := len(entries) - 1; i >= 0; i-- { // newest first
		a := entries[i].Actor
		if a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}
