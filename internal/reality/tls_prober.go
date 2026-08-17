package reality

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/netip"
	"time"
)

// TLSProber verifies one candidate end to end: DNS resolution and TCP 443
// connection happen inside tls.Dial, the handshake is restricted to TLS 1.3,
// and certificate chain plus hostname verification use the system roots (or
// an explicitly trusted pool for tests). No other ports are contacted.
type TLSProber struct {
	dialTimeout time.Duration
	rootCAs     *x509.CertPool
	dial        func(network, address string, timeout time.Duration) (net.Conn, error)
}

func NewTLSProber(dialTimeout time.Duration, rootCAs *x509.CertPool) (*TLSProber, error) {
	if dialTimeout <= 0 {
		return nil, errors.New("TLS prober requires a positive dial timeout")
	}
	return &TLSProber{
		dialTimeout: dialTimeout,
		rootCAs:     rootCAs,
		dial: func(network, address string, timeout time.Duration) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: timeout}
			return dialer.Dial(network, address)
		},
	}, nil
}

// newTLSProberWithDial keeps tests hermetic: the handshake, TLS 1.3
// restriction and certificate verification stay real while the TCP dial
// targets a local listener.
func newTLSProberWithDial(dialTimeout time.Duration, rootCAs *x509.CertPool, dial func(network, address string, timeout time.Duration) (net.Conn, error)) (*TLSProber, error) {
	if dialTimeout <= 0 || dial == nil {
		return nil, errors.New("TLS prober requires a timeout and dial function")
	}
	return &TLSProber{dialTimeout: dialTimeout, rootCAs: rootCAs, dial: dial}, nil
}

func (prober *TLSProber) Probe(ctx context.Context, domain string) (Target, error) {
	if prober == nil || prober.dialTimeout <= 0 {
		return Target{}, errors.New("TLS prober is not initialized")
	}
	host, port := splitTarget(domain)
	if host == "" || !validDomain(host) {
		return Target{}, errors.New("invalid target")
	}
	if address, parseErr := netip.ParseAddr(host); parseErr == nil && address.IsValid() {
		return Target{}, errors.New("IP targets are not probed")
	}
	address := net.JoinHostPort(host, port)
	started := time.Now()
	connection, dialErr := prober.dial("tcp", address, prober.dialTimeout)
	if dialErr != nil {
		return Target{}, dialErr
	}
	defer connection.Close()
	tlsConnection := tls.Client(connection, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		RootCAs:    prober.rootCAs,
	})
	handshakeContext, cancel := context.WithTimeout(ctx, prober.dialTimeout)
	defer cancel()
	if err := tlsConnection.HandshakeContext(handshakeContext); err != nil {
		return Target{}, err
	}
	state := tlsConnection.ConnectionState()
	if !state.HandshakeComplete || state.Version != tls.VersionTLS13 {
		return Target{}, errors.New("TLS 1.3 handshake did not complete")
	}
	latency := time.Since(started)
	if latency <= 0 {
		latency = time.Nanosecond
	}
	return Target{Domain: domain, TLS13: true, Latency: latency}, nil
}

// splitTarget accepts "domain" (defaults to port 443) or "domain:port".
func splitTarget(domain string) (string, string) {
	if host, _, err := net.SplitHostPort(domain); err == nil {
		return host, mustPort(domain)
	}
	return domain, "443"
}

func mustPort(address string) string {
	_, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return "443"
	}
	return port
}
