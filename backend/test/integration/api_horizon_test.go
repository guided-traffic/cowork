//go:build integration

package integration

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fakeissuer"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// into files a ticket into a horizon.
func into(horizon apigen.Horizon) func(*apigen.TicketCreate) {
	return func(b *apigen.TicketCreate) { b.Horizon = &horizon }
}

// docs/adr/0010 D3, docs/adr/0014 D2 as amended 2026-10-04: a filing names its
// horizon and its place in it. Without a horizon the ticket is later, without
// a place at the bottom — the end of its horizon; another horizon is the
// override, set by the filer without a reason; the filing act records both.
func TestFilingIntoAHorizonAtAPlace(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}

	first := e.file(t, member, "ALPHA", task("first next", into(apigen.HorizonNext)))
	assert.Equal(t, apigen.HorizonNext, first.Horizon)
	set := first.HorizonSet.MustGet()
	assert.Equal(t, apigen.HorizonNext, set.Value)
	assert.True(t, set.Reason.IsNull(), "a filing gives no reason")
	assert.Equal(t, e.MemberA, set.By.MustGet().Id, "set by the filer")

	plain := e.file(t, member, "ALPHA", task("plain"))
	assert.Equal(t, apigen.HorizonLater, plain.Horizon, "no horizon is later")
	assert.True(t, plain.HorizonSet.IsNull())
	explicit := e.file(t, member, "ALPHA", task("explicit later", into(apigen.HorizonLater)))
	assert.True(t, explicit.HorizonSet.IsNull(), "later named is no override")

	e.file(t, member, "ALPHA", task("before first", into(apigen.HorizonNext), func(b *apigen.TicketCreate) { b.Before = &first.Number }))
	after := e.file(t, member, "ALPHA", task("after first", into(apigen.HorizonNext), func(b *apigen.TicketCreate) { b.After = &first.Number }))
	e.file(t, member, "ALPHA", task("end of next", into(apigen.HorizonNext)))
	assert.Equal(t, []string{"before first", "first next", "after first", "end of next"},
		e.titles(t, member, e.projectTickets("ALPHA"), "horizon=next"), "the places in the horizon, the end without one")
	assert.Equal(t, []string{"before first", "first next", "after first", "plain", "explicit later", "end of next"},
		e.titles(t, member, e.projectTickets("ALPHA"), ""))

	var horizon, neighbour string
	require.NoError(t, f.QueryRow(e.ctx, "SELECT after->>'urgency', after->>'after' FROM audit_events WHERE ticket_id = $1 AND action = 'created'",
		after.Id).Scan(&horizon, &neighbour))
	assert.Equal(t, "next", horizon)
	assert.Equal(t, first.Key, neighbour)

	dropped := e.file(t, member, "ALPHA", task("dropped"))
	require.Equal(t, http.StatusOK, e.move(t, member, dropped, apigen.Transition{From: apigen.TicketStateFiled,
		To: apigen.TicketStateDropped, Reason: ptr("not needed")}).StatusCode())
	before := len(e.titles(t, member, e.projectTickets("ALPHA"), "include_terminal=true"))

	res := e.create(t, member, "ALPHA", task("x", into(apigen.HorizonNext), func(b *apigen.TicketCreate) { b.After = &plain.Number }))
	body := problemIn(t, http.StatusBadRequest, res.StatusCode(), res.Body, "validation_failed")
	assert.Equal(t, "/after", pointerOf(body))
	assert.Contains(t, string(res.Body), plain.Key+" stands in the horizon later, not in next")

	res = e.create(t, member, "ALPHA", task("x", func(b *apigen.TicketCreate) { b.After, b.Before = &plain.Number, &explicit.Number }))
	body = problemIn(t, http.StatusBadRequest, res.StatusCode(), res.Body, "validation_failed")
	assert.Equal(t, "/before", pointerOf(body))

	res = e.create(t, member, "ALPHA", task("x", func(b *apigen.TicketCreate) { b.Before = &dropped.Number }))
	problemIn(t, http.StatusConflict, res.StatusCode(), res.Body, "state_conflict")

	res = e.create(t, member, "ALPHA", task("x", func(b *apigen.TicketCreate) { b.After = ptr(99999) }))
	body = problemIn(t, http.StatusBadRequest, res.StatusCode(), res.Body, "validation_failed")
	assert.Equal(t, "/after", pointerOf(body))

	assert.Len(t, e.titles(t, member, e.projectTickets("ALPHA"), "include_terminal=true"), before, "a refused filing files nothing")
}

