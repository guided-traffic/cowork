package domain

import (
	"errors"
	"math/big"
	"strings"
)

// Rank keys (docs/adr/0014 D2). A project's manual order is one sortable
// string per open ticket, and a move writes one key between its new
// neighbours' keys. A key is a base-62 fraction: the digits 0-9, A-Z, a-z in
// their ASCII order — the byte order PostgreSQL's "C" collation compares — read
// as 0.d1d2d3…, so comparing two keys compares their values. A key never ends
// with "0": "V0" would be "V" written longer, and a key ending in "0" could
// leave no key below it. "" stands for an open end: 0 below every key, 1 above.
const rankDigits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// rankBase is the number of digits.
const rankBase = len(rankDigits)

// MaxRankLength is the longest key the rank column takes.
const MaxRankLength = 128

// RankRebalanceLength is the longest key a place is given before its
// project's keys are spread again (docs/adr/0014 Consequences): a key longer
// than this means moves have worn its gap down, and RankSpread gives every
// key of the project room again long before a gap runs out at MaxRankLength.
const RankRebalanceLength = 32

// The errors of RankBetween.
var (
	// ErrRankKey is a bound that is not a rank key.
	ErrRankKey = errors.New("not a rank key: 1 to 128 of 0-9A-Za-z, the last not 0")
	// ErrRankOrder is a lower bound that is not below the upper one.
	ErrRankOrder = errors.New("the lower rank key is not below the upper one")
	// ErrRankTooLong is a place whose neighbours lie so close that no key
	// between them fits the column; the keys around it need rebalancing.
	ErrRankTooLong = errors.New("no rank key between the neighbours fits 128 characters")
)

// ValidRank reports whether s is a rank key: 1 to 128 digits, the last not
// "0".
func ValidRank(s string) bool {
	if s == "" || len(s) > MaxRankLength || s[len(s)-1] == '0' {
		return false
	}
	for i := range len(s) {
		if strings.IndexByte(rankDigits, s[i]) < 0 {
			return false
		}
	}
	return true
}

// RankBetween returns a key strictly between a and b; "" for a is the open
// lower end, "" for b the open upper end, and both open is the first key of
// a project. Between two keys it takes the middle; at an open end it moves by
// at most the square of the distance to that end, so a run of filings at the
// bottom or of moves to the top leaves room shrinking like 1/n and the keys
// growing like 2·log62(n) — ten thousand of them stay within five characters.
func RankBetween(a, b string) (string, error) {
	for _, k := range []string{a, b} {
		if k != "" && !ValidRank(k) {
			return "", ErrRankKey
		}
	}
	var key string
	switch {
	case a == "" && b == "":
		key = string(rankDigits[rankBase/2])
	case b == "":
		key = rankAfter(a)
	case a == "":
		key = rankBefore(b)
	case a >= b:
		return "", ErrRankOrder
	default:
		key = rankMiddle(a, b)
	}
	if len(key) > MaxRankLength {
		return "", ErrRankTooLong
	}
	return key, nil
}

// RankSpread returns n keys in ascending order, evenly spaced over keys of
// one width with at least 62 places between two of them and around the ends —
// the room of the next moves, as migration 17 spread the first keys. The width
// is the smallest that gives that room, so the keys are as short as n allows:
// two characters up to 61 tickets, three up to 3,843.
func RankSpread(n int) []string {
	if n <= 0 {
		return nil
	}
	width := 2
	for rankScale(width-1).Cmp(big.NewInt(int64(n)+1)) < 0 {
		width++
	}
	step := new(big.Int).Quo(rankScale(width), big.NewInt(int64(n)+1))
	keys := make([]string, n)
	for i := range keys {
		keys[i] = rankFormat(new(big.Int).Mul(step, big.NewInt(int64(i)+1)), width)
	}
	return keys
}

