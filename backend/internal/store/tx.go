package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// Reader is one read-only transaction. Its embedded queries are the generated
// read-only ones, and the transaction itself is READ ONLY, so nothing reached
// through a Reader can write.
type Reader struct {
	*readq.Queries
	tx pgx.Tx
	// TenantID is the tenant the transaction is bound to; uuid.Nil in an
	// installation transaction, where only the person-scoped policies admit
	// rows.
	TenantID uuid.UUID
	// UserID is the person the transaction reads for.
	UserID uuid.UUID
}

// Writer is a Reader that may also mutate. Only Mutate hands one out, and
// Mutate commits nothing without the acts recorded on it.
type Writer struct {
	*Reader
	*writeq.Queries
	events []Event
	result *Result
	// caller is the system actor of a job, whose acts a job that works tenant
	// by tenant writes in each tenant (RunJob, flushIn); flushed says it wrote
	// some, so the job commits.
	caller  Caller
	flushed bool
	// written are the acts the transaction wrote, which the metrics count once
	// it has committed (countActs).
	written []writtenAct
}

// writtenAct is an act as the metrics count it: its action and who it is
// attributed to (docs/adr/0060 D4).
type writtenAct struct {
	action string
	actor  metrics.Actor
}

// actorOf is who an act is attributed to: a system actor's, a person's agent's
// — a token flagged as an agent's or a request the agent header marks — or the
// person's own (docs/adr/0036).
func actorOf(caller Caller, e Event) metrics.Actor {
	switch {
	case e.System != "" || caller.System != "":
		return metrics.ActorSystem
	case caller.Agent != "":
		return metrics.ActorAgent
	}
	return metrics.ActorPerson
}

// countActs records the acts of a transaction that has committed; the acts of
// one that rolled back are never counted.
func countActs(m *metrics.Metrics, w *Writer) {
	for _, a := range w.written {
		m.Act(a.action, a.actor)
	}
}

// Event is one act, written as one audit row with the caller's facts in the
// mutation's transaction (docs/adr/0026 D1, D2).
type Event struct {
	EntityType string
	EntityID   uuid.UUID
	// TicketID and TicketKey name the ticket the entity belongs to; the key
	// survives the purge of the ticket (docs/adr/0024 D2).
	TicketID  uuid.UUID
	TicketKey string
	// Action is an audit_action value.
	Action string
	// Before and After hold the changed fields only; nil writes NULL.
	Before any
	After  any
	Reason string
	Note   string
	// ExplainedBy is the comment written in the same request
	// (docs/adr/0015 D2).
	ExplainedBy uuid.UUID
	// Refs are the other tickets the payload names; a reader who cannot see
	// one of them reads the act without its payload.
	Refs []uuid.UUID
	// IdempotencyKey is a key the client sent where the act stores no
	// response; it is recorded, not stored (docs/adr/0045 D7). A keyed
	// mutation records its own key on every act instead.
	IdempotencyKey uuid.UUID
	// System names the system actor of an act a request's transaction records
	// on another's behalf — the identity provider's derivation of memberships
	// in an administrator's change of a mapping (docs/adr/0030 D6); empty for
	// the caller's own act.
	System string
	// Membership announces the act on the tenant's event stream as
	// membership.changed (docs/adr/0054 D2); nil for every other act.
	Membership *MembershipChange
	// NewProject is the project the act created: published so that the
	// tenant's streams admit its events before the next one is filtered, and
	// sent to no client (docs/adr/0054 D3); uuid.Nil for every other act.
	NewProject uuid.UUID
	// ProjectRank announces an act on a project's rank as a whole — the sort
	// by the score (docs/adr/0014 D3) — as project.changed; nil for every
	// other act.
	ProjectRank *ProjectChange
	// Notices are whom the act tells in their inbox and why
	// (docs/adr/0020 D2); none for an act that tells nobody.
	Notices []Notice
	// InboxOf is the person whose inbox the act changed without a notice —
	// their own notifications marked read —, whose streams hear it.
	InboxOf uuid.UUID
	// Published is what the act's publication tells of its ticket when the
	// ticket is gone by the time the act is written — a purge's; nil reads the
	// ticket's facts at publication.
	Published *TicketFacts
}