// docs/adr/0043 D4 as amended 2026-10-04 and 2026-10-05: an agent files into a
// horizon other than later with set-horizon, and names a place with rank.
func TestAnAgentFilesIntoAHorizonWithItsCapabilities(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	baselineToken, _, err := fixtures(t).Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true, Capabilities: []string{}})
	require.NoError(t, err)
	baseline := caller{Token: baselineToken, Agent: "claude-code/opus/s1"}
	assisted := caller{Token: e.tk.AssistedAgentA, Agent: "claude-code/opus/s1"}
	full := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	anchor := e.file(t, member, "ALPHA", task("anchor", into(apigen.HorizonNext)))

	res := e.create(t, baseline, "ALPHA", task("x", into(apigen.HorizonNow)))
	body := problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Equal(t, "missing capability: set-horizon", body["detail"])
	assert.Equal(t, apigen.HorizonLater, e.file(t, baseline, "ALPHA", task("later by the baseline", into(apigen.HorizonLater))).Horizon,
		"later needs nothing beyond the baseline")

	assert.Equal(t, apigen.HorizonNext, e.file(t, assisted, "ALPHA", task("next by the assisted", into(apigen.HorizonNext))).Horizon)
	res = e.create(t, assisted, "ALPHA", task("x", into(apigen.HorizonNext), func(b *apigen.TicketCreate) { b.After = &anchor.Number }))
	body = problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Contains(t, body["detail"], "rank")

	placed := e.file(t, full, "ALPHA", task("next by the agent", into(apigen.HorizonNext), func(b *apigen.TicketCreate) { b.Before = &anchor.Number }))
	assert.Equal(t, e.MemberA, placed.HorizonSet.MustGet().By.MustGet().Id, "an agent sets the horizon as its person")
	assert.Equal(t, []string{"next by the agent", "anchor", "next by the assisted"},
		e.titles(t, member, e.projectTickets("ALPHA"), "horizon=next"))
}

// errorAt is the current value a 412 names under a pointer, and whether it
// names the pointer at all.
func errorAt(body map[string]any, pointer string) (any, bool) {
	errs, _ := body["errors"].([]any)
	for _, e := range errs {
		if m, ok := e.(map[string]any); ok && m["pointer"] == pointer {
			return m["current"], true
		}
	}
	return nil, false
}

