package api

import (
	"encoding/base64"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// The longest cursor of the prerequisite tree — a node at the deepest level,
// under the longest project key and number — fits the 512 characters the
// document allows a cursor (components/parameters.yaml, Cursor), so a page
// never ends where the next one could not be asked for (docs/adr/0048 D1).
func TestATreeCursorFitsTheDocument(t *testing.T) {
	codec := newCursorCodec([]byte("0123456789abcdef0123456789abcdef"))
	path := make([]uuid.UUID, treeDepth)
	for i := range path {
		path[i] = uuid.New()
	}
	scope := treeScope(tenantScope{ID: uuid.New()}, "ABCDEFGHIJ", math.MaxInt32, false)
	cursor := codec.encode(treeOp, scope, encodeTreePath(path))
	assert.LessOrEqual(t, len(cursor), 512)

	position, perr := codec.decode(treeOp, scope, cursor)
	require.Nil(t, perr)
	got, perr := decodeTreePath(position)
	require.Nil(t, perr)
	assert.Equal(t, path, got)
}

// A cursor of the prerequisites does not page the dependents, nor another
// ticket's tree.
func TestATreeCursorBelongsToItsTicketAndDirection(t *testing.T) {
	codec := newCursorCodec([]byte("0123456789abcdef0123456789abcdef"))
	ts := tenantScope{ID: uuid.New()}
	cursor := codec.encode(treeOp, treeScope(ts, "ALPHA", 1, false), encodeTreePath([]uuid.UUID{uuid.New()}))
	for name, scope := range map[string]string{
		"the dependents":   treeScope(ts, "ALPHA", 1, true),
		"another ticket":   treeScope(ts, "ALPHA", 2, false),
		"another project":  treeScope(ts, "BETA", 1, false),
		"another tenant's": treeScope(tenantScope{ID: uuid.New()}, "ALPHA", 1, false),
	} {
		_, perr := codec.decode(treeOp, scope, cursor)
		require.NotNil(t, perr, name)
		assert.Equal(t, problem.InvalidCursor, perr.Code, name)
	}
}

// A position that is no path of the tree is refused as a cursor that does
// not belong to the list.
func TestATreePathIsWholeIds(t *testing.T) {
	deep := make([]byte, (treeDepth+1)*16)
	for name, position := range map[string]string{
		"not base64url":         "!!",
		"empty":                 "",
		"a broken id":           base64.RawURLEncoding.EncodeToString(make([]byte, 17)),
		"deeper than the tree":  base64.RawURLEncoding.EncodeToString(deep),
		"another list's id key": uuid.New().String(),
	} {
		_, perr := decodeTreePath(position)
		require.NotNil(t, perr, name)
		assert.Equal(t, problem.InvalidCursor, perr.Code, name)
	}
}
