package api

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/auth"
	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// docs/adr/0031 D2: HttpOnly; Secure; SameSite=Lax; Path=/, no Domain — and the
// __Host- prefix, which the browser enforces.
func TestSessionCookieAttributes(t *testing.T) {
	header := sessionCookie("abc", 12*time.Hour)
	res := &http.Response{Header: http.Header{"Set-Cookie": {header}}}
	cookies := res.Cookies()
	require.Len(t, cookies, 1)
	c := cookies[0]
	assert.Equal(t, "__Host-cowork-session", c.Name)
	assert.Equal(t, "abc", c.Value)
	assert.True(t, c.HttpOnly)
	assert.True(t, c.Secure, "Secure in every environment")
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
	assert.Empty(t, c.Domain, "no Domain")
	assert.Equal(t, 12*3600, c.MaxAge)
	assert.NotContains(t, strings.ToLower(header), "domain")

	cleared := (&http.Response{Header: http.Header{"Set-Cookie": {clearedSessionCookie()}}}).Cookies()
	require.Len(t, cleared, 1)
	assert.Equal(t, auth.SessionCookie, cleared[0].Name)
	assert.Empty(t, cleared[0].Value)
	assert.Less(t, cleared[0].MaxAge, 0, "the browser forgets it")
	assert.True(t, cleared[0].Secure && cleared[0].HttpOnly && cleared[0].Path == "/", "the clearing cookie has the attributes of the cookie it replaces")
}

func request(method string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/me/tokens", nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	return r
}

// docs/adr/0037 D1: on every unsafe method of a session the Origin — or without
// one the Referer — equals COWORK_BASE_URL exactly and X-Requested-With:
// cowork is present; neither Origin nor Referer is refused.
func TestCSRFRule(t *testing.T) {
	const base = "https://cowork.example.com"
	h := &handler{opts: Options{BaseOrigin: base}}
	cowork := []string{"X-Requested-With", "cowork"}

	for name, c := range map[string]struct {
		r    *http.Request
		code *problem.Code
	}{
		"a read is not checked":               {request(http.MethodGet), nil},
		"a HEAD is not checked":               {request(http.MethodHead), nil},
		"origin and header":                   {request(http.MethodPost, append([]string{"Origin", base}, cowork...)...), nil},
		"every unsafe method":                 {request(http.MethodDelete, append([]string{"Origin", base}, cowork...)...), nil},
		"PUT":                                 {request(http.MethodPut, append([]string{"Origin", base}, cowork...)...), nil},
		"PATCH":                               {request(http.MethodPatch, append([]string{"Origin", base}, cowork...)...), nil},
		"the Referer without an Origin":       {request(http.MethodPost, append([]string{"Referer", base + "/t/acme/tickets/COW-1?x=1"}, cowork...)...), nil},
		"a Referer with the default port":     {request(http.MethodPost, append([]string{"Referer", "https://cowork.example.com:443/x"}, cowork...)...), nil},
		"no header":                           {request(http.MethodPost, "Origin", base), &problem.Csrf},
		"the wrong header value":              {request(http.MethodPost, "Origin", base, "X-Requested-With", "XMLHttpRequest"), &problem.Csrf},
		"another origin":                      {request(http.MethodPost, append([]string{"Origin", "https://evil.example.com"}, cowork...)...), &problem.Csrf},
		"another scheme":                      {request(http.MethodPost, append([]string{"Origin", "http://cowork.example.com"}, cowork...)...), &problem.Csrf},
		"another port":                        {request(http.MethodPost, append([]string{"Origin", base + ":8443"}, cowork...)...), &problem.Csrf},
		"a subdomain":                         {request(http.MethodPost, append([]string{"Origin", "https://a.cowork.example.com"}, cowork...)...), &problem.Csrf},
		"a prefix of the host":                {request(http.MethodPost, append([]string{"Origin", base + ".evil.example.com"}, cowork...)...), &problem.Csrf},
		"the null origin":                     {request(http.MethodPost, append([]string{"Origin", "null"}, cowork...)...), &problem.Csrf},
		"neither Origin nor Referer":          {request(http.MethodPost, cowork...), &problem.Csrf},
		"a Referer of another site":           {request(http.MethodPost, append([]string{"Referer", "https://evil.example.com/" + base}, cowork...)...), &problem.Csrf},
		"a Referer that is no URL":            {request(http.MethodPost, append([]string{"Referer", "::"}, cowork...)...), &problem.Csrf},
		"Origin wins over a matching Referer": {request(http.MethodPost, append([]string{"Origin", "https://evil.example.com", "Referer", base + "/"}, cowork...)...), &problem.Csrf},
		"two Origin headers":                  {request(http.MethodPost, append([]string{"Origin", base, "Origin", base}, cowork...)...), &problem.Csrf},
		"two Referer headers":                 {request(http.MethodPost, append([]string{"Referer", base + "/", "Referer", base + "/"}, cowork...)...), &problem.Csrf},
	} {
		t.Run(name, func(t *testing.T) {
			perr := h.csrf(c.r)
			if c.code == nil {
				assert.Nil(t, perr)
				return
			}
			require.NotNil(t, perr)
			assert.Equal(t, *c.code, perr.Code)
			assert.Equal(t, http.StatusForbidden, perr.Code.Status)
		})
	}

	// The login is origin-checked and needs no custom header (docs/adr/0037 D5).
	assert.Nil(t, h.checkOrigin(request(http.MethodPost, "Origin", base)))
	assert.NotNil(t, h.checkOrigin(request(http.MethodPost, "Origin", "https://evil.example.com")))
	assert.NotNil(t, h.checkOrigin(request(http.MethodPost)))
}

