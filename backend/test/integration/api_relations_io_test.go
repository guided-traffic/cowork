//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// The relations across teams in an export, an import and a purge
// (docs/adr/0051 D9 and docs/adr/0024 D2 as made concrete 2026-10-10).

// The export writes a parent and a link end of another team by their keys and
// nothing else of them — never a head's text —, and leaves out a parent and a
// link end the reader may not see (docs/adr/0051 D9).
func TestTheExportWritesTheKeyOfAParentInAnotherTeam(t *testing.T) {
	e := newRelEnv(t)
	ctx := context.Background()
	adminA, memberA, memberB, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent in A", func(c *apigen.TicketCreate) { c.Body = ptr("PARENT-BODY") }))
	other := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("Linked from B"))
	pre := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("A prerequisite in A"))
	child := e.fileIn(t, both, e.SlugB, "BETA", task("A child in B", func(c *apigen.TicketCreate) { c.Parent = ptr(parent.Key) }))
	require.Equal(t, http.StatusCreated, e.s.do(t, both, http.MethodPut,
		ticketPathOf(e.SlugB, child)+"/links/relates-to/"+e.SlugA+"/"+shortOf(other), nil).StatusCode)
	require.Equal(t, http.StatusCreated, e.s.do(t, both, http.MethodPut,
		ticketPathOf(e.SlugA, pre)+"/links/blocks/"+e.SlugB+"/"+shortOf(child), nil).StatusCode)
	secret := e.fileIn(t, adminA, e.SlugA, "ALPHA", task("The secret parent", func(c *apigen.TicketCreate) {
		c.Security, c.Threat = apigen.SecurityClassLive, ptr("it leaks")
	}))
	underSecret := e.fileIn(t, memberB, e.SlugB, "BETA", task("Under a secret"))
	require.NoError(t, e.f.Exec(ctx, "UPDATE tickets SET parent_id = $1 WHERE id = $2", secret.Id, underSecret.Id))
	require.NoError(t, e.f.Exec(ctx, `INSERT INTO ticket_links (tenant_id, type, source_id, target_id, created_by)
		VALUES ($1, 'found-in', $2, $3, $4)`, e.A, secret.Id, underSecret.Id, e.AdminA))

	res := e.s.do(t, memberB, http.MethodGet, "/api/v1/teams/"+e.SlugB+"/projects/BETA/export", nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	files := untar(t, raw)
	doc := string(files[child.Key+".md"])
	require.NotEmpty(t, doc, "the child's document: %v", keysOfFiles(files))
	assert.Contains(t, doc, "\nparent: "+parent.Key+"\n")
	assert.NotContains(t, doc, "The parent in A")
	assert.NotContains(t, doc, "PARENT-BODY")
	assert.NotContains(t, string(files[underSecret.Key+".md"]), "parent:", "a parent the reader may not see is left out")
	var links []apigen.ExportLink
	require.NoError(t, json.Unmarshal(files["links.json"], &links))
	assert.Contains(t, links, apigen.ExportLink{Source: child.Key, Type: apigen.LinkTypeRelatesTo, Target: other.Key})
	assert.Contains(t, links, apigen.ExportLink{Source: pre.Key, Type: apigen.LinkTypeBlocks, Target: child.Key})
	for _, name := range []string{"links.json", "manifest.json", child.Key + ".md"} {
		assert.NotContains(t, string(files[name]), "Linked from B", "%s holds a head's text", name)
		assert.NotContains(t, string(files[name]), "A prerequisite in A", "%s holds a head's text", name)
		assert.NotContains(t, string(files[name]), secret.Key, "%s names a ticket the reader may not see", name)
	}
}

func keysOfFiles(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	return out
}

// importIntoBeta sends a dry run of an upload to team B's project BETA and
// executes it, answering the executed job.
func (e relEnv) importIntoBeta(t *testing.T, c caller, parts ...namedFile) apigen.ImportJob {
	t.Helper()
	contentType, body := uploadOf(t, parts...)
	cl := e.s.client(t, c)
	dry, err := cl.CreateImportWithBodyWithResponse(e.ctx, e.SlugB, "BETA", contentType, bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, dry.StatusCode(), string(dry.Body))
	done, err := cl.ExecuteImportWithResponse(e.ctx, e.SlugB, "BETA", dry.JSON201.Id, apigen.ImportExecution{})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, done.StatusCode(), string(done.Body))
	return *done.JSON200
}

