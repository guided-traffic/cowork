//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

// scoreOf is a ticket's score, which it must have.
func scoreOf(t *testing.T, tk apigen.Ticket) float64 {
	t.Helper()
	s, err := tk.Score.Get()
	require.NoError(t, err, "%s has a score", tk.Key)
	return s
}

// storedScore requires the score a ticket stores to be what the function
// computes from its inputs as they stand (docs/adr/0014 D4).
func storedScore(t *testing.T, f *fixture.DB, id uuid.UUID) {
	t.Helper()
	var (
		in      domain.ScoreInputs
		key     float64
		version int
	)
	require.NoError(t, f.QueryRow(context.Background(),
		`SELECT t.severity, coalesce(t.urgency_override, t.urgency_derived), t.opened_at,
		        (SELECT count(*) FROM ticket_interest i WHERE i.ticket_id = t.id AND i.weight = 'need'),
		        (SELECT count(*) FROM ticket_interest i WHERE i.ticket_id = t.id AND i.weight = 'urgent'),
		        t.score_key, t.score_version
		 FROM tickets t WHERE t.id = $1`, id).Scan(&in.Severity, &in.Horizon, &in.OpenedAt, &in.Need, &in.Urgent, &key, &version))
	assert.Equal(t, domain.ScoreKey(in), key, "the stored score is the function's")
	assert.Equal(t, domain.ScoreVersion, version)
}