// The check fails closed: an installation without COWORK_BASE_URL has no origin
// to compare with, so no write of a cookie passes (docs/adr/0037 D6).
func TestCSRFFailsClosedWithoutABaseURL(t *testing.T) {
	h := &handler{}
	perr := h.csrf(request(http.MethodPost, "Origin", "https://cowork.example.com", "X-Requested-With", "cowork"))
	require.NotNil(t, perr)
	assert.Equal(t, problem.Csrf, perr.Code)
	assert.Nil(t, h.csrf(request(http.MethodGet)), "a read never mutates")
}

// docs/adr/0031 D3 as amended 2026-10-07: every request of a session moves its
// idle clock — a read of any kind, from any site, a write that passes the CSRF
// check — but a write the CSRF check refuses.
func TestWhatMovesTheIdleClock(t *testing.T) {
	const base = "https://cowork.example.com"
	h := &handler{opts: Options{BaseOrigin: base}}
	write := []string{"Origin", base, "X-Requested-With", "cowork"}
	for name, c := range map[string]struct {
		r     *http.Request
		moves bool
	}{
		"a read":                     {request(http.MethodGet), true},
		"a read from another site":   {request(http.MethodGet, "Origin", "https://evil.example.com"), true},
		"a HEAD":                     {request(http.MethodHead), true},
		"a POST":                     {request(http.MethodPost, write...), true},
		"a PUT":                      {request(http.MethodPut, write...), true},
		"a PATCH":                    {request(http.MethodPatch, write...), true},
		"a DELETE":                   {request(http.MethodDelete, write...), true},
		"a write of another origin":  {request(http.MethodPost, "Origin", "https://evil.example.com", "X-Requested-With", "cowork"), false},
		"a write of a sibling host":  {request(http.MethodPost, "Origin", "https://a.example.com", "X-Requested-With", "cowork"), false},
		"a write without the header": {request(http.MethodPost, "Origin", base), false},
	} {
		assert.Equal(t, c.moves, h.movesIdleClock(c.r), name)
	}
	assert.False(t, (&handler{}).movesIdleClock(request(http.MethodPost, write...)),
		"without COWORK_BASE_URL no write passes the check, and none moves the clock")
	assert.True(t, (&handler{}).movesIdleClock(request(http.MethodGet)), "a read moves it all the same")
}