// docs/adr/0010 D3 as amended 2026-10-05: PUT …/horizon sets the ticket's
// horizon over If-Match; later clears the horizon set, and on a ticket with
// none set it changes nothing. A reason is optional for a person, kept with a
// horizon set and recorded on the act either way, and required of an agent —
// for later too —, which needs set-horizon; a token stored with that
// capability's name before holds it. The act keeps its name, overridden, and
// its payload the columns' names (docs/adr/0010 D1). The export names the
// horizon by its word.
func TestSettingTheHorizon(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Placed by hand"))
	set := func(c caller, number, version int, value apigen.Horizon, reason *string) *apigen.SetHorizonResponse {
		t.Helper()
		etag := strconv.Quote(strconv.Itoa(version))
		res, err := e.s.client(t, c).SetHorizonWithResponse(e.ctx, e.SlugA, "ALPHA", number,
			&apigen.SetHorizonParams{IfMatch: &etag}, apigen.HorizonUpdate{Value: value, Reason: reason})
		require.NoError(t, err)
		return res
	}
	acts := func() int64 {
		t.Helper()
		n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'overridden'", tk.Id)
		require.NoError(t, err)
		return n
	}

	res := set(member, tk.Number, tk.Version, apigen.HorizonNext, nil)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	tk = *res.JSON200
	assert.Equal(t, apigen.HorizonNext, tk.Horizon)
	s := tk.HorizonSet.MustGet()
	assert.Equal(t, apigen.HorizonNext, s.Value)
	assert.True(t, s.Reason.IsNull(), "a person's drag needs no reason")
	assert.Equal(t, e.MemberA, s.By.MustGet().Id)
	assert.Equal(t, strconv.Quote(strconv.Itoa(tk.Version)), res.HTTPResponse.Header.Get("ETag"))

	res = set(member, tk.Number, tk.Version, apigen.HorizonNow, ptr("a customer is down"))
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	tk = *res.JSON200
	assert.Equal(t, "a customer is down", tk.HorizonSet.MustGet().Reason.MustGet())

	res = set(member, tk.Number, tk.Version, apigen.HorizonLater, ptr("not this month"))
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	tk = *res.JSON200
	assert.Equal(t, apigen.HorizonLater, tk.Horizon)
	assert.True(t, tk.HorizonSet.IsNull(), "later clears the horizon set")
	var after *string
	var reason string
	require.NoError(t, f.QueryRow(e.ctx, `SELECT after->>'urgency_override', reason FROM audit_events
		WHERE ticket_id = $1 AND action = 'overridden' ORDER BY id DESC LIMIT 1`, tk.Id).Scan(&after, &reason))
	assert.Nil(t, after, "the act's payload keeps the columns' name")
	assert.Equal(t, "not this month", reason, "the act records the reason the ticket does not keep")
	assert.EqualValues(t, 3, acts())

	version := tk.Version
	res = set(member, tk.Number, tk.Version, apigen.HorizonLater, nil)
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, version, res.JSON200.Version, "later on a ticket with none set changes nothing")
	assert.EqualValues(t, 3, acts())

	narrow, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true, Capabilities: []string{"interest"}})
	require.NoError(t, err)
	res = set(caller{Token: narrow, Agent: "claude-code/opus/s1"}, tk.Number, tk.Version, apigen.HorizonNow, ptr("x"))
	body := problemIn(t, http.StatusForbidden, res.StatusCode(), res.Body, "agent_forbidden")
	assert.Equal(t, "missing capability: set-horizon", body["detail"])
	agent := caller{Token: e.tk.AgentA, Agent: "claude-code/opus/s1"}
	for _, value := range []apigen.Horizon{apigen.HorizonNow, apigen.HorizonLater} {
		res := set(agent, tk.Number, tk.Version, value, nil)
		body := problemIn(t, http.StatusBadRequest, res.StatusCode(), res.Body, "validation_failed")
		assert.Equal(t, "/reason", pointerOf(body), "an agent's %s needs a reason", value)
	}

	only, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.MemberA, Agent: true, Capabilities: []string{"set-horizon"}})
	require.NoError(t, err)
	res = set(caller{Token: only, Agent: "claude-code/opus/s1"}, tk.Number, tk.Version, apigen.HorizonRelease, ptr("gates 2.0"))
	require.Equal(t, http.StatusOK, res.StatusCode(), "set-horizon is the capability: %s", res.Body)
	tk = *res.JSON200
	recorded, err := f.QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE ticket_id = $1 AND action = 'overridden'
		AND agent_capabilities = ARRAY['set-horizon']::text[] AND reason = 'gates 2.0'`, tk.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, recorded, "the act records the agent's set")

	path := ticketPath(e.SlugA, "ALPHA", tk.Number) + "/horizon"
	assertProblem(t, e.s.do(t, member, http.MethodPut, path, map[string]any{"value": "next"}),
		http.StatusPreconditionRequired, "precondition_required")
	res = set(member, tk.Number, tk.Version-1, apigen.HorizonNext, nil)
	body = problemIn(t, http.StatusPreconditionFailed, res.StatusCode(), res.Body, "precondition_failed")
	current, ok := errorAt(body, "/horizon")
	require.True(t, ok, "a 412 names the horizon: %v", body)
	assert.Equal(t, "release", current)
	current, ok = errorAt(body, "/horizon_set")
	require.True(t, ok, "and the horizon set: %v", body)
	assert.Equal(t, "release", current.(map[string]any)["value"])
	assertProblem(t, e.s.do(t, member, http.MethodPut, path, map[string]any{"value": "soon"}, "If-Match", `"1"`),
		http.StatusBadRequest, "validation_failed")

	markdown := e.s.do(t, member, http.MethodGet, ticketPath(e.SlugA, "ALPHA", tk.Number)+"/markdown", nil)
	require.Equal(t, http.StatusOK, markdown.StatusCode)
	raw, err := io.ReadAll(markdown.Body)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "\nhorizon: release\n", "the export names the horizon by its word")
	assert.NotContains(t, string(raw), "urgency")
}

// docs/adr/0010 D3: the horizon set names the person who set it as a ticket
// names its reporter, wherever a ticket is answered — the write that sets it,
// the ticket, the project's list and the person-level lists: a local account
// by its username and display name, a person of the identity provider by the
// display name, the username null. Each sets the horizon of a ticket the
// other filed, so the setter is never the reporter.
func TestTheHorizonSetNamesThePersonWhoSetIt(t *testing.T) {
	w := newWorld(t)
	f := fixtures(t)
	is := fakeissuer.Start(t)
	e := ticketEnv{world: w, tk: issueTokens(t, w), ctx: context.Background(),
		s: newAPI(t, withIdentity(fakeProvider(t, is), []string{"cowork-users"}, ""))}
	require.NoError(t, f.Account(e.ctx, e.MemberA, testPassword, e.A, false))
	yes := true
	grace := providerPerson(t, f, is.URL, uniqueSlug("grace")+"@example.com", &yes, []string{"cowork-users"})
	require.NoError(t, f.Member(e.ctx, e.A, grace, domain.RoleMember))
	graceToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: grace})
	require.NoError(t, err)
	local, provider := caller{Token: e.tk.MemberA}, caller{Token: graceToken}

	named := func(c caller) apigen.Person {
		t.Helper()
		res := e.s.do(t, c, http.MethodGet, "/api/v1/me", nil)
		require.Equal(t, http.StatusOK, res.StatusCode)
		me := decode[apigen.Me](t, res)
		return apigen.Person{Id: me.Id, Username: me.Username, DisplayName: me.DisplayName}
	}
	byLocal, byProvider := named(local), named(provider)
	require.False(t, byLocal.Username.IsNull(), "a local account has a username")
	require.True(t, byProvider.Username.IsNull(), "a person of the identity provider has none")
	require.NotEmpty(t, byProvider.DisplayName)

	set := func(c caller, tk apigen.Ticket, value apigen.Horizon) apigen.Ticket {
		t.Helper()
		etag := strconv.Quote(strconv.Itoa(tk.Version))
		res, err := e.s.client(t, c).SetHorizonWithResponse(e.ctx, e.SlugA, tk.Project, tk.Number,
			&apigen.SetHorizonParams{IfMatch: &etag}, apigen.HorizonUpdate{Value: value})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		return *res.JSON200
	}
	setter := func(tk apigen.Ticket) apigen.Person { return tk.HorizonSet.MustGet().By.MustGet() }

	first := set(local, e.file(t, provider, "ALPHA", task("filed by a person of the identity provider")), apigen.HorizonNext)
	second := set(provider, e.file(t, local, "ALPHA", task("filed by a local account")), apigen.HorizonNow)
	expected := map[string]apigen.Person{first.Key: byLocal, second.Key: byProvider}
	assert.Equal(t, byLocal, setter(first), "the answer of the write")
	assert.Equal(t, byProvider, setter(second), "the answer of the write")

	for _, tk := range []apigen.Ticket{first, second} {
		res := e.get(t, local, tk.Project, tk.Number)
		require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
		assert.Equal(t, expected[tk.Key], setter(*res.JSON200), "the ticket %s", tk.Key)
	}

	res := e.s.do(t, local, http.MethodGet, e.projectTickets("ALPHA"), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	listed := 0
	for _, tk := range decode[apigen.TicketList](t, res).Items {
		if person, ok := expected[tk.Key]; ok {
			assert.Equal(t, person, setter(tk), "the project's list, %s", tk.Key)
			listed++
		}
	}
	assert.Equal(t, len(expected), listed, "the project's list holds both")

	res = e.s.do(t, local, http.MethodGet, "/api/v1/me/next", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	listed = 0
	for _, it := range decode[apigen.MyTicketList](t, res).Items {
		if person, ok := expected[it.Ticket.Key]; ok {
			assert.Equal(t, person, setter(it.Ticket), "next for me, %s", it.Ticket.Key)
			listed++
		}
	}
	assert.Equal(t, len(expected), listed, "next for me holds both, assigned to nobody")
}

// docs/adr/0010 D1 as amended 2026-10-06: a filing names its horizon as
// horizon; urgency, the name it had before, is a field a filing no longer has,
// and nothing is filed with it.
func TestAFilingTakesTheHorizonUnderItsNameOnly(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	filed := e.file(t, member, "ALPHA", task("by its name", into(apigen.HorizonNext)))
	assert.Equal(t, apigen.HorizonNext, filed.Horizon)
	assert.Equal(t, apigen.HorizonNext, filed.HorizonSet.MustGet().Value)

	body := assertProblem(t, e.s.do(t, member, http.MethodPost, e.projectTickets("ALPHA"), map[string]any{"type": "task",
		"title": "by the name before", "severity": "low", "security": "none", "effort": "S", "urgency": "next"}),
		http.StatusBadRequest, "validation_failed")
	assert.Equal(t, "/", pointerOf(body))
	assert.Equal(t, `property "urgency" is unsupported`, body["errors"].([]any)[0].(map[string]any)["message"])
	assert.Equal(t, []string{"by its name"}, e.titles(t, member, e.projectTickets("ALPHA"), ""))
}

// docs/adr/0049 D1, D4: both ticket lists filter by horizon; a value outside
// the vocabulary is refused at query:horizon, and urgency, the name it had
// before (docs/adr/0010 D1 as amended 2026-10-06), as a parameter the lists
// no longer have.
func TestTheHorizonFilter(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	e.file(t, member, "ALPHA", task("now", into(apigen.HorizonNow)))
	e.file(t, member, "ALPHA", task("next", into(apigen.HorizonNext)))
	e.file(t, member, "ALPHA", task("later"))
	for _, path := range []string{e.projectTickets("ALPHA"), e.tenantTickets()} {
		sorted := func(query string) []string {
			got := e.titles(t, member, path, query)
			slices.Sort(got)
			return got
		}
		assert.Equal(t, []string{"now"}, sorted("horizon=now"), path)
		assert.Equal(t, []string{"next", "now"}, sorted("horizon=now&horizon=next"), path)
		assert.Equal(t, []string{"next", "now"}, sorted("horizon=!later"), path)
		for query, pointer := range map[string]string{"horizon=soon": "query:horizon", "urgency=now": "query:urgency"} {
			body := assertProblem(t, e.s.do(t, member, http.MethodGet, path+"?"+query, nil), http.StatusBadRequest, "validation_failed")
			assert.Equal(t, pointer, pointerOf(body), "%s?%s", path, query)
		}
	}
}

// docs/adr/0043 D4 as amended 2026-10-06: the capability is set-horizon and
// nothing else. A set is answered as it is stored — the token, the token
// list, the tenant's tokens, /me/token's request set —, a set that names
// override-urgency is refused with 400, and the chat holds set-horizon for a
// horizon it sets. The checks of the stored sets refuse the old name as well
// (migration 40, TestTheNarrowingMigrationRewritesAgainAndRefusesTheOldName).
func TestTheCapabilityIsSetHorizonOnly(t *testing.T) {
	w := newWorld(t)
	names := withAccounts(t, w)
	s := newAPI(t, withLogin)
	b := s.browser(t)
	b.mustLogin(names["memberA"], testPassword)
	f := fixtures(t)
	ctx := context.Background()

	agent, agentID, err := f.Token(ctx, fixture.TokenSpec{UserID: w.MemberA, Agent: true, Capabilities: []string{"rank", "set-horizon"}})
	require.NoError(t, err)
	mine := decode[apigen.CurrentToken](t, s.do(t, caller{Token: agent, Agent: "claude-code/opus/s1"}, http.MethodGet, "/api/v1/me/token", nil))
	assert.Equal(t, []apigen.Capability{"rank", "set-horizon"}, mine.Capabilities)
	assert.Equal(t, []apigen.Capability{"rank", "set-horizon"}, mine.Request.Capabilities, "no name beside set-horizon")

	listed := decode[apigen.TokenList](t, b.request(http.MethodGet, "/api/v1/me/tokens", nil))
	idx := slices.IndexFunc(listed.Items, func(tok apigen.Token) bool { return tok.Id == agentID })
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, []apigen.Capability{"rank", "set-horizon"}, listed.Items[idx].Capabilities)
	admin, _, err := f.Token(ctx, fixture.TokenSpec{UserID: w.AdminA, Scope: domain.ScopeAdmin})
	require.NoError(t, err)
	tenant := decode[apigen.MemberTokenList](t, s.do(t, caller{Token: admin}, http.MethodGet, "/api/v1/teams/"+w.SlugA+"/tokens", nil))
	idx = slices.IndexFunc(tenant.Items, func(tok apigen.MemberToken) bool { return tok.Id == agentID })
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, []apigen.Capability{"rank", "set-horizon"}, tenant.Items[idx].Capabilities)

	refused := b.request(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "an old form", "scope": "write",
		"agent": true, "capabilities": []string{"override-urgency", "rank"}})
	assertProblem(t, refused, http.StatusBadRequest, "validation_failed")
	refused = b.request(http.MethodPut, "/api/v1/me/chat", map[string]any{"capabilities": []string{"override-urgency", "rank"}})
	assertProblem(t, refused, http.StatusBadRequest, "validation_failed")

	created := b.request(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "a new form", "scope": "write",
		"agent": true, "capabilities": []string{"set-horizon", "rank"}})
	require.Equal(t, http.StatusCreated, created.StatusCode)
	made := decode[apigen.TokenCreated](t, created)
	assert.Equal(t, []apigen.Capability{"set-horizon", "rank"}, made.Capabilities)
	stored, err := f.QueryCount(ctx, `SELECT count(*) FROM tokens WHERE id = $1
		AND capabilities = ARRAY['set-horizon', 'rank']::text[]`, made.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, stored, "a new token stores its set as sent, and nothing beside it")

	chat := b.request(http.MethodPut, "/api/v1/me/chat", map[string]any{"capabilities": []string{"set-horizon", "rank"}})
	require.Equal(t, http.StatusOK, chat.StatusCode)
	assert.Equal(t, []apigen.Capability{"rank", "set-horizon"}, decode[apigen.ChatCapabilities](t, chat).Capabilities)
	stored, err = f.QueryCount(ctx, `SELECT count(*) FROM chat_capabilities WHERE user_id = $1
		AND capabilities = ARRAY['rank', 'set-horizon']::text[]`, w.MemberA)
	require.NoError(t, err)
	assert.EqualValues(t, 1, stored, "the chat stores its set in the catalogue's order, and nothing beside it")

	filed := b.request(http.MethodPost, "/api/v1/teams/"+w.SlugA+"/projects/ALPHA/tickets", task("for the chat"))
	require.Equal(t, http.StatusCreated, filed.StatusCode)
	tk := decode[apigen.Ticket](t, filed)
	marked := b.request(http.MethodPut, ticketPath(w.SlugA, "ALPHA", tk.Number)+"/horizon",
		map[string]any{"value": "next", "reason": "the person asked"},
		withHeader("X-Cowork-Agent", "chat/stub:model/c1"), withHeader("If-Match", strconv.Quote(strconv.Itoa(tk.Version))))
	require.Equal(t, http.StatusOK, marked.StatusCode, "the chat holds set-horizon, from the set its person chose")
	assert.Equal(t, apigen.HorizonNext, decode[apigen.Ticket](t, marked).Horizon)
}