// docs/adr/0014 D3, D4, docs/adr/0013 D3, docs/adr/0010 D3: a ticket's score
// is its severity, its horizon and the need and urgent stakes in it, plus its
// age; it follows every change of an input, leaves the ticket's version alone
// when a stake moves it, and is none while the ticket is done.
func TestTheScoreFollowsItsInputs(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, admin, viewer := caller{Token: e.tk.MemberA}, caller{Token: e.tk.AdminA}, caller{Token: e.tk.ViewerA}
	read := func(number int) apigen.Ticket {
		t.Helper()
		res := e.get(t, member, "ALPHA", number)
		require.Equal(t, http.StatusOK, res.StatusCode())
		return *res.JSON200
	}

	tk := e.file(t, member, "ALPHA", task("Scored"))
	assert.Equal(t, 4.0, scoreOf(t, tk), "medium 3, later 1, no age yet")
	assert.Equal(t, domain.ScoreVersion, tk.ScoreVersion.MustGet())
	storedScore(t, f, tk.Id)

	res := e.patch(t, member, tk, apigen.TicketPatch{Severity: ptr(apigen.SeverityHigh)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	tk = *res.JSON200
	assert.Equal(t, 6.0, scoreOf(t, tk), "high 5")

	e.send(t, member, http.StatusOK, http.MethodPut, ticketPath(e.SlugA, "ALPHA", tk.Number)+"/urgency-override",
		map[string]any{"value": "now"}, "If-Match", strconv.Quote(strconv.Itoa(tk.Version)))
	tk = read(tk.Number)
	assert.Equal(t, 13.0, scoreOf(t, tk), "the horizon now 8")
	storedScore(t, f, tk.Id)

	version := tk.Version
	require.Equal(t, http.StatusCreated, e.stake(t, admin, tk, apigen.InterestWeightNeed, "for the release").StatusCode())
	assert.Equal(t, 14.0, scoreOf(t, read(tk.Number)), "a need counts one")
	require.Equal(t, http.StatusCreated, e.stake(t, member, tk, apigen.InterestWeightUrgent, "blocks me").StatusCode())
	require.Equal(t, http.StatusCreated, e.stake(t, viewer, tk, apigen.InterestWeightWatch, "").StatusCode())
	tk = read(tk.Number)
	assert.Equal(t, 16.0, scoreOf(t, tk), "an urgent stake two, a watch nothing")
	assert.Equal(t, version, tk.Version, "a stake leaves the ticket's version")
	storedScore(t, f, tk.Id)
	e.send(t, member, http.StatusNoContent, http.MethodDelete, ticketPath(e.SlugA, "ALPHA", tk.Number)+"/interest", nil)
	tk = read(tk.Number)
	assert.Equal(t, 14.0, scoreOf(t, tk), "a stake removed counts no more")

	done := e.move(t, member, tk, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDone, Note: ptr("verified")})
	require.Equal(t, http.StatusOK, done.StatusCode(), string(done.Body))
	assert.True(t, done.JSON200.Score.IsNull(), "a done ticket has no score")
	assert.True(t, done.JSON200.ScoreVersion.IsNull())
	back := e.move(t, member, *done.JSON200, apigen.Transition{From: apigen.TicketStateDone, To: apigen.TicketStateFiled, Reason: ptr("too early")})
	require.Equal(t, http.StatusOK, back.StatusCode(), string(back.Body))
	assert.Equal(t, 14.0, scoreOf(t, *back.JSON200), "reopened, it scores as before")

	into := e.file(t, member, "ALPHA", task("Into next", func(b *apigen.TicketCreate) { b.Urgency = ptr(apigen.UrgencyNext) }))
	assert.Equal(t, 6.0, scoreOf(t, into), "filed into next: 3 and 3")

	// Age counts from opened_at: forty-five days are one and a half.
	_, number, err := f.Ticket(e.ctx, e.A, e.ProjectA, e.MemberA, "Older")
	require.NoError(t, err)
	older := read(number)
	assert.True(t, older.Score.IsNull(), "a ticket a release before the score filed has none until an input changes")
	require.NoError(t, f.Exec(e.ctx, "UPDATE tickets SET opened_at = now() - interval '45 days' WHERE id = $1", older.Id))
	res = e.patch(t, member, older, apigen.TicketPatch{Severity: ptr(apigen.SeverityLow)})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	assert.Equal(t, 3.5, scoreOf(t, *res.JSON200), "low 1, later 1, a month and a half of age")
	storedScore(t, f, older.Id)
}

// sortRank sorts the project's rank by the score as c.
func (e ticketEnv) sortRank(t *testing.T, c caller, project string) *http.Response {
	t.Helper()
	return e.s.do(t, c, http.MethodPut, "/api/v1/tenants/"+e.SlugA+"/projects/"+project+"/rank", map[string]any{"by": "score"})
}

// docs/adr/0014 D3: "sort by score" reorders a project's open tickets to their
// score in one recorded act, published as project.changed; a ticket the
// caller cannot see keeps its key and its place; each ticket it moved gets a
// new version, as a move gives one, and the hidden one keeps its own;
// a rank that follows the score already records nothing; it needs rank of an
// agent and the member role.
func TestSortByScore(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, admin := caller{Token: e.tk.MemberA}, caller{Token: e.tk.AdminA}
	low := e.file(t, member, "ALPHA", task("low", func(b *apigen.TicketCreate) { b.Severity = apigen.SeverityLow }))
	secret := e.file(t, admin, "ALPHA", task("secret", func(b *apigen.TicketCreate) {
		b.Severity, b.Security, b.Threat = apigen.SeverityHigh, apigen.SecurityClassLive, ptr("leak")
	}))
	critical := e.file(t, member, "ALPHA", task("critical", func(b *apigen.TicketCreate) { b.Severity = apigen.SeverityCritical }))
	e.file(t, member, "ALPHA", task("medium"))
	hidden := keyOf(t, f, secret.Id)
	require.Equal(t, []string{"low", "critical", "medium"}, e.titles(t, member, e.projectTickets("ALPHA"), ""))
	stream := e.openStream(t, e.s, member, e.SlugA, "")

	res := e.sortRank(t, member, "ALPHA")
	if res.StatusCode != http.StatusOK {
		require.Equal(t, http.StatusOK, res.StatusCode, "%v", problemBody(t, res))
	}
	assert.Equal(t, apigen.ProjectRankSorted{Moved: 3, ScoreVersion: domain.ScoreVersion}, decode[apigen.ProjectRankSorted](t, res))
	assert.Equal(t, []string{"critical", "medium", "low"}, e.titles(t, member, e.projectTickets("ALPHA"), ""))
	assert.Equal(t, hidden, keyOf(t, f, secret.Id), "the hidden ticket keeps its key")
	assert.Equal(t, []string{"critical", "secret", "medium", "low"}, e.titles(t, admin, e.projectTickets("ALPHA"), ""),
		"and its place among the keys the others took")
	assert.Equal(t, 2, e.get(t, member, "ALPHA", low.Number).JSON200.Version, "a ticket the sort moved has a new version")
	assert.Equal(t, 2, e.get(t, member, "ALPHA", critical.Number).JSON200.Version)
	assert.Equal(t, 1, e.get(t, admin, "ALPHA", secret.Number).JSON200.Version, "the hidden ticket was not moved")

	m, ok := stream.next(t, time.Second)
	require.True(t, ok, "the sort is published")
	assert.Equal(t, "project.changed", m.Event)
	assert.JSONEq(t, `{"key":"`+e.SlugA+`/ALPHA","kind":"ranked"}`, m.Data)

	var after string
	n, err := f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE entity_type = 'project' AND entity_id = $1 AND action = 'ranked'", e.ProjectA)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "one act")
	require.NoError(t, f.QueryRow(e.ctx, "SELECT after::text FROM audit_events WHERE entity_type = 'project' AND entity_id = $1", e.ProjectA).Scan(&after))
	assert.JSONEq(t, `{"by":"score","moved":3,"score_version":1}`, after)

	// docs/adr/0015 D1: the activity of each ticket the sort moved shows it.
	sorts := func(c caller, number int) []apigen.Activity {
		t.Helper()
		res := e.s.do(t, c, http.MethodGet, ticketPath(e.SlugA, "ALPHA", number)+"/activity", nil)
		require.Equal(t, http.StatusOK, res.StatusCode)
		var out []apigen.Activity
		for _, a := range decode[apigen.ActivityList](t, res).Items {
			if a.EntityType == "project" {
				out = append(out, a)
			}
		}
		return out
	}
	moved := sorts(member, low.Number)
	require.Len(t, moved, 1)
	assert.Equal(t, apigen.AuditActionRanked, moved[0].Action)
	assert.Equal(t, "score", moved[0].After.MustGet()["by"])
	assert.False(t, moved[0].Redacted)
	assert.Empty(t, sorts(admin, secret.Number), "the hidden ticket was not moved, and its activity holds no sort")

	again := e.sortRank(t, member, "ALPHA")
	require.Equal(t, http.StatusOK, again.StatusCode)
	assert.Equal(t, 0, decode[apigen.ProjectRankSorted](t, again).Moved, "the rank follows the score already")
	n, err = f.QueryCount(e.ctx, "SELECT count(*) FROM audit_events WHERE entity_type = 'project' AND entity_id = $1", e.ProjectA)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "and nothing is recorded")

	assertProblem(t, e.sortRank(t, caller{Token: e.tk.ViewerA}, "ALPHA"), http.StatusForbidden, "forbidden")
	assertProblem(t, e.sortRank(t, caller{Token: e.tk.AssistedAgentA}, "ALPHA"), http.StatusForbidden, "agent_forbidden")
	assertProblem(t, e.s.do(t, member, http.MethodPut, "/api/v1/tenants/"+e.SlugA+"/projects/ALPHA/rank", map[string]any{"by": "rank"}),
		http.StatusBadRequest, "validation_failed")
	assertProblem(t, e.sortRank(t, caller{Token: e.tk.MemberB}, "ALPHA"), http.StatusNotFound, "not_found")
	restricted, err := f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id = $1", restricted))
	assertProblem(t, e.sortRank(t, member, "HIDDEN"), http.StatusNotFound, "not_found")
}

