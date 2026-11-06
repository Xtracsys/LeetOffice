package httpui

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"leetoffice/internal/agentaccess"
)

const maxAgentAccessForm = 64 << 10

func (u *UI) handleAuthenticatedAgentInbox(w http.ResponseWriter, r *http.Request) {
	setAgentAccessHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	actor, _, scopes, ok := u.authenticateAgentRequest(r)
	if !ok {
		http.Error(w, "valid agent bearer credential required", http.StatusUnauthorized)
		return
	}
	if !hasScope(scopes, "inbox") {
		http.Error(w, "credential does not grant the inbox tool scope", http.StatusForbidden)
		return
	}
	copyReq := r.Clone(r.Context())
	copyURL := *r.URL
	copyReq.URL = &copyURL
	query := copyReq.URL.Query()
	query.Set("actor", actor)
	copyReq.URL.RawQuery = query.Encode()
	u.handleAgentInbox(w, copyReq)
}

func (u *UI) authenticateAgentRequest(r *http.Request) (actor, displayName string, scopes []string, ok bool) {
	if u.Access == nil {
		return "", "", nil, false
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", "", nil, false
	}
	return u.Access.Authenticate(parts[1])
}

func (u *UI) handleAgentAccess(w http.ResponseWriter, r *http.Request) {
	setAgentAccessHeaders(w)
	if u.Access == nil {
		http.Error(w, "agent access manager unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.URL.Path == "/settings/agents" || r.URL.Path == "/settings/agents/" {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		u.renderAgentAccess(w, "", "")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !sameLocalOrigin(r) {
		http.Error(w, "cross-origin agent access action rejected", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentAccessForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	var (
		message string
		token   string
		err     error
	)
	switch strings.TrimPrefix(r.URL.Path, "/settings/agents/") {
	case "create":
		var agent agentaccess.Agent
		agent, token, err = u.Access.Create(r.FormValue("display_name"), r.Form["scope"])
		if err == nil {
			message = "Created " + agent.DisplayName + " (" + agent.ID + "). Copy the credential now; it cannot be recovered."
		}
	case "rotate":
		var agent agentaccess.Agent
		agent, token, err = u.Access.Rotate(r.FormValue("agent_id"))
		if err == nil {
			message = "Rotated credential for " + agent.DisplayName + ". The previous credential is invalid immediately."
		}
	case "disable":
		var agent agentaccess.Agent
		agent, err = u.Access.Disable(r.FormValue("agent_id"))
		if err == nil {
			message = "Disabled " + agent.DisplayName + "."
		}
	case "enable":
		var agent agentaccess.Agent
		agent, err = u.Access.Enable(r.FormValue("agent_id"))
		if err == nil {
			message = "Enabled " + agent.DisplayName + "."
		}
	case "revoke":
		var agent agentaccess.Agent
		agent, err = u.Access.Revoke(r.FormValue("agent_id"))
		if err == nil {
			message = "Revoked " + agent.DisplayName + ". This identity cannot be re-enabled or rotated."
		}
	case "scopes":
		var agent agentaccess.Agent
		agent, err = u.Access.SetScopes(r.FormValue("agent_id"), r.Form["scope"])
		if err == nil {
			message = "Updated tool grants for " + agent.DisplayName + "."
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		u.renderAgentAccess(w, "", err.Error())
		return
	}
	u.renderAgentAccess(w, token, message)
}

func setAgentAccessHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func sameLocalOrigin(r *http.Request) bool {
	if r == nil || r.Host == "" {
		return false
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host, port, ok := parseLocalAuthority(r.Host, scheme)
	if !ok {
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Opaque != "" ||
		!strings.EqualFold(parsed.Scheme, scheme) || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	originHost, originPort, ok := parseLocalAuthority(parsed.Host, scheme)
	if !ok || originHost != host || originPort != port {
		return false
	}

	// Origin is mandatory; Fetch Metadata can only further restrict the request.
	site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	return site == "" || site == "same-origin"
}

func parseLocalAuthority(authority, scheme string) (host, port string, ok bool) {
	parsed, err := url.Parse("//" + authority)
	if err != nil || parsed.Host != authority || parsed.User != nil || parsed.Hostname() == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}

	host = strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" {
		return "", "", false
	}
	if strings.HasSuffix(authority, ":") {
		return "", "", false
	}

	port = parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", false
	}
	return host, strconv.Itoa(portNumber), true
}

func hasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}

func (u *UI) renderAgentAccess(w http.ResponseWriter, oneTimeToken, message string) {
	setAgentAccessHeaders(w)
	agents, events := u.Access.List()
	var b strings.Builder
	b.WriteString(`<div class="eyebrow"><span class="pulse"></span>FILE / AGENT ACCESS · LOCAL NODE</div><h1>Agent identities</h1>`)
	b.WriteString(`<p class="meta">Credentials and identities are stored only on this node, outside the synchronized git store. The MCP server ignores caller-supplied actor/name values and attributes requests to the credential's immutable ID.</p>`)
	b.WriteString(`<p class="meta">This admin page is available only through the loopback-only local UI. Treat tokens as passwords and keep them in the MCP client's private environment configuration.</p>`)
	if message != "" {
		fmt.Fprintf(&b, `<div class="card surface"><p class="meta">%s</p></div>`, html.EscapeString(message))
	}
	if oneTimeToken != "" {
		fmt.Fprintf(&b, `<div class="card"><h3 style="margin-top:0">One-time credential</h3><p class="meta">Copy this now. It is shown only in this creation/rotation response and only its verifier is stored.</p><div class="termcard" style="overflow-wrap:anywhere">%s</div><p class="meta">Configure this credential as <code>LEETOFFICE_AGENT_TOKEN</code> for stdio, or in a private HTTP Bearer header. Protect the client configuration with local OS permissions.</p></div>`, html.EscapeString(oneTimeToken))
	}

	b.WriteString(`<div class="card"><h3 style="margin-top:0">Create identity</h3><form method="post" action="/settings/agents/create">`)
	b.WriteString(`<label class="field"><span>Immutable display name</span><input type="text" name="display_name" maxlength="80" required></label><p class="meta">Choose only the MCP tools this agent needs; no tools are granted by default.</p><div class="row">`)
	for _, tool := range agentaccess.AvailableScopes() {
		fmt.Fprintf(&b, `<label class="field"><input type="checkbox" name="scope" value="%s"> <code>%s</code></label>`, html.EscapeString(tool), html.EscapeString(tool))
	}
	b.WriteString(`</div><button type="submit">create agent credential</button></form></div>`)

	b.WriteString(`<h2>Identities</h2>`)
	if len(agents) == 0 {
		b.WriteString(`<p class="meta">No agent identities have been created.</p>`)
	}
	for _, agent := range agents {
		status := "enabled"
		if agent.Revoked {
			status = "revoked"
		} else if agent.Disabled {
			status = "disabled"
		}
		fmt.Fprintf(&b, `<div class="card surface"><h3 style="margin-top:0">%s</h3><p class="meta"><span class="mono">%s</span> · %s · created %s</p><p class="meta">Granted tools: %s</p>`,
			html.EscapeString(agent.DisplayName), html.EscapeString(agent.ID), html.EscapeString(status), html.EscapeString(agent.CreatedAt), html.EscapeString(strings.Join(agent.Scopes, ", ")))
		if !agent.Revoked {
			fmt.Fprintf(&b, `<form method="post" action="/settings/agents/scopes"><input type="hidden" name="agent_id" value="%s"><p class="meta">Tool grants</p><div class="row">`, html.EscapeString(agent.ID))
			granted := make(map[string]bool, len(agent.Scopes))
			for _, scope := range agent.Scopes {
				granted[scope] = true
			}
			for _, tool := range agentaccess.AvailableScopes() {
				checked := ""
				if granted[tool] {
					checked = " checked"
				}
				fmt.Fprintf(&b, `<label class="field"><input type="checkbox" name="scope" value="%s"%s> <code>%s</code></label>`, html.EscapeString(tool), checked, html.EscapeString(tool))
			}
			b.WriteString(`</div><button class="ghost" type="submit">save tool grants</button></form><div class="row" style="margin-top:12px">`)
			fmt.Fprintf(&b, `<form method="post" action="/settings/agents/rotate"><input type="hidden" name="agent_id" value="%s"><button class="ghost" type="submit">rotate credential</button></form>`, html.EscapeString(agent.ID))
			if agent.Disabled {
				fmt.Fprintf(&b, `<form method="post" action="/settings/agents/enable"><input type="hidden" name="agent_id" value="%s"><button class="ghost" type="submit">enable</button></form>`, html.EscapeString(agent.ID))
			} else {
				fmt.Fprintf(&b, `<form method="post" action="/settings/agents/disable"><input type="hidden" name="agent_id" value="%s"><button class="ghost" type="submit">disable</button></form>`, html.EscapeString(agent.ID))
			}
			fmt.Fprintf(&b, `<form method="post" action="/settings/agents/revoke"><input type="hidden" name="agent_id" value="%s"><button type="submit">revoke permanently</button></form></div>`, html.EscapeString(agent.ID))
		}
		b.WriteString(`</div>`)
	}

	b.WriteString(`<h2>Administrative history</h2>`)
	if len(events) == 0 {
		b.WriteString(`<p class="meta">No agent access actions yet.</p>`)
	} else {
		b.WriteString(`<table><tr><th>when</th><th>identity</th><th>action</th><th>operator</th></tr>`)
		for _, event := range events {
			fmt.Fprintf(&b, `<tr><td class="mono">%s</td><td class="mono">%s</td><td>%s</td><td class="mono">%s</td></tr>`,
				html.EscapeString(event.At), html.EscapeString(event.AgentID), html.EscapeString(event.Action), html.EscapeString(event.Actor))
		}
		b.WriteString(`</table>`)
	}
	b.WriteString(`<p class="meta"><a class="btn ghost" href="/agents">connection instructions</a> <a class="btn ghost" href="/settings">settings</a></p>`)
	_, _ = w.Write([]byte(xbPageActor("Agent identities", "settings", b.String(), u.Config.Actor)))
}
