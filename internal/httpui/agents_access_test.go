package httpui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leetoffice/internal/agentaccess"
	"leetoffice/internal/chat"
)

func TestAgentAccessUIShowsCredentialOnlyOnIssue(t *testing.T) {
	ui, _, _ := newUI(t)
	identityDir := filepath.Join(t.TempDir(), "identity")
	access, err := agentaccess.Open(identityDir)
	if err != nil {
		t.Fatal(err)
	}
	ui.Access = access
	h := httptest.NewServer(ui.Handler())
	defer h.Close()

	page := get(t, h, "/settings/agents")
	if !strings.Contains(page, "Immutable display name") || !strings.Contains(page, "write_doc") ||
		!strings.Contains(page, "loopback-only local UI") {
		t.Fatalf("agent access page incomplete:\n%.700s", page)
	}

	form := url.Values{
		"display_name": {"Build helper"}, "scope": {"read_doc", "search"},
	}
	request, err := http.NewRequest(http.MethodPost, h.URL+"/settings/agents/create", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", h.URL)
	res, err := h.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	createdBody, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("create: status=%d err=%v", res.StatusCode, err)
	}
	token := extractOneTimeToken(t, string(createdBody))
	if !strings.Contains(string(createdBody), "cannot be recovered") {
		t.Fatal("one-time credential warning missing")
	}
	agents, _ := access.List()
	if len(agents) != 1 || agents[0].DisplayName != "Build helper" || agents[0].ID == "" {
		t.Fatalf("identity not visible after creation: %+v", agents)
	}
	actor, _, scopes, ok := access.Authenticate(token)
	if !ok || actor != "agent:"+agents[0].ID || len(scopes) != 2 {
		t.Fatalf("created UI credential not usable: %q %v %v", actor, scopes, ok)
	}
	raw, err := os.ReadFile(filepath.Join(identityDir, "agents.json"))
	if err != nil || strings.Contains(string(raw), token) {
		t.Fatalf("plaintext token persisted: %v", err)
	}

	freshPage := get(t, h, "/settings/agents")
	if strings.Contains(freshPage, token) {
		t.Fatal("stored credential was displayed again on a fresh page")
	}

	crossSiteForm := url.Values{"display_name": {"Cross-site"}, "scope": {"read_doc"}}
	crossSiteRequest, err := http.NewRequest(http.MethodPost, h.URL+"/settings/agents/create", strings.NewReader(crossSiteForm.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	crossSiteRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	crossSiteRequest.Header.Set("Origin", "https://attacker.invalid")
	blocked, err := h.Client().Do(crossSiteRequest)
	if err != nil {
		t.Fatal(err)
	}
	blocked.Body.Close()
	if blocked.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin admin action status = %d", blocked.StatusCode)
	}
}

func TestSameLocalOrigin(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		fetch  string
		want   bool
	}{
		{
			name: "localhost loopback origin",
			host: "localhost:7667", origin: "http://localhost:7667", fetch: "same-origin", want: true,
		},
		{
			name: "IPv4 loopback origin",
			host: "127.0.0.1:7667", origin: "http://127.0.0.1:7667", fetch: "same-origin", want: true,
		},
		{
			name: "mismatched origin port",
			host: "localhost:7667", origin: "http://localhost:7668", fetch: "same-origin",
		},
		{
			name: "mismatched origin scheme",
			host: "localhost:7667", origin: "https://localhost:7667", fetch: "same-origin",
		},
		{
			name: "attacker host matching origin",
			host: "attacker.example:7667", origin: "http://attacker.example:7667", fetch: "same-origin",
		},
		{
			name: "missing Origin despite fetch metadata",
			host: "127.0.0.1:7667", fetch: "same-origin",
		},
		{
			name: "missing browser origin metadata",
			host: "127.0.0.1:7667",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://"+tt.host+"/settings/agents/create", nil)
			r.Host = tt.host
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.fetch != "" {
				r.Header.Set("Sec-Fetch-Site", tt.fetch)
			}
			if got := sameLocalOrigin(r); got != tt.want {
				t.Fatalf("sameLocalOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func extractOneTimeToken(t *testing.T, body string) string {
	t.Helper()
	marker := `class="termcard" style="overflow-wrap:anywhere">`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("one-time token display missing: %.500s", body)
	}
	start += len(marker)
	end := strings.Index(body[start:], "</div>")
	if end < 0 {
		t.Fatalf("one-time token display was not closed: %.500s", body)
	}
	return body[start : start+end]
}

func TestHTTPAgentInboxRequiresCredentialAndBindsActor(t *testing.T) {
	ui, _, _ := newUI(t)
	access, err := agentaccess.Open(filepath.Join(t.TempDir(), "identity"))
	if err != nil {
		t.Fatal(err)
	}
	ui.Access = access
	bound, token, err := access.Create("Inbox reader", []string{"inbox"})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := access.Create("Other inbox", []string{"inbox"})
	if err != nil {
		t.Fatal(err)
	}
	ui.Config.Subscribe("agent:"+bound.ID, []string{"ops"})
	ui.Config.Subscribe("agent:"+other.ID, []string{"ops"})
	if _, _, err := chat.Send(ui.Store, ui.Repo, "human:josh", "ops", "message for @agent:"+bound.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := chat.Send(ui.Store, ui.Repo, "human:josh", "ops", "private for @agent:"+other.ID); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(ui.Handler())
	defer h.Close()

	unauth, err := h.Client().Get(h.URL + "/api/agent/inbox?actor=agent:" + other.ID)
	if err != nil {
		t.Fatal(err)
	}
	unauth.Body.Close()
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated inbox status = %d", unauth.StatusCode)
	}

	request, err := http.NewRequest(http.MethodGet, h.URL+"/api/agent/inbox?actor=agent:"+other.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	res, err := h.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("authenticated inbox: status=%d err=%v body=%s", res.StatusCode, err, body)
	}
	if !strings.Contains(string(body), "message for @agent:"+bound.ID) || strings.Contains(string(body), "private for @agent:"+other.ID) {
		t.Fatalf("caller-supplied actor was not ignored: %s", body)
	}
}
