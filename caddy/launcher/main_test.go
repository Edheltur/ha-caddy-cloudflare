package main

import (
	"os"
	"strings"
	"testing"
)

func TestOptions(t *testing.T) {
	for _, token := range []string{strings.Repeat("a", 40), "cfut_" + strings.Repeat("x", 64), "cfat_" + strings.Repeat("z", 32)} {
		if _, err := readOptions(strings.NewReader(`{"cloudflare_api_token":"` + token + `","config_format":"json"}`)); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range []string{
		`{"cloudflare_api_token":"TOP-SECRET","config_format":"json"}`,
		`{"cloudflare_api_token":"TOP-SECRET`,
		`{"cloudflare_api_token":"` + strings.Repeat("a", 40) + `","config_format":"../../tmp"}`,
		`{"cloudflare_api_token":"` + strings.Repeat("a", 40) + `","config_format":"json","other":true}`,
		`{"cloudflare_api_token":"` + strings.Repeat("a", 40) + `","config_format":"json"} {}`,
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
