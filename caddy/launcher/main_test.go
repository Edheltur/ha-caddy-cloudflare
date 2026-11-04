package main

import (
	"encoding/json"
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

func TestInlineOptions(t *testing.T) {
	content := "{\n admin off\n}\n"
	raw, _ := json.Marshal(options{Token: strings.Repeat("a", 40), Format: "caddyfile", Config: content})
	o, err := readOptions(strings.NewReader(string(raw)))
	if err != nil || o.Config != content {
		t.Fatal("multiline configuration not preserved", err)
	}
	for _, content := range []string{" \n", strings.Repeat("x", 128*1024+1), "bad\x00config"} {
		raw, _ := json.Marshal(options{Token: strings.Repeat("a", 40), Format: "json", Config: content})
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
