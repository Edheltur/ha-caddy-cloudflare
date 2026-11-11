package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"strings"
	"testing"
)

func TestOptions(t *testing.T) {
	for _, token := range []string{strings.Repeat("a", 40), "cfut_" + strings.Repeat("x", 64), "cfat_" + strings.Repeat("z", 32)} {
		if _, err := readOptions(strings.NewReader(`{"cloudflare_api_token":"` + token + `","caddy_config":"{}"}`)); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range []string{
		`{"cloudflare_api_token":"TOP-SECRET"}`,
		`{"cloudflare_api_token":"TOP-SECRET`,
		`{"cloudflare_api_token":"` + strings.Repeat("a", 40) + `"}`,
		`{"cloudflare_api_token":"` + strings.Repeat("a", 40) + `","caddy_config":"{}","other":true}`,
		`{"cloudflare_api_token":"` + strings.Repeat("a", 40) + `","caddy_config":"{}"} {}`,
	} {
		_, err := readOptions(strings.NewReader(data))
		if err == nil {
			t.Fatal("accepted invalid input")
		}
		if strings.Contains(err.Error(), "TOP-SECRET") {
			t.Fatal("secret leaked")
		}
	}
}

func TestEnvironmentAllowlist(t *testing.T) {
	t.Setenv("SUPERVISOR_TOKEN", "supervisor-secret")
	t.Setenv("HASSIO_TOKEN", "legacy-secret")
	e := strings.Join(childEnv(options{Token: "cloudflare-secret"}), "\n")
	if strings.Contains(e, "supervisor-secret") || strings.Contains(e, "legacy-secret") {
		t.Fatal("inherited token")
	}
}

func TestStorageRejectsSymlink(t *testing.T) {
	d := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, d+"/caddy"); err != nil {
		t.Fatal(err)
	}
	if prepareStorage(d) == nil {
		t.Fatal("followed symlink")
	}
}

func TestInlineOptions(t *testing.T) {
	content := "{\n \"admin\": {\"disabled\": true}\n}\n"
	raw, _ := json.Marshal(options{Token: strings.Repeat("a", 40), Config: content})
	o, err := readOptions(strings.NewReader(string(raw)))
	if err != nil || o.Config != content {
		t.Fatal("multiline configuration not preserved", err)
	}
	for _, content := range []string{"", " \n", "null", "[]", "{ admin off }", "{} {}", strings.Repeat("x", 128*1024+1), "bad\x00config"} {
		raw, _ := json.Marshal(options{Token: strings.Repeat("a", 40), Config: content})
		if _, err := readOptions(strings.NewReader(string(raw))); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
}

func TestWriteConfigReplacesSymlink(t *testing.T) {
	d := t.TempDir()
	target := t.TempDir() + "/untouched"
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, d+"/addon-config"); err != nil {
		t.Fatal(err)
	}
	path, err := writeConfig(d, "first\nsecond\n")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(target)
	if string(b) != "keep" {
		t.Fatal("followed destination symlink")
	}
	b, err = os.ReadFile(path)
	if err != nil || string(b) != "first\nsecond\n" {
		t.Fatal("wrong configuration content", err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("unsafe configuration permissions", err)
	}
	if _, err := writeConfig(d, "updated"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if string(b) != "updated" {
		t.Fatal("stale configuration")
	}
}

func TestClientCA(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	good := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	for _, value := range []string{good, good + good} {
		if err := validateCA(value); err != nil {
			t.Fatal(err)
		}
	}
	cert.IsCA = false
	der, err = x509.CreateCertificate(rand.Reader, cert, cert, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	leaf := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	for _, value := range []string{" ", good + "secret", leaf, "-----BEGIN PRIVATE KEY-----\nSECRET\n-----END PRIVATE KEY-----", strings.Repeat("x", 128*1024+1)} {
		if err := validateCA(value); err == nil {
			t.Fatal("accepted invalid CA")
		} else if strings.Contains(err.Error(), "SECRET") {
			t.Fatal("leaked content")
		}
	}
}
