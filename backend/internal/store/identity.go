package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/metrics"
	"github.com/guided-traffic/cowork/backend/internal/store/readq"
	"github.com/guided-traffic/cowork/backend/internal/store/writeq"
)

// What the identity provider decides is the act of the system actor
// system:identity-provider, whose transactions name it in app.job
// (docs/adr/0021 D3, docs/adr/0030 D6): the login through the provider, the
// groups refresh of its sessions, the gate of its persons' tokens, and the
// derivation of memberships from the group mappings.
const (
	jobIdentity    = "identity-provider"
	systemIdentity = "system:identity-provider"
)

// identityLockNamespace is the first key of the lock that orders what the
// identity provider decides about one person, "cowi": a login, a refresh, a
// token's gate check and a mapping's change derive the person's memberships
// one after the other, each on what the one before committed.
// tenantLockNamespace, "cowt", orders the changes of who administers a tenant
// (LockTenant). The order is the tenant's lock first, then the persons' locks
// by ascending id: no transaction takes a tenant's lock after a person's. A
// transaction takes the lock of its own tenant and of no other, so the
// tenants' locks need no order among themselves.
const (
	identityLockNamespace int32 = 0x636f7769
	tenantLockNamespace   int32 = 0x636f7774
)

// LockTenant takes the lock that orders the changes of the tenant's
// memberships until the transaction ends (the security review of 2026-10-04,
// m4): two administrators who each take the other's admin role away — by a
// grant, a mapping or the deactivation of the other's account — decide one
// after the other, and the second meets last_admin. It is taken before any
// person's lock.
func (w *Writer) LockTenant(ctx context.Context) error {
	return w.lock(ctx, tenantLockNamespace, w.TenantID, "tenant membership")
}

// The causes of a derivation (docs/adr/0030 D6), recorded as the reason of the
// identity provider's acts; causeGate and causeIssuer are the causes of a
// session it ends.
const (
	CauseLogin   = "login"
	CauseRefresh = "refresh"
	CauseToken   = "token"
	CauseMapping = "mapping"
	causeGate    = "gate"
	causeIssuer  = "identity-provider"
)

// The entity and the fields the identity provider's acts record.
const (
	sourceMapping    = "mapping"
	entityUser       = "user"
	fieldUser        = "user"
	fieldRole        = "role"
	fieldGlobalAdmin = "global_admin"
	fieldName        = "name"
)

// mapped is a membership the mappings derived.
type mapped struct {
	ID   uuid.UUID
	Role domain.Role
}

// applyMapped brings one person's mapped membership in one tenant to the role
// the tenant's mappings give their groups — none for want "" — and records the
// change as the identity provider's act, announced to the tenant's members
// (docs/adr/0030 D2, D6). A grant is never touched (D3).
func (w *Writer) applyMapped(ctx context.Context, tenantID, person uuid.UUID, cur *mapped, want domain.Role, cause string) error {
	change := &MembershipChange{Person: person, Audience: AudienceMembers}
	act := Event{System: systemIdentity, EntityType: EntityMembership, Reason: cause, Membership: change}
	switch {
	case cur == nil && want == "":
		return nil
	case cur == nil:
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := w.InsertMappedMembership(ctx, writeq.InsertMappedMembershipParams{ID: id, TenantID: tenantID, UserID: person, Role: want}); err != nil {
			return fmt.Errorf("derive a membership: %w", err)
		}
		act.EntityID, act.Action = id, "created"
		act.After = map[string]any{fieldUser: person, fieldRole: want, "source": sourceMapping}
	case want == "":
		if err := w.DeleteMembership(ctx, cur.ID); err != nil {
			return fmt.Errorf("remove a derived membership: %w", err)
		}
		act.EntityID, act.Action = cur.ID, "deleted"
		act.Before = map[string]any{fieldUser: person, fieldRole: cur.Role, "source": sourceMapping}
	case cur.Role != want:
		if err := w.SetMembershipRole(ctx, writeq.SetMembershipRoleParams{Role: want, ID: cur.ID}); err != nil {
			return fmt.Errorf("change a derived membership: %w", err)
		}
		act.EntityID, act.Action = cur.ID, "updated"
		act.Before, act.After = map[string]any{fieldRole: cur.Role}, map[string]any{fieldRole: want}
	default:
		return nil
	}
	w.Record(act)
	return nil
}

