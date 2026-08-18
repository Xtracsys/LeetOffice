package mcp

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"leetoffice/internal/chat"
	"leetoffice/internal/config"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

// --- search (§5) -----------------------------------------------------------

func (s *Server) toolSearch(args map[string]any) (any, error) {
	query := argStr(args, "query")
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	var tags []string
	if raw, ok := args["tags"].([]any); ok {
		for _, t := range raw {
			if str, ok := t.(string); ok {
				tags = append(tags, str)
			}
		}
	}
	limit := argInt(args, "limit", 10)
	if s.search == nil {
		return nil, fmt.Errorf("no search backend configured")
	}
	return s.search(query, argStr(args, "type"), tags, limit)
}

// --- read_doc (§5) ---------------------------------------------------------

func (s *Server) toolReadDoc(args map[string]any) (any, error) {
	d, err := s.store.Resolve(argStr(args, "id_or_slug"))
	if err != nil {
		return nil, err
	}
	blockID := argStr(args, "block_id")
	return map[string]any{
		"doc":  d,
		"text": renderText(d, blockID),
	}, nil
}

// --- write_doc (§5) --------------------------------------------------------

func (s *Server) toolWriteDoc(args map[string]any) (any, error) {
	key := argStr(args, "id_or_slug")
	content := argStr(args, "content")
	blockID := argStr(args, "block_id")
	replace := argBool(args, "replace")

	d, err := s.store.Resolve(key)
	if err != nil {
		// unknown doc → create it
		d = store.NewDoc(store.TypeDoc, slugify(key), titleify(key))
	}
	switch {
	case blockID == "":
		d.AddParagraph(content)
	case replace:
		blk := d.Block(blockID)
		if blk == nil {
			return nil, fmt.Errorf("block %q not found", blockID)
		}
		blk.Content = content
	default:
		blk := d.Block(blockID)
		if blk == nil {
			return nil, fmt.Errorf("block %q not found", blockID)
		}
		d.AddBlock(store.Block{Type: blk.Type, Content: content})
	}
	sha, err := s.commit(d, "write_doc: "+d.Slug)
	if err != nil {
		return nil, err
	}
	return map[string]any{"version": d.Version, "commit_sha": sha}, nil
}

// --- create_task (§5) ------------------------------------------------------

var slugUnwanted = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugUnwanted.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "untitled"
	}
	return s
}

func titleify(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Untitled"
	}
	return s
}

func (s *Server) toolCreateTask(args map[string]any) (any, error) {
	title := argStr(args, "title")
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	slug := slugify(title)
	if _, err := s.store.Load(slug); err == nil {
		slug = slug + "-" + store.NewID()[:6] // keep slugs unique
	}
	d := store.NewDoc(store.TypeTask, slug, title)
	meta := map[string]any{"done": false}
	if a := argStr(args, "assignee"); a != "" {
		meta["assignee"] = a
	}
	blk := d.AddBlock(store.Block{Type: store.BlockTaskItem, Content: title, Meta: meta})
	if body := argStr(args, "body"); body != "" {
		d.AddParagraph(body)
	}

	// optional links to existing docs
	if raw, ok := args["links"].([]any); ok {
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			target, err := s.store.Resolve(argStr(m, "doc"))
			if err != nil {
				return nil, fmt.Errorf("link target: %w", err)
			}
			tblock := argStr(m, "block")
			if tblock == "" && len(target.Blocks) > 0 {
				tblock = target.Blocks[0].ID
			}
			if err := store.AddLink(d, blk.ID, target, tblock, argStr(m, "label")); err != nil {
				return nil, err
			}
			if err := s.store.Save(target, s.actor); err != nil {
				return nil, err
			}
		}
	}

	sha, err := s.commit(d, "create_task: "+title)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"task_id":    d.ID,
		"url":        "doc://tasks/" + d.Slug,
		"commit_sha": sha,
	}, nil
}

// --- link (§5) -------------------------------------------------------------

