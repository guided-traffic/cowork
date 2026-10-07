package api

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/github"
	"github.com/guided-traffic/cowork/backend/internal/store"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// entityGitHubSecret is the entity of the acts on a tenant's webhook secret.
const entityGitHubSecret = "github_webhook_secret"

// webhookEvents are the events to choose in GitHub's webhook settings
// (docs/adr/0071 D4, D7).
var webhookEvents = []string{github.EventPullRequest, github.EventPush}

// GetGitHubIntegration answers whether the tenant takes GitHub's webhook, for
// its administrators (docs/adr/0071 D1, D7): when and by whom the secret was
// made, never the secret, and where GitHub posts.
func (s *Server) GetGitHubIntegration(ctx context.Context, _ apigen.GetGitHubIntegrationRequestObject) (apigen.GetGitHubIntegrationResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, adminRead); perr != nil {
		return nil, perr
	}
	out := apigen.GitHubIntegration{WebhookPath: webhookPath(t.Slug), Events: webhookEvents,
		Secret: nullableOf[apigen.GitHubSecret](nil)}
	err := s.db.InTenant(ctx, t.ID, func(r *store.Reader) error {
		row, err := r.GetGitHubWebhook(ctx, t.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		v := secretView(row)
		out.Secret = nullableOf(&v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return apigen.GetGitHubIntegration200JSONResponse(out), nil
}

func secretView(row readq.GetGitHubWebhookRow) apigen.GitHubSecret {
	return apigen.GitHubSecret{CreatedAt: row.CreatedAt,
		CreatedBy: personView(row.CreatedBy, row.CreatedByUsername, row.CreatedByName)}
}

// CreateGitHubSecret makes the tenant's webhook secret, or rotates it: an
// administration act in a browser session, never an agent's — the pipeline
// holds the session, administer the role, the scope and the agent
// (docs/adr/0071 D1, docs/adr/0035 D5, docs/adr/0043 D3). The secret is drawn
// here, sealed under the key derived for it and bound to the tenant, and
// answered once; the act records whether one was replaced and never the
// secret.
func (s *Server) CreateGitHubSecret(ctx context.Context, _ apigen.CreateGitHubSecretRequestObject) (apigen.CreateGitHubSecretResponseObject, error) {
	t := tenantFrom(ctx)
	p := principal(ctx)
	if perr := auth.Authorize(p, t.Role, administer); perr != nil {
		return nil, perr
	}
	secret := newWebhookSecret()
	sealed := s.h.webhookSealer.Seal([]byte(secret), t.ID[:])
	var (
		row      readq.GetGitHubWebhookRow
		replaced bool
	)
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		_, err := w.GetGitHubWebhook(ctx, t.ID)
		switch {
		case err == nil:
			replaced = true
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if err := w.SetGitHubWebhookSecret(ctx, writeq.SetGitHubWebhookSecretParams{TenantID: t.ID, Secret: sealed,
			CreatedBy: p.PersonID, CreatedAt: s.h.opts.Now()}); err != nil {
			return err
		}
		if row, err = w.GetGitHubWebhook(ctx, t.ID); err != nil {
			return err
		}
		w.Record(store.Event{EntityType: entityGitHubSecret, EntityID: t.ID, Action: actionCreated,
			After: map[string]any{"replaced": replaced}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	v := secretView(row)
	return apigen.CreateGitHubSecret201JSONResponse{Secret: secret, CreatedAt: v.CreatedAt, CreatedBy: v.CreatedBy,
		Replaced: replaced}, nil
}

// RevokeGitHubSecret removes the tenant's webhook secret: an administration
// act with admin scope, never an agent's; a token may, because it only takes
// access away (docs/adr/0035 D5). From now on every delivery answers like an
// unknown tenant; the links made stay.
func (s *Server) RevokeGitHubSecret(ctx context.Context, _ apigen.RevokeGitHubSecretRequestObject) (apigen.RevokeGitHubSecretResponseObject, error) {
	t := tenantFrom(ctx)
	if perr := auth.Authorize(principal(ctx), t.Role, administer); perr != nil {
		return nil, perr
	}
	_, err := s.db.Mutate(ctx, t.ID, func(w *store.Writer) error {
		n, err := w.DeleteGitHubWebhookSecret(ctx, t.ID)
		if err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNoChange
		}
		w.Record(store.Event{EntityType: entityGitHubSecret, EntityID: t.ID, Action: actionRevoked})
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNoChange) {
		return nil, err
	}
	return apigen.RevokeGitHubSecret204Response{}, nil
}
