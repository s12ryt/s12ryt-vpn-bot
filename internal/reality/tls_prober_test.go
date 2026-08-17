package reality

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"
)

func startTLSListener(t *testing.T, minVersion, maxVersion uint16) (addr string, pool *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "local.invalid"},
		DNSNames:     []string{"local.invalid"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   minVersion,
		MaxVersion:   maxVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				buffer := make([]byte, 1)
				_, _ = connection.Read(buffer)
				_ = connection.Close()
			}()
		}
	}()
	t.Cleanup(func() { listener.Close() })
	pool = x509.NewCertPool()
	parsed, parseErr := x509.ParseCertificate(der)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	pool.AddCert(parsed)
	return listener.Addr().String(), pool
}

func localDialProber(t *testing.T, addr string, pool *x509.CertPool) *TLSProber {
	t.Helper()
	prober, err := newTLSProberWithDial(5*time.Second, pool, func(_, _ string, _ time.Duration) (net.Conn, error) {
		return net.Dial("tcp", addr)
	})
	if err != nil {
		t.Fatalf("newTLSProberWithDial() error = %v", err)
	}
	return prober
}

func TestTLSProberAcceptsTLS13WithTrustedCertificate(t *testing.T) {
	addr, pool := startTLSListener(t, tls.VersionTLS13, tls.VersionTLS13)
	prober := localDialProber(t, addr, pool)

	target, err := prober.Probe(context.Background(), "local.invalid:"+portOf(addr))
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if target.Domain == "" || !target.TLS13 || target.Latency <= 0 {
		t.Fatalf("target = %#v", target)
	}
}

func TestTLSProberRejectsTLS12OnlyEndpoints(t *testing.T) {
	addr, pool := startTLSListener(t, tls.VersionTLS12, tls.VersionTLS12)
	prober := localDialProber(t, addr, pool)
	domain := "local.invalid:" + portOf(addr)

	if _, err := prober.Probe(context.Background(), domain); err == nil {
		t.Fatal("TLS 1.2 endpoint must be rejected")
	}
}

func TestTLSProberRejectsUnreachableEndpoints(t *testing.T) {
	prober, err := NewTLSProber(500*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("NewTLSProber() error = %v", err)
	}
	if _, err := prober.Probe(context.Background(), "local.invalid:1"); err == nil {
		t.Fatal("unreachable endpoint must be rejected")
	}
}

func TestTLSProberRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewTLSProber(0, nil); err == nil {
		t.Fatal("zero timeout must be rejected")
	}
	if _, err := NewTLSProber(10*time.Second, nil); err != nil {
		t.Fatalf("system roots default must be accepted: %v", err)
	}
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "443"
	}
	return port
}