// TicketFacts are what a published act carries of its ticket: the project,
// the version and the confidential rule's inputs (docs/adr/0054 D2, D3).
type TicketFacts struct {
	Project      uuid.UUID
	Version      int32
	Confidential bool
	Assignee     *uuid.UUID
	Reporter     uuid.UUID
}

// Record adds an act to the mutation.
func (w *Writer) Record(e Event) {
	w.events = append(w.events, e)
}

// Result is the response of a keyed POST: stored with its act and replayed
// for a repetition of the same request (docs/adr/0045 D4). It never holds a
// secret or attachment bytes (D6).
type Result struct {
	Status  int               `json:"-"`
	Headers map[string]string `json:"-"`
	Body    []byte            `json:"-"`
}

// Respond sets the response a keyed mutation stores with its act.
func (w *Writer) Respond(r Result) {
	w.result = &r
}

var (
	// ErrNoChange is returned by a mutation's function when the request
	// changes nothing; Mutate rolls back and writes no act.
	ErrNoChange = errors.New("no change")
	// ErrNoAct is returned when a mutation recorded no act; nothing commits.
	ErrNoAct = errors.New("a mutation recorded no act")
	// ErrIdempotencyMismatch is returned when a key comes back with a
	// different request (docs/adr/0045 D4).
	ErrIdempotencyMismatch = errors.New("idempotency key reused with a different request")
)

// InTenant runs fn in a read-only transaction bound to tenantID: the
// transaction's settings carry the tenant and the person, every tenant-bound
// policy admits that tenant's rows only (docs/adr/0021 D3), and the
// visibility predicates read the person from the same settings.
func (db *DB) InTenant(ctx context.Context, tenantID uuid.UUID, fn func(r *Reader) error) error {
	if tenantID == uuid.Nil {
		return errors.New("store: InTenant without a tenant")
	}
	return db.read(ctx, tenantID, fn)
}

// Installation runs fn in a read-only transaction bound to no tenant: only the
// person-scoped policies admit rows — the person's own memberships, tenants
// and tokens (docs/adr/0021 D6).
func (db *DB) Installation(ctx context.Context, fn func(r *Reader) error) error {
	return db.read(ctx, uuid.Nil, fn)
}

