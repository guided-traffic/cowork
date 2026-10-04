package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// docs/adr/0030 D5: a session of the identity provider reads its groups again
// once they are older than the interval, and waits after the issuer could not
// be reached; a local session never does.
func TestRefreshDue(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	ahead := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	interval := 15 * time.Minute
	assert.False(t, RefreshDue(MethodLocal, ago(time.Hour), nil, now, interval), "a local session")
	assert.False(t, RefreshDue(MethodOIDC, ago(14*time.Minute), nil, now, interval), "within the interval")
	assert.True(t, RefreshDue(MethodOIDC, ago(15*time.Minute), nil, now, interval), "at the interval")
	assert.True(t, RefreshDue(MethodOIDC, nil, nil, now, interval), "never refreshed")
	assert.False(t, RefreshDue(MethodOIDC, ago(time.Hour), ahead(30*time.Second), now, interval), "waiting after the issuer failed")
	assert.True(t, RefreshDue(MethodOIDC, ago(time.Hour), ago(time.Second), now, interval), "the wait is over")
	assert.False(t, RefreshDue(MethodOIDC, ago(time.Hour), nil, now, 0), "no interval")
}

// docs/adr/0035 D8: a token's person of the identity provider is checked once
// per interval; a local person never.
func TestGateDue(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	assert.False(t, GateDue(false, nil, now, time.Minute), "a local person")
	assert.True(t, GateDue(true, nil, now, time.Minute))
	assert.False(t, GateDue(true, ago(59*time.Second), now, time.Minute))
	assert.True(t, GateDue(true, ago(time.Minute), now, time.Minute))
}