// The import sets a parent of another team for a person who reads it, and
// reports it as not set for one who does not — as a key that names nothing;
// nothing in the import refuses (docs/adr/0051 D9, docs/adr/0063).
func TestTheImportResolvesAParentInAnotherTeamForItsReader(t *testing.T) {
	e := newRelEnv(t)
	memberA, memberB, both := caller{Token: e.tk.MemberA}, caller{Token: e.tk.MemberB}, caller{Token: e.tk.Both}
	parent := e.fileIn(t, memberA, e.SlugA, "ALPHA", task("The parent in A"))

	job := e.importIntoBeta(t, both, ticketFile(1, "parent: "+parent.Key+"\n"))
	file := reported(t, &job, "001-ticket-1.md")
	assert.Equal(t, parent.Key, file.Parent.MustGet())
	imported := e.readIn(t, both, e.SlugB, apigen.Ticket{Project: "BETA", Number: 1})
	assert.Equal(t, parent.Key, imported.Parent.MustGet(), "the reader's import sets it")

	job = e.importIntoBeta(t, memberB, ticketFile(2, "parent: "+parent.Key+"\n"))
	file = reported(t, &job, "002-ticket-2.md")
	assert.True(t, file.Parent.IsNull())
	var warned bool
	for _, w := range file.Warnings {
		warned = warned || strings.Contains(w.Message, "the parent is not set: "+parent.Key+" is no ticket you can read")
	}
	assert.True(t, warned, "the report says why: %+v", file.Warnings)
	assert.True(t, e.readIn(t, memberB, e.SlugB, apigen.Ticket{Project: "BETA", Number: 2}).ParentHead.IsNull())

	// A link to a ticket of another team the person reads is made, its act on
	// that ticket recorded in its own team's record; one whose source is that
	// ticket is a member's of its team to set, reported and omitted.
	job = e.importIntoBeta(t, both,
		ticketFile(3, "filed-from: "+parent.Key+"\n"), ticketFile(4, "blocked-by: "+parent.Key+"\n"))
	three := reported(t, &job, "003-ticket-3.md")
	require.Len(t, three.Links, 1)
	assert.Equal(t, parent.Key, three.Links[0].Key)
	acts, err := e.f.QueryCount(e.ctx, `SELECT count(*) FROM audit_events
		WHERE tenant_id = $1 AND ticket_id = $2 AND action = 'linked'`, e.A, parent.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, acts, "the link's act on the ticket of A is in A's record")
	four := reported(t, &job, "004-ticket-4.md")
	assert.Empty(t, four.Links)
	warned = false
	for _, w := range four.Warnings {
		warned = warned || strings.Contains(w.Message, "its source is a ticket of another team")
	}
	assert.True(t, warned, "%+v", four.Warnings)

	// A block waits on a ticket of its own team (docs/adr/0009 D2): a blocked
	// file whose block names a ticket of another team cannot be imported, the
	// report says why, and the execution writes the rest.
	blocked := namedFile{name: "005-ticket-5.md", body: []byte("---\nid: T5\ntitle: ticket 5\nstate: blocked\nblocked-by: " +
		parent.Key + "\nblocked-reason: waits on A\nblocked-from: in-progress\nseverity: low\nsecurity: none\nurgency: later\n" +
		"effort: S\nopened: 2026-10-01\n---\n\n## Current state\n\nText.\n")}
	job = e.importIntoBeta(t, both, blocked, ticketFile(6, ""))
	five := reported(t, &job, "005-ticket-5.md")
	assert.Equal(t, apigen.ImportOutcomeError, five.Outcome)
	failed := false
	for _, m := range five.Errors {
		failed = failed || strings.Contains(m.Message, parent.Key+" is a ticket of another team")
	}
	assert.True(t, failed, "%+v", five.Errors)
	assert.Equal(t, apigen.ImportOutcomeCreated, reported(t, &job, "006-ticket-6.md").Outcome)
}

