package domain

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The digits are in byte order, which is the order PostgreSQL's "C"
// collation compares (docs/adr/0014 D2).
func TestRankDigitsAreInByteOrder(t *testing.T) {
	require.Len(t, rankDigits, 62)
	for i := 1; i < len(rankDigits); i++ {
		assert.Less(t, rankDigits[i-1], rankDigits[i])
	}
}

func TestValidRank(t *testing.T) {
	for _, k := range []string{"1", "V", "z", "0V", "a0b", "Zz", strings.Repeat("z", 128), strings.Repeat("0", 127) + "1"} {
		assert.True(t, ValidRank(k), k)
	}
	for _, k := range []string{"", "0", "V0", "a-b", "Ä", "V.1", " V", strings.Repeat("z", 129)} {
		assert.False(t, ValidRank(k), k)
	}
}

func TestRankBetweenRefuses(t *testing.T) {
	for _, c := range []struct{ a, b string }{{"V0", ""}, {"", "0"}, {"x y", "z"}, {"V", strings.Repeat("z", 129)}} {
		_, err := RankBetween(c.a, c.b)
		assert.ErrorIs(t, err, ErrRankKey, "%q, %q", c.a, c.b)
	}
	for _, c := range []struct{ a, b string }{{"V", "V"}, {"W", "V"}, {"V1", "V"}} {
		_, err := RankBetween(c.a, c.b)
		assert.ErrorIs(t, err, ErrRankOrder, "%q, %q", c.a, c.b)
	}
}

func TestRankBetweenPlaces(t *testing.T) {
	for _, c := range []struct{ a, b, want string }{
		{"", "", "V"},
		{"V", "X", "W"},
		{"V", "W", "VV"},
		{"V", "V1", "V0V"},
		{"Vz", "W", "VzV"},
		{"0V", "1", "0l"},
		{"1", "3", "2"},
		{"A", "a", "N"},
		// An open end moves by the least it can: one step of the last digit
		// while that stays within the square of the distance to the end.
		{"V", "", "W"},
		{"", "V", "U"},
		{"z", "", "z1"},
		{"", "1", "0z"},
		{"0001", "", "1"},
	} {
		got, err := RankBetween(c.a, c.b)
		require.NoError(t, err, "%q, %q", c.a, c.b)
		assert.Equal(t, c.want, got, "%q, %q", c.a, c.b)
		assertBetween(t, c.a, got, c.b)
	}
}

// Random places in a growing list: the front, the back and in between. Every
// key is valid, strictly between its neighbours, and the list stays in order
// without a duplicate.
func TestRankBetweenRandomInserts(t *testing.T) {
	r := rand.New(rand.NewPCG(14, 2))
	var keys []string
	for range 5000 {
		var i int
		switch r.IntN(3) {
		case 0:
			i = 0
		case 1:
			i = len(keys)
		default:
			i = r.IntN(len(keys) + 1)
		}
		lo, hi := neighbours(keys, i)
		k, err := RankBetween(lo, hi)
		require.NoError(t, err, "%q, %q", lo, hi)
		assertBetween(t, lo, k, hi)
		keys = slices.Insert(keys, i, k)
	}
	assert.True(t, slices.IsSorted(keys))
	assert.Len(t, slices.Compact(slices.Clone(keys)), len(keys), "no key twice")
}

// Ten thousand filings at the bottom, or moves to the top, stay within five
// characters: an open end gives up the square of its distance at most.
func TestRankEndsStayShort(t *testing.T) {
	for name, front := range map[string]bool{"appends": false, "prepends": true} {
		t.Run(name, func(t *testing.T) {
			var keys []string
			longest := 0
			for range 10000 {
				lo, hi := "", ""
				if len(keys) > 0 {
					if front {
						hi = keys[0]
					} else {
						lo = keys[len(keys)-1]
					}
				}
				k, err := RankBetween(lo, hi)
				require.NoError(t, err)
				assertBetween(t, lo, k, hi)
				longest = max(longest, len(k))
				if front {
					keys = slices.Insert(keys, 0, k)
				} else {
					keys = append(keys, k)
				}
			}
			assert.LessOrEqual(t, longest, 5)
			assert.True(t, slices.IsSorted(keys))
		})
	}
}

// Moves into one and the same gap halve it each time; the key grows by about
// one character every six moves, and when the next would pass 128 characters
// the answer is ErrRankTooLong, never a key the column refuses. How many moves
// a gap takes depends on the side (docs/adr/0014 Residual risks): about 760
// when each lands directly after the same ticket, about 630 when each lands
// directly before it.
func TestRankOneGapRunsOut(t *testing.T) {
	for name, c := range map[string]struct {
		after bool
		moves int
	}{
		"directly after the same ticket":  {true, 762},
		"directly before the same ticket": {false, 635},
	} {
		t.Run(name, func(t *testing.T) {
			lo, hi := "V", "W"
			for i := range 2000 {
				k, err := RankBetween(lo, hi)
				if err != nil {
					require.ErrorIs(t, err, ErrRankTooLong)
					assert.Equal(t, c.moves, i)
					return
				}
				assertBetween(t, lo, k, hi)
				if c.after {
					hi = k
				} else {
					lo = k
				}
			}
			t.Fatal("the gap never ran out")
		})
	}
}

func neighbours(keys []string, i int) (string, string) {
	lo, hi := "", ""
	if i > 0 {
		lo = keys[i-1]
	}
	if i < len(keys) {
		hi = keys[i]
	}
	return lo, hi
}

func assertBetween(t *testing.T, lo, k, hi string) {
	t.Helper()
	assert.True(t, ValidRank(k), "%q is a key", k)
	if lo != "" {
		assert.Less(t, lo, k)
	}
	if hi != "" {
		assert.Less(t, k, hi)
	}
}