func (s *Server) toolLink(args map[string]any) (any, error) {
	src, err := s.store.Resolve(argStr(args, "from_doc"))
	if err != nil {
		return nil, fmt.Errorf("from_doc: %w", err)
	}
	dst, err := s.store.Resolve(argStr(args, "to_doc"))
	if err != nil {
		return nil, fmt.Errorf("to_doc: %w", err)
	}
	srcBlock := argStr(args, "from_block")
	if srcBlock == "" && len(src.Blocks) > 0 {
		srcBlock = src.Blocks[0].ID
	}
	dstBlock := argStr(args, "to_block")
	if dstBlock == "" && len(dst.Blocks) > 0 {
		dstBlock = dst.Blocks[0].ID
	}
	if err := store.AddLink(src, srcBlock, dst, dstBlock, argStr(args, "label")); err != nil {
		return nil, err
	}
	if err := s.store.Save(dst, s.actor); err != nil {
		return nil, err
	}
	if _, err := s.commit(src, fmt.Sprintf("link: %s→%s", src.Slug, dst.Slug)); err != nil {
		return nil, err
	}
	// AddLink generates the edge id; the newest link on the source block is it.
	edgeID := ""
	if blk := src.Block(srcBlock); blk != nil && len(blk.Links) > 0 {
		edgeID = blk.Links[len(blk.Links)-1].ID
	}
	return map[string]any{"edge_id": edgeID}, nil
}

// --- audit_query (§5) ------------------------------------------------------

func (s *Server) toolAuditQuery(args map[string]any) (any, error) {
	path := ""
	if key := argStr(args, "doc_id"); key != "" {
		d, err := s.store.Resolve(key)
		if err != nil {
			return nil, err
		}
		path = docPath(d)
	}
	since := time.Time{}
	if raw := argStr(args, "since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, fmt.Errorf("since must be RFC3339: %w", err)
		}
		since = t
	}
	entries, err := s.repo.AuditLog(path, since, argStr(args, "actor"), argInt(args, "limit", 50))
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		change := e.Msg
		if len(e.Files) > 0 {
			change = fmt.Sprintf("%s (%s)", strings.TrimSpace(e.Msg), strings.Join(e.Files, ", "))
		}
		out = append(out, map[string]any{
			"change": change, "actor": e.Actor,
			"when": e.When.UTC().Format(time.RFC3339), "commit_sha": e.Commit,
		})
	}
	return out, nil
}

// --- diff (§5) -------------------------------------------------------------

func (s *Server) toolDiff(args map[string]any) (any, error) {
	d, err := s.store.Resolve(argStr(args, "id_or_slug"))
	if err != nil {
		return nil, err
	}
	path := docPath(d)
	fromV, fromSet := argIntOK(args, "from_version")
	toV, toSet := argIntOK(args, "to_version")

	from, err := s.diffSide(d, path, fromV, fromSet, true)
	if err != nil {
		return nil, err
	}
	to, err := s.diffSide(d, path, toV, toSet, false)
	if err != nil {
		return nil, err
	}
	note := "diff: selected versions"
	if !fromSet && !toSet {
		note = "diff: current version vs HEAD~1"
	}
	if from == nil {
		note = "no prior version in history — diffing against empty document"
		from = &store.Doc{Schema: store.SchemaURL, ID: d.ID, Blocks: []store.Block{}}
	}
	res := leetSync.DiffDocs(from, to)
	return map[string]any{
		"unified_diff":   res.Unified,
		"blocks_added":   res.BlocksAdded,
		"blocks_removed": res.BlocksRemoved,
		"note":           note,
		"from_version":   from.Version,
		"to_version":     to.Version,
	}, nil
}

