package api

import (
	"net"
	"net/netip"
	"strings"
)

// trustedProxies are the networks of the proxies that stand between a client
// and the backend (COWORK_TRUSTED_PROXIES, docs/adr/0035 D2). An address inside
// one of them is a hop of ours: what it forwarded in X-Forwarded-For is read,
// and nothing else is.
type trustedProxies []netip.Prefix

// newTrustedProxies normalises the configured networks, so that an address
// is found in them whichever way it is written: a network given as an
// IPv4-mapped IPv6 prefix becomes the IPv4 prefix, because every address is
// unmapped before it is looked up.
func newTrustedProxies(prefixes []netip.Prefix) trustedProxies {
	out := make(trustedProxies, 0, len(prefixes))
	for _, p := range prefixes {
		if !p.IsValid() {
			continue
		}
		if p.Addr().Is4In6() && p.Bits() >= 96 {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		out = append(out, p.Masked())
	}
	return out
}

func (t trustedProxies) contains(a netip.Addr) bool {
	for _, p := range t {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// clientAddress is the address of the client of a request, found by walking
// X-Forwarded-For from the right (docs/adr/0035 D2, docs/adr/0033 D6):
//
//   - the walk starts at the TCP peer, remote, which nobody can forge;
//   - while the current address is inside a trusted network, the entry to its
//     left in the header becomes the current address;
//   - the first address that is not trusted is the client. What stands to its
//     left was written by it or by someone further out, and is never read;
//   - an entry that is no address stops the walk at the current one, which is
//     then the client: a header that cannot be read to its end is read as far
//     as it can be trusted;
//   - a header that runs out while every address is trusted leaves the
//     leftmost of them.
//
// With no trusted network, or a peer outside them, the peer is the client and
// the header is not looked at. The address comes back in one canonical form —
// unmapped, without a zone, compressed — so that a client has one bucket
// however a proxy writes it. A remote that is no address at all (a test
// server, a socket) is returned as it is.
func (t trustedProxies) clientAddress(remote string, forwardedFor []string) string {
	host := remote
	if h, _, err := net.SplitHostPort(remote); err == nil {
		host = h
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	current := canonicalAddr(peer)
	if !t.contains(current) {
		return current.String()
	}
	for chain := newHops(forwardedFor); t.contains(current); {
		entry, ok := chain.next()
		if !ok {
			break
		}
		next, err := netip.ParseAddr(entry)
		if err != nil {
			break
		}
		current = canonicalAddr(next)
	}
	return current.String()
}

func canonicalAddr(a netip.Addr) netip.Addr { return a.Unmap().WithZone("") }

// hops reads the entries of the X-Forwarded-For header lines from the right,
// one at a time: a header line of a megabyte costs what the hops actually
// walked cost, not a split of the whole. It never changes the lines it is
// given, which are the request's own.
type hops struct {
	lines []string
	line  int // the line being read, from its end
	end   int // the unread part of that line is lines[line][:end]
}

func newHops(lines []string) *hops {
	h := &hops{lines: lines, line: len(lines) - 1}
	if h.line >= 0 {
		h.end = len(lines[h.line])
	}
	return h
}

// next returns the entry to the left of the one it returned before, without
// blanks around it, and false when there is none. An empty entry is returned
// empty, and no address.
func (h *hops) next() (string, bool) {
	if h.line < 0 {
		return "", false
	}
	text := h.lines[h.line][:h.end]
	i := strings.LastIndexByte(text, ',')
	if i < 0 {
		h.line--
		if h.line >= 0 {
			h.end = len(h.lines[h.line])
		}
		return strings.TrimSpace(text), true
	}
	h.end = i
	return strings.TrimSpace(text[i+1:]), true
}
