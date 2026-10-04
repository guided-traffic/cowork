package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// EventChannel is the channel the committed acts are published on
// (docs/adr/0054 D4).
const EventChannel = "cowork_events"

// Notification is a published act: what a stream filters on and what it
// tells — a key and a version, never content (docs/adr/0054 D2, D3). The
// payload stays far below PostgreSQL's 8000 bytes.
type Notification struct {
	// ID is the audit row's id, the event id a client replays from.
	ID           uuid.UUID  `json:"id"`
	Tenant       uuid.UUID  `json:"tenant"`
	Project      uuid.UUID  `json:"project"`
	Entity       string     `json:"entity"`
	Action       string     `json:"action"`
	Key          string     `json:"key"`
	Version      int32      `json:"version"`
	Confidential bool       `json:"confidential"`
	Assignee     *uuid.UUID `json:"assignee,omitempty"`
	Reporter     uuid.UUID  `json:"reporter"`
	// Person and Mapping are the keys of a membership act besides Project;
	// Audience says who of the tenant may hear of it (MembershipChange).
	Person   *uuid.UUID `json:"person,omitempty"`
	Mapping  *uuid.UUID `json:"mapping,omitempty"`
	Audience string     `json:"audience,omitempty"`
}

// EntityMembership is the entity of every notification of a membership act:
// a grant, a derived membership, a group mapping, a project's restriction or
// access list (docs/adr/0054 D2).
const EntityMembership = "membership"

// EntityProject is the entity of the notification of a project's creation,
// which changes what the tenant's streams may admit and is sent to no client
// (docs/adr/0054 D3).
const EntityProject = "project"

// The audiences of a membership act. Every member of the tenant hears of a
// membership or a project's restriction — the member list is theirs to read
// anyway, and a project that is restricted or opened was visible to them at
// one of the two moments; only the administrators hear of a mapping, which
// only they read; and an access entry reaches the administrators and the
// person it names, never the members who do not see the project
// (docs/adr/0034 D3).
const (
	AudienceMembers         = "members"
	AudienceAdmins          = "admins"
	AudienceAdminsAndPerson = "admins-and-person"
)

// MembershipChange is what a membership act announces: the keys of what
// changed, uuid.Nil where one does not apply, and who may hear of it.
type MembershipChange struct {
	Person, Project, Mapping uuid.UUID
	Audience                 string
}

// silent are the acts a stream does not carry: data leaving the system
// changes nothing a client shows, and time follows its own visibility
// (docs/adr/0026 D5, docs/adr/0034 D5).
var silent = map[string]bool{"downloaded": true, "exported": true, "time_entry": true}

// publish notifies the listeners of a ticket's act, a membership act or a
// project's creation in a tenant. NOTIFY inside the transaction is delivered when it commits and
// never when it rolls back (docs/adr/0054 D4).
func (w *Writer) publish(ctx context.Context, tenantID, id uuid.UUID, e Event) error {
	if tenantID != uuid.Nil && e.Membership != nil {
		return w.notify(ctx, membershipNotification(tenantID, id, e))
	}
	if tenantID != uuid.Nil && e.NewProject != uuid.Nil {
		return w.notify(ctx, Notification{ID: id, Tenant: tenantID, Project: e.NewProject, Entity: EntityProject, Action: e.Action})
	}
	if tenantID == uuid.Nil || e.TicketID == uuid.Nil || silent[e.Action] || silent[e.EntityType] {
		return nil
	}
	facts, err := w.TicketFacts(ctx, writeq.TicketFactsParams{TenantID: tenantID, ID: e.TicketID})
	if err != nil {
		return fmt.Errorf("read the published ticket: %w", err)
	}
	return w.notify(ctx, Notification{ID: id, Tenant: tenantID, Project: facts.ProjectID, Entity: e.EntityType,
		Action: e.Action, Key: e.TicketKey, Version: facts.Version, Confidential: facts.Confidential,
		Assignee: facts.AssigneeID, Reporter: facts.ReporterID})
}

func membershipNotification(tenantID, id uuid.UUID, e Event) Notification {
	m := e.Membership
	n := Notification{ID: id, Tenant: tenantID, Project: m.Project, Entity: EntityMembership, Action: e.Action,
		Audience: m.Audience}
	if m.Person != uuid.Nil {
		n.Person = &m.Person
	}
	if m.Mapping != uuid.Nil {
		n.Mapping = &m.Mapping
	}
	return n
}

func (w *Writer) notify(ctx context.Context, n Notification) error {
	payload, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("encode the notification: %w", err)
	}
	if _, err := w.tx.Exec(ctx, "SELECT pg_notify($1, $2)", EventChannel, string(payload)); err != nil {
		return fmt.Errorf("publish the act: %w", err)
	}
	return nil
}

// Listen holds one connection outside the pool on the channel and hands
// every notification to deliver until ctx ends (docs/adr/0054 D4). up is told
// true once listening and false while the connection is lost; it reconnects
// with a growing pause.
func (db *DB) Listen(ctx context.Context, deliver func(Notification), up func(bool)) {
	pause := time.Second
	for ctx.Err() == nil {
		listening, err := db.listenOnce(ctx, deliver, up)
		if ctx.Err() != nil {
			return
		}
		up(false)
		db.logger.Warn("the event listener lost its connection", "error", err)
		if listening {
			pause = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
		pause = min(2*pause, 30*time.Second)
	}
}

func (db *DB) listenOnce(ctx context.Context, deliver func(Notification), up func(bool)) (bool, error) {
	conn, err := pgx.ConnectConfig(ctx, db.pool.Config().ConnConfig.Copy())
	if err != nil {
		return false, fmt.Errorf("connect the listener: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	if _, err := conn.Exec(ctx, "LISTEN "+EventChannel); err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}
	up(true)
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, fmt.Errorf("wait for a notification: %w", err)
		}
		var msg Notification
		if err := json.Unmarshal([]byte(n.Payload), &msg); err != nil {
			db.logger.Error("an event notification does not decode", "error", err)
			continue
		}
		deliver(msg)
	}
}
