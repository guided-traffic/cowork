package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// LoginWindow is the window of the lockout (docs/adr/0033 D6): the failures of
// a username within it count, and a lock of the `window` mode ends with it.
const LoginWindow = 15 * time.Minute

// AddressWindow is the window of the per-address throttle (docs/adr/0033 D6).
const AddressWindow = time.Minute

// loginLockNamespace is the first key of the advisory lock that orders the
// attempts of one username, "cowl": two attempts against the same name — known
// or not — are decided one after the other, so a parallel burst cannot make
// more guesses than the lock allows.
const loginLockNamespace int32 = 0x636f776c

// loginNote is how long a lock stays silent in the audit record after an attempt
// against it was written: a hammered lock writes one row an hour.
const loginNote = time.Hour

const systemLogin = "system:login"

// loginRead runs fn in a read-only transaction that names the login as its job
// (docs/adr/0021 D3), which the policies of the login's tables admit.
func (db *DB) loginRead(ctx context.Context, fn func(r *Reader) error) error {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	caller := Caller{System: systemLogin}
	if err := setContext(ctx, tx, uuid.Nil, caller, "login"); err != nil {
		return err
	}
	if err := fn(newReader(tx, uuid.Nil, caller)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// LoginAccount is what the login reads of a username.
type LoginAccount struct {
	// Found is false when the username names no local account.
	Found       bool
	UserID      uuid.UUID
	Hash        string
	Deactivated bool
	GlobalAdmin bool
	// PasswordChangeRequired says the password is temporary (docs/adr/0033 D4).
	PasswordChangeRequired bool
	// Initialised is false while no tenant exists (docs/adr/0032 D5).
	Initialised bool
}

// LookupLogin reads the local account a username names and whether the
// installation has a tenant yet. It does not say whether the password fits.
func (db *DB) LookupLogin(ctx context.Context, username string) (LoginAccount, error) {
	var acc LoginAccount
	err := db.loginRead(ctx, func(r *Reader) error {
		row, err := r.GetLoginAccount(ctx, &username)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		default:
			acc = LoginAccount{Found: true, UserID: row.ID, Hash: row.PasswordHash, Deactivated: row.DeactivatedAt != nil,
				GlobalAdmin: row.GlobalAdmin, PasswordChangeRequired: row.PasswordChangeRequired}
		}
		acc.Initialised, err = r.TenantsExist(ctx)
		return err
	})
	return acc, err
}

// LocalLoginAvailable reports whether an active local account exists: whether
// the login page offers the local form (docs/adr/0033 D8).
func (db *DB) LocalLoginAvailable(ctx context.Context) (bool, error) {
	var available bool
	err := db.loginRead(ctx, func(r *Reader) error {
		var err error
		available, err = r.LocalLoginAvailable(ctx)
		return err
	})
	return available, err
}

// AddressAttempts counts the attempts of an address since a time
// (docs/adr/0033 D6).
func (db *DB) AddressAttempts(ctx context.Context, address []byte, since time.Time) (int64, error) {
	var n int64
	err := db.loginRead(ctx, func(r *Reader) error {
		var err error
		n, err = r.CountAddressAttempts(ctx, readq.CountAddressAttemptsParams{Address: address, Since: since})
		return err
	})
	return n, err
}

// LoginAttempt is one processed attempt to prove a password: a login, or the
// current password of a change.
type LoginAttempt struct {
	// Username is the name as presented after normalisation; "" for a value
	// that cannot be a username. It is the key of the failures and the lock,
	// whether or not an account has it.
	Username string
	Account  LoginAccount
	// Verified says the password fitted an active local account's hash.
	Verified bool
	// Refusal names why a verified attempt is refused all the same, such as
	// the init state; empty for a login that goes on.
	Refusal string
	// Address is the keyed hash of the client address (docs/adr/0035 D2), never
	// the address.
	Address []byte
	Now     time.Time
	// MaxFailures failures within Window lock the username; 0 locks never.
	MaxFailures int
	Window      time.Duration
	// Sticky makes a lock stay until an administrator unlocks it
	// (COWORK_LOGIN_LOCKOUT=admin).
	Sticky bool
	// Context says what was attempted, for the audit record: "login" or
	// "password change".
	Context   string
	RequestID uuid.UUID
	// SourceHash is the keyed hash of the client address the audit rows of the
	// attempt carry (docs/adr/0035 D2).
	SourceHash []byte
}

// LoginOutcome is what RecordLoginAttempt decided.
type LoginOutcome int

// The outcomes. Whatever the outcome, the person asking is answered the same
// way but for LoginSucceeded and LoginRefused.
const (
	// LoginSucceeded: the password fitted and no lock held.
	LoginSucceeded LoginOutcome = iota
	// LoginFailed: a wrong password, an unknown or deactivated account.
	LoginFailed
	// LoginLocked: the username was locked, whatever the password.
	LoginLocked
	// LoginRefused: the password fitted but Refusal says no.
	LoginRefused
)

// RecordLoginAttempt decides an attempt under the username's lock and records
// it (docs/adr/0033 D6): the attempt is counted for the address throttle and,
// when it failed or met a lock, for the lockout; the failure that reaches
// MaxFailures within Window locks the username; the failures, the lock and an
// attempt against a lock are audit rows without the attempted password and
// without an unknown username. The password was verified before the call,
// against the account's hash or a dummy, and only its result is passed in.
//
// It commits without an act when it has none to record (a login that went on,
// an attempt against a lock that was noted recently), which Mutate refuses: a
// counted attempt is bookkeeping, like the token's last-used day.
func (db *DB) RecordLoginAttempt(ctx context.Context, in LoginAttempt) (LoginOutcome, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return LoginFailed, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	caller := Caller{System: systemLogin, RequestID: in.RequestID, SourceHash: in.SourceHash}
	if err := setContext(ctx, tx, uuid.Nil, caller, "login"); err != nil {
		return LoginFailed, err
	}
	w := &Writer{Reader: newReader(tx, uuid.Nil, caller), Queries: writeq.New(tx)}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, hashtext($2))", loginLockNamespace, in.Username); err != nil {
		return LoginFailed, fmt.Errorf("take the login lock: %w", err)
	}
	outcome, err := w.decideAttempt(ctx, in)
	if err != nil {
		return LoginFailed, err
	}
	if len(w.events) > 0 {
		if err := w.writeEvents(ctx, uuid.Nil, caller, Idempotency{}, false); err != nil {
			return LoginFailed, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return LoginFailed, fmt.Errorf("commit transaction: %w", err)
	}
	return outcome, nil
}

