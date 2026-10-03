package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRolesAndScopesAreOrdered(t *testing.T) {
	assert.True(t, RoleAdmin.AtLeast(RoleMember))
	assert.True(t, RoleMember.AtLeast(RoleMember))
	assert.False(t, RoleViewer.AtLeast(RoleMember))
	assert.False(t, Role("owner").AtLeast(RoleViewer), "an unknown role is below every role")
	assert.Equal(t, RoleViewer, RoleMember.Min(RoleViewer))
	assert.True(t, ScopeAdmin.AtLeast(ScopeWrite))
	assert.False(t, ScopeRead.AtLeast(ScopeWrite))
	assert.False(t, Scope("").AtLeast(ScopeRead))
}

// docs/adr/0007 D1: the slash ends the tenant, the last hyphen ends the
// project key; the number is a positive integer.
func TestParseTicketKey(t *testing.T) {
	cases := map[string]TicketKey{
		"guided-traffic/VKO-12": {Tenant: "guided-traffic", Project: "VKO", Number: 12},
		"VKO-12":                {Project: "VKO", Number: 12},
		"acme/A1-1":             {Tenant: "acme", Project: "A1", Number: 1},
		"my-tenant-2/COW-999":   {Tenant: "my-tenant-2", Project: "COW", Number: 999},
		"COW-2147483647":        {Project: "COW", Number: 2147483647},
	}
	for in, want := range cases {
		got, err := ParseTicketKey(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "VKO", "VKO-", "VKO-0", "VKO-012", "VKO--1", "vko-1", "V-1", "VKO-1x",
		"Acme/VKO-1", "/VKO-1", "acme/VKO", "acme/VK-O-1", "VKOVKOVKOVK-1", "acme/sub/VKO-1",
		"VKO-2147483648", "VKO-4294967297"} {
		_, err := ParseTicketKey(in)
		assert.Error(t, err, in)
	}
	assert.Equal(t, "acme/VKO-12", FullKey("acme", "VKO", 12))
}

func TestKeyInTenant(t *testing.T) {
	short, err := ParseTicketKey("VKO-3")
	require.NoError(t, err)
	k, err := short.InTenant("acme")
	require.NoError(t, err)
	assert.Equal(t, "acme", k.Tenant)

	full, err := ParseTicketKey("other/VKO-3")
	require.NoError(t, err)
	_, err = full.InTenant("acme")
	assert.Error(t, err, "a full key of another tenant is refused")
}

// docs/adr/0010 D3, rule set v1, first match.
func TestDeriveUrgency(t *testing.T) {
	cases := []struct {
		in   UrgencyInputs
		u    Urgency
		rule string
	}{
		{UrgencyInputs{State: StateFiled}, UrgencyLater, "v1:default"},
		{UrgencyInputs{State: StateBlocked, BlockKind: BlockRelease}, UrgencyRelease, "v1:release-block"},
		{UrgencyInputs{State: StateBlocked, BlockKind: BlockDecision}, UrgencyIcebox, "v1:icebox-block"},
		{UrgencyInputs{State: StateBlocked, BlockKind: BlockHuman}, UrgencyIcebox, "v1:icebox-block"},
		{UrgencyInputs{State: StateBlocked, BlockKind: BlockProduct}, UrgencyIcebox, "v1:icebox-block"},
		{UrgencyInputs{State: StateBlocked, BlockKind: BlockExternal}, UrgencyLater, "v1:default"},
		{UrgencyInputs{State: StateInProgress, OpenDecisionBlocker: true}, UrgencyIcebox, "v1:icebox-decision"},
		{UrgencyInputs{State: StateBlocked, BlockKind: BlockRelease, OpenDecisionBlocker: true}, UrgencyRelease, "v1:release-block"},
	}
	for _, c := range cases {
		u, rule := DeriveUrgency(c.in)
		assert.Equal(t, c.u, u, "%+v", c.in)
		assert.Equal(t, c.rule, rule, "%+v", c.in)
	}
}

func TestValidProgress(t *testing.T) {
	for _, p := range []int{0, 5, 50, 95, 100} {
		assert.True(t, ValidProgress(p), p)
	}
	for _, p := range []int{-5, 1, 99, 105} {
		assert.False(t, ValidProgress(p), p)
	}
}

func TestSecurityClassesThatMakeATicketConfidential(t *testing.T) {
	assert.True(t, SecurityLive.MakesConfidential())
	assert.True(t, SecurityBoundary.MakesConfidential())
	assert.False(t, SecurityHardening.MakesConfidential())
	assert.False(t, SecurityNone.MakesConfidential())
}

// docs/adr/0009 D2–D4: every pair of states, and exactly the matrix's pairs
// move; a blocked ticket leaves only to where it came from.
func TestClassifyMove(t *testing.T) {
	states := []TicketState{StateFiled, StateAnalysed, StateDecided, StateInProgress, StateBlocked, StateDone, StateDropped}
	allowed := map[[2]TicketState]Move{
		{StateFiled, StateAnalysed}:      MoveForward,
		{StateAnalysed, StateDecided}:    MoveForward,
		{StateDecided, StateInProgress}:  MoveForward,
		{StateInProgress, StateDone}:     MoveDone,
		{StateInProgress, StateDecided}:  MoveBackward,
		{StateInProgress, StateAnalysed}: MoveBackward,
		{StateDecided, StateAnalysed}:    MoveBackward,
		{StateFiled, StateBlocked}:       MoveBlock,
		{StateAnalysed, StateBlocked}:    MoveBlock,
		{StateDecided, StateBlocked}:     MoveBlock,
		{StateInProgress, StateBlocked}:  MoveBlock,
		{StateBlocked, StateDecided}:     MoveUnblock,
		{StateFiled, StateDropped}:       MoveDrop,
		{StateAnalysed, StateDropped}:    MoveDrop,
		{StateDecided, StateDropped}:     MoveDrop,
		{StateInProgress, StateDropped}:  MoveDrop,
		{StateBlocked, StateDropped}:     MoveDrop,
		{StateDone, StateFiled}:          MoveReopen,
		{StateDropped, StateFiled}:       MoveReopen,
	}
	for _, from := range states {
		for _, to := range states {
			assert.Equal(t, allowed[[2]TicketState{from, to}], ClassifyMove(from, to, StateDecided), "%s → %s", from, to)
		}
	}
	assert.True(t, MoveBlock.NeedsReason())
	assert.False(t, MoveForward.NeedsReason())
	assert.False(t, MoveDone.NeedsReason(), "done needs a note, not a reason")
}

// docs/adr/0016 D3: the allow-list as a fixture; a change to it is a change
// of this table.
func TestDetectAttachmentType(t *testing.T) {
	for _, c := range []struct {
		name, body, stored, detected string
		ok                           bool
	}{
		{"png", "\x89PNG\r\n\x1a\n0000", TypePNG, TypePNG, true},
		{"jpeg", "\xff\xd8\xff\xe0", TypeJPEG, TypeJPEG, true},
		{"gif", "GIF89a", TypeGIF, TypeGIF, true},
		{"webp", "RIFF0000WEBPVP8 ", TypeWebP, TypeWebP, true},
		{"pdf", "%PDF-1.7\n", TypePDF, TypePDF, true},
		{"markdown", "# Title\n\nSome *text*.\n", TypeText, TypeText, true},
		{"patch", "--- a/x\n+++ b/x\n@@ -1 +1 @@\n", TypeText, TypeText, true},
		{"svg", `<svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>`, TypeSVG, TypeText, true},
		{"svg with prolog", `<?xml version="1.0"?><svg/>`, TypeSVG, "text/xml; charset=utf-8", true},
		{"html", "<!DOCTYPE html><html><script>alert(1)</script></html>", "", "text/html; charset=utf-8", false},
		{"xml", `<?xml version="1.0"?><root/>`, "", "text/xml; charset=utf-8", false},
		{"zip", "PK\x03\x04", "", "application/zip", false},
		{"binary", "\x00\x01\x02\x03", "", "application/octet-stream", false},
	} {
		stored, detected, ok := DetectAttachmentType([]byte(c.body))
		assert.Equal(t, c.ok, ok, c.name)
		assert.Equal(t, c.stored, stored, c.name)
		assert.Equal(t, c.detected, detected, c.name)
	}
	assert.True(t, InlineAttachment(TypePNG))
	assert.False(t, InlineAttachment(TypeSVG), "an SVG is a download")
	assert.False(t, InlineAttachment(TypePDF))
}

func TestSanitizeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"report.pdf":             "report.pdf",
		"../../etc/passwd":       "passwd",
		`C:\Users\x\evil.png`:    "evil.png",
		"a\"b\x00c\nd.txt":       "abcd.txt",
		"cafe\u0301.md":          "café.md",
		"":                       "attachment",
		"..":                     "attachment",
		"  spaced  ":             "spaced",
		strings.Repeat("é", 200): strings.Repeat("é", 127),
		"invoice\u202Etxt.exe":   "invoicetxt.exe",
	} {
		assert.Equal(t, want, SanitizeFileName(in), in)
	}
}

// docs/adr/0016 D5: a download's name ends in an extension of the type its
// bytes are.
func TestFileNameFor(t *testing.T) {
	for _, c := range []struct{ name, typ, want string }{
		{"shot.png", TypePNG, "shot.png"},
		{"photo.JPEG", TypeJPEG, "photo.JPEG"},
		{"run.bat", TypeText, "run.bat.txt"},
		{"notes.md", TypeText, "notes.md"},
		{"fix.patch", TypeText, "fix.patch"},
		{"report", TypePDF, "report.pdf"},
		{"image.png", TypeGIF, "image.png.gif"},
		{".md", TypeText, ".md.txt"},
		{strings.Repeat("a", 255), TypeSVG, strings.Repeat("a", 251) + ".svg"},
	} {
		assert.Equal(t, c.want, FileNameFor(c.name, c.typ), c.name)
	}
}