// docs/adr/0014 Consequences: moves into one and the same gap wear it down,
// and before it runs out the project's keys are spread again — 800 moves
// there, more than a gap takes, all succeed; every key stays short; a ticket
// the mover cannot see keeps its place among the others; no ticket's version
// moves by the rebalancing.
func TestEightHundredMovesIntoOneGap(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	member, admin := caller{Token: e.tk.MemberA}, caller{Token: e.tk.AdminA}
	first := e.file(t, member, "ALPHA", task("first"))
	e.file(t, admin, "ALPHA", task("hidden", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak")
	}))
	last := e.file(t, member, "ALPHA", task("last"))
	p := e.file(t, member, "ALPHA", task("p"))
	q := e.file(t, member, "ALPHA", task("q"))
	before := keyOf(t, f, first.Id)

	for i := range 800 {
		mover := p
		if i%2 == 1 {
			mover = q
		}
		res := e.rankMove(t, member, mover, side(false, last.Number))
		require.Equal(t, http.StatusOK, res.StatusCode(), "move %d: %s", i, string(res.Body))
	}

	assert.Equal(t, []string{"first", "p", "q", "last"}, e.titles(t, member, e.projectTickets("ALPHA"), ""),
		"the last mover sits directly before the ticket every move named")
	assert.Equal(t, []string{"first", "hidden", "p", "q", "last"}, e.titles(t, admin, e.projectTickets("ALPHA"), ""),
		"the hidden ticket kept its place")
	assert.NotEqual(t, before, keyOf(t, f, first.Id), "the keys were spread again")
	for number, key := range rankKeys(t, f, e.ProjectA) {
		assert.LessOrEqual(t, len(key), domain.RankRebalanceLength, "ticket %d", number)
	}
	assert.Equal(t, 1, e.get(t, member, "ALPHA", first.Number).JSON200.Version, "a rebalancing is no move")
	assert.EqualValues(t, 400, rankedActs(t, f, p.Id), "every move of p recorded, none of the rebalancing")
}