func (w *Writer) decideAttempt(ctx context.Context, in LoginAttempt) (LoginOutcome, error) {
	locked, lock, err := w.activeLock(ctx, in)
	if err != nil {
		return LoginFailed, err
	}
	entity := Event{EntityType: entityUser, EntityID: in.Account.UserID, Note: in.Context}
	attempt := writeq.InsertLoginAttemptParams{Username: in.Username, Address: in.Address, CreatedAt: in.Now}
	switch {
	case locked:
		return w.attemptAgainstLock(ctx, in, lock, entity, attempt)
	case in.Verified:
		return w.verifiedAttempt(ctx, in, entity, attempt)
	}
	return w.failedAttempt(ctx, in, entity, attempt)
}

// attemptAgainstLock counts an attempt that met a lock as a failure — whatever
// its password — and writes it to the audit record at most once an hour.
func (w *Writer) attemptAgainstLock(ctx context.Context, in LoginAttempt, lock readq.GetLoginLockRow, entity Event, attempt writeq.InsertLoginAttemptParams) (LoginOutcome, error) {
	attempt.Failed = true
	if err := w.InsertLoginAttempt(ctx, attempt); err != nil {
		return LoginFailed, fmt.Errorf("count the attempt: %w", err)
	}
	if lock.NotedAt == nil || lock.NotedAt.Before(in.Now.Add(-loginNote)) {
		if err := w.NoteLoginLock(ctx, writeq.NoteLoginLockParams{NotedAt: &in.Now, Username: in.Username}); err != nil {
			return LoginFailed, fmt.Errorf("note the lock: %w", err)
		}
		w.Record(withAction(entity, "login_failed", "locked"))
	}
	return LoginLocked, nil
}

