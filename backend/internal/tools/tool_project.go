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
			"listActivity", "listTenantTickets", opGetTicket},
	}, nil, func(ctx context.Context, s *Session, _ struct{}) (string, error) {
		text, silent, err := Start(ctx, s, StartOptions{Record: true})
		if silent {
			text = "This directory is bound to no project: it has no git remote and no .cowork.yaml. The tools still " +
				"reach any ticket by its full key, tenant/PROJECT-n."
		}
		return text, err
	})
}

type createProjectInput struct {
	Tenant string `json:"tenant" jsonschema:"the tenant's slug"`
	Key    string `json:"key" jsonschema:"the project key: upper case, 2 to 10 letters and digits, starting with a letter"`
	Name   string `json:"name"`
	Remote string `json:"remote" jsonschema:"the git remote to bind, as the repository has it"`
	Path   string `json:"path,omitempty" jsonschema:"a sub-directory of a monorepo the project stands for"`
}

func createProjectTool() Tool {
	return define(Tool{
		Name: "create_project",
		Description: "Create a project and bind a repository to it, in one recorded act (docs/adr/0066 D3, D5) — only after " +
			"the person said yes to the proposal session_start showed. Idempotent over the remote: a repository the tenant " +
			"binds already answers with its project and creates nothing. Never archives, restricts or deletes.",
		Operations: []string{"createProject"},
		limits: limitsOf("Creating a project needs the create-project capability and a tenant where the person may create "+
			"projects. "+refusalNote, capCreateProject),
	}, func(s *jsonschema.Schema) {
		s.Properties["key"].Pattern = `^[A-Z][A-Z0-9]{1,9}$`
		s.Properties["tenant"].Pattern = `^[a-z0-9][a-z0-9-]{1,62}$`
		minLen := 1
		s.Properties["name"].MinLength = &minLen
		s.Properties["remote"].MinLength = &minLen
	}, func(ctx context.Context, s *Session, in createProjectInput) (string, error) {
		body := apigen.ProjectCreate{Key: in.Key, Name: strings.TrimSpace(in.Name), Repository: &apigen.RepositoryBind{Remote: in.Remote}}
		if in.Path != "" {
			body.Repository.Path = &in.Path
		}
		res, err := s.API.CreateProjectWithResponse(ctx, in.Tenant, &apigen.CreateProjectParams{IdempotencyKey: s.key()}, body)
		if err := check(res, err, http.StatusCreated, http.StatusOK); err != nil {
			return "", err
		}
		p := res.JSON201
		created := p != nil
		if !created {
			p = res.JSON200
		}
		if s.Binding() == nil {
			s.setBinding(&Binding{Tenant: in.Tenant, Project: p.Key, ProjectName: p.Name, Source: sourceRemote, Remote: in.Remote, Path: in.Path})
		}
		var out strings.Builder
		if created {
			fmt.Fprintf(&out, "Created the project %s/%s (%s) and bound the repository %s to it. This session is bound to it.\n",
				in.Tenant, p.Key, p.Name, in.Remote)
		} else {
			fmt.Fprintf(&out, "The repository is bound already, to %s/%s (%s); nothing was created.\n", in.Tenant, p.Key, p.Name)
		}
		fmt.Fprintf(&out, "\nA .cowork.yaml is not needed: the remote binds the repository. Offer one only for a fork or a "+
			"repository without a remote, and write it only on the person's yes (docs/adr/0066 D4):\n\n```yaml\ntenant: %s\nproject: %s\n```\n",
			in.Tenant, p.Key)
		return out.String(), nil
	})
}