// RederiveGroup re-derives, in the transaction's tenant, the mapped membership
// of every person of the configured issuer whose groups — as of their last
// login or refresh — include the group, after an administrator made, changed
// or removed a mapping of it (docs/adr/0030 D2, D7): the member list shows the
// effect at once. Each person's groups are read again once their lock is held,
// and a person who cannot act — deactivated, or refused by the gate at their
// last login or refresh — is left as they are (the security review of
// 2026-10-04, m3). The derivation is the identity provider's act in the
// administrator's transaction, so the transaction names the provider for it
// and the administrator again after it. The caller holds the tenant's lock.
func (w *Writer) RederiveGroup(ctx context.Context, group, issuer string) error {
	if _, err := w.tx.Exec(ctx, "SELECT set_config('app.job', $1, true)", jobIdentity); err != nil {
		return fmt.Errorf("name the identity provider: %w", err)
	}
	persons, err := w.ListPersonsInGroup(ctx, readq.ListPersonsInGroupParams{GroupName: group, Issuer: &issuer})
	if err != nil {
		return fmt.Errorf("find the persons of the group: %w", err)
	}
	for _, id := range persons {
		if err := w.lock(ctx, identityLockNamespace, id, "identity"); err != nil {
			return err
		}
		p, err := w.GetPersonForDerivation(ctx, id)
		if err != nil {
			return fmt.Errorf("read the person: %w", err)
		}
		if p.DeactivatedAt != nil || p.GateCheckedAt == nil || p.OidcIssuer == nil || *p.OidcIssuer != issuer {
			continue
		}
		role, err := w.MappedRoleInTenant(ctx, readq.MappedRoleInTenantParams{TenantID: w.TenantID, Groups: nonNilStrings(p.OidcGroups)})
		if err != nil {
			return fmt.Errorf("read the mapped role: %w", err)
		}
		cur, err := w.mappedIn(ctx, w.TenantID, id)
		if err != nil {
			return err
		}
		if err := w.applyMapped(ctx, w.TenantID, id, cur, domain.Role(role), CauseMapping); err != nil {
			return err
		}
	}
	if _, err := w.tx.Exec(ctx, "SELECT set_config('app.job', '', true)"); err != nil {
		return fmt.Errorf("leave the identity provider: %w", err)
	}
	return nil
}

func (w *Writer) mappedIn(ctx context.Context, tenantID, person uuid.UUID) (*mapped, error) {
	row, err := w.GetMappedMembership(ctx, readq.GetMappedMembershipParams{TenantID: tenantID, UserID: person})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the derived membership: %w", err)
	}
	return &mapped{ID: row.ID, Role: row.Role}, nil
}

// identityTx is one transaction of the identity provider about one person.
// app.job names the provider; app.user_id names the person once known, which
// the policies of the person's sessions and memberships read; app.session_hash
// names the session a login replaces or a refresh holds.
type identityTx struct {
	tx      pgx.Tx
	w       *Writer
	caller  Caller
	metrics *metrics.Metrics
}

func (db *DB) beginIdentity(ctx context.Context, requestID uuid.UUID, sourceHash, sessionHash []byte) (*identityTx, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	caller := Caller{System: systemIdentity, RequestID: requestID, SourceHash: sourceHash, SessionHash: sessionHash}
	if err := setContext(ctx, tx, uuid.Nil, caller, jobIdentity); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return &identityTx{tx: tx, w: &Writer{Reader: newReader(tx, uuid.Nil, caller), Queries: writeq.New(tx)}, caller: caller,
		metrics: db.metrics}, nil
}

func (t *identityTx) rollback(ctx context.Context) { _ = t.tx.Rollback(ctx) }

func (t *identityTx) commit(ctx context.Context) error {
	if err := t.tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	countActs(t.metrics, t.w)
	return nil
}

func (t *identityTx) set(ctx context.Context, name, value string) error {
	if _, err := t.tx.Exec(ctx, "SELECT set_config($1, $2, true)", name, value); err != nil {
		return fmt.Errorf("set %s: %w", name, err)
	}
	return nil
}

// forPerson names the person and takes the lock that orders the identity
// provider's decisions about them.
func (t *identityTx) forPerson(ctx context.Context, person uuid.UUID) error {
	if err := t.set(ctx, "app.user_id", person.String()); err != nil {
		return err
	}
	t.w.UserID = person
	return t.w.lock(ctx, identityLockNamespace, person, "identity")
}

// flushIn writes the acts recorded so far as the provider's, in a tenant or —
// with uuid.Nil — at the installation's level.
func (t *identityTx) flushIn(ctx context.Context, tenantID uuid.UUID) error {
	if len(t.w.events) == 0 {
		return nil
	}
	if err := t.set(ctx, "app.tenant_id", uuidText(tenantID)); err != nil {
		return err
	}
	if err := t.w.flush(ctx, tenantID, t.caller); err != nil {
		return err
	}
	return t.set(ctx, "app.tenant_id", "")
}