// next reads "next for me" as c, with a query, and returns the keys.
func (e ticketEnv) next(t *testing.T, c caller, query string) ([]string, apigen.MyTicketList) {
	t.Helper()
	res := e.s.do(t, c, http.MethodGet, "/api/v1/me/next"+query, nil)
	if res.StatusCode != http.StatusOK {
		require.Equal(t, http.StatusOK, res.StatusCode, "%v", problemBody(t, res))
	}
	l := decode[apigen.MyTicketList](t, res)
	keys := make([]string, 0, len(l.Items))
	for _, it := range l.Items {
		keys = append(keys, it.Ticket.Key)
	}
	return keys, l
}

// docs/adr/0018 D3 as amended 2026-10-05, docs/adr/0014 D5: "next for me"
// holds the person's open tickets and the unassigned open tickets of the
// projects they see, across their tenants, by score — another person's ticket,
// a done one, a project restricted away from them and a confidential ticket
// they may not read are absent; each item names its tenant and its place in
// its project's rank, which counts no ticket the person cannot see; tenant and
// project narrow it, a restricted token reads its tenant or its project.
func TestNextForMeAcrossTenants(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	sev := func(s apigen.Severity) func(*apigen.TicketCreate) {
		return func(b *apigen.TicketCreate) { b.Severity = s }
	}
	toBoth := func(b *apigen.TicketCreate) { b.Assignee = &e.Both }
	secretly := func(b *apigen.TicketCreate) { b.Security, b.Threat = apigen.SecurityClassLive, ptr("leak") }

	gamma, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	hidden, err := f.Project(e.ctx, e.A, "HIDDEN", "Hidden")
	require.NoError(t, err)
	mine := e.fileIn(t, admin, e.SlugA, "ALPHA", task("mine", toBoth))                               // 3 + 1
	unassigned := e.fileIn(t, admin, e.SlugA, "ALPHA", task("unassigned", sev(apigen.SeverityHigh))) // 5 + 1
	e.fileIn(t, admin, e.SlugA, "ALPHA", task("theirs", sev(apigen.SeverityCritical), func(b *apigen.TicketCreate) { b.Assignee = &e.MemberA }))
	done := e.fileIn(t, admin, e.SlugA, "ALPHA", task("done", sev(apigen.SeverityCritical), toBoth))
	e.send(t, admin, http.StatusOK, http.MethodPost, ticketPath(e.SlugA, "ALPHA", done.Number)+"/transitions",
		map[string]any{"from": "filed", "to": "done", "note": "verified by hand"})
	e.fileIn(t, admin, e.SlugA, "ALPHA", task("secret", sev(apigen.SeverityCritical), secretly))
	secretMine := e.fileIn(t, admin, e.SlugA, "ALPHA", task("secret and mine", sev(apigen.SeverityLow), secretly, toBoth)) // 1 + 1
	inGamma := e.fileIn(t, admin, e.SlugA, "GAMMA", task("gamma", sev(apigen.SeverityCritical)))                           // 8 + 1
	e.fileIn(t, admin, e.SlugA, "HIDDEN", task("hidden", sev(apigen.SeverityCritical)))
	inB := e.fileIn(t, memberB, e.SlugB, "BETA", task("in B", func(b *apigen.TicketCreate) { b.Urgency = ptr(apigen.UrgencyNow) })) // 3 + 8
	require.NoError(t, f.Exec(e.ctx, "UPDATE projects SET restricted = true WHERE id IN ($1, $2)", gamma, hidden))
	require.NoError(t, f.Exec(e.ctx, "INSERT INTO project_access (tenant_id, project_id, user_id, role) VALUES ($1, $2, $3, 'member')",
		e.A, gamma, e.Both))

	want := []string{inB.Key, inGamma.Key, unassigned.Key, mine.Key, secretMine.Key}
	keys, list := e.next(t, both, "")
	assert.Equal(t, want, keys, "by score: 11, 9, 6, 4, 2")
	assert.Equal(t, apigen.TenantRef{Slug: e.SlugB, Name: "Tenant B"}, list.Items[0].Tenant)
	assert.Equal(t, apigen.TenantRef{Slug: e.SlugA, Name: "Tenant A"}, list.Items[1].Tenant)
	assert.Equal(t, 11.0, scoreOf(t, list.Items[0].Ticket))
	places := map[string]int{}
	for _, it := range list.Items {
		places[it.Ticket.Key] = it.Place
	}
	assert.Equal(t, map[string]int{inB.Key: 1, inGamma.Key: 1, mine.Key: 1, unassigned.Key: 2, secretMine.Key: 4}, places,
		"the place in the horizon's rank among what the person sees: the secret before secretMine is not counted")

	assert.Equal(t, want, walk(t, func(query string) ([]string, *string) {
		keys, l := e.next(t, both, query)
		next, err := l.NextCursor.Get()
		if err != nil {
			return keys, nil
		}
		return keys, &next
	}), "the cursor walks the same order, one per page")

	onlyB, _ := e.next(t, both, "?tenant="+e.SlugB)
	assert.Equal(t, []string{inB.Key}, onlyB)
	onlyGamma, _ := e.next(t, both, "?tenant="+e.SlugA+"&project=GAMMA")
	assert.Equal(t, []string{inGamma.Key}, onlyGamma)
	nothing, _ := e.next(t, both, "?tenant="+e.SlugA+"&project=HIDDEN")
	assert.Empty(t, nothing, "a project hidden from the person lists nothing")
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/next?project=GAMMA", nil), http.StatusBadRequest, "validation_failed")
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/next?tenant=no-such-tenant", nil), http.StatusNotFound, "not_found")
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/next?limit=1&cursor=tampered", nil), http.StatusBadRequest, "invalid_cursor")
	_, page := e.next(t, both, "?limit=1&tenant="+e.SlugA)
	assertProblem(t, e.s.do(t, both, http.MethodGet, "/api/v1/me/next?limit=1&tenant="+e.SlugB+"&cursor="+page.NextCursor.MustGet(), nil),
		http.StatusBadRequest, "invalid_cursor")

	ofB, _ := e.next(t, memberB, "")
	assert.Equal(t, []string{inB.Key}, ofB, "the unassigned ticket of B is the member's to take up, too")
	tenantToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.B})
	require.NoError(t, err)
	byTenant, _ := e.next(t, caller{Token: tenantToken}, "")
	assert.Equal(t, []string{inB.Key}, byTenant)
	projectToken, _, err := f.Token(e.ctx, fixture.TokenSpec{UserID: e.Both, TenantID: e.A, ProjectID: e.ProjectA})
	require.NoError(t, err)
	byProject, _ := e.next(t, caller{Token: projectToken}, "")
	assert.Equal(t, []string{unassigned.Key, mine.Key, secretMine.Key}, byProject)
}

