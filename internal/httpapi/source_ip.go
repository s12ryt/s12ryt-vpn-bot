package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

type SourceIPResolver struct {
	trusted []netip.Prefix
}

func NewSourceIPResolver(trusted []netip.Prefix) SourceIPResolver {
	return SourceIPResolver{trusted: append([]netip.Prefix(nil), trusted...)}
}

func (resolver SourceIPResolver) Resolve(request *http.Request) netip.Addr {
	peer, ok := parseRemoteAddress(request.RemoteAddr)
	if !ok || !resolver.isTrusted(peer) {
		return peer
	}
	if forwarded := request.Header.Get("Forwarded"); forwarded != "" {
		if address, ok := parseForwarded(forwarded); ok {
			return address
		}
		return peer
	}
	if forwardedFor := request.Header.Get("X-Forwarded-For"); forwardedFor != "" {
		first, _, _ := strings.Cut(forwardedFor, ",")
		if address, err := netip.ParseAddr(strings.TrimSpace(first)); err == nil {
			return address.Unmap()
		}
	}
	return peer
}

func (resolver SourceIPResolver) isTrusted(address netip.Addr) bool {
	for _, prefix := range resolver.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseRemoteAddress(value string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		host = value
	}
	address, err := netip.ParseAddr(strings.TrimSpace(host))
	return address.Unmap(), err == nil
}

func parseForwarded(value string) (netip.Addr, bool) {
	first, _, _ := strings.Cut(value, ",")
	for _, parameter := range strings.Split(first, ";") {
		name, rawValue, found := strings.Cut(strings.TrimSpace(parameter), "=")
		if !found || !strings.EqualFold(name, "for") {
			continue
		}
		rawValue = strings.TrimSpace(rawValue)
		if strings.HasPrefix(rawValue, `"`) {
			unquoted, err := strconv.Unquote(rawValue)
			if err != nil {
				return netip.Addr{}, false
			}
			rawValue = unquoted
		}
		return parseForwardedAddress(rawValue)
	}
	return netip.Addr{}, false
}

func parseForwardedAddress(value string) (netip.Addr, bool) {
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap(), true
	}
	if addressPort, err := netip.ParseAddrPort(value); err == nil {
		return addressPort.Addr().Unmap(), true
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		address, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
		return address.Unmap(), err == nil
	}
	return netip.Addr{}, false
}
