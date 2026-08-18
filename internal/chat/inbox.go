package chat

import (
	"regexp"
	"sort"
	"strings"

	"leetoffice/internal/config"
	"leetoffice/internal/store"
)

// mentionRe matches @agent:<id> (id is letters, digits, dot, underscore, dash).
var mentionRe = regexp.MustCompile(`(?i)@agent:([a-z0-9._-]+)`)

// MentionedAgents returns canonical agent:<id> names addressed in text.
// Humans are never auto-routed.
func MentionedAgents(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		id := strings.ToLower(m[1])
		name := "agent:" + id
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// Mentions reports whether text addresses actor (@agent:<id>, case-insensitive).
func Mentions(text, actor string) bool {
	want := config.ActorKey(actor)
	if !strings.HasPrefix(want, "agent:") {
		return false
	}
	for _, name := range MentionedAgents(text) {
		if config.ActorKey(name) == want {
			return true
		}
	}
	return false
}

// InboxItem is one mention-routed message for a subscribed agent.
type InboxItem struct {
	Channel   string `json:"channel"`
	ID        string `json:"id"`
	Content   string `json:"content"`
	Author    string `json:"author"`
	Kind      string `json:"kind"`
	TS        string `json:"ts"`
	Mentioned bool   `json:"mentioned"`
}

// CollectInbox is a view over channel message blocks: subscribed channels,
// newer than since/cursor, only messages that @-mention actor.
func CollectInbox(s *store.Store, sub config.AgentSubscription, channel, since string, limit int) ([]InboxItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	channel = Normalize(channel)
	chans, err := Channels(s)
	if err != nil {
		return nil, err
	}
	var items []InboxItem
	for _, d := range chans {
		if channel != "" && d.Slug != channel {
			continue
		}
		if len(sub.Channels) > 0 && !watches(sub, d.Slug) {
			continue
		}
		cut := since
		if cut == "" && sub.Cursor != nil {
			cut = sub.Cursor[d.Slug]
		}
		for _, msg := range Messages(d) {
			if cut != "" && msg.At <= cut {
				continue
			}
			if !Mentions(msg.Text, sub.Actor) {
				continue
			}
			items = append(items, InboxItem{
				Channel:   d.Slug,
				ID:        msg.ID,
				Content:   msg.Text,
				Author:    msg.Author,
				Kind:      "message",
				TS:        msg.At,
				Mentioned: true,
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].TS == items[j].TS {
			return items[i].Channel < items[j].Channel
		}
		return items[i].TS < items[j].TS
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func watches(sub config.AgentSubscription, slug string) bool {
	if len(sub.Channels) == 0 {
		return true
	}
	for _, ch := range sub.Channels {
		if Normalize(ch) == slug {
			return true
		}
	}
	return false
}
