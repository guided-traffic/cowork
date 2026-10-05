package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// SessionTouchInterval is how often a session's idle clock moves: a request
// moves it only when the last move is older than this, so a busy page costs one
// write a minute, not one per request, and the idle limit is exact to the
// minute (docs/adr/0031 D3).
const SessionTouchInterval = time.Minute

// SessionRecord is a presented session and its person, as the resolver needs
// them.
type SessionRecord struct {
	Session readq.GetSessionByHashRow
	Person  readq.GetUserRow
}

// LookupSession finds the session whose cookie hashes to hash, and its person.
// The lookup transaction names the hash in app.session_hash, which the
// sessions policy admits exactly that row for (docs/adr/0021 D6); the person
// is read once the session names it. An unknown hash is ErrNotFound. Whether
// the session has expired is for the caller to judge, by its clock
// (docs/adr/0031 D3).
func (db *DB) LookupSession(ctx context.Context, hash [sha256.Size]byte) (SessionRecord, error) {
	var rec SessionRecord
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return rec, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.session_hash', $1, true)", hex.EncodeToString(hash[:])); err != nil {
		return rec, fmt.Errorf("set transaction context: %w", err)
	}
	q := readq.New(tx)
	rec.Session, err = q.GetSessionByHash(ctx, hash[:])
	if err != nil {
		return rec, notFound(err)
	}
	if err := setContext(ctx, tx, uuid.Nil, Caller{UserID: rec.Session.UserID, SessionHash: hash[:]}, ""); err != nil {
		return rec, err
	}
	rec.Person, err = q.GetUser(ctx, rec.Session.UserID)
	if err != nil {
		return rec, fmt.Errorf("read the session's person: %w", notFound(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return rec, fmt.Errorf("commit transaction: %w", err)
	}
	return rec, nil
}

// TouchSession moves a session's idle clock to now unless it moved within
// SessionTouchInterval. Like the token's last-used day it is bookkeeping, not
// an act, and one of the few writes outside Mutate (docs/adr/0027 D3).
func (db *DB) TouchSession(ctx context.Context, rec SessionRecord, now time.Time) error {
	if rec.Session.LastSeenAt.After(now.Add(-SessionTouchInterval)) {
		return nil
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, uuid.Nil, Caller{UserID: rec.Session.UserID}, ""); err != nil {
		return err
	}
	if err := writeq.New(tx).TouchSession(ctx, writeq.TouchSessionParams{
		Now: now, SessionID: rec.Session.ID, OlderThan: now.Add(-SessionTouchInterval),
	}); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// NewSession is what a login creates.
type NewSession struct {
	PersonID uuid.UUID
	// Hash is the SHA-256 of the cookie value; the value itself is never
	// stored (docs/adr/0031 D1).
	Hash [sha256.Size]byte
	// UserAgentHash is the SHA-256 of the User-Agent, or nil.
	UserAgentHash []byte
	// Now, Expires are the creation time and the absolute limit.
	Now, Expires time.Time
	// Replaces is the hash of the session cookie the login request presented,
	// which the new session replaces: a login while a session exists replaces
	// it (docs/adr/0031 D5). Nil when the request presented none.
	Replaces  []byte
	RequestID uuid.UUID
	// SourceHash is the keyed hash of the login's client address, which its
	// audit row carries (docs/adr/0035 D2).
	SourceHash []byte
}

// CreateSession ends the session the login presented, if any, stores the new
// one and records the login as an act of its person — in one transaction. The
// session's id and cookie appear in no audit row (docs/adr/0031 D7).
func (db *DB) CreateSession(ctx context.Context, in NewSession) error {
	ctx = WithCaller(ctx, Caller{UserID: in.PersonID, SessionHash: in.Replaces, RequestID: in.RequestID, SourceHash: in.SourceHash})
	_, err := db.Mutate(ctx, uuid.Nil, func(w *Writer) error {
		if len(in.Replaces) > 0 {
			if _, err := w.DeleteSessionByHash(ctx, in.Replaces); err != nil {
				return fmt.Errorf("end the replaced session: %w", err)
			}
		}
		if err := w.InsertSession(ctx, writeq.InsertSessionParams{
			UserID: in.PersonID, TokenHash: in.Hash[:], UserAgentHash: in.UserAgentHash,
			CreatedAt: in.Now, ExpiresAt: in.Expires, Method: MethodLocal,
		}); err != nil {
			return fmt.Errorf("store the session: %w", err)
		}
		w.Record(Event{EntityType: entityUser, EntityID: in.PersonID, Action: "logged_in"})
		return nil
	})
	return err
}

// ExpireSessions deletes the sessions past their absolute limit or idle for
// longer than idle, and records one act per run that removed any
// (docs/adr/0031 D3, docs/adr/0027 D5). A session past a limit is refused at
// its next request whether or not this job has run; the job only keeps the
// table from growing.
func (db *DB) ExpireSessions(ctx context.Context, now time.Time, idle time.Duration) (removed int64, err error) {
	_, err = db.RunJob(ctx, "session-expiry", 2, func(w *Writer) error {
		n, err := w.DeleteExpiredSessions(ctx, writeq.DeleteExpiredSessionsParams{Now: now, IdleBefore: now.Add(-idle)})
		if err != nil {
			return fmt.Errorf("delete expired sessions: %w", err)
		}
		removed = n
		if n == 0 {
			return nil
		}
		w.Record(Event{EntityType: "sessions", Action: actionExpired, After: map[string]int64{fieldRemoved: n}})
		return nil
	})
	return removed, err
}
