package integration

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestACMEStaging(t *testing.T) {
	if os.Getenv("ACME_ALLOW_DNS_WRITE") != "yes" {
		t.Skip("ACME staging requires explicit DNS write opt-in")
	}
	token := os.Getenv("CF_API_TOKEN")
	domain := os.Getenv("ACME_TEST_DOMAIN")
	if token == "" || !regexp.MustCompile(`(?i)^[a-z0-9-]+(\.[a-z0-9-]+)+$`).MatchString(domain) {
		t.Fatal("set a scoped Cloudflare token and a test hostname")
	}
	bin := binary(t)
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	require(t, os.Mkdir(state, 0700))
	address := loopback(t)
	config := defaultConfig(t)
	config["storage"].(object)["root"] = state
	srv := server(config)
	srv["listen"] = []string{address}
	srv["tls_connection_policies"] = []object{{"match": object{"sni": []string{domain}}, "protocol_min": "tls1.2"}}
	srv["routes"] = []object{{"match": []object{{"host": []string{domain}}}, "handle": []object{{"handler": "static_response", "body": "staging-ok"}}}}
	tlsApp := app(config, "tls")
	tlsApp["certificates"].(object)["automate"] = []string{domain}
	policy := tlsApp["automation"].(object)["policies"].([]any)[0].(object)
	policy["subjects"] = []string{domain}
	policy["issuers"].([]any)[0].(object)["ca"] = "https://acme-staging-v02.api.letsencrypt.org/directory"
	path := filepath.Join(dir, "caddy.json")
	write(t, path, encode(t, config))

	// Staging roots are not public. Still verify the certificate names this host.
	settings := &tls.Config{ServerName: domain, InsecureSkipVerify: true, VerifyConnection: func(state tls.ConnectionState) error {
		return state.PeerCertificates[0].VerifyHostname(domain)
	}}
	var previous map[string][32]byte
	for range 2 {
		stop := caddy(t, bin, path, token, true)
		await(t, 5*time.Minute, func() bool { code, _ := request(address, settings, domain); return code == 200 })
		current := hashes(t, state, func(path string) bool { ext := filepath.Ext(path); return ext == ".crt" || ext == ".key" })
		var accountKey, serverCert bool
		for path := range current {
			rel, err := filepath.Rel(state, path)
			require(t, err)
			accountKey = accountKey || (strings.HasPrefix(rel, "acme/") && filepath.Ext(path) == ".key")
			serverCert = serverCert || (strings.HasPrefix(rel, "certificates/") && filepath.Ext(path) == ".crt")
		}
		if !accountKey || !serverCert {
			t.Fatal("missing ACME account key or server certificate")
		}
		if previous != nil && !reflect.DeepEqual(previous, current) {
			t.Fatal("ACME keys or certificate changed after restart")
		}
		previous = current
		stop()
	}
}