func (db *DB) read(ctx context.Context, tenantID uuid.UUID, fn func(r *Reader) error) error {
	caller, _ := CallerFrom(ctx)
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, tenantID, caller, ""); err != nil {
		return err
	}
	if err := fn(newReader(tx, tenantID, caller)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func newReader(tx pgx.Tx, tenantID uuid.UUID, caller Caller) *Reader {
	return &Reader{Queries: readq.New(tx), tx: tx, TenantID: tenantID, UserID: caller.UserID}
}

// setContext writes the transaction-local settings the policies read. An empty
// value leaves a setting unset, which every policy reads as "matches nothing".
func setContext(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, caller Caller, job string) error {
	_, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true),
		set_config('app.user_id', $2, true),
		set_config('app.restricted_project_id', $3, true),
		set_config('app.job', $4, true),
		set_config('app.session_hash', $5, true)`,
		uuidText(tenantID), uuidText(caller.UserID), uuidText(caller.RestrictedProjectID), job,
		hex.EncodeToString(caller.SessionHash))
	if err != nil {
		return fmt.Errorf("set transaction context: %w", err)
	}
	return nil
}

func uuidText(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// Mutate runs fn in a read-write transaction bound to tenantID (uuid.Nil: no
// tenant) and commits it together with one audit row per act fn recorded
// (docs/adr/0027 D3, docs/adr/0026 D2). A function that records no act, or
// fails, commits nothing; a function that finds nothing to change returns
// ErrNoChange, which Mutate hands back so the caller answers without an act.
//
// When the context carries an idempotency key (WithIdempotency), the response
// fn sets with Respond is stored with the act; a repetition of the same
// request returns that stored response without running fn again, and a
// repetition with a different request is ErrIdempotencyMismatch
// (docs/adr/0045 D4). The returned Result is non-nil only for such a replay.
func (db *DB) Mutate(ctx context.Context, tenantID uuid.UUID, fn func(w *Writer) error) (*Result, error) {
	caller, ok := CallerFrom(ctx)
	if !ok || (caller.UserID == uuid.Nil) == (caller.System == "") {
		return nil, errors.New("store: Mutate needs a person or a system caller")
	}
	idem, keyed := idempotencyFrom(ctx)
	if keyed && caller.UserID == uuid.Nil {
		return nil, errors.New("store: an idempotency key needs a person")
	}
	for attempt := 0; ; attempt++ {
		if keyed {
			replay, err := db.replay(ctx, caller, idem)
			if replay != nil || err != nil {
				return replay, err
			}
		}
		stored, err := db.mutateOnce(ctx, tenantID, caller, idem, keyed, fn)
		switch {
		case err != nil:
			return nil, err
		case stored:
			return nil, nil
		case attempt > 0:
			return nil, errors.New("store: the idempotency key is held by a concurrent request")
		}
		// A concurrent request with the same key committed first; the next
		// round replays its response.
	}
}

// mutateOnce runs one attempt of Mutate. stored is false when the act was
// rolled back because a concurrent request stored the same key first.
func (db *DB) mutateOnce(ctx context.Context, tenantID uuid.UUID, caller Caller, idem Idempotency, keyed bool, fn func(w *Writer) error) (stored bool, err error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, tenantID, caller, ""); err != nil {
		return false, err
	}
	w := &Writer{Reader: newReader(tx, tenantID, caller), Queries: writeq.New(tx)}
	if err := fn(w); err != nil {
		return false, err
	}
	if len(w.events) == 0 {
		return false, ErrNoAct
	}
	if err := w.writeEvents(ctx, tenantID, caller, idem, keyed); err != nil {
		return false, err
	}
	if keyed {
		ok, err := w.storeResult(ctx, tenantID, caller, idem)
		if err != nil || !ok {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit transaction: %w", err)
	}
	countActs(db.metrics, w)
	return true, nil
}

func (w *Writer) writeEvents(ctx context.Context, tenantID uuid.UUID, caller Caller, idem Idempotency, keyed bool) error {
	for _, e := range w.events {
		before, err := jsonOrNil(e.Before)
		if err != nil {
			return err
		}
		after, err := jsonOrNil(e.After)
		if err != nil {
			return err
		}
		// The id is made here, not returned: an INSERT … RETURNING must pass
		// the read policy, which a system actor's installation-level row
		// does not.
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("make the audit row's id: %w", err)
		}
		p := writeq.InsertAuditEventParams{
			ID:                   id,
			TenantID:             uuidPtr(tenantID),
			ActorUserID:          uuidPtr(caller.UserID),
			ActorSystem:          strPtr(caller.System),
			Agent:                strPtr(caller.Agent),
			TokenID:              uuidPtr(caller.TokenID),
			TokenName:            tokenName(caller),
			SourceHash:           caller.SourceHash,
			EntityType:           e.EntityType,
			EntityID:             uuidPtr(e.EntityID),
			TicketID:             uuidPtr(e.TicketID),
			TicketKey:            strPtr(e.TicketKey),
			Action:               e.Action,
			Before:               before,
			After:                after,
			Reason:               strPtr(e.Reason),
			Note:                 strPtr(e.Note),
			ExplainedByCommentID: uuidPtr(e.ExplainedBy),
			Refs:                 append([]uuid.UUID{}, e.Refs...),
			RequestID:            uuidPtr(caller.RequestID),
		}
		if caller.Agent != "" {
			p.AgentCapabilities = append([]string{}, caller.Capabilities...)
		}
		if e.System != "" {
			// The act is a system actor's, recorded in the caller's request: it
			// carries the request, never the caller's person, token or agent.
			p.ActorUserID, p.ActorSystem, p.Agent, p.AgentCapabilities = nil, &e.System, nil, nil
			p.TokenID, p.TokenName = nil, nil
		}
		switch {
		case keyed:
			p.IdempotencyKey = uuidPtr(idem.Key)
		case e.IdempotencyKey != uuid.Nil:
			p.IdempotencyKey = uuidPtr(e.IdempotencyKey)
		}
		if err := w.InsertAuditEvent(ctx, p); err != nil {
			return fmt.Errorf("write audit row: %w", err)
		}
		w.written = append(w.written, writtenAct{action: e.Action, actor: actorOf(caller, e)})
		if err := w.deliver(ctx, tenantID, id, caller, e); err != nil {
			return err
		}
		if err := w.publish(ctx, tenantID, id, e); err != nil {
			return err
		}
	}
	return nil
}

// flush writes the acts recorded so far for tenantID — which the transaction's
// app.tenant_id must name, as the audit policy demands — as caller's, and
// forgets them: a transaction that acts in several tenants, or for several
// actors, writes each batch in its own context.
func (w *Writer) flush(ctx context.Context, tenantID uuid.UUID, caller Caller) error {
	err := w.writeEvents(ctx, tenantID, caller, Idempotency{}, false)
	w.flushed = w.flushed || len(w.events) > 0
	w.events = nil
	return err
}

// inTenant binds a job's transaction to one tenant for fn — the settings the
// policies read and the Reader's tenant — writes the acts fn recorded there as
// the job's, and binds it to no tenant again (docs/adr/0021 D3, D4).
func (w *Writer) inTenant(ctx context.Context, tenantID uuid.UUID, fn func() error) error {
	if _, err := w.tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		return fmt.Errorf("bind the job to its tenant: %w", err)
	}
	w.TenantID = tenantID
	if err := fn(); err != nil {
		return err
	}
	if err := w.flush(ctx, tenantID, w.caller); err != nil {
		return err
	}
	w.TenantID = uuid.Nil
	if _, err := w.tx.Exec(ctx, "SELECT set_config('app.tenant_id', '', true)"); err != nil {
		return fmt.Errorf("release the job from its tenant: %w", err)
	}
	return nil
}

// storeResult writes the keyed response; false means a concurrent request
// holds the same unexpired key.
func (w *Writer) storeResult(ctx context.Context, tenantID uuid.UUID, caller Caller, idem Idempotency) (bool, error) {
	if w.result == nil {
		return false, errors.New("store: a keyed mutation set no response")
	}
	if w.result.Status < 100 || w.result.Status > 599 {
		return false, fmt.Errorf("store: a keyed mutation set the status %d", w.result.Status)
	}
	status := int16(w.result.Status)
	headers, err := json.Marshal(nonNilMap(w.result.Headers))
	if err != nil {
		return false, fmt.Errorf("encode stored headers: %w", err)
	}
	_, err = w.StoreIdempotencyKey(ctx, writeq.StoreIdempotencyKeyParams{
		TokenID:         uuidPtr(caller.TokenID),
		UserID:          caller.UserID,
		TenantID:        uuidPtr(tenantID),
		Key:             idem.Key,
		Fingerprint:     idem.Fingerprint[:],
		ResponseStatus:  status,
		ResponseHeaders: headers,
		ResponseBody:    nonNilBytes(w.result.Body),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store idempotency key: %w", err)
	}
	return true, nil
}

// replay returns the stored response for idem, ErrIdempotencyMismatch for a
// different request under the same key, or nil when the key is unused.
func (db *DB) replay(ctx context.Context, caller Caller, idem Idempotency) (*Result, error) {
	var (
		row   readq.GetIdempotencyKeyRow
		found bool
	)
	err := db.Installation(ctx, func(r *Reader) error {
		var err error
		row, err = r.GetIdempotencyKey(ctx, readq.GetIdempotencyKeyParams{UserID: caller.UserID, TokenID: uuidPtr(caller.TokenID), Key: idem.Key})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil || !found {
		return nil, err
	}
	if !bytes.Equal(row.Fingerprint, idem.Fingerprint[:]) {
		return nil, ErrIdempotencyMismatch
	}
	res := &Result{Status: int(row.ResponseStatus), Body: row.ResponseBody}
	if err := json.Unmarshal(row.ResponseHeaders, &res.Headers); err != nil {
		return nil, fmt.Errorf("decode stored headers: %w", err)
	}
	return res, nil
}

func jsonOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode audit diff: %w", err)
	}
	return b, nil
}

// tokenName is the name an audit row records beside its token: the token's
// name as the request presented it, never without the token (docs/adr/0036
// D6).
func tokenName(c Caller) *string {
	if c.TokenID == uuid.Nil {
		return nil
	}
	return strPtr(c.TokenName)
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilBytes(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}
