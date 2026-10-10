//go:build integration

package integration

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/api"
	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// exportOf fetches an export as c, at a project's route or, for "", the
// tenant's; the archive unpacked when it is one.
func (e ticketEnv) exportOf(t *testing.T, c caller, project string) (*http.Response, []byte, map[string][]byte) {
	t.Helper()
	p := fmt.Sprintf("/api/v1/teams/%s/export", e.SlugA)
	if project != "" {
		p = fmt.Sprintf("/api/v1/teams/%s/projects/%s/export", e.SlugA, project)
	}
	res := e.s.do(t, c, http.MethodGet, p, nil)
	if res.StatusCode != http.StatusOK {
		return res, nil, nil
	}
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, body, untar(t, body)
}

func manifestOf(t *testing.T, files map[string][]byte) apigen.ExportManifest {
	t.Helper()
	var m apigen.ExportManifest
	require.NoError(t, json.Unmarshal(files["manifest.json"], &m))
	return m
}

// docs/adr/0051 D5, the round trip that keeps grammar v1 honest: a project
// exported, imported into an empty project and exported again is the same
// archive up to the keys and the times — every column, the stages a parent
// derives, the assignee by identity, the questions in every answer form, the
// notes of done and dropped, a block on a ticket, the confidential flag, and
// every link once.
func TestTheExportRoundTripsThroughTheImport(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, member := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}
	_, err := f.Project(e.ctx, e.A, "COPY", "Copy")
	require.NoError(t, err)

	parent := e.file(t, member, "ALPHA", task("Parent: with a colon", func(b *apigen.TicketCreate) {
		b.Type, b.Assignee, b.Horizon = apigen.TicketTypeFeature, &e.MemberA, ptr(apigen.HorizonNow)
		b.Body = ptr("## Current state\n\nWith \"quotes\" and a [link](https://example.com).\n\n```markdown\n## Open questions\n```")
	}))
	child := e.file(t, member, "ALPHA", task("Child", func(b *apigen.TicketCreate) {
		b.Type, b.Severity, b.Parent = apigen.TicketTypeBug, apigen.SeverityHigh, ptr(parent.Key)
	}))
	e.staged(t, member, child, 100, 50, 0)
	answered := e.ask(t, member, parent, apigen.QuestionCreate{Question: "Which way?", Options: ptr("- A\n- B"), Recommendation: ptr("A")})
	require.Equal(t, http.StatusCreated, answered.StatusCode(), string(answered.Body))
	require.Equal(t, http.StatusOK, e.answer(t, member, parent, *answered.JSON201, "A, as recommended.\n\nRecorded here.").StatusCode())
	gone := e.ask(t, member, parent, apigen.QuestionCreate{Question: "Withdrawn?"})
	require.Equal(t, http.StatusOK, e.withdraw(t, member, parent, *gone.JSON201).StatusCode())
	require.Equal(t, http.StatusCreated, e.ask(t, member, parent, apigen.QuestionCreate{Question: "Still open?"}).StatusCode())

	blocked := e.walk(t, member, e.file(t, member, "ALPHA", task("Blocked one")), toAnalysed)
	res := e.move(t, member, blocked, apigen.Transition{From: toAnalysed, To: apigen.TicketStateBlocked,
		Block: &apigen.BlockSet{Kind: apigen.BlockKindHuman}, Reason: ptr("waits on the owner")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	done := e.walk(t, member, e.file(t, member, "ALPHA", task("Done one")), toAnalysed, toDecided, toInProgress, toDone)
	dropped := e.file(t, member, "ALPHA", task("Dropped one"))
	res = e.move(t, member, dropped, apigen.Transition{From: apigen.TicketStateFiled, To: apigen.TicketStateDropped, Reason: ptr("superseded")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	secret := e.file(t, admin, "ALPHA", task("Secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("a member reads what they must not")
	}))
	blocker := e.file(t, member, "ALPHA", task("Blocker"))
	waits := e.walk(t, member, e.file(t, member, "ALPHA", task("Waits on a ticket")), toAnalysed)
	res = e.move(t, member, waits, apigen.Transition{From: toAnalysed, To: apigen.TicketStateBlocked,
		Block: &apigen.BlockSet{Kind: apigen.BlockKindTicket, Ticket: ptr(blocker.Key)}, Reason: ptr("needs the blocker")})
	require.Equal(t, http.StatusOK, res.StatusCode(), string(res.Body))
	for _, l := range []struct {
		from apigen.Ticket
		typ  apigen.LinkType
		to   apigen.Ticket
	}{
		{blocker, apigen.LinkTypeBlocks, child}, {parent, apigen.LinkTypeRelatesTo, done},
		{dropped, apigen.LinkTypeDuplicates, done}, {secret, apigen.LinkTypeFoundIn, parent},
	} {
		require.Equal(t, http.StatusCreated, e.link(t, admin, l.from, l.typ, l.to).StatusCode)
	}

	_, archive, before := e.exportOf(t, admin, "ALPHA")
	created := e.dryRun(t, admin, "COPY", namedFile{name: "alpha.tar.gz", body: archive})
	require.Equal(t, http.StatusCreated, created.StatusCode(), string(created.Body))
	assert.Zero(t, created.JSON201.Summary.Error)
	assert.Zero(t, created.JSON201.Summary.Conflict)
	assert.Equal(t, 8, created.JSON201.Summary.Create)
	executed := e.execute(t, admin, "COPY", created.JSON201.Id)
	require.Equal(t, http.StatusOK, executed.StatusCode(), string(executed.Body))
	_, _, after := e.exportOf(t, admin, "COPY")

	assert.Equal(t, comparable(t, e, before, "ALPHA"), comparable(t, e, after, "COPY"),
		"the archive round-trips up to the keys and the times (docs/adr/0051 D5)")
	m := manifestOf(t, after)
	assert.Equal(t, 8, m.Tickets)
	assert.Equal(t, "COPY", m.Projects[0].Key)
}

// comparable is an archive with the project's keys and the export's time
// taken out: each document and manifest by its path as of the project's key
// PROJECT, the links' ends sorted where a link is symmetric.
func comparable(t *testing.T, e ticketEnv, files map[string][]byte, project string) map[string]string {
	t.Helper()
	keys := strings.NewReplacer(e.SlugA+"/"+project+"-", e.SlugA+"/PROJECT-")
	out := map[string]string{}
	for name, body := range files {
		switch name {
		case "manifest.json":
			m := manifestOf(t, files)
			m.ExportedAt, m.Projects[0].Key, m.Projects[0].Name = time.Time{}, "", ""
			out[name] = string(mustJSON(t, m))
		case "links.json":
			var links []apigen.ExportLink
			require.NoError(t, json.Unmarshal(body, &links))
			for i, l := range links {
				l.Source, l.Target = keys.Replace(l.Source), keys.Replace(l.Target)
				if l.Type == apigen.LinkTypeRelatesTo && l.Source > l.Target {
					l.Source, l.Target = l.Target, l.Source
				}
				links[i] = l
			}
			sort.Slice(links, func(i, j int) bool { return string(mustJSON(t, links[i])) < string(mustJSON(t, links[j])) })
			out[name] = string(mustJSON(t, links))
		default:
			out[keys.Replace(name)] = keys.Replace(string(body))
		}
	}
	return out
}

// docs/adr/0051 D4, D6, docs/adr/0065 D5, docs/adr/0059 D3: the export holds
// what its reader sees and counts the confidential tickets it leaves out;
// whoever reads exports, an agent too; a restricted project the reader
// cannot see is absent from the tenant's archive without a count; the
// attachments' metadata and every link once; every export recorded.
func TestTheExportFollowsItsReader(t *testing.T) {
	e := newTicketEnv(t)
	f := fixtures(t)
	admin, member := caller{Token: e.tk.AdminA}, caller{Token: e.tk.MemberA}
	open := e.file(t, member, "ALPHA", task("Open to all"))
	secret := e.file(t, admin, "ALPHA", task("Secret", func(b *apigen.TicketCreate) {
		b.Security, b.Threat = apigen.SecurityClassLive, ptr("a member reads what they must not")
	}))
	require.Equal(t, http.StatusCreated, e.link(t, admin, open, apigen.LinkTypeRelatesTo, secret).StatusCode)
	upload := e.uploadTo(t, member, open, "notes.txt", "text/plain", []byte("plain notes\n"), nil, "")
	require.Equal(t, http.StatusCreated, upload.StatusCode)
	gamma, err := f.Project(e.ctx, e.A, "GAMMA", "Gamma")
	require.NoError(t, err)
	_, _, err = f.Ticket(e.ctx, e.A, gamma, e.AdminA, "Behind a restriction")
	require.NoError(t, err)
	require.NoError(t, f.Exec(e.ctx, `UPDATE projects SET restricted = true WHERE id = $1`, gamma))

	res, _, files := e.exportOf(t, member, "ALPHA")
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "application/gzip", res.Header.Get("Content-Type"))
	assert.Equal(t, fmt.Sprintf(`attachment; filename="%s-ALPHA-%s.tar.gz"`, e.SlugA, time.Now().UTC().Format("20060102")),
		res.Header.Get("Content-Disposition"))
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	m := manifestOf(t, files)
	assert.Equal(t, apigen.ExportManifestFormat("cowork export v1"), m.Format)
	assert.Equal(t, 1, m.Tickets)
	assert.Equal(t, 1, m.ConfidentialNotIncluded, "n confidential tickets not included (docs/adr/0065 D5)")
	assert.Equal(t, []apigen.ExportManifestProject{{Key: "ALPHA", Name: "Alpha", Tickets: 1, ConfidentialNotIncluded: 1}}, m.Projects)
	assert.Contains(t, m.ExportedBy, "<local:")
	assert.Contains(t, files, open.Key+".md")
	assert.NotContains(t, files, secret.Key+".md")
	assert.Equal(t, "[]\n", string(files["links.json"]), "a link whose other end the reader cannot see is absent")
	var attachments []apigen.ExportAttachment
	require.NoError(t, json.Unmarshal(files["attachments.json"], &attachments))
	require.Len(t, attachments, 1)
	assert.Equal(t, open.Key, attachments[0].Ticket)
	assert.Equal(t, "notes.txt", attachments[0].Name)
	assert.Contains(t, attachments[0].Url, "/attachments/")
	assert.Contains(t, string(files[open.Key+".md"]), "attachments:\n  - notes.txt")

	_, _, files = e.exportOf(t, admin, "ALPHA")
	assert.Zero(t, manifestOf(t, files).ConfidentialNotIncluded)
	assert.Contains(t, string(files[secret.Key+".md"]), "\nconfidential: true\n")
	var links []apigen.ExportLink
	require.NoError(t, json.Unmarshal(files["links.json"], &links))
	assert.Len(t, links, 1)

	for _, c := range []caller{{Token: e.tk.ViewerA}, {Token: e.tk.AgentA}} {
		r, _, _ := e.exportOf(t, c, "ALPHA")
		assert.Equal(t, http.StatusOK, r.StatusCode, "whoever reads the project exports it, an agent too")
	}
	r, _, _ := e.exportOf(t, caller{Token: e.tk.MemberB}, "ALPHA")
	assertProblem(t, r, http.StatusNotFound, "not_found")
	r, _, _ = e.exportOf(t, member, "GAMMA")
	assertProblem(t, r, http.StatusNotFound, "not_found")

	_, _, tenant := e.exportOf(t, member, "")
	tm := manifestOf(t, tenant)
	assert.Equal(t, []apigen.ExportManifestProject{{Key: "ALPHA", Name: "Alpha", Tickets: 1, ConfidentialNotIncluded: 1}}, tm.Projects,
		"a restricted project the reader cannot see is absent, without a count")
	_, _, tenant = e.exportOf(t, admin, "")
	tm = manifestOf(t, tenant)
	assert.Len(t, tm.Projects, 2)
	assert.Equal(t, 3, tm.Tickets)
	assert.Contains(t, tenant, e.SlugA+"/GAMMA-1.md")

	for _, c := range []struct {
		entity string
		want   int64
	}{{"project", 4}, {"tenant", 2}} {
		n, err := f.QueryCount(e.ctx, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND action = 'exported' AND entity_type = $2`,
			e.A, c.entity)
		require.NoError(t, err)
		assert.Equal(t, c.want, n, "every export is a recorded act (docs/adr/0059 D3)")
	}
}

// docs/adr/0051 D4, D7: the export streams its archive and reads the tickets
// a page at a time, so a project whose documents hold far more than the
// export may take is exported whole while the replica's heap grows by a
// fraction of it — the archive complete, the manifest's count the documents'.
func TestTheExportStreamsALargeProjectWithinAMemoryBound(t *testing.T) {
	e := newTicketEnv(t)
	const tickets, bodyChars = 600, 32 * 6000
	require.NoError(t, fixtures(t).Exec(e.ctx, `WITH n AS (UPDATE ticket_counters SET last_number = last_number + $3
	        WHERE tenant_id = $1 AND project_id = $2 RETURNING last_number)
	    INSERT INTO tickets (tenant_id, project_id, number, type, title, body, severity, security, urgency_derived,
	                         urgency_rule, effort, reporter_id)
	    SELECT $1, $2, n.last_number - $3 + g, 'task', 'bulk ' || g, repeat(md5(g::text), 6000), 'medium', 'none',
	           'later', 'v2:default', 'S', $4
	    FROM n, generate_series(1, $3) g`, e.A, e.ProjectA, tickets, e.AdminA))
	// The archive is read as it arrives, never held: the validation of the
	// other tests would buffer it.
	s := newAPI(t, func(o *api.Options) { o.ValidateResponses = false })

	defer debug.SetGCPercent(debug.SetGCPercent(10))
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak atomic.Uint64
	stop, sampled := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
		}
	}()
	res := s.do(t, caller{Token: e.tk.AdminA}, http.MethodGet, fmt.Sprintf("/api/v1/teams/%s/projects/ALPHA/export", e.SlugA), nil)
	require.Equal(t, http.StatusOK, res.StatusCode)
	gz, err := gzip.NewReader(res.Body)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	docs, chars := 0, 0
	var m apigen.ExportManifest
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if h.Name == "manifest.json" {
			require.NoError(t, json.NewDecoder(tr).Decode(&m))
			continue
		}
		n, err := io.Copy(io.Discard, tr)
		require.NoError(t, err)
		if strings.HasSuffix(h.Name, ".md") {
			docs++
			chars += int(n)
		}
	}
	close(stop)
	<-sampled

	assert.Equal(t, tickets, docs)
	assert.Equal(t, tickets, m.Tickets)
	assert.Greater(t, chars, tickets*bodyChars, "every document carries its whole body")
	grown := int64(peak.Load()) - int64(base.HeapAlloc)
	t.Logf("the heap grew by %d bytes exporting %d bytes of bodies", grown, tickets*bodyChars)
	assert.Less(t, grown, int64(tickets*bodyChars/4),
		"the heap grew by %d bytes exporting %d bytes of bodies: the export holds a page, not the archive", grown, tickets*bodyChars)
}
