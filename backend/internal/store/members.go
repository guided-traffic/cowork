package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/store/readq"
)

// PersonMatch is what a lookup by address or username found.
type PersonMatch int

// The matches.
const (
	PersonNone PersonMatch = iota
	PersonFound
	// PersonAmbiguous: several active persons have the address, which is a
	// display attribute and not an identity (docs/adr/0029 D5).
	PersonAmbiguous
)

// PersonLookup is what FindPerson looks a person up by.
type PersonLookup struct {
	TenantID uuid.UUID
	// Key is an e-mail address — a key with "@" — or a local account's
	// username.
	Key string
	// Issuer is the configured issuer, whose persons alone an address finds.
	Issuer string
	// EmailTrusted lets an address match a person about whose address the
	// issuer said nothing; without it only one the issuer marked verified
	// matches (COWORK_OIDC_EMAIL_TRUSTED).
	EmailTrusted bool
}

// FindPerson looks up a person a tenant's administrator grants a role to
// (docs/adr/0030 D3): by e-mail address — a key with "@" — among the persons of
// the configured issuer, or by a local account's username. The person must
// exist and be active. The transaction is
// the administrator's, in their tenant, and names the key in
// app.person_lookup: the users policy admits the persons that match it and no
// other person of the installation.
func (db *DB) FindPerson(ctx context.Context, in PersonLookup) (uuid.UUID, PersonMatch, error) {
	tenantID, key := in.TenantID, in.Key
	caller, _ := CallerFrom(ctx)
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return uuid.Nil, PersonNone, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, tenantID, caller, ""); err != nil {
		return uuid.Nil, PersonNone, err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.person_lookup', $1, true)", key); err != nil {
		return uuid.Nil, PersonNone, fmt.Errorf("set transaction context: %w", err)
	}
	r := newReader(tx, tenantID, caller)
	if !strings.Contains(key, "@") {
		id, err := r.FindPersonByUsername(ctx, &key)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, PersonNone, nil
		}
		if err != nil {
			return uuid.Nil, PersonNone, fmt.Errorf("find the person: %w", err)
		}
		return id, PersonFound, nil
	}
	ids, err := r.FindPersonsByEmail(ctx, readq.FindPersonsByEmailParams{Email: key, EmailTrusted: in.EmailTrusted, Issuer: in.Issuer})
	if err != nil {
		return uuid.Nil, PersonNone, fmt.Errorf("find the person: %w", err)
	}
	switch len(ids) {
	case 0:
		return uuid.Nil, PersonNone, nil
	case 1:
		return ids[0], PersonFound, nil
	}
	return uuid.Nil, PersonAmbiguous, nil
}