// A purge ends the purged ticket's relations into another team — its child
// there becomes a root, its version unchanged; the links between them go —,
// each change recorded in the record of the team it changes; the explicit
// purge of a browser session and the job alike (docs/adr/0024 D2 as made
// concrete 2026-10-10).
func TestAPurgeEndsTheRelationsIntoAnotherTeam(t *testing.T) {
	e := newRelEnv(t)
	ctx := context.Background()
	adminA, both := caller{Token: e.tk.AdminA}, caller{Token: e.tk.Both}
	session := sessionOf(t, e.AdminA)
	db := openRuntime(t)

	type purged struct{ parent, child, fromB, toB apigen.Ticket }
	arrange := func(name string) purged {
		var p purged
		p.parent = e.fileIn(t, adminA, e.SlugA, "ALPHA", task(name+" to be purged"))
		p.child = e.fileIn(t, both, e.SlugB, "BETA", task(name+" child in B", func(c *apigen.TicketCreate) { c.Parent = ptr(p.parent.Key) }))
		p.fromB = e.fileIn(t, both, e.SlugB, "BETA", task(name+" blocks the purged"))
		p.toB = e.fileIn(t, both, e.SlugB, "BETA", task(name+" relates to the purged"))
		require.Equal(t, http.StatusCreated, e.s.do(t, both, http.MethodPut,
			ticketPathOf(e.SlugB, p.fromB)+"/links/blocks/"+e.SlugA+"/"+shortOf(p.parent), nil).StatusCode)
		require.Equal(t, http.StatusCreated, e.s.do(t, both, http.MethodPut,
			ticketPathOf(e.SlugA, p.parent)+"/links/relates-to/"+e.SlugB+"/"+shortOf(p.toB), nil).StatusCode)
		p.child = e.readIn(t, both, e.SlugB, p.child)
		require.Equal(t, http.StatusNoContent, e.s.do(t, adminA, http.MethodDelete, ticketPathOf(e.SlugA, p.parent), nil).StatusCode)
		return p
	}
	check := func(name string, p purged, actor string) {
		t.Helper()
		var parent *string
		var version int
		require.NoError(t, e.f.QueryRow(ctx, "SELECT parent_id::text, version FROM tickets WHERE id = $1", p.child.Id).Scan(&parent, &version))
		assert.Nil(t, parent, "%s: the child in B is a root", name)
		assert.Equal(t, p.child.Version, version, "%s: its version unchanged", name)
		links, err := e.f.QueryCount(ctx, "SELECT count(*) FROM ticket_links WHERE source_id = $1 OR target_id = $1", p.parent.Id)
		require.NoError(t, err)
		assert.Zero(t, links, "%s: no link names the purged ticket", name)
		// The detach names the purged ticket in its refs alone, no id of it in
		// its payload; a link's removal keeps the keys of its ends, redacted
		// by the same refs.
		for _, act := range []struct {
			ticket, action, reason string
			before                 bool
		}{
			{p.child.Id.String(), "updated", "the parent was purged", false},
			{p.fromB.Id.String(), "unlinked", "the other ticket was purged", true},
			{p.toB.Id.String(), "unlinked", "the other ticket was purged", true},
		} {
			n, err := e.f.QueryCount(ctx, `SELECT count(*) FROM audit_events
				WHERE tenant_id = $1 AND ticket_id = $2 AND action = $3 AND reason = $4
				  AND coalesce(actor_system, actor_user_id::text) = $5
				  AND token_id IS NULL AND agent IS NULL AND (before IS NOT NULL) = $7 AND $6 = ANY (refs)`,
				e.B, act.ticket, act.action, act.reason, actor, p.parent.Id, act.before)
			require.NoError(t, err)
			assert.EqualValues(t, 1, n, "%s: the act %s on %s in B's record, naming the purged ticket in its refs alone",
				name, act.action, act.ticket)
		}
		assert.True(t, e.readIn(t, both, e.SlugB, p.child).ParentHead.IsNull(), name)
	}

	explicit := arrange("explicit")
	res := e.s.do(t, session, http.MethodDelete, "/api/v1/teams/"+e.SlugA+"/deleted-tickets/"+url.PathEscape(shortOf(explicit.parent)), nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	check("the explicit purge, whose administrator holds no role in B", explicit, "system:ticket-purge")

	job := arrange("the job's")
	require.NoError(t, e.f.Exec(ctx, "UPDATE tickets SET deleted_at = now() - interval '31 days' WHERE id = $1", job.parent.Id))
	_, err := db.PurgeDeletedTickets(ctx, time.Now())
	require.NoError(t, err)
	check("the job", job, "system:ticket-purge")
}