func TestSessionLiveHonoursBothLimits(t *testing.T) {
	h := &handler{opts: Options{SessionIdle: 2 * time.Hour}}
	created := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	absolute := created.Add(12 * time.Hour)
	for name, c := range map[string]struct {
		seen, now time.Time
		live      bool
	}{
		"just used":                 {created, created, true},
		"inside both":               {created.Add(time.Hour), created.Add(2 * time.Hour), true},
		"idle for the whole window": {created, created.Add(2 * time.Hour), false},
		"idle just inside":          {created, created.Add(2*time.Hour - time.Second), true},
		"kept alive past the idle":  {created.Add(5 * time.Hour), created.Add(6 * time.Hour), true},
		"at the absolute limit":     {absolute.Add(-time.Minute), absolute, false},
		"just before it":            {absolute.Add(-time.Minute), absolute.Add(-time.Second), true},
		"used a second ago, but past the absolute limit": {absolute.Add(time.Second), absolute.Add(2 * time.Second), false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.live, h.sessionLive(absolute, c.seen, c.now))
		})
	}
}

// The client address is hashed with a key derived from the server key: the
// same address one hash, whatever its port or notation, and another server key
// another hash. Which address is the client's is clientAddress's to say.
func TestAddressHash(t *testing.T) {
	a := &handler{addressKey: newAddressKey([]byte("0123456789abcdef0123456789abcdef"))}
	b := &handler{addressKey: newAddressKey([]byte("fedcba9876543210fedcba9876543210"))}

	h1 := a.addressHash("203.0.113.7:51234")
	assert.Len(t, h1, 32)
	assert.Equal(t, h1, a.addressHash("203.0.113.7:9"), "the port is no part of the address")
	assert.Equal(t, h1, a.addressHash("[::ffff:203.0.113.7]:80"), "an IPv4-mapped address is the IPv4 address")
	assert.NotEqual(t, h1, a.addressHash("203.0.113.8:51234"))
	assert.NotEqual(t, h1, b.addressHash("203.0.113.7:51234"), "keyed by the server key")
	assert.Equal(t, a.addressHash("[fe80::1%eth0]:80"), a.addressHash("[fe80::1]:80"), "a zone is no part of it")
	v6 := a.addressHash("[2001:db8:1:2::5]:443")
	assert.Equal(t, v6, a.addressHash("[2001:db8:1:2:ffff:ffff:ffff:9]:1"), "an IPv6 address counts by its /64")
	assert.NotEqual(t, v6, a.addressHash("[2001:db8:1:3::5]:443"), "another /64 is another bucket")
	assert.NotContains(t, string(h1), "203.0.113.7", "the address is not in the hash")
	assert.Equal(t, a.addressHash("no-port"), a.addressHash("no-port"), "a peer without a port is still hashed")
}

