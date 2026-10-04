package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	jose "github.com/go-jose/go-jose/v4"
)

// keySet is the issuer's signing keys, fetched from its jwks_uri and fetched
// again when a token names a key it does not know — the rotation the standard
// prescribes (docs/adr/0029 D1). It is cowork's own, not go-oidc's, for two
// reasons (the security review of 2026-10-04, item 12, m7): a refresh must
// tell keys that could not be fetched — the issuer's trouble, the session is
// served — from a signature no key verifies — the session ends; and no error
// of it may carry the issuer's answer into a log.
type keySet struct {
	client *http.Client
	url    string
	algs   []jose.SignatureAlgorithm

	mu    sync.Mutex
	keys  []jose.JSONWebKey
	fetch sync.Mutex
}

// keysProbe is how a verification learns that the keys could not be fetched:
// go-oidc formats the key set's error with %v, which ends the chain errors.Is
// would follow, so the probe travels in the context instead.
type keysProbe struct{ unavailable bool }

type probeKey struct{}

func withProbe(ctx context.Context, p *keysProbe) context.Context {
	return context.WithValue(ctx, probeKey{}, p)
}

func markUnavailable(ctx context.Context) {
	if p, ok := ctx.Value(probeKey{}).(*keysProbe); ok {
		p.unavailable = true
	}
}

var errNoKey = errors.New("no key of the issuer verifies the token's signature")

// VerifySignature checks the token's signature with the cached keys, and with
// the keys fetched again when none of them verifies it.
func (k *keySet) VerifySignature(ctx context.Context, raw string) ([]byte, error) {
	jws, err := jose.ParseSigned(raw, k.algs)
	if err != nil || len(jws.Signatures) != 1 {
		return nil, errors.New("the token is no signed JWT of an accepted algorithm")
	}
	kid := jws.Signatures[0].Header.KeyID
	k.mu.Lock()
	cached := k.keys
	k.mu.Unlock()
	if payload, ok := verifyWith(jws, cached, kid); ok {
		return payload, nil
	}
	keys, err := k.refresh(ctx)
	if err != nil {
		markUnavailable(ctx)
		return nil, err
	}
	if payload, ok := verifyWith(jws, keys, kid); ok {
		return payload, nil
	}
	return nil, errNoKey
}

func verifyWith(jws *jose.JSONWebSignature, keys []jose.JSONWebKey, kid string) ([]byte, bool) {
	for i := range keys {
		key := &keys[i]
		if (kid != "" && key.KeyID != kid) || key.Use == "enc" {
			continue
		}
		if payload, err := jws.Verify(key); err == nil {
			return payload, true
		}
	}
	return nil, false
}

// refresh fetches the keys, one fetch at a time.
func (k *keySet) refresh(ctx context.Context) ([]jose.JSONWebKey, error) {
	k.fetch.Lock()
	defer k.fetch.Unlock()
	var set jose.JSONWebKeySet
	err := getJSON(ctx, k.client, k.url, func(r io.Reader) error { return json.NewDecoder(r).Decode(&set) })
	if err != nil {
		return nil, errors.New("the issuer's keys could not be fetched: " + shortError(err))
	}
	keys := make([]jose.JSONWebKey, 0, len(set.Keys))
	for _, key := range set.Keys {
		if key.Valid() && key.IsPublic() {
			keys = append(keys, key)
		}
	}
	k.mu.Lock()
	k.keys = keys
	k.mu.Unlock()
	return keys, nil
}

// supportedAlgs are the signature algorithms an ID token may use: the
// asymmetric ones, of which the issuer names the ones it signs with.
var supportedAlgs = map[string]jose.SignatureAlgorithm{
	"RS256": jose.RS256, "RS384": jose.RS384, "RS512": jose.RS512,
	"ES256": jose.ES256, "ES384": jose.ES384, "ES512": jose.ES512,
	"PS256": jose.PS256, "PS384": jose.PS384, "PS512": jose.PS512, "EdDSA": jose.EdDSA,
}

// acceptedAlgs is what the issuer names that cowork verifies, RS256 when it
// names nothing, as the standard makes it mandatory.
func acceptedAlgs(named []string) ([]jose.SignatureAlgorithm, []string) {
	if len(named) == 0 {
		named = []string{"RS256"}
	}
	var algs []jose.SignatureAlgorithm
	var names []string
	for _, n := range named {
		if a, ok := supportedAlgs[n]; ok {
			algs, names = append(algs, a), append(names, n)
		}
	}
	return algs, names
}