// rankMiddle is the middle key between a and b, a < b, b not empty ("" for b
// inside the recursion is 1): the common prefix, then the middle digit, or —
// where the first differing digits are adjacent — a digit more.
func rankMiddle(a, b string) string {
	if b != "" {
		n := 0
		for n < len(b) && rankDigitAt(a, n) == b[n] {
			n++
		}
		if n > 0 {
			return b[:n] + rankMiddle(rankTail(a, n), b[n:])
		}
	}
	lo, hi := 0, rankBase
	if a != "" {
		lo = strings.IndexByte(rankDigits, a[0])
	}
	if b != "" {
		hi = strings.IndexByte(rankDigits, b[0])
	}
	if hi-lo > 1 {
		return string(rankDigits[(lo+hi+1)/2])
	}
	if len(b) > 1 {
		return b[:1]
	}
	return string(rankDigits[lo]) + rankMiddle(rankTail(a, 1), "")
}

// rankDigitAt is a's digit at i, "0" past its end.
func rankDigitAt(a string, i int) byte {
	if i < len(a) {
		return a[i]
	}
	return rankDigits[0]
}

// rankTail is a without its first n digits.
func rankTail(a string, n int) string {
	if n >= len(a) {
		return ""
	}
	return a[n:]
}

// rankAfter is the shortest key above a that lies no further from a than the
// square of a's distance to 1.
func rankAfter(a string) string {
	width := 2*len(a) + 2
	one := rankScale(width)
	av := rankValue(a, width)
	d := new(big.Int).Sub(one, av)
	limit := new(big.Int).Add(av, new(big.Int).Quo(new(big.Int).Mul(d, d), one))
	for l := 1; l <= width; l++ {
		unit := rankScale(width - l)
		c := new(big.Int).Quo(av, unit)
		c.Add(c, big.NewInt(1)).Mul(c, unit)
		if c.Cmp(limit) <= 0 {
			return rankFormat(c, width)
		}
	}
	// Unreachable: d is at least one unit of a's last digit, so the limit
	// lies at least 62² units of width digits above a.
	return rankFormat(new(big.Int).Add(av, big.NewInt(1)), width)
}

// rankBefore is the shortest key below b that lies no further from b than
// the square of b's distance to 0.
func rankBefore(b string) string {
	width := 2*len(b) + 2
	one := rankScale(width)
	bv := rankValue(b, width)
	limit := new(big.Int).Sub(bv, new(big.Int).Quo(new(big.Int).Mul(bv, bv), one))
	for l := 1; l <= width; l++ {
		unit := rankScale(width - l)
		c := new(big.Int).Sub(bv, big.NewInt(1))
		c.Quo(c, unit).Mul(c, unit)
		if c.Sign() > 0 && c.Cmp(limit) >= 0 {
			return rankFormat(c, width)
		}
	}
	// Unreachable, as in rankAfter.
	return rankFormat(new(big.Int).Sub(bv, big.NewInt(1)), width)
}

// rankScale is 62 to the power of n.
func rankScale(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(int64(rankBase)), big.NewInt(int64(n)), nil)
}

// rankValue is k's value in units of width digits; k has at most width.
func rankValue(k string, width int) *big.Int {
	v := new(big.Int)
	base := big.NewInt(int64(rankBase))
	for i := range width {
		v.Mul(v, base).Add(v, big.NewInt(int64(strings.IndexByte(rankDigits, rankDigitAt(k, i)))))
	}
	return v
}

// rankFormat writes v, a value in units of width digits, as a key: width
// digits without their trailing zeros.
func rankFormat(v *big.Int, width int) string {
	out := make([]byte, width)
	rest := new(big.Int).Set(v)
	base := big.NewInt(int64(rankBase))
	digit := new(big.Int)
	for i := width - 1; i >= 0; i-- {
		rest.QuoRem(rest, base, digit)
		out[i] = rankDigits[digit.Int64()]
	}
	return strings.TrimRight(string(out), rankDigits[:1])
}
