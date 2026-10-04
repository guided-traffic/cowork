package store

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// The reasons a notification gives its person (docs/adr/0020 D2), the values
// of notification_reason.
const (
	NoticeAssigned      = "assigned"
	NoticeAsked         = "asked"
	NoticeAnswered      = "answered"
	NoticeStateChanged  = "state_changed"
	NoticeBlockerClosed = "blocker_closed"
	NoticeCommented     = "commented"
	NoticeUrgent        = "urgent"
)

// EntityInbox is the entity of a published change of a person's inbox: a
// notification for them, or theirs marked read. Only that person's
// person-level streams hear it, as inbox.changed (docs/adr/0054 D1, D2).
const EntityInbox = "inbox"

// ReadRetention is how long a read notification is kept; an unread one stays
// (docs/adr/0020 D6).
const ReadRetention = 90 * 24 * time.Hour

// Notice is one reason an act tells persons in their inbox (docs/adr/0020 D2):
// the persons it names, the watchers of its ticket, or — for a ticket that
// reached done or dropped — the watchers of every ticket it blocks, each told
// about the ticket they watch. Mutate leaves out the actor and every person
// who is no active member or cannot see the ticket the notification is about
// and the ticket the act is on (NoticeRecipients).
type Notice struct {
	Reason string
	// People are the persons the act names: the assignee, the person asked,
	// the asker.
	People []uuid.UUID
	// Watchers adds the watchers of the act's ticket (docs/adr/0013 D6).
	Watchers bool
	// Blocked tells the watchers of the tickets the act's ticket blocks
	// instead, about those tickets.
	Blocked bool
}

// deliver writes the notifications of an act in its transaction, referencing
// its audit row (docs/adr/0020 D3), and tells every person whose inbox
// changed — those it notified, and the person of InboxOf — on their
// person-level streams.
func (w *Writer) deliver(ctx context.Context, tenantID, auditID uuid.UUID, caller Caller, e Event) error {
	if tenantID == uuid.Nil {
		return nil
	}
	changed := map[uuid.UUID]bool{}
	if e.InboxOf != uuid.Nil {
		changed[e.InboxOf] = true
	}
	for _, n := range e.Notices {
		if e.TicketID == uuid.Nil {
			return fmt.Errorf("store: a notice of %s on %s names no ticket", e.Action, e.EntityType)
		}
		about := []uuid.UUID{e.TicketID}
		if n.Blocked {
			var err error
			if about, err = w.ListBlockedTickets(ctx, writeq.ListBlockedTicketsParams{TenantID: tenantID, TicketID: e.TicketID}); err != nil {
				return fmt.Errorf("read the tickets a ticket blocks: %w", err)
			}
		}
		for _, ticket := range about {
			told, err := w.tell(ctx, tenantID, auditID, caller, e.TicketID, ticket, n)
			if err != nil {
				return err
			}
			for _, person := range told {
				changed[person] = true
			}
		}
	}
	for _, person := range slices.SortedFunc(maps.Keys(changed), func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }) {
		if err := w.notify(ctx, Notification{ID: auditID, Tenant: tenantID, Entity: EntityInbox, Person: &person}); err != nil {
			return err
		}
	}
	return nil
}

// tell writes one notice's notifications about one ticket and returns whom it
// told.
func (w *Writer) tell(ctx context.Context, tenantID, auditID uuid.UUID, caller Caller, actTicket, ticket uuid.UUID, n Notice) ([]uuid.UUID, error) {
	people := slices.Clone(n.People)
	if n.Watchers || n.Blocked {
		watchers, err := w.ListWatchers(ctx, writeq.ListWatchersParams{TenantID: tenantID, TicketID: ticket})
		if err != nil {
			return nil, fmt.Errorf("read the watchers: %w", err)
		}
		people = append(people, watchers...)
	}
	if len(people) == 0 {
		return nil, nil
	}
	recipients, err := w.NoticeRecipients(ctx, writeq.NoticeRecipientsParams{People: people, Actor: uuidPtr(caller.UserID),
		TenantID: tenantID, TicketID: ticket, ActTicketID: actTicket})
	if err != nil {
		return nil, fmt.Errorf("read the recipients: %w", err)
	}
	if len(recipients) == 0 {
		return nil, nil
	}
	if err := w.InsertNotifications(ctx, writeq.InsertNotificationsParams{TenantID: tenantID, TicketID: ticket,
		AuditEventID: auditID, Reason: n.Reason, Recipients: recipients}); err != nil {
		return nil, fmt.Errorf("write the notifications: %w", err)
	}
	return recipients, nil
}

// ExpireNotifications deletes the notifications read longer than
// ReadRetention ago (docs/adr/0020 D6) and records one act per run that
// removed any.
func (db *DB) ExpireNotifications(ctx context.Context, now time.Time) (removed int64, err error) {
	_, err = db.RunJob(ctx, "notification-expiry", 5, func(w *Writer) error {
		before := now.Add(-ReadRetention)
		n, err := w.DeleteReadNotifications(ctx, before)
		if err != nil {
			return fmt.Errorf("delete read notifications: %w", err)
		}
		removed = n
		if n == 0 {
			return nil
		}
		w.Record(Event{EntityType: "notifications", Action: actionExpired, After: map[string]int64{fieldRemoved: n}})
		return nil
	})
	return removed, err
}
