package integration

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func checkPrivateFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	require(t, err)
	stat := info.Sys().(*syscall.Stat_t)
	if info.Mode().Perm() != 0600 || stat.Uid != 65532 || stat.Gid != 65532 {
		t.Fatalf("%s: want mode 0600 and UID/GID 65532, got %o and %d/%d", path, info.Mode().Perm(), stat.Uid, stat.Gid)
	}
}

func checkProcess(t *testing.T, pid string) {
	t.Helper()
	proc := filepath.Join("/proc", pid)
	fields := map[string][]string{}
	for _, line := range strings.Split(string(read(t, filepath.Join(proc, "status"))), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[key] = strings.Fields(value)
		}
	}
	for _, key := range []string{"Uid", "Gid"} {
		if !reflect.DeepEqual(fields[key], []string{"65532", "65532", "65532", "65532"}) {
			t.Fatalf("unexpected %s: %v", key, fields[key])
		}
	}
	for _, key := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
		if len(fields[key]) != 1 {
			t.Fatalf("missing %s", key)
		}
		value, err := strconv.ParseUint(fields[key][0], 16, 64)
		require(t, err)
		if value != 0 {
			t.Fatalf("%s not empty: %x", key, value)
		}
	}
	if !reflect.DeepEqual(fields["NoNewPrivs"], []string{"1"}) {
		t.Fatal("no_new_privs is disabled")
	}
	env, err := os.ReadFile(filepath.Join(proc, "environ"))
	if err != nil && !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	if bytes.Contains(env, []byte("must-not-inherit")) {
		t.Fatal("Supervisor credentials inherited")
	}
}

func TestRuntime(t *testing.T) {
	image := os.Getenv("CADDY_TEST_IMAGE")
	if image == "" {
		t.Skip("set CADDY_TEST_IMAGE to the built Docker image")
	}
	if os.Geteuid() != 0 {
		t.Fatal("run the runtime test as root to inspect process and file permissions")
	}
	data := t.TempDir()
	require(t, os.Chmod(data, 0755))
	config := internalConfig(t, "0.0.0.0:8443", "/data/caddy")
	policy := server(config)["tls_connection_policies"].([]object)[0]
	state := filepath.Join(data, "caddy")
	caFile := filepath.Join(state, "client-ca.pem")
	ca := issue(t, "runtime-client-ca", nil, 0, false)
	client := issue(t, "runtime-client", ca, x509.ExtKeyUsageClientAuth, false)
	optionsFile := filepath.Join(data, "options.json")
	var previous map[string][32]byte
	for _, name := range []string{"first-start", "client-ca", "clear-client-ca"} {
		t.Run(name, func(t *testing.T) {
			settings := &tls.Config{ServerName: "localhost", InsecureSkipVerify: true}
			var caPEM string
			if name == "client-ca" {
				caPEM = string(ca.pem)
				policy["client_authentication"] = object{"mode": "require_and_verify", "ca": object{"provider": "file", "pem_files": []string{"/data/caddy/client-ca.pem"}}}
				settings.Certificates = []tls.Certificate{client.pair}
			} else {
				delete(policy, "client_authentication")
			}
			options := object{"cloudflare_api_token": testToken, "caddy_config": string(encode(t, config)), "client_ca_pem": caPEM}
			write(t, optionsFile, encode(t, options))
			id := command(t, "docker", "run", "-d", "--cap-drop=NET_RAW", "--security-opt=no-new-privileges", "-p", "127.0.0.1::8443", "--mount", "type=bind,source="+data+",target=/data", "-e", "SUPERVISOR_TOKEN=must-not-inherit", "-e", "HASSIO_TOKEN=must-not-inherit", image)
			t.Cleanup(func() { command(t, "docker", "rm", "-f", id) })
			address := command(t, "docker", "port", id, "8443/tcp")
			await(t, 20*time.Second, func() bool { code, _ := request(address, settings, "localhost"); return code == 200 })

			pid := command(t, "docker", "inspect", "-f", "{{.State.Pid}}", id)
			checkProcess(t, pid)
			generated := filepath.Join(state, "addon-config")
			checkPrivateFile(t, generated)
			var parsed object
			require(t, json.Unmarshal(read(t, generated), &parsed))
			if !bytes.Equal(encode(t, parsed), encode(t, config)) {
				t.Fatal("generated config differs from app option")
			}
			if name == "client-ca" {
				checkPrivateFile(t, caFile)
				if !bytes.Equal(read(t, caFile), ca.pem) {
					t.Fatal("generated CA differs from app option")
				}
				withoutClient := settings.Clone()
				withoutClient.Certificates = nil
				wantStatus(t, address, withoutClient, "localhost", 0)
			} else if _, err := os.Stat(caFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("CA file must be absent: %v", err)
			}
			current := hashes(t, state, func(path string) bool { return filepath.Ext(path) == ".key" })
			if len(current) < 3 {
				t.Fatal("missing internal CA or server keys")
			}
			for path := range current {
				checkPrivateFile(t, path)
			}
			if previous != nil && !reflect.DeepEqual(previous, current) {
				t.Fatal("private keys changed after restart")
			}
			previous = current

			command(t, "docker", "stop", "-t", "10", id)
			log := command(t, "docker", "logs", id)
			if strings.Contains(log, testToken) || strings.Contains(log, "must-not-inherit") {
				t.Fatal("credentials in container logs")
			}
			if code := command(t, "docker", "inspect", "-f", "{{.State.ExitCode}}", id); code != "0" {
				t.Fatalf("container exit code %s", code)
			}
		})
	}

	// Check the actual image's early token validation with synthetic input.
	write(t, optionsFile, []byte(`{"cloudflare_api_token":"MALFORMED-SECRET","caddy_config":"{}"}`))
	id := command(t, "docker", "create", "--mount", "type=bind,source="+data+",target=/data", image)
	t.Cleanup(func() { command(t, "docker", "rm", "-f", id) })
	command(t, "docker", "start", id)
	if code := command(t, "docker", "wait", id); code == "0" {
		t.Fatal("container accepted malformed token")
	}
	log := command(t, "docker", "logs", id)
	if strings.Contains(log, "MALFORMED-SECRET") || !strings.Contains(log, "invalid Cloudflare token") {
		t.Fatal("malformed token not safely rejected")
	}
}
