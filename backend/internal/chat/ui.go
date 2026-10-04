package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/tools"
)

type openTicketInput struct {
	Key string `json:"key" jsonschema:"the ticket, PROJECT-n or tenant/PROJECT-n"`
}

type openProjectInput struct {
	Project string `json:"project,omitempty" jsonschema:"the project's key; the project of the person's page when left out"`
}

// uiTools are the chat's own tools: each shows the person a page of this UI —
// a ticket, a project's backlog, its board — once the API has said the person
// may see it, and only in the turn's tenant.
func uiTools(t Turn, ev Events) []tools.Tool {
	board := func(view string) func(ctx context.Context, s *tools.Session, in openProjectInput) (string, error) {
		return func(ctx context.Context, s *tools.Session, in openProjectInput) (string, error) {
			key := strings.TrimSpace(in.Project)
			if key == "" {
				key = t.Page.Project
			}
			if k, ok := strings.CutPrefix(key, t.Tenant+"/"); ok {
				key = k
			}
			if !domain.ValidProjectKey(key) {
				return "", tools.Usage("name the project by its key, such as COW; the person's page shows none")
			}
			res, err := s.API.GetProjectWithResponse(ctx, t.Tenant, key)
			if err != nil {
				return "", err
			}
			if res.StatusCode() != http.StatusOK || res.JSON200 == nil {
				return "", &tools.APIError{Status: res.StatusCode(), Problem: res.ApplicationproblemJSONDefault, Body: string(res.Body)}
			}
			ev.UI("/t/" + t.Tenant + "/p/" + key + "/" + view)
			return fmt.Sprintf("Opened the %s of %s/%s — %s on the person's screen.", view, t.Tenant, key, res.JSON200.Name), nil
		}
	}
	return []tools.Tool{
		tools.Define(tools.Tool{
			Name:        "open_ticket",
			Description: "Show the person a ticket's page in cowork, in this tenant. Answers whether it was opened.",
			ReadOnly:    true,
			Operations:  []string{"getTicket"},
		}, nil, func(ctx context.Context, s *tools.Session, in openTicketInput) (string, error) {
			k, err := domain.ParseTicketKey(strings.TrimSpace(in.Key))
			switch {
			case err != nil:
				return "", tools.Usage("%q is not a ticket key: write PROJECT-n", in.Key)
			case k.Tenant != "" && k.Tenant != t.Tenant:
				return "", tools.Usage("the chat shows the pages of the tenant %s only", t.Tenant)
			}
			res, err := s.API.GetTicketWithResponse(ctx, t.Tenant, k.Project, int(k.Number))
			if err != nil {
				return "", err
			}
			if res.StatusCode() != http.StatusOK || res.JSON200 == nil {
				return "", &tools.APIError{Status: res.StatusCode(), Problem: res.ApplicationproblemJSONDefault, Body: string(res.Body)}
			}
			ev.UI("/t/" + t.Tenant + "/tickets/" + domain.ShortKey(k.Project, k.Number))
			return fmt.Sprintf("Opened %s — %s on the person's screen.", res.JSON200.Key, res.JSON200.Title), nil
		}),
		tools.Define(tools.Tool{
			Name:        "open_backlog",
			Description: "Show the person a project's backlog in cowork, in this tenant: the project of the page when none is named.",
			ReadOnly:    true,
			Operations:  []string{"getProject"},
		}, nil, board("backlog")),
		tools.Define(tools.Tool{
			Name:        "open_board",
			Description: "Show the person a project's board in cowork, in this tenant: the project of the page when none is named.",
			ReadOnly:    true,
			Operations:  []string{"getProject"},
		}, nil, board("board")),
	}
}
