package mcpcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

// tokenReport is what token check finds (docs/adr/0070 D2).
type tokenReport struct {
	Installation string   `json:"installation"`
	Version      string   `json:"version"`
	Valid        bool     `json:"valid"`
	Problem      string   `json:"problem,omitempty"`
	Person       string   `json:"person,omitempty"`
	Username     string   `json:"username,omitempty"`
	Name         string   `json:"name,omitempty"`
	Scope        string   `json:"scope,omitempty"`
	AgentToken   bool     `json:"agent_token"`
	Tenant       string   `json:"tenant,omitempty"`
	Project      string   `json:"project,omitempty"`
	Capabilities []string `json:"capabilities"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
	TokenPage    string   `json:"token_page"`
}

// tokenCheck reports whether COWORK_TOKEN works against COWORK_URL, whose it
// is, its scope, restriction and capabilities and when it expires — the
// first step of every troubleshooting (docs/adr/0070 D2). It asks with the
// agent header the server sends, so the capabilities are the server's.
func tokenCheck(ctx context.Context, e Env, jsonOut bool) int {
	cfg, err := loadConfig(e.Lookup)
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitError
	}
	ctx, cancel := context.WithTimeout(ctx, startBudget)
	defer cancel()
	c := connect(e, cfg, "cowork-mcp", "", "token-check")
	r := tokenReport{Installation: cfg.url, TokenPage: c.session.TokenPage(), Capabilities: []string{}}
	version, verr := c.session.API.GetVersionWithResponse(ctx)
	if verr == nil && version.JSON200 != nil {
		r.Version = version.JSON200.Version
	}
	tok, err := c.session.ReadToken(ctx)
	if err == nil {
		var me *apigen.GetMeResponse
		if me, err = c.session.API.GetMeWithResponse(ctx); err == nil && me.JSON200 != nil {
			r.Person = me.JSON200.DisplayName
			r.Username, _ = me.JSON200.Username.Get()
		}
	}
	var api *tools.APIError
	switch {
	case errors.As(err, &api):
		r.Problem = api.Error()
	case err != nil:
		r.Problem = fmt.Sprintf("%s cannot be reached: %v", cfg.url, err)
	default:
		r.Valid, r.Name, r.Scope, r.AgentToken = true, tok.Name, tok.Scope, tok.Flagged
		r.Tenant, r.Project, r.Capabilities = tok.Tenant, tok.Project, append(r.Capabilities, tok.Capabilities...)
		r.ExpiresAt = tok.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if jsonOut {
		enc := json.NewEncoder(e.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
	} else {
		writeTokenReport(e.Stdout, r, tok)
	}
	if !r.Valid {
		return exitError
	}
	return exitOK
}

func writeTokenReport(w io.Writer, r tokenReport, tok tools.Token) {
	if !r.Valid {
		fmt.Fprintf(w, "The token in COWORK_TOKEN does not work against %s: %s\nMake a new one on %s.\n", r.Installation, r.Problem, r.TokenPage)
		return
	}
	fmt.Fprintf(w, "The token works against %s", r.Installation)
	if r.Version != "" {
		fmt.Fprintf(w, " (cowork %s)", r.Version)
	}
	fmt.Fprintln(w, ".")
	person := r.Person
	if r.Username != "" {
		person += " (" + r.Username + ")"
	}
	kind := "a plain token"
	if r.AgentToken {
		kind = "an agent token"
	}
	restriction := "none"
	switch {
	case r.Project != "":
		restriction = "the project " + r.Tenant + "/" + r.Project
	case r.Tenant != "":
		restriction = "the tenant " + r.Tenant
	}
	caps := strings.Join(r.Capabilities, ", ")
	if caps == "" {
		caps = "none beyond the baseline"
	}
	left := time.Until(tok.ExpiresAt).Round(24 * time.Hour)
	fmt.Fprintf(w, "Person:        %s\nToken:         %s, scope %s, %s\nRestriction:   %s\n", person, r.Name, r.Scope, kind, restriction)
	fmt.Fprintf(w, "As an agent:   the requests of cowork-mcp are an agent's; capabilities: %s\n", caps)
	fmt.Fprintf(w, "Expires:       %s (in %d days)\nToken page:    %s\n", tok.ExpiresAt.UTC().Format(time.DateOnly), int(left.Hours()/24), r.TokenPage)
}

// lookupReport is what lookup finds (docs/adr/0070 D2).
type lookupReport struct {
	Bound   bool                     `json:"bound"`
	Binding *tools.Binding           `json:"binding,omitempty"`
	Remotes []tools.Remote           `json:"remotes"`
	File    *tools.BindingFile       `json:"binding_file,omitempty"`
	Lookup  *apigen.RepositoryLookup `json:"lookup,omitempty"`
	Notes   []string                 `json:"notes"`
}

// lookupBinding prints the binding of the working directory's remotes, or
// the proposal, without starting a session: the memory is not written
// (docs/adr/0070 D2).
func lookupBinding(ctx context.Context, e Env, jsonOut bool) int {
	cfg, err := loadConfig(e.Lookup)
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitError
	}
	ctx, cancel := context.WithTimeout(ctx, startBudget)
	defer cancel()
	c := connect(e, cfg, "cowork-mcp", "", "lookup")
	sit, err := tools.Resolve(ctx, c.session)
	if err != nil {
		fmt.Fprintf(e.Stderr, "cowork-mcp: %v\n", err)
		return exitError
	}
	r := lookupReport{Bound: sit.Binding != nil, Binding: sit.Binding, Remotes: sit.Remotes, File: sit.File, Lookup: sit.Lookup, Notes: sit.Notes}
	if r.Remotes == nil {
		r.Remotes = []tools.Remote{}
	}
	if r.Notes == nil {
		r.Notes = []string{}
	}
	if jsonOut {
		enc := json.NewEncoder(e.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
		return exitOK
	}
	writeLookup(e.Stdout, sit)
	return exitOK
}

func writeLookup(w io.Writer, sit tools.Situation) {
	for _, r := range sit.Remotes {
		fmt.Fprintf(w, "Remote %s: %s\n", r.Name, r.URL)
	}
	if sit.File != nil {
		fmt.Fprintf(w, "Binding file: %s (%s/%s)\n", sit.File.File, sit.File.Tenant, sit.File.Project)
	}
	if b := sit.Binding; b != nil {
		fmt.Fprintf(w, "Bound to %s (%s) by the %s", b.Key(), b.ProjectName, b.Source)
		if b.Identity != "" {
			fmt.Fprintf(w, " %s", b.Identity)
		}
		if b.Path != "" {
			fmt.Fprintf(w, ", sub-directory %s", b.Path)
		}
		fmt.Fprintln(w, ".")
		if b.Drift != "" {
			fmt.Fprintf(w, "Drift: %s.\n", b.Drift)
		}
	} else {
		fmt.Fprintln(w, "Bound to no project.")
		if l := sit.Lookup; l != nil {
			describeUnbound(w, l)
		}
	}
	for _, n := range sit.Notes {
		fmt.Fprintln(w, n)
	}
}

func describeUnbound(w io.Writer, l *apigen.RepositoryLookup) {
	if l.Status == apigen.RepositoryLookupStatusAmbiguous {
		for _, b := range l.Bindings {
			fmt.Fprintf(w, "Several projects bind it: %s/%s\n", b.Tenant.Slug, b.Project.Key)
		}
		return
	}
	if p, err := l.Proposal.Get(); err == nil {
		fmt.Fprintf(w, "Proposal: a project %q for %s", p.Name, p.Identity)
		for _, t := range p.Tenants {
			fmt.Fprintf(w, "; in %s as %s", t.Slug, t.Key)
		}
		fmt.Fprintf(w, " (%s).\n", p.Reason)
	} else if why, err := l.ProposalUnavailable.Get(); err == nil {
		fmt.Fprintf(w, "No proposal: %s.\n", why)
	}
}
