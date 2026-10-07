// Package github reads the deliveries of GitHub's webhook for the inbound
// integration of docs/adr/0071: the signature over the raw body, the two
// events cowork takes — a pull request, and a push to the repository's
// default branch — and the ticket keys their texts carry by the conventions of
// docs/adr/0068. It is pure: it calls nothing at GitHub and touches no store.
package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// The headers of a delivery (docs/adr/0071 D3, D4). Only the signature covers
// anything — the body; the event and the delivery id are unsigned.
const (
	SignatureHeader = "X-Hub-Signature-256"
	EventHeader     = "X-GitHub-Event"
	DeliveryHeader  = "X-GitHub-Delivery"
)

// signaturePrefix starts the value of SignatureHeader.
const signaturePrefix = "sha256="

// Sign returns the value GitHub sends in SignatureHeader for body under
// secret: sha256= and the hex HMAC-SHA256.
func Sign(secret, body []byte) string {
	return signaturePrefix + hex.EncodeToString(mac(secret, body))
}

// Verify reports whether header is the signature of body under secret. The
// comparison takes the same time wherever the two differ (hmac.Equal), so an
// answer's timing tells nothing of how much of a guess was right
// (docs/adr/0071 D3).
func Verify(secret, body []byte, header string) bool {
	given, ok := strings.CutPrefix(header, signaturePrefix)
	if !ok {
		return false
	}
	sum, err := hex.DecodeString(given)
	if err != nil {
		return false
	}
	return hmac.Equal(sum, mac(secret, body))
}

func mac(secret, body []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return m.Sum(nil)
}
