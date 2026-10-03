package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prefixes(t testing.TB, cidrs ...string) trustedProxies {
	t.Helper()
	parsed := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		parsed = append(parsed, netip.MustParsePrefix(c))
	}
	return newTrustedProxies(parsed)
}

// docs/adr/0035 D2: the client of a request is the first address that is not a
// trusted proxy, walking X-Forwarded-For from the right, starting at the TCP
// peer; what stands to its left is never read.
func TestClientAddressWalksFromTheRight(t *testing.T) {
	proxies := prefixes(t, "10.0.0.0/8", "fd00::/8")
	for name, tc := range map[string]struct {
		trusted trustedProxies
		remote  string
		header  []string
		want    string
	}{
		"no header, an untrusted peer":           {proxies, "198.51.100.9:4000", nil, "198.51.100.9"},
		"no header, a trusted peer":              {proxies, "10.0.0.5:4000", nil, "10.0.0.5"},
		"a trusted peer with one hop":            {proxies, "10.0.0.5:4000", []string{"203.0.113.1"}, "203.0.113.1"},
		"a peer without a port":                  {proxies, "10.0.0.5", []string{"203.0.113.1"}, "203.0.113.1"},
		"blanks around the entries":              {proxies, "10.0.0.5:4000", []string{"  203.0.113.1  ,  10.0.0.9 "}, "203.0.113.1"},
		"a chain of trusted hops":                {proxies, "10.0.0.5:4000", []string{"203.0.113.1, 10.0.0.9, 10.0.0.8"}, "203.0.113.1"},
		"every hop trusted: the leftmost":        {proxies, "10.0.0.5:4000", []string{"10.0.0.7, 10.0.0.8"}, "10.0.0.7"},
		"an untrusted hop in the middle":         {proxies, "10.0.0.5:4000", []string{"203.0.113.1, 198.51.100.9, 10.0.0.9"}, "198.51.100.9"},
		"a spoofed entry to the left":            {proxies, "10.0.0.5:4000", []string{"198.51.100.77, 203.0.113.1"}, "203.0.113.1"},
		"a spoofed trusted-looking entry":        {proxies, "10.0.0.5:4000", []string{"10.0.0.1, 203.0.113.1"}, "203.0.113.1"},
		"a spoofed chain to the left":            {proxies, "10.0.0.5:4000", []string{"198.51.100.77, 10.0.0.1, 10.0.0.2, 203.0.113.1"}, "203.0.113.1"},
		"an untrusted peer's header is ignored":  {proxies, "198.51.100.9:4000", []string{"203.0.113.1"}, "198.51.100.9"},
		"even a header full of trusted hops":     {proxies, "198.51.100.9:4000", []string{"10.0.0.1, 10.0.0.2"}, "198.51.100.9"},
		"an empty trusted list reads no header":  {nil, "10.0.0.5:4000", []string{"203.0.113.1"}, "10.0.0.5"},
		"an empty list, an untrusted peer":       {nil, "198.51.100.9:4000", []string{"203.0.113.1"}, "198.51.100.9"},
		"several header lines are one chain":     {proxies, "10.0.0.5:4000", []string{"203.0.113.1", "10.0.0.9"}, "203.0.113.1"},
		"several lines, a spoof on the first":    {proxies, "10.0.0.5:4000", []string{"198.51.100.77", "203.0.113.1, 10.0.0.9"}, "203.0.113.1"},
		"a client in a trusted network":          {proxies, "10.0.0.5:4000", []string{"10.9.9.9"}, "10.9.9.9"},
		"IPv6 peer and client":                   {proxies, "[fd00::5]:4000", []string{"2001:db8::1"}, "2001:db8::1"},
		"IPv6 written another way":               {proxies, "[fd00::5]:4000", []string{"2001:DB8:0:0:0:0:0:1"}, "2001:db8::1"},
		"IPv6 chain with an IPv4 hop":            {proxies, "10.0.0.5:4000", []string{"2001:db8::1, fd00::9"}, "2001:db8::1"},
		"an IPv4-mapped peer is the IPv4 peer":   {proxies, "[::ffff:10.0.0.5]:4000", []string{"203.0.113.1"}, "203.0.113.1"},
		"an IPv4-mapped client is the IPv4 one":  {proxies, "10.0.0.5:4000", []string{"::ffff:203.0.113.1"}, "203.0.113.1"},
		"an IPv4-mapped hop is trusted as IPv4":  {proxies, "10.0.0.5:4000", []string{"203.0.113.1, ::ffff:10.0.0.9"}, "203.0.113.1"},
		"a zone is no part of the address":       {prefixes(t, "fe80::/10"), "[fe80::1%eth0]:80", []string{"fe80::2%eth0"}, "fe80::2"},
		"a peer that is no address":              {proxies, "no-such-peer", []string{"203.0.113.1"}, "no-such-peer"},
		"a peer address without a trusted entry": {prefixes(t, "192.0.2.0/24"), "10.0.0.5:4000", []string{"203.0.113.1"}, "10.0.0.5"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.trusted.clientAddress(tc.remote, tc.header))
		})
	}
}

