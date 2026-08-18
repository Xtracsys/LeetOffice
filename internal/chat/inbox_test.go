package chat

import (
	"testing"

	"leetoffice/internal/config"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

func TestMentionedAgents(t *testing.T) {
	got := MentionedAgents("hey @agent:hermes and @agent:HERMES plus @agent:codex — not @human:josh")
	if len(got) != 2 || got[0] != "agent:hermes" || got[1] != "agent:codex" {
		t.Fatalf("got %#v", got)
	}
	if Mentions("no one", "agent:hermes") {
		t.Fatal("unaddressed")
	}
	if !Mentions("ping @agent:Hermes", "agent:hermes") {
		t.Fatal("case fold")
	}
	if Mentions("ping @agent:hermes", "human:josh") {
		t.Fatal("humans are never auto-routed")
	}
}

func TestCollectInboxMentionOnly(t *testing.T) {
	dir := t.TempDir()
	s, err := store.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := leetSync.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Send(s, repo, "human:josh", "general", "standup notes"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Send(s, repo, "human:josh", "general", "need @agent:hermes on this"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Send(s, repo, "human:josh", "ops", "@agent:codex only"); err != nil {
		t.Fatal(err)
	}
	sub := config.AgentSubscription{Actor: "agent:hermes"} // all channels
	items, err := CollectInbox(s, sub, "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Channel != "general" || !items[0].Mentioned {
		t.Fatalf("inbox %#v", items)
	}
	if items[0].Author != "human:josh" || items[0].Kind != "message" {
		t.Fatalf("item %#v", items[0])
	}

	// since_ts skips the mention if it is not newer
	cut := items[0].TS
	later, err := CollectInbox(s, sub, "", cut, 50)
	if err != nil || len(later) != 0 {
		t.Fatalf("since cursor: %#v %v", later, err)
	}
}
