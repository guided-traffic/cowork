package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

func sessionStartTool() Tool {
	return define(Tool{
		Name:    "session_start",
		Surface: Terminal,
		Description: "Read where this session stands: the project the repository is bound to — by its git remote or a " +
			".cowork.yaml — or the proposal to create one; the person's active ticket with its context, or the top of the " +
			"backlog; and what happened since the last session. The SessionStart hook shows the same at the start; call it " +
			"again to refresh.",
		Operations: []string{"lookupRepository", "getProject", "listProjectTickets", "listMyNext", "exportTicketContext",
			"listActivity", "listTeamTickets", opGetTicket},
	}, nil, func(ctx context.Context, s *Session, _ struct{}) (string, error) {
		text, silent, err := Start(ctx, s, StartOptions{Record: true})
		if silent {
			text = "This directory is bound to no project: it has no git remote and no .cowork.yaml. The tools still " +
				"reach any ticket by its full key, team/PROJECT-n."
		}
		return text, err
	})
}

// createProjectInput takes team, required as the agent reads the schema, and
// tenant, its name before, deprecated and taken in its place for one release
// (docs/adr/0005 D1): Call moves tenant to team before the schema holds the
// arguments, and refuses the two when they differ. The schema keeps team
// required rather than offer the one of the two as an anyOf at its top level,
// which a model's API may refuse in a tool's input schema.
type createProjectInput struct {
	Team   string `json:"team" jsonschema:"the team's slug"`
	Tenant string `json:"tenant,omitempty" jsonschema:"deprecated: the team's slug under its name before, taken for one release in place of team; give team"`
	Key    string `json:"key" jsonschema:"the project key: upper case, 2 to 10 letters and digits, starting with a letter"`
	Name   string `json:"name"`
	Remote string `json:"remote" jsonschema:"the git remote to bind, as the repository has it"`
	Path   string `json:"path,omitempty" jsonschema:"a sub-directory of a monorepo the project stands for"`
}

func createProjectTool() Tool {
	return define(Tool{
		Name: "create_project",
		Description: "Create a project and bind a repository to it, in one recorded act (docs/adr/0066 D3, D5) — only after " +
			"the person said yes to the proposal session_start showed. Idempotent over the remote: a repository the team " +
			"binds already answers with its project and creates nothing. Never archives, restricts or deletes.",
		Operations: []string{"createProject"},
		limits: limitsOf("Creating a project needs the create-project capability and a team where the person may create "+
			"projects. "+refusalNote, capCreateProject),
		renamed: []renamedArgument{{now: "team", before: "tenant"}},
	}, func(s *jsonschema.Schema) {
		s.Properties["key"].Pattern = `^[A-Z][A-Z0-9]{1,9}$`
		s.Properties["team"].Pattern = `^[a-z0-9][a-z0-9-]{1,62}$`
		s.Properties["tenant"].Pattern = s.Properties["team"].Pattern
		s.Properties["tenant"].Deprecated = true
		minLen := 1
		s.Properties["name"].MinLength = &minLen
		s.Properties["remote"].MinLength = &minLen
	}, func(ctx context.Context, s *Session, in createProjectInput) (string, error) {
		body := apigen.ProjectCreate{Key: in.Key, Name: strings.TrimSpace(in.Name), Repository: &apigen.RepositoryBind{Remote: in.Remote}}
		if in.Path != "" {
			body.Repository.Path = &in.Path
		}
		res, err := s.API.CreateProjectWithResponse(ctx, in.Team, &apigen.CreateProjectParams{IdempotencyKey: s.key()}, body)
		if err := check(res, err, http.StatusCreated, http.StatusOK); err != nil {
			return "", err
		}
		p := res.JSON201
		created := p != nil
		if !created {
			p = res.JSON200
		}
		if s.Binding() == nil {
			s.setBinding(&Binding{Team: in.Team, Project: p.Key, ProjectName: p.Name, Source: sourceRemote, Remote: in.Remote, Path: in.Path})
		}
		var out strings.Builder
		if created {
			fmt.Fprintf(&out, "Created the project %s/%s (%s) and bound the repository %s to it. This session is bound to it.\n",
				in.Team, p.Key, p.Name, in.Remote)
		} else {
			fmt.Fprintf(&out, "The repository is bound already, to %s/%s (%s); nothing was created.\n", in.Team, p.Key, p.Name)
		}
		// The file outlives this release, so it names the team under the key
		// every cowork-mcp of the rollback window reads: tenant, which this
		// release reads as the deprecated name of team, while the release before
		// refuses a file that names team and drops the binding
		// (docs/adr/0005 D1). The contract release offers team.
		fmt.Fprintf(&out, "\nA .cowork.yaml is not needed: the remote binds the repository. Offer one only for a fork or a "+
			"repository without a remote, and write it only on the person's yes, as it stands — tenant is the key every "+
			"cowork-mcp in use reads (docs/adr/0066 D4):\n\n```yaml\ntenant: %s\nproject: %s\n```\n",
			in.Team, p.Key)
		return out.String(), nil
	})
}
