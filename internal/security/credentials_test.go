package security

import (
	"path/filepath"
	"testing"
)

func TestTokenHasEnoughEntropy(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) < 40 || a == b {
		t.Fatalf("unexpected tokens: %q %q", a, b)
	}
}

func TestCertificateFingerprintRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "agent.crt")
	key := filepath.Join(dir, "agent.key")
	want, err := GenerateCertificate(cert, key, []string{"localhost", "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := CertificateFingerprint(cert)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