// docs/adr/0014 D4, docs/adr/0028 D3: migration 33 scores every ticket with
// version 1 as the function does, and restores the forced row-level security
// it lifts for its backfill.
func TestScoreMigrationScoresEveryTicket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("cowork_it_score_%d", time.Now().UnixNano())
	require.NoError(t, createDatabase(ctx, env.AdminURL, name))
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), time.Minute)
		defer dropCancel()
		assert.NoError(t, dropDatabase(dropCtx, env.AdminURL, name))
	})
	adminURL, err := withUserAndDatabase(env.AdminURL, "", "", name)
	require.NoError(t, err)
	ownerURL, err := withUserAndDatabase(env.AdminURL, ownerRole, ownerRole, name)
	require.NoError(t, err)

	migrateTo(t, ownerURL, 30)
	f, err := fixture.Connect(ctx, adminURL)
	require.NoError(t, err)
	t.Cleanup(f.Close)
	person, err := f.Person(ctx, uniqueSlug("scorer"), "Scorer")
	require.NoError(t, err)
	other, err := f.Person(ctx, uniqueSlug("stakeholder"), "Stakeholder")
	require.NoError(t, err)
	tenant, err := f.Tenant(ctx, uniqueSlug("score"), "Score")
	require.NoError(t, err)
	project, err := f.Project(ctx, tenant, "SCORE", "Score")
	require.NoError(t, err)
	type ticket struct {
		severity, horizon string
		need, urgent      bool
	}
	cases := []ticket{{"critical", "now", true, true}, {"high", "release", false, true}, {"medium", "next", true, false},
		{"low", "later", false, false}, {"cosmetic", "icebox", true, false}}
	ids := make([]uuid.UUID, len(cases))
	for i, c := range cases {
		id, _, err := f.Ticket(ctx, tenant, project, person, "ticket "+c.severity)
		require.NoError(t, err)
		ids[i] = id
		require.NoError(t, f.Exec(ctx, `UPDATE tickets SET severity = $2, urgency_override = $3::urgency, urgency_override_at = now(),
			opened_at = now() - make_interval(days => $4) WHERE id = $1`, id, c.severity, c.horizon, 10*i))
		if c.need {
			require.NoError(t, f.Exec(ctx, `INSERT INTO ticket_interest (tenant_id, ticket_id, user_id, weight) VALUES ($1, $2, $3, 'need')`,
				tenant, id, other))
		}
		if c.urgent {
			require.NoError(t, f.Exec(ctx, `INSERT INTO ticket_interest (tenant_id, ticket_id, user_id, weight) VALUES ($1, $2, $3, 'urgent')`,
				tenant, id, person))
		}
	}

	_, err = store.Migrate(ctx, ownerURL, runtimeRole)
	require.NoError(t, err)

	for i, id := range ids {
		var (
			in      domain.ScoreInputs
			key     float64
			version int
		)
		require.NoError(t, f.QueryRow(ctx, `SELECT t.severity, coalesce(t.urgency_override, t.urgency_derived), t.opened_at,
		        (SELECT count(*) FROM ticket_interest i WHERE i.ticket_id = t.id AND i.weight = 'need'),
		        (SELECT count(*) FROM ticket_interest i WHERE i.ticket_id = t.id AND i.weight = 'urgent'),
		        t.score_key, t.score_version
		 FROM tickets t WHERE t.id = $1`, id).Scan(&in.Severity, &in.Horizon, &in.OpenedAt, &in.Need, &in.Urgent, &key, &version))
		assert.InDelta(t, domain.ScoreKey(in), key, 1e-9, "case %d: the migration computes what the function does", i)
		assert.Equal(t, 1, version, "case %d", i)
	}
	for _, table := range []string{"tickets", "ticket_interest"} {
		var forced bool
		require.NoError(t, f.QueryRow(ctx, "SELECT relforcerowsecurity FROM pg_class WHERE oid = $1::regclass", table).Scan(&forced))
		assert.True(t, forced, "row-level security is forced on %s again", table)
	}
}

// The score in a list is what the ticket itself shows; a stake's event
// changes a cached ticket's score without a new version (docs/adr/0050 D1).
func TestTheListShowsTheScore(t *testing.T) {
	e := newTicketEnv(t)
	member := caller{Token: e.tk.MemberA}
	tk := e.file(t, member, "ALPHA", task("Listed", func(b *apigen.TicketCreate) { b.Severity = apigen.SeverityCritical }))
	res := e.s.do(t, member, http.MethodGet, e.projectTickets("ALPHA"), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	var list struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&list))
	require.Len(t, list.Items, 1)
	assert.JSONEq(t, "9", string(list.Items[0]["score"]), "critical 8, later 1")
	assert.JSONEq(t, "1", string(list.Items[0]["score_version"]))
	assert.Equal(t, 9.0, scoreOf(t, tk))
}
