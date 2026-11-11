package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io/fs"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	haHost     = "ha.example.com"
	publicHost = "public.example.com"
	testToken  = "test_only_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
)

type object = map[string]any

func require(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require(t, err)
	return data
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	require(t, os.WriteFile(path, data, 0600))
}

func encode(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require(t, err)
	return data
}

func defaultConfig(t *testing.T) object {
	t.Helper()
	var manifest struct {
		Options struct {
			Config string `yaml:"caddy_config"`
		}
	}
	require(t, yaml.Unmarshal(read(t, "../../config.yaml"), &manifest))
	var config object
	require(t, json.Unmarshal([]byte(manifest.Options.Config), &config))
	return config
}

func app(config object, name string) object { return config["apps"].(object)[name].(object) }
func server(config object) object           { return app(config, "http")["servers"].(object)["proxy"].(object) }

func binary(t *testing.T) string {
	t.Helper()
	path := os.Getenv("CADDY_TEST_BINARY")
	if path == "" {
		t.Skip("set CADDY_TEST_BINARY to the built Caddy binary")
	}
	path, err := filepath.Abs(path)
	require(t, err)
	return path
}

func loopback(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	require(t, err)
	defer l.Close()
	return l.Addr().String()
}

func await(t *testing.T, limit time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("service did not become ready before timeout")
}

func caddy(t *testing.T, bin, path, token string, private bool) func() {
	t.Helper()
	cmd := exec.Command(bin, "run", "--config", path)
	cmd.Env = append(os.Environ(), "CF_API_TOKEN="+token)
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	require(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(syscall.SIGTERM)
		var err error
		select {
		case err = <-done:
		case <-time.After(25 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			err = errors.New("Caddy did not stop in time")
		}
		if strings.Contains(log.String(), token) {
			t.Error("credential appeared in Caddy logs")
		}
		if err != nil {
			if !private {
				t.Log(strings.ReplaceAll(log.String(), token, "[redacted]"))
			}
			t.Errorf("Caddy exit: %v", err)
		}
	}
	t.Cleanup(stop)
	return stop
}

func dialTLS(address string, config *tls.Config) (*tls.Conn, error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", address, config)
	if err == nil {
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	}
	return conn, err
}

type certificate struct {
	pair tls.Certificate
	cert *x509.Certificate
	pem  []byte
	key  []byte
}

func issue(t *testing.T, name string, parent *certificate, usage x509.ExtKeyUsage, expired bool) *certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require(t, err)
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-48 * time.Hour), NotAfter: now.Add(48 * time.Hour), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature}
	issuer := template
	signer := key
	if parent == nil {
		template.IsCA = true
		template.KeyUsage |= x509.KeyUsageCertSign
	} else {
		issuer = parent.cert
		signer = parent.pair.PrivateKey.(*ecdsa.PrivateKey)
		template.ExtKeyUsage = []x509.ExtKeyUsage{usage}
	}
	if usage == x509.ExtKeyUsageServerAuth {
		template.DNSNames = []string{name}
	}
	if expired {
		template.NotAfter = now.Add(-time.Hour)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, &key.PublicKey, signer)
	require(t, err)
	cert, err := x509.ParseCertificate(der)
	require(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require(t, err)
	return &certificate{pair: pair, cert: cert, pem: certPEM, key: keyPEM}
}

func hashes(t *testing.T, root string, include func(string) bool) map[string][32]byte {
	t.Helper()
	sums := map[string][32]byte{}
	require(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && include(path) {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sums[path] = sha256.Sum256(data)
		}
		return nil
	}))
	return sums
}

func command(t *testing.T, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, strings.ReplaceAll(string(out), testToken, "[redacted]"))
	}
	return strings.TrimSpace(string(out))
}
