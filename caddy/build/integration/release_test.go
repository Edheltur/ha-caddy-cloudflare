package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercise the release guard without credentials or a registry. Registry
// outages must never be interpreted as permission to overwrite a version.
func TestReleaseTagGuard(t *testing.T) {
	dir := t.TempDir()
	docker := filepath.Join(dir, "docker")
	require(t, os.WriteFile(docker, []byte("#!/usr/bin/env bash\nprintf '%s\\n' \"$REGISTRY_ERROR\" >&2\nexit \"$REGISTRY_STATUS\"\n"), 0700))
	for _, tc := range []struct {
		name, status, message string
		allowed               bool
	}{
		{"exists", "0", "", false},
		{"missing", "1", "ERROR: ghcr.io/owner/app:0.1.0: not found", true},
		{"manifest-unknown", "1", "manifest unknown", true},
		{"unauthorized", "1", "401 Unauthorized", false},
		{"forbidden", "1", "403 Forbidden", false},
		{"rate-limit", "1", "429 Too Many Requests", false},
		{"timeout", "1", "context deadline exceeded", false},
		{"dns", "1", "dial tcp: lookup ghcr.io: no such host", false},
		{"empty-error", "1", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "../../../.github/scripts/require-new-tag.sh", "ghcr.io/owner/app:0.1.0")
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "REGISTRY_STATUS="+tc.status, "REGISTRY_ERROR="+tc.message)
			output, err := cmd.CombinedOutput()
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v, error=%v, output=%s", tc.allowed, err, output)
			}
		})
	}
}