// A malformed entry stops the walk at the hop before it: that hop is the
// client, and nothing to the left of the malformed entry is read.
func TestClientAddressStopsAtAMalformedEntry(t *testing.T) {
	proxies := prefixes(t, "10.0.0.0/8")
	for name, tc := range map[string]struct {
		header []string
		want   string
	}{
		"right next to the peer":        {[]string{"203.0.113.1, garbage"}, "10.0.0.5"},
		"after one trusted hop":         {[]string{"203.0.113.1, garbage, 10.0.0.9"}, "10.0.0.9"},
		"after two trusted hops":        {[]string{"203.0.113.1, garbage, 10.0.0.9, 10.0.0.8"}, "10.0.0.9"},
		"an empty header":               {[]string{""}, "10.0.0.5"},
		"a trailing comma":              {[]string{"203.0.113.1,"}, "10.0.0.5"},
		"an empty entry between":        {[]string{"203.0.113.1,, 10.0.0.9"}, "10.0.0.9"},
		"an entry with a port":          {[]string{"203.0.113.1:8080"}, "10.0.0.5"},
		"a bracketed IPv6 with a port":  {[]string{"[2001:db8::1]:8080"}, "10.0.0.5"},
		"unknown, as some proxies say":  {[]string{"unknown"}, "10.0.0.5"},
		"a name":                        {[]string{"client.example.com"}, "10.0.0.5"},
		"a malformed line after a good": {[]string{"203.0.113.1", "garbage"}, "10.0.0.5"},
		"a good line after a malformed": {[]string{"garbage", "203.0.113.1"}, "203.0.113.1"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, proxies.clientAddress("10.0.0.5:4000", tc.header))
		})
	}
}

// A network given as an IPv4-mapped IPv6 prefix trusts the IPv4 addresses it
// names, and an address outside every network is not trusted.
func TestTrustedProxiesNormalisesItsNetworks(t *testing.T) {
	mapped := prefixes(t, "::ffff:10.0.0.0/104")
	assert.Equal(t, "203.0.113.1", mapped.clientAddress("10.0.0.5:1", []string{"203.0.113.1"}), "the mapped prefix is the IPv4 one")
	assert.Equal(t, "198.51.100.9", mapped.clientAddress("198.51.100.9:1", []string{"203.0.113.1"}))

	masked := newTrustedProxies([]netip.Prefix{netip.MustParsePrefix("10.1.2.3/8")})
	require.Len(t, masked, 1)
	assert.Equal(t, "10.0.0.0/8", masked[0].String(), "host bits are dropped")

	assert.Empty(t, newTrustedProxies([]netip.Prefix{{}}), "an invalid prefix trusts nothing")
	assert.Empty(t, newTrustedProxies(nil))
}

// The walk reads the request's header lines and changes none of them.
func TestClientAddressLeavesTheHeaderAlone(t *testing.T) {
	lines := []string{"198.51.100.77, 203.0.113.1", "10.0.0.9, 10.0.0.8"}
	before := append([]string(nil), lines...)
	assert.Equal(t, "203.0.113.1", prefixes(t, "10.0.0.0/8").clientAddress("10.0.0.5:1", lines))
	assert.Equal(t, before, lines)
}

// A header of a megabyte of trusted hops is walked as far as it goes, and
// nothing in it makes the walk anything but what it is.
func TestClientAddressOfALongChain(t *testing.T) {
	long := "203.0.113.1" + strings.Repeat(", 10.0.0.9", 20000)
	assert.Equal(t, "203.0.113.1", prefixes(t, "10.0.0.0/8").clientAddress("10.0.0.5:1", []string{long}))
}

// The request's own headers reach the walk: every X-Forwarded-For line, and the
// connection's remote address.
func TestWithClientReadsTheRequest(t *testing.T) {
	r, err := http.NewRequest(http.MethodPost, "/auth/local", nil)
	require.NoError(t, err)
	r.RemoteAddr = "10.0.0.5:4000"
	r.Header.Add("X-Forwarded-For", "198.51.100.77")
	r.Header.Add("X-Forwarded-For", "203.0.113.1, 10.0.0.9")
	r.Header.Set("User-Agent", "agent/1")

	trusted := prefixes(t, "10.0.0.0/8")
	got := clientFrom(withClient(context.Background(), r, trusted))
	assert.Equal(t, "203.0.113.1", got.Client)
	assert.Equal(t, "agent/1", got.UserAgent)
	assert.Equal(t, "10.0.0.5", clientFrom(withClient(context.Background(), r, nil)).Client, "no trusted proxy: the peer")
}

// The property that matters, for any input: a peer outside the trusted
// networks is the client, whatever the header says.
func FuzzClientAddress(f *testing.F) {
	for _, seed := range []string{"", "203.0.113.1", "10.0.0.1, 10.0.0.2", "garbage, 10.0.0.9", ",,,", "[::1]:80", "::ffff:10.0.0.9",
		"203.0.113.1, 198.51.100.9, 10.0.0.9", "fe80::1%eth0", " , "} {
		f.Add(seed, "198.51.100.9:4000")
		f.Add(seed, "10.0.0.5:4000")
	}
	proxies := prefixes(f, "10.0.0.0/8", "fd00::/8")
	f.Fuzz(func(t *testing.T, header, remote string) {
		got := proxies.clientAddress(remote, []string{header})
		peer, err := netip.ParseAddr(hostOf(remote))
		if err != nil {
			assert.Equal(t, hostOf(remote), got, "a peer that is no address is returned as it is")
			return
		}
		if !proxies.contains(canonicalAddr(peer)) {
			assert.Equal(t, canonicalAddr(peer).String(), got, "an untrusted peer is the client")
		}
		_, err = netip.ParseAddr(got)
		assert.NoError(t, err, "whatever the header says, an address comes back, never a piece of it")
	})
}

func hostOf(remote string) string {
	if h, _, err := net.SplitHostPort(remote); err == nil {
		return h
	}
	return remote
}