// verifiedAttempt counts an attempt whose password fitted; a refusal for
// another reason, such as the init state, is recorded.
func (w *Writer) verifiedAttempt(ctx context.Context, in LoginAttempt, entity Event, attempt writeq.InsertLoginAttemptParams) (LoginOutcome, error) {
	if err := w.InsertLoginAttempt(ctx, attempt); err != nil {
		return LoginFailed, fmt.Errorf("count the attempt: %w", err)
	}
	if in.Refusal == "" {
		return LoginSucceeded, nil
	}
	w.Record(withAction(entity, "login_failed", in.Refusal))
	return LoginRefused, nil
}

// failedAttempt counts and records a failure, and locks the username when this
// is the failure that reaches the limit within the window.
func (w *Writer) failedAttempt(ctx context.Context, in LoginAttempt, entity Event, attempt writeq.InsertLoginAttemptParams) (LoginOutcome, error) {
	attempt.Failed = true
	if err := w.InsertLoginAttempt(ctx, attempt); err != nil {
		return LoginFailed, fmt.Errorf("count the attempt: %w", err)
	}
	w.Record(withAction(entity, "login_failed", failureReason(in.Account)))
	if in.MaxFailures <= 0 {
		return LoginFailed, nil
	}
	n, err := w.CountLoginFailures(ctx, readq.CountLoginFailuresParams{Username: in.Username, Since: in.Now.Add(-in.Window)})
	if err != nil {
		return LoginFailed, fmt.Errorf("count the failures: %w", err)
	}
	if n >= int64(in.MaxFailures) {
		if err := w.UpsertLoginLock(ctx, writeq.UpsertLoginLockParams{Username: in.Username, LockedAt: in.Now,
			Sticky: in.Sticky && in.Account.Found}); err != nil {
			return LoginFailed, fmt.Errorf("lock the username: %w", err)
		}
		w.Record(withAction(entity, "locked", ""))
	}
	return LoginFailed, nil
}

// activeLock reads the lock of a username and whether it holds now: a sticky
// one always, the others while their window lasts.
func (w *Writer) activeLock(ctx context.Context, in LoginAttempt) (bool, readq.GetLoginLockRow, error) {
	lock, err := w.GetLoginLock(ctx, in.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, lock, nil
	}
	if err != nil {
		return false, lock, fmt.Errorf("read the lock: %w", err)
	}
	return lock.Sticky || lock.LockedAt.After(in.Now.Add(-in.Window)), lock, nil
}

func withAction(e Event, action, reason string) Event {
	e.Action, e.Reason = action, reason
	return e
}

func failureReason(acc LoginAccount) string {
	switch {
	case !acc.Found:
		return "unknown_account"
	case acc.Deactivated:
		return "account_deactivated"
	}
	return "wrong_password"
}

// ExpireLoginState deletes the attempts older than the window and the locks of
// the `window` mode that ended with it, and records one act per run that
// removed any (docs/adr/0027 D5). A lock that ended holds nothing at the next
// attempt whether or not this job has run.
func (db *DB) ExpireLoginState(ctx context.Context, now time.Time, window time.Duration) (removed int64, err error) {
	_, err = db.RunJob(ctx, "login-expiry", 3, func(w *Writer) error {
		attempts, err := w.DeleteExpiredLoginAttempts(ctx, now.Add(-window))
		if err != nil {
			return fmt.Errorf("delete expired login attempts: %w", err)
		}
		locks, err := w.DeleteExpiredLoginLocks(ctx, now.Add(-window))
		if err != nil {
			return fmt.Errorf("delete expired login locks: %w", err)
		}
		removed = attempts + locks
		if removed == 0 {
			return nil
		}
		w.Record(Event{EntityType: "login_attempts", Action: actionExpired,
			After: map[string]int64{"attempts": attempts, "locks": locks}})
		return nil
	})
	return removed, err
}
