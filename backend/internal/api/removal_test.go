package api

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The id of a child's relation names the child under its parent and nothing
// else: it opens under that parent alone, shows no ticket's id, fits the
// document's parameter (components/parameters.yaml, ChildRelation), and a
// position this server sealed for another purpose names no child
// (docs/adr/0008 D2 as amended 2026-10-10).
func TestAChildHandleNamesTheChildUnderItsParentAlone(t *testing.T) {
	s := &Server{cursors: newCursorCodec([]byte("0123456789abcdef0123456789abcdef"))}
	parent, child := uuid.New(), uuid.New()
	handle := s.childHandle(parent, child)

	got, ok := s.openChildHandle(handle, parent)
	require.True(t, ok)
	assert.Equal(t, child, got)
	assert.Equal(t, handle, s.childHandle(parent, child), "one relation, one id")
	assert.NotContains(t, handle, child.String())
	assert.NotContains(t, handle, strings.ReplaceAll(child.String(), "-", ""))
	assert.LessOrEqual(t, len(handle), 512)
	assert.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9_-]+$`), handle)

	for name, h := range map[string]string{
		"under another parent":         handle,
		"a position of the relations":  s.cursors.sealPosition("1/" + child.String()),
		"not sealed by this server":    (&Server{cursors: newCursorCodec([]byte("fedcba9876543210fedcba9876543210"))}).childHandle(parent, child),
		"no handle at all":             "child",
		"the child's id in plain text": child.String(),
	} {
		at := parent
		if name == "under another parent" {
			at = uuid.New()
		}
		_, ok := s.openChildHandle(h, at)
		assert.False(t, ok, name)
	}
}