// diffSide loads one end of a diff. Omitted from_version (or 0) is HEAD~1;
// omitted to_version is the current store doc. A set non-zero version is
// looked up in git history by doc.Version.
func (s *Server) diffSide(current *store.Doc, path string, ver int, set, isFrom bool) (*store.Doc, error) {
	if !set && !isFrom {
		return current, nil
	}
	if !set || ver == 0 {
		raw, err := s.repo.FileAtCommit(path, 1)
		if err != nil {
			if isFrom {
				return nil, nil // empty base
			}
			return nil, fmt.Errorf("no parent revision to diff")
		}
		got, err := store.ExtractDoc(raw)
		if err != nil || got.ID != current.ID {
			if isFrom {
				return nil, nil
			}
			return nil, fmt.Errorf("parent revision is not this document")
		}
		return got, nil
	}
	if ver == current.Version {
		return current, nil
	}
	got, err := s.repo.DocAtVersion(path, ver)
	if err != nil {
		return nil, err
	}
	if got.ID != current.ID {
		return nil, fmt.Errorf("version %d at %s is a different document", ver, path)
	}
	return got, nil
}

// --- chat (§5 surface extension: agents are teammates, not just editors) ----

func (s *Server) toolListChannels(args map[string]any) (any, error) {
	chans, err := chat.Channels(s.store)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(chans))
	for _, d := range chans {
		msgs := chat.Messages(d)
		out = append(out, map[string]any{
			"channel": d.Slug, "messages": len(msgs),
			"last_activity": chat.LastActivity(d),
		})
	}
	return out, nil
}

func (s *Server) toolSendMessage(args map[string]any) (any, error) {
	channel := argStr(args, "channel")
	text := argStr(args, "text")
	if channel == "" || text == "" {
		return nil, fmt.Errorf("channel and text are required")
	}
	msg, sha, err := chat.Send(s.store, s.repo, s.actor, channel, text)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"message_id": msg.ID, "channel": chat.Normalize(channel),
		"at": msg.At, "commit_sha": sha,
		"mentioned": chat.MentionedAgents(text),
	}, nil
}

// sessionActor is the MCP --actor. A request may name the same actor
// (or a bare id); it may not read another agent's inbox.
func (s *Server) sessionActor(args map[string]any) (string, error) {
	want := config.CanonicalActor(s.actor)
	got := strings.TrimSpace(argStr(args, "actor"))
	if got == "" {
		return want, nil
	}
	got = config.CanonicalActor(got)
	if !config.SameActor(got, want) {
		return "", fmt.Errorf("actor %q does not match this MCP session (%s)", got, want)
	}
	return want, nil
}

func (s *Server) persistCfg() error {
	if s.cfg == nil {
		return fmt.Errorf("subscribe/inbox need a node config")
	}
	if s.cfgPath == "" {
		return nil
	}
	return s.cfg.Save(s.cfgPath)
}

func argStrings(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (s *Server) toolSubscribe(args map[string]any) (any, error) {
	actor, err := s.sessionActor(args)
	if err != nil {
		return nil, err
	}
	if s.cfg == nil {
		return nil, fmt.Errorf("subscribe needs a node config")
	}
	sub := s.cfg.Subscribe(actor, argStrings(args, "channels"))
	if err := s.persistCfg(); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "channels": sub.Channels}, nil
}

func (s *Server) toolInbox(args map[string]any) (any, error) {
	actor, err := s.sessionActor(args)
	if err != nil {
		return nil, err
	}
	if s.cfg == nil {
		return nil, fmt.Errorf("inbox needs a node config")
	}
	sub, ok := s.cfg.Subscription(actor)
	if !ok {
		return nil, fmt.Errorf("subscribe first")
	}
	items, err := chat.CollectInbox(s.store, sub, argStr(args, "channel"), argStr(args, "since_ts"), argInt(args, "limit", 50))
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "count": len(items)}, nil
}

func (s *Server) toolMarkRead(args map[string]any) (any, error) {
	actor, err := s.sessionActor(args)
	if err != nil {
		return nil, err
	}
	if s.cfg == nil {
		return nil, fmt.Errorf("mark_read needs a node config")
	}
	ch := argStr(args, "channel")
	ts := argStr(args, "ts")
	if ch == "" || ts == "" {
		return nil, fmt.Errorf("channel and ts are required")
	}
	if !s.cfg.MarkRead(actor, chat.Normalize(ch), ts) {
		return nil, fmt.Errorf("subscribe first")
	}
	if err := s.persistCfg(); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "channel": chat.Normalize(ch), "ts": ts}, nil
}