// What the database keeps of a keyed request is an HMAC under a key derived
// from the server key: bound to the operation, its scope and its body, and no
// plain hash of a body that can carry a temporary password (docs/adr/0045 D4).
func TestIdempotencyFingerprintIsKeyedByTheServerKey(t *testing.T) {
	serverKey := []byte("0123456789abcdef0123456789abcdef")
	a := &Server{h: &handler{fingerprintKey: newFingerprintKey(serverKey)}}
	b := &Server{h: &handler{fingerprintKey: newFingerprintKey([]byte("fedcba9876543210fedcba9876543210"))}}
	body := []byte(`{"display_name":"Sam","role":"member","temporary_password":"Welcome-2026!","username":"sam"}`)

	fp := a.fingerprint("createAccount", "tenant", body)
	assert.Equal(t, fp, a.fingerprint("createAccount", "tenant", body), "one request, one fingerprint")
	assert.NotEqual(t, fp, a.fingerprint("createAccount", "tenant", append(body, ' ')), "bound to the body")
	assert.NotEqual(t, fp, a.fingerprint("createAccount", "other", body), "bound to the scope")
	assert.NotEqual(t, fp, a.fingerprint("createTenant", "tenant", body), "bound to the operation")
	assert.NotEqual(t, fp, b.fingerprint("createAccount", "tenant", body), "keyed by the server key")
	assert.NotEqual(t, sha256.Sum256(append([]byte("createAccount\ntenant\n"), body...)), fp, "no plain hash")
	assert.NotEqual(t, newAddressKey(serverKey), newFingerprintKey(serverKey), "apart from the address key")
}

func TestOptionDefaults(t *testing.T) {
	var o Options
	withDefaults(&o)
	assert.Equal(t, 12*time.Hour, o.SessionLifetime)
	assert.Equal(t, 2*time.Hour, o.SessionIdle)
	assert.Equal(t, 12, o.PasswordMinLength)
	assert.Equal(t, "window", o.LoginLockout)
	assert.Equal(t, 90*24*time.Hour, o.TokenDefaultLifetime)
	assert.Equal(t, 365*24*time.Hour, o.TokenMaxLifetime)
	assert.Zero(t, o.LoginMaxFailures, "0 switches the limit off, it is no default")
}

// The credentials an operation takes are read from the document: the pipeline
// obeys what the document says (docs/adr/0046 D6).
func TestCredentialsComeFromTheDocument(t *testing.T) {
	doc, _, err := loadDocument("test")
	require.NoError(t, err)
	opCredentials := map[string]credentials{}
	for _, path := range doc.Paths.InMatchingOrder() {
		for _, op := range doc.Paths.Value(path).Operations() {
			opCredentials[op.OperationID] = credentialsOf(doc, op)
		}
	}
	both := credentials{bearer: true, session: true}
	for id, want := range map[string]credentials{
		"getMe": both, "listMyTokens": both, "getTenant": both, "createTicket": both, "streamEvents": both,
		"listAccounts": both, "unlockAccount": {session: true}, "deactivateAccount": both, "endAccountSessions": both,
		"createMyToken": {session: true}, "changeMyPassword": {session: true}, "createTenant": {session: true}, "logout": {session: true},
		"createAccount": {session: true}, "resetAccountPassword": {session: true},
		"getChatAvailability": both, "runChatTurn": {session: true},
		"getVersion": {}, "getOpenAPI": {}, "loginLocal": {}, "getAuthOptions": {},
	} {
		got, ok := opCredentials[id]
		require.True(t, ok, id)
		assert.Equal(t, want, got, id)
	}
	for _, path := range doc.Paths.InMatchingOrder() {
		if post := doc.Paths.Value(path).Post; post != nil && post.OperationID == "loginLocal" {
			assert.True(t, originChecked(post), "the login is origin-checked")
		}
	}
}

// docs/adr/0036 D3, docs/adr/0035 D5: a session the header marks as an
// agent's is refused what only a session does — a token, a password, a role
// — after the CSRF check and before anything else; on a route either
// credential takes, it passes on to the agent rules like any agent's request.
func TestAnAgentSessionIsRefusedWhatOnlyASessionDoes(t *testing.T) {
	const base = "https://cowork.example.com"
	h := &handler{opts: Options{BaseOrigin: base}}
	write := request(http.MethodPost, "Origin", base, "X-Requested-With", "cowork")
	agent := auth.Principal{Session: true, Agent: "chat/m/c", Capabilities: auth.AllCapabilities}
	person := auth.Principal{Session: true}
	sessionOnly, either := credentials{session: true}, credentials{bearer: true, session: true}

	for _, op := range []string{"createMyToken", "changeMyPassword", "setMemberGrant", "runChatTurn", "logout"} {
		perr := h.sessionRules(write, agent, operation(op), sessionOnly)
		require.NotNil(t, perr, op)
		assert.Equal(t, problem.AgentForbidden, perr.Code, op)
		assert.Nil(t, h.sessionRules(write, person, operation(op), sessionOnly), "a person's session may: %s", op)
	}
	assert.Nil(t, h.sessionRules(write, agent, operation("createTicket"), either), "the agent rules decide the rest")
	perr := h.sessionRules(request(http.MethodPost), agent, operation("createMyToken"), sessionOnly)
	require.NotNil(t, perr)
	assert.Equal(t, problem.Csrf, perr.Code, "the CSRF check comes first")
}

