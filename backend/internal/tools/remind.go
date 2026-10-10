package tools

import (
	"context"
	"fmt"
	"net/http"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
)

// Remind is the Stop hook's check (docs/adr/0067 D4): a ticket of the bound
// project assigned to the person and in progress, on which the person
// recorded nothing — no comment, no change of the body, no transition, no act
// at all — since the session started, while the repository shows work since
// then. It returns the one-line reminder, or "" — a hint, never a veto. A
// session that only read is not reminded: without a commit or a changed file
// since the start there is nothing to record.
func Remind(ctx context.Context, s *Session) (string, error) {
	sit, err := Resolve(ctx, s)
	if err != nil || sit.Binding == nil || s.Memory == nil {
		return "", err
	}
	b := *sit.Binding
	started, ok, err := s.Memory.LastStart(MemoryKey{Installation: s.Installation, Team: b.Team, Project: b.Project})
	if err != nil || !ok {
		return "", err
	}
	if worked, err := s.Workspace.WorkedSince(ctx, started); err == nil && !worked {
		return "", nil
	}
	mine, err := listTickets(ctx, s, b.Team, b.Project, ticketQuery{states: []string{stateInProgress}, assignee: "me", limit: startOtherActive})
	if err != nil || len(mine) == 0 {
		return "", err
	}
	me, err := s.API.GetMeWithResponse(ctx)
	if err := check(me, err, http.StatusOK); err != nil {
		return "", err
	}
	for _, t := range mine {
		ref, err := s.resolveKey(t.Key)
		if err != nil {
			return "", err
		}
		acts, err := activitySince(ctx, s, ref, started)
		if err != nil {
			return "", err
		}
		if !actedOn(acts, me.JSON200.Id.String()) {
			return fmt.Sprintf("%s is still in progress and nothing was recorded on it since this session started (%s) — "+
				"record_state, or finish_work with a verification note?", t.Key, stamp(started)), nil
		}
	}
	return "", nil
}

// actedOn reports whether the person did anything on the ticket among the
// acts.
func actedOn(acts []apigen.Activity, person string) bool {
	for _, a := range acts {
		if p, err := a.Actor.Get(); err == nil && p.Id.String() == person {
			return true
		}
	}
	return false
}