// deriveEverywhere brings the person's mapped memberships in every tenant in
// line with their groups and the tenants' mappings (docs/adr/0030 D2): made
// where a mapping now gives a role, changed where the highest mapped role
// moved, removed where no mapping gives one any more. Each change is the
// identity provider's act in its tenant, with the cause.
func (t *identityTx) deriveEverywhere(ctx context.Context, person uuid.UUID, groups []string, cause string) error {
	rows, err := t.w.ListMappedRoles(ctx, nonNilStrings(groups))
	if err != nil {
		return fmt.Errorf("read the mapped roles: %w", err)
	}
	want := map[uuid.UUID]domain.Role{}
	for _, r := range rows {
		want[r.TenantID] = r.Role
	}
	current, err := t.w.ListMappedMembershipsOfUser(ctx, person)
	if err != nil {
		return fmt.Errorf("read the derived memberships: %w", err)
	}
	have := map[uuid.UUID]mapped{}
	for _, c := range current {
		have[c.TenantID] = mapped{ID: c.ID, Role: c.Role}
	}
	tenants := make([]uuid.UUID, 0, len(want)+len(have))
	for id := range want {
		tenants = append(tenants, id)
	}
	for id := range have {
		if _, ok := want[id]; !ok {
			tenants = append(tenants, id)
		}
	}
	slices.SortFunc(tenants, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, tenant := range tenants {
		var cur *mapped
		if m, ok := have[tenant]; ok {
			cur = &m
		}
		if err := t.w.applyMapped(ctx, tenant, person, cur, want[tenant], cause); err != nil {
			return err
		}
		if err := t.flushIn(ctx, tenant); err != nil {
			return err
		}
	}
	return nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// OIDCLogin is a login through the identity provider whose ID token was
// verified (docs/adr/0029 D1): what the issuer says of the person, and what the
// gate made of their groups.
type OIDCLogin struct {
	Issuer, Subject string
	DisplayName     string
	// Email is "" when the issuer named no address; EmailVerified is nil when
	// it said nothing about it.
	Email         string
	EmailVerified *bool
	Groups        []string
	// Admitted says the groups pass the gate, Admin that they hold the
	// administrator group (docs/adr/0030 D1).
	Admitted, Admin bool
	// ReadAt is when the issuer said the groups: the person's snapshot is as
	// fresh as that.
	ReadAt time.Time
	// Session is the session the login makes, its PersonID left to the store.
	Session NewSession
	// RefreshTokenSealed is the issuer's refresh token, sealed for the session;
	// nil when the issuer gave none.
	RefreshTokenSealed []byte
}

// The methods of a session (docs/adr/0031 D1).
const (
	MethodLocal = "local"
	MethodOIDC  = "oidc"
)

// OIDCOutcome is what CompleteOIDCLogin decided.
type OIDCOutcome int

// The outcomes: a session, or one of the two refusals the login page names.
const (
	OIDCLoggedIn OIDCOutcome = iota
	// OIDCNotAllowed: outside the gate, or deactivated (docs/adr/0030 D1,
	// docs/adr/0024 D5).
	OIDCNotAllowed
	// OIDCNotInitialised: no tenant exists and the person is not in the
	// administrator group (docs/adr/0032 D5).
	OIDCNotInitialised
)

// OIDCLoginResult is the outcome, the person when one exists, and why a login
// was refused, for the log.
type OIDCLoginResult struct {
	Outcome OIDCOutcome
	Person  uuid.UUID
	Reason  string
}

// CompleteOIDCLogin decides a verified login in one transaction (docs/adr/0029
// D5, docs/adr/0030 D1, D2, docs/adr/0032 D5): the gate, a deactivated person
// and the init state refuse it — recorded as login_refused at the
// installation's level, with the person when one exists, and no person made;
// otherwise the person is found by issuer and subject or made, their display
// attributes, groups and administrator flag refreshed, their memberships
// derived in every tenant, and the session made as the local login makes it,
// the one the request presented ended (docs/adr/0031 D5).
func (db *DB) CompleteOIDCLogin(ctx context.Context, in OIDCLogin) (OIDCLoginResult, error) {
	t, err := db.beginIdentity(ctx, in.Session.RequestID, in.Session.SourceHash, in.Session.Replaces)
	if err != nil {
		return OIDCLoginResult{}, err
	}
	defer t.rollback(ctx)
	issuer, subject := in.Issuer, in.Subject
	existing, err := t.w.GetPersonByIdentity(ctx, readq.GetPersonByIdentityParams{Issuer: &issuer, Subject: &subject})
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return OIDCLoginResult{}, fmt.Errorf("find the person: %w", err)
	}
	if found {
		if err := t.forPerson(ctx, existing.ID); err != nil {
			return OIDCLoginResult{}, err
		}
	}
	initialised, err := t.w.TenantsExist(ctx)
	if err != nil {
		return OIDCLoginResult{}, fmt.Errorf("ask whether a tenant exists: %w", err)
	}
	if refusal := refusalOf(in, found && existing.DeactivatedAt != nil, initialised); refusal.Reason != "" {
		return t.refused(ctx, refusal, found, existing, in)
	}
	person, err := t.keepPerson(ctx, found, existing, in)
	if err != nil {
		return OIDCLoginResult{}, err
	}
	if err := t.flushIn(ctx, uuid.Nil); err != nil {
		return OIDCLoginResult{}, err
	}
	if err := t.deriveEverywhere(ctx, person, in.Groups, CauseLogin); err != nil {
		return OIDCLoginResult{}, err
	}
	if err := t.startSession(ctx, person, in); err != nil {
		return OIDCLoginResult{}, err
	}
	return OIDCLoginResult{Outcome: OIDCLoggedIn, Person: person}, t.commit(ctx)
}

// refused acts on a refused login and records it: a known, active person the
// gate refused has left it.
func (t *identityTx) refused(ctx context.Context, r OIDCLoginResult, found bool, existing readq.GetPersonByIdentityRow, in OIDCLogin) (OIDCLoginResult, error) {
	if found && !in.Admitted && existing.DeactivatedAt == nil {
		if err := t.leftTheGate(ctx, existing.ID, in); err != nil {
			return OIDCLoginResult{}, err
		}
	}
	return t.refuse(ctx, r, existing.ID)
}

// leftTheGate acts on the freshest word there is that a known person is outside
// the gate, a refused login (docs/adr/0030 D1, D5): their groups are kept,
// their sessions end, and their tokens meet the gate with these groups at their
// next request (docs/adr/0035 D8). Their memberships stay as they are (the
// security review of 2026-10-04, m5): a person outside the gate cannot use
// them, and a refused login does not take a tenant's administrator away.
func (t *identityTx) leftTheGate(ctx context.Context, person uuid.UUID, in OIDCLogin) error {
	if err := t.storeSnapshot(ctx, person, in.Groups, in.Admin, false, readTime(in), CauseLogin); err != nil {
		return err
	}
	n, err := t.w.DeleteSessionsOfUser(ctx, person)
	if err != nil {
		return fmt.Errorf("end the sessions: %w", err)
	}
	if n > 0 {
		t.w.Record(Event{EntityType: entityUser, EntityID: person, Action: "revoked", Reason: causeGate,
			After: map[string]any{"sessions_ended": n}})
	}
	return nil
}

// refusalOf decides whether a verified login is refused, and why: the gate
// first, then a deactivated person, then the init state.
func refusalOf(in OIDCLogin, deactivated, initialised bool) OIDCLoginResult {
	switch {
	case !in.Admitted:
		return OIDCLoginResult{Outcome: OIDCNotAllowed, Reason: "outside the gate"}
	case deactivated:
		return OIDCLoginResult{Outcome: OIDCNotAllowed, Reason: "deactivated"}
	case !initialised && !in.Admin:
		return OIDCLoginResult{Outcome: OIDCNotInitialised, Reason: "not initialised"}
	}
	return OIDCLoginResult{}
}

// keepPerson refreshes the person the login names, or makes them.
func (t *identityTx) keepPerson(ctx context.Context, found bool, existing readq.GetPersonByIdentityRow, in OIDCLogin) (uuid.UUID, error) {
	if found {
		return t.updatePerson(ctx, existing, in)
	}
	return t.createPerson(ctx, in)
}

// refuse records a refused login (docs/adr/0026): the reason, and the person
// when one exists. Who was refused without being a person is in no row.
func (t *identityTx) refuse(ctx context.Context, r OIDCLoginResult, person uuid.UUID) (OIDCLoginResult, error) {
	reason := map[OIDCOutcome]string{OIDCNotAllowed: "not_allowed", OIDCNotInitialised: "not_initialised"}[r.Outcome]
	t.w.Record(Event{EntityType: entityUser, EntityID: person, Action: "login_refused", Reason: reason, Note: r.Reason})
	if err := t.flushIn(ctx, uuid.Nil); err != nil {
		return OIDCLoginResult{}, err
	}
	r.Person = person
	return r, t.commit(ctx)
}

func (t *identityTx) createPerson(ctx context.Context, in OIDCLogin) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	issuer, subject := in.Issuer, in.Subject
	if err := t.w.InsertProviderPerson(ctx, writeq.InsertProviderPersonParams{
		ID: id, DisplayName: in.DisplayName, GlobalAdmin: in.Admin, Issuer: &issuer, Subject: &subject,
		Email: strPtr(in.Email), EmailVerified: in.EmailVerified, Groups: nonNilStrings(in.Groups), Now: ptrTo(readTime(in)),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("make the person: %w", err)
	}
	if err := t.forPerson(ctx, id); err != nil {
		return uuid.Nil, err
	}
	t.w.Record(Event{EntityType: entityUser, EntityID: id, Action: "created", Reason: CauseLogin, After: map[string]any{
		fieldName: in.DisplayName, "identity": MethodOIDC, fieldGlobalAdmin: in.Admin}})
	return id, nil
}

func (t *identityTx) updatePerson(ctx context.Context, cur readq.GetPersonByIdentityRow, in OIDCLogin) (uuid.UUID, error) {
	if err := t.w.UpdateProviderPerson(ctx, writeq.UpdateProviderPersonParams{
		DisplayName: in.DisplayName, Email: strPtr(in.Email), EmailVerified: in.EmailVerified, GlobalAdmin: in.Admin,
		Groups: nonNilStrings(in.Groups), Now: ptrTo(readTime(in)), ID: cur.ID,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("refresh the person: %w", err)
	}
	before := map[string]any{fieldName: cur.DisplayName, "email_verified": cur.EmailVerified, fieldGlobalAdmin: cur.GlobalAdmin}
	after := map[string]any{fieldName: in.DisplayName, "email_verified": in.EmailVerified, fieldGlobalAdmin: in.Admin}
	t.recordChanges(cur.ID, before, after, unnamed{
		email:  derefOr(cur.Email, "") != in.Email,
		groups: !sameGroups(cur.OidcGroups, in.Groups),
	}, CauseLogin)
	return cur.ID, nil
}

// unnamed is what changed about a person that an audit row says changed but
// never names: the address and the groups.
type unnamed struct {
	email, groups bool
}

// sameGroups reports whether two lists name the same groups, in any order.
func sameGroups(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// recordChanges records what the issuer changed about a person, the changed
// fields only, and nothing when nothing changed. The values compare as the
// audit row writes them, in JSON. A changed address is recorded as
// email_changed and never as the address (the security review of 2026-10-04,
// m11), and changed groups as groups_changed and never as the groups
// (docs/adr/0030 D6, the owner's answer of 2026-10-04): the audit record is
// append-only, so what it names could not be erased on request, and group
// names can say more about a person than their role in a tenant — the
// memberships the groups caused are recorded, tenant by tenant, anyway.
func (t *identityTx) recordChanges(person uuid.UUID, before, after map[string]any, changed unnamed, cause string) {
	changedBefore, changedAfter := map[string]any{}, map[string]any{}
	for k, v := range after {
		b, _ := json.Marshal(before[k])
		a, _ := json.Marshal(v)
		if !bytes.Equal(a, b) {
			changedBefore[k], changedAfter[k] = before[k], v
		}
	}
	if changed.email {
		changedAfter["email_changed"] = true
	}
	if changed.groups {
		changedAfter["groups_changed"] = true
	}
	if len(changedAfter) == 0 {
		return
	}
	t.w.Record(Event{EntityType: entityUser, EntityID: person, Action: "updated", Reason: cause, Before: changedBefore, After: changedAfter})
}

// readTime is when the login read the person from the issuer.
func readTime(in OIDCLogin) time.Time {
	if in.ReadAt.IsZero() {
		return in.Session.Now
	}
	return in.ReadAt
}

func ptrTo[T any](v T) *T { return &v }

func derefOr[T any](p *T, zero T) T {
	if p == nil {
		return zero
	}
	return *p
}

// startSession ends the session the request presented, makes the new one with
// the login's groups and sealed refresh token, and records the login as the
// person's act, like the local login's (docs/adr/0031 D5, D7).
func (t *identityTx) startSession(ctx context.Context, person uuid.UUID, in OIDCLogin) error {
	s := in.Session
	if len(s.Replaces) > 0 {
		if _, err := t.w.DeleteSessionByHash(ctx, s.Replaces); err != nil {
			return fmt.Errorf("end the replaced session: %w", err)
		}
	}
	now := s.Now
	if err := t.w.InsertSession(ctx, writeq.InsertSessionParams{
		UserID: person, TokenHash: s.Hash[:], UserAgentHash: s.UserAgentHash, CreatedAt: s.Now, ExpiresAt: s.Expires,
		Method: MethodOIDC, Groups: nonNilStrings(in.Groups), GroupsRefreshedAt: &now, RefreshTokenSealed: in.RefreshTokenSealed,
	}); err != nil {
		return fmt.Errorf("store the session: %w", err)
	}
	t.w.Record(Event{EntityType: entityUser, EntityID: person, Action: "logged_in", Note: MethodOIDC})
	return t.w.flush(ctx, uuid.Nil, Caller{UserID: person, RequestID: s.RequestID, SourceHash: s.SourceHash})
}

// RefreshVerdict is what a groups refresh found out (docs/adr/0030 D5).
type RefreshVerdict int

// The verdicts.
const (
	// RefreshRead: the issuer said the person's groups now.
	RefreshRead RefreshVerdict = iota
	// RefreshJudged: nothing was read — no refresh token, no provider, no
	// groups claim in the refresh's answer — and the person's groups as they
	// stand are judged against the gate as it is now. Neither the person's
	// groups nor their memberships change (the security review of 2026-10-04,
	// M2): the session's older groups never go over the person's newer ones.
	RefreshJudged
	// RefreshRefused: the issuer refused the refresh; the session ends.
	RefreshRefused
	// RefreshUnreachable: the issuer could not be asked; the session is served
	// and the next attempt waits.
	RefreshUnreachable
)

// RefreshDecision is the verdict, and with RefreshRead the groups and when the
// issuer was asked. Sealed is a refresh token the issuer rotated, sealed for
// the session; nil keeps the stored one.
type RefreshDecision struct {
	Verdict RefreshVerdict
	Groups  []string
	ReadAt  time.Time
	Sealed  []byte
}

// RefreshClaim asks for the lease of a session's groups refresh.
type RefreshClaim struct {
	// Hash is the session cookie's SHA-256, PersonID its person.
	Hash     []byte
	PersonID uuid.UUID
	Now      time.Time
	// Interval is COWORK_OIDC_GROUPS_REFRESH; Lease how long the claimant may
	// take before another request may claim the refresh again.
	Interval, Lease time.Duration
}

// RefreshLease is a claimed refresh: the lease, which the answer must still
// find, and the sealed refresh token — nil when none is kept.
type RefreshLease struct {
	Until  time.Time
	Sealed []byte
}

// ClaimSessionRefresh claims a session's due groups refresh in one short
// transaction (the security review of 2026-10-04, M1): false when it is not
// due, or another request holds it, and the request is then served on the
// groups the session holds without waiting. The caller asks the issuer with no
// connection held, and gives the answer to ApplySessionRefresh.
func (db *DB) ClaimSessionRefresh(ctx context.Context, in RefreshClaim) (RefreshLease, bool, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return RefreshLease{}, false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := setContext(ctx, tx, uuid.Nil, Caller{UserID: in.PersonID, SessionHash: in.Hash}, ""); err != nil {
		return RefreshLease{}, false, err
	}
	until := in.Now.Add(in.Lease).Truncate(time.Microsecond).UTC()
	sealed, err := writeq.New(tx).ClaimSessionRefresh(ctx, writeq.ClaimSessionRefreshParams{
		LeaseUntil: &until, TokenHash: in.Hash, DueBefore: ptrTo(in.Now.Add(-in.Interval)), Now: &in.Now,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RefreshLease{}, false, nil
	}
	if err != nil {
		return RefreshLease{}, false, fmt.Errorf("claim the refresh: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RefreshLease{}, false, fmt.Errorf("commit transaction: %w", err)
	}
	return RefreshLease{Until: until, Sealed: sealed}, true, nil
}

// SessionRefresh is the answer of a claimed refresh, to apply.
type SessionRefresh struct {
	Hash     []byte
	PersonID uuid.UUID
	// Lease is the claim's; a session whose lease is another's by now — the
	// claimant took too long — is left to the other.
	Lease      time.Time
	Now        time.Time
	RetryAfter time.Duration
	RequestID  uuid.UUID
	SourceHash []byte
	Decision   RefreshDecision
	// Judge decides on a person's issuer and groups: whether the gate admits
	// them, and whether they hold the administrator group.
	Judge func(issuer *string, groups []string) (admitted, admin bool)
}

// RefreshResult says whether the session ended, and why.
type RefreshResult struct {
	Ended  bool
	Reason string
}

// ApplySessionRefresh applies a refresh's answer in one short transaction,
// under the person's lock and the session's row lock (docs/adr/0030 D5,
// docs/adr/0031 D3, D4): the issuer's refusal ends the session (revoked, cause
// identity-provider); an issuer that could not be asked defers the next
// attempt; groups the issuer said now are the session's and — unless the
// person's are newer — the person's, and the person's memberships are derived
// from them while the gate admits them; groups judged without a read change the
// session's row only. A person the gate refuses loses every session at once
// (revoked, cause gate).
func (db *DB) ApplySessionRefresh(ctx context.Context, in SessionRefresh) (RefreshResult, error) {
	t, err := db.beginIdentity(ctx, in.RequestID, in.SourceHash, in.Hash)
	if err != nil {
		return RefreshResult{}, err
	}
	defer t.rollback(ctx)
	if err := t.forPerson(ctx, in.PersonID); err != nil {
		return RefreshResult{}, err
	}
	row, err := t.w.LockSessionForRefresh(ctx, in.Hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return RefreshResult{Ended: true, Reason: "the session ended meanwhile"}, nil
	}
	if err != nil {
		return RefreshResult{}, fmt.Errorf("lock the session: %w", err)
	}
	if row.RefreshRetryAt == nil || !row.RefreshRetryAt.Equal(in.Lease) {
		return RefreshResult{Reason: "another request holds the refresh now"}, t.commit(ctx)
	}
	switch in.Decision.Verdict {
	case RefreshUnreachable:
		retry := in.Now.Add(in.RetryAfter)
		if err := t.w.DeferSessionRefresh(ctx, writeq.DeferSessionRefreshParams{RetryAt: &retry,
			RefreshTokenSealed: in.Decision.Sealed, SessionID: row.ID}); err != nil {
			return RefreshResult{}, fmt.Errorf("defer the refresh: %w", err)
		}
		return RefreshResult{Reason: "the issuer could not be reached"}, t.commit(ctx)
	case RefreshRefused:
		n, err := t.w.DeleteSessionByHash(ctx, in.Hash)
		if err != nil {
			return RefreshResult{}, fmt.Errorf("end the session: %w", err)
		}
		return t.ended(ctx, in.PersonID, n, causeIssuer)
	}
	return t.applyGroups(ctx, in, row.ID)
}

// applyGroups applies a refresh that read the groups, or judges the person's
// groups as they stand when it read nothing — or something older than the
// person's (M2).
func (t *identityTx) applyGroups(ctx context.Context, in SessionRefresh, session uuid.UUID) (RefreshResult, error) {
	d := in.Decision
	person, err := t.w.GetUser(ctx, in.PersonID)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("read the person: %w", err)
	}
	read := d.Verdict == RefreshRead && (person.OidcGroupsAt == nil || !person.OidcGroupsAt.After(d.ReadAt))
	groups := d.Groups
	if !read {
		// Nothing read, or a newer word than this one — another session's
		// refresh, a login: the person's groups as they stand are judged.
		groups = person.OidcGroups
	}
	admitted, admin := in.Judge(person.OidcIssuer, groups)
	if err := t.w.SetSessionGroups(ctx, writeq.SetSessionGroupsParams{Groups: nonNilStrings(groups), RefreshedAt: &in.Now,
		RefreshTokenSealed: d.Sealed, SessionID: session}); err != nil {
		return RefreshResult{}, fmt.Errorf("store the groups: %w", err)
	}
	if read {
		if err := t.keepSnapshot(ctx, in.PersonID, groups, admin, admitted, d.ReadAt, CauseRefresh); err != nil {
			return RefreshResult{}, err
		}
	}
	if !admitted {
		n, err := t.w.DeleteSessionsOfUser(ctx, in.PersonID)
		if err != nil {
			return RefreshResult{}, fmt.Errorf("end the sessions: %w", err)
		}
		return t.ended(ctx, in.PersonID, n, causeGate)
	}
	return RefreshResult{}, t.commit(ctx)
}

// EndProviderSessions ends every session of a person the gate no longer admits
// before any refresh is due — a person of another issuer than the configured
// one, or of an identity provider that is no longer configured (the security
// review of 2026-10-04, m6) — recorded as revoked, cause gate.
func (db *DB) EndProviderSessions(ctx context.Context, person uuid.UUID, requestID uuid.UUID, sourceHash []byte) error {
	t, err := db.beginIdentity(ctx, requestID, sourceHash, nil)
	if err != nil {
		return err
	}
	defer t.rollback(ctx)
	if err := t.forPerson(ctx, person); err != nil {
		return err
	}
	n, err := t.w.DeleteSessionsOfUser(ctx, person)
	if err != nil {
		return fmt.Errorf("end the sessions: %w", err)
	}
	_, err = t.ended(ctx, person, n, causeGate)
	return err
}

// RefreshDue reports whether a session's groups are to be read again: a
// session of the identity provider whose groups are older than the interval,
// unless the issuer could not be reached and the wait after it has not passed.
func RefreshDue(method string, refreshedAt, retryAt *time.Time, now time.Time, interval time.Duration) bool {
	if method != MethodOIDC || interval <= 0 {
		return false
	}
	if retryAt != nil && now.Before(*retryAt) {
		return false
	}
	return refreshedAt == nil || !refreshedAt.After(now.Add(-interval))
}

// keepSnapshot stores the person's groups as the issuer said them and, while
// the gate admits them, derives the person's memberships from them.
func (t *identityTx) keepSnapshot(ctx context.Context, person uuid.UUID, groups []string, admin, admitted bool, readAt time.Time, cause string) error {
	if err := t.storeSnapshot(ctx, person, groups, admin, admitted, readAt, cause); err != nil {
		return err
	}
	if !admitted {
		return nil
	}
	return t.deriveEverywhere(ctx, person, groups, cause)
}

// storeSnapshot stores the person's groups and administrator flag as the
// issuer said them at readAt, and records what changed. The gate's check is
// stamped only when the gate admitted the groups: a person outside it meets the
// gate at their next token request, not one interval later
// (docs/adr/0035 D8), and counts as no administrator of a tenant (m5).
func (t *identityTx) storeSnapshot(ctx context.Context, person uuid.UUID, groups []string, admin, admitted bool, readAt time.Time, cause string) error {
	cur, err := t.w.GetUser(ctx, person)
	if err != nil {
		return fmt.Errorf("read the person: %w", err)
	}
	var checked *time.Time
	if admitted {
		checked = &readAt
	}
	if err := t.w.SetPersonGroups(ctx, writeq.SetPersonGroupsParams{Groups: nonNilStrings(groups), Now: &readAt, GateCheckedAt: checked,
		GlobalAdmin: admin, ID: person}); err != nil {
		return fmt.Errorf("store the person's groups: %w", err)
	}
	t.recordChanges(person, map[string]any{fieldGlobalAdmin: cur.GlobalAdmin}, map[string]any{fieldGlobalAdmin: admin},
		unnamed{groups: !sameGroups(cur.OidcGroups, groups)}, cause)
	return t.flushIn(ctx, uuid.Nil)
}

// ended records the sessions the identity provider ended and commits.
func (t *identityTx) ended(ctx context.Context, person uuid.UUID, n int64, cause string) (RefreshResult, error) {
	t.w.Record(Event{EntityType: entityUser, EntityID: person, Action: "revoked", Reason: cause,
		After: map[string]any{"sessions_ended": n}})
	if err := t.flushIn(ctx, uuid.Nil); err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{Ended: true, Reason: cause}, t.commit(ctx)
}

// TokenGate asks for a token's gate check of its person (docs/adr/0035 D8).
type TokenGate struct {
	PersonID   uuid.UUID
	Now        time.Time
	Interval   time.Duration
	RequestID  uuid.UUID
	SourceHash []byte
	// Judge decides on the person's issuer and groups as of their last login
	// or refresh: whether the gate admits them, and whether they hold the
	// administrator group.
	Judge func(issuer *string, groups []string) (admitted, admin bool)
}

// CheckTokenGate checks a token's person against the gate with their stored
// groups, once per interval (docs/adr/0035 D8): admitted, their memberships are
// derived (cause token) and the check stamped; outside, nothing is written and
// the token is refused — not revoked — so every request checks again and the
// token works once the person is back inside.
func (db *DB) CheckTokenGate(ctx context.Context, in TokenGate) (bool, error) {
	t, err := db.beginIdentity(ctx, in.RequestID, in.SourceHash, nil)
	if err != nil {
		return false, err
	}
	defer t.rollback(ctx)
	if err := t.forPerson(ctx, in.PersonID); err != nil {
		return false, err
	}
	cur, err := t.w.GetUser(ctx, in.PersonID)
	if err != nil {
		return false, fmt.Errorf("read the person: %w", err)
	}
	admitted, admin := in.Judge(cur.OidcIssuer, cur.OidcGroups)
	if !admitted {
		return false, nil
	}
	if cur.GateCheckedAt != nil && cur.GateCheckedAt.After(in.Now.Add(-in.Interval)) {
		// Another request checked the person meanwhile.
		return true, t.commit(ctx)
	}
	if err := t.w.StampGateCheck(ctx, writeq.StampGateCheckParams{Now: &in.Now, GlobalAdmin: admin, ID: in.PersonID}); err != nil {
		return false, fmt.Errorf("stamp the check: %w", err)
	}
	t.recordChanges(in.PersonID, map[string]any{fieldGlobalAdmin: cur.GlobalAdmin}, map[string]any{fieldGlobalAdmin: admin}, unnamed{}, CauseToken)
	if err := t.flushIn(ctx, uuid.Nil); err != nil {
		return false, err
	}
	if err := t.deriveEverywhere(ctx, in.PersonID, cur.OidcGroups, CauseToken); err != nil {
		return false, err
	}
	return true, t.commit(ctx)
}

// GateDue reports whether a token's person is to be checked against the gate:
// a person of the identity provider not checked within the interval.
func GateDue(provider bool, checkedAt *time.Time, now time.Time, interval time.Duration) bool {
	return provider && (checkedAt == nil || !checkedAt.After(now.Add(-interval)))
}