// operation is an operation of the document by its id alone.
func operation(id string) *openapi3.Operation { return &openapi3.Operation{OperationID: id} }

// docs/adr/0026 D5 as amended 2026-10-07: a session's request for one of the
// five recorded reads comes from the installation's own pages — Sec-Fetch-Site
// same-origin, or none for the address bar — or from a browser that sends no
// such header; a page on a sibling host or another site is refused. Other
// reads and a token's request are not looked at.
func TestARecordedReadComesFromTheInstallationsOwnPages(t *testing.T) {
	h := &handler{opts: Options{BaseOrigin: "https://cowork.example.com"}}
	person := auth.Principal{Session: true}
	token := auth.Principal{Scope: domain.ScopeRead}
	either := credentials{bearer: true, session: true}
	read := func(site ...string) *http.Request {
		r := request(http.MethodGet)
		for _, s := range site {
			r.Header.Add("Sec-Fetch-Site", s)
		}
		return r
	}

	doc, _, err := loadDocument("test")
	require.NoError(t, err)
	var recorded []string
	for _, path := range doc.Paths.InMatchingOrder() {
		for method, op := range doc.Paths.Value(path).Operations() {
			if recordedRead(op) {
				recorded = append(recorded, op.OperationID)
				assert.Equal(t, http.MethodGet, method, "%s is a read", op.OperationID)
			}
		}
	}
	assert.ElementsMatch(t, []string{"downloadAttachment", "exportTicket", "exportTicketContext", "exportProject", "exportTeam",
		// The twins under the family before, which answer as their team paths
		// do (docs/adr/0023 D1).
		"downloadAttachmentDeprecated", "exportTicketDeprecated", "exportTicketContextDeprecated", "exportProjectDeprecated",
		"exportTenant"}, recorded, "the five reads that record an act, and their twins")

	marked := &openapi3.Operation{OperationID: "downloadAttachment", Extensions: map[string]any{"x-cowork-recorded-read": true}}
	for name, c := range map[string]struct {
		r       *http.Request
		refused bool
	}{
		"same-origin":                {read("same-origin"), false},
		"none":                       {read("none"), false},
		"no header":                  {read(), false},
		"same-site":                  {read("same-site"), true},
		"cross-site":                 {read("cross-site"), true},
		"cross-site shouted":         {read(" Cross-Site "), true},
		"a second header cross-site": {read("same-origin", "cross-site"), true},
		"a list with same-site":      {read("same-origin, same-site"), true},
		"a value no browser sends":   {read("elsewhere"), false},
	} {
		perr := h.sessionRules(c.r, person, marked, either)
		if !c.refused {
			assert.Nil(t, perr, name)
			continue
		}
		require.NotNil(t, perr, name)
		assert.Equal(t, problem.Csrf, perr.Code, name)
		assert.Equal(t, http.StatusForbidden, perr.Code.Status, name)
	}
	assert.Nil(t, h.sessionRules(read("cross-site"), person, operation("getTicket"), either), "a read that records nothing")
	assert.Nil(t, h.sessionRules(read("cross-site"), token, marked, either), "a token's request carries no cookie")
}
