package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"

	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// defaultPageSize is the page of a list request without a limit
// (docs/adr/0048 D1).
const defaultPageSize = 50

// cursorCodec signs list cursors so a client can neither forge nor edit one,
// and seals the positions a client must not read (docs/adr/0048 D1, D5). Its
// keys are derived from the server key under labels of their own, so the
// cursors and any later use of the server key never share a key.
type cursorCodec struct {
	key []byte
	// sealer and nonceKey seal a position (sealPosition).
	sealer   cipher.AEAD
	nonceKey []byte
}

func newCursorCodec(sessionKey []byte) cursorCodec {
	derive := func(label string) []byte {
		key, err := hkdf.Key(sha256.New, sessionKey, nil, label, sha256.Size)
		if err != nil {
			panic(err) // only an impossible key length fails
		}
		return key
	}
	block, err := aes.NewCipher(derive("cowork cursor position v1"))
	if err != nil {
		panic(err) // a 32-byte key never fails
	}
	sealer, err := cipher.NewGCM(block)
	if err != nil {
		panic(err) // the standard nonce and tag sizes never fail
	}
	return cursorCodec{key: derive("cowork cursor v1"), sealer: sealer, nonceKey: derive("cowork cursor nonce v1")}
}

// cursorPayload binds a position to the list it came from: the operation and
// the scope (the path parameters' identities).
type cursorPayload struct {
	Op    string `json:"o"`
	Scope string `json:"s"`
	After string `json:"a"`
}

// encode returns the cursor of the position after the given sort value.
func (c cursorCodec) encode(op, scope, after string) string {
	payload, _ := json.Marshal(cursorPayload{Op: op, Scope: scope, After: after})
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(c.sign(payload))
}

// decode returns the sort value a cursor carries. A tampered cursor, or one
// from another list or scope, is invalid_cursor.
func (c cursorCodec) decode(op, scope, cursor string) (string, *problem.Error) {
	encodedPayload, encodedMAC, ok := strings.Cut(cursor, ".")
	if !ok {
		return "", invalidCursor()
	}
	payload, err1 := base64.RawURLEncoding.DecodeString(encodedPayload)
	mac, err2 := base64.RawURLEncoding.DecodeString(encodedMAC)
	if err1 != nil || err2 != nil || !hmac.Equal(mac, c.sign(payload)) {
		return "", invalidCursor()
	}
	var p cursorPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.Op != op || p.Scope != scope {
		return "", invalidCursor()
	}
	return p.After, nil
}

// invalidCursor answers a cursor this list did not hand out.
func invalidCursor() *problem.Error {
	return &problem.Error{Code: problem.InvalidCursor, Detail: "the cursor does not belong to this list",
		Errors: []problem.FieldError{{Pointer: "query:cursor", Message: "invalid cursor"}}}
}

// sealedPositionSize is the length a sealed position is padded to: room for
// the longest rank position — a key of 128 characters, the dot and a ticket
// number — so a sealed position tells nothing by its length.
const sealedPositionSize = 144

// sealPosition encrypts a position the client must not read: the rank key of
// a project's list, computed over tickets the caller may not see
// (docs/adr/0014 D2). AES-256-GCM, deterministic: the nonce is an HMAC of the
// padded position, so two positions share a nonce only when they are equal,
// and a page and its weak ETag stay the same while the list does. The
// signature of the cursor around it still binds it to its list.
func (c cursorCodec) sealPosition(position string) string {
	plain := make([]byte, max(sealedPositionSize, len(position)))
	copy(plain, position)
	m := hmac.New(sha256.New, c.nonceKey)
	m.Write(plain)
	n := c.sealer.NonceSize()
	nonce := m.Sum(nil)[:n:n]
	return base64.RawURLEncoding.EncodeToString(c.sealer.Seal(nonce, nonce, plain, nil))
}

// openPosition returns the position sealPosition sealed; false for anything
// it did not seal under this server key.
func (c cursorCodec) openPosition(sealed string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	n := c.sealer.NonceSize()
	if err != nil || len(raw) < n {
		return "", false
	}
	plain, err := c.sealer.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(plain), "\x00"), true
}

func (c cursorCodec) sign(payload []byte) []byte {
	m := hmac.New(sha256.New, c.key)
	m.Write(payload)
	return m.Sum(nil)
}

// pageSize is the requested limit, the default when none was given, clamped
// to COWORK_MAX_PAGE_SIZE (docs/adr/0039 D2: clamped, not refused).
func (h *handler) pageSize(limit *int) int {
	n := defaultPageSize
	if limit != nil && *limit > 0 {
		n = *limit
	}
	if h.opts.MaxPageSize > 0 && n > h.opts.MaxPageSize {
		n = h.opts.MaxPageSize
	}
	return n
}

// limitArg is the LIMIT of a page query: one row more than the page, which
// tells whether a next page exists.
func limitArg(size int) int32 {
	if size < 1 {
		return 2
	}
	if size >= math.MaxInt32-1 {
		return math.MaxInt32
	}
	return int32(size + 1)
}

// page trims a result fetched with one row more than the page size and
// returns the cursor of the next page, or nil at the end. key returns a row's
// sort value.
func page[T any](h *handler, rows []T, size int, op, scope string, key func(T) string) ([]T, *string) {
	if len(rows) <= size {
		return rows, nil
	}
	rows = rows[:size]
	next := h.server.cursors.encode(op, scope, key(rows[len(rows)-1]))
	return rows, &next
}
