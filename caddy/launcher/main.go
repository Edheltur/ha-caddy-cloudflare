// Linux-only startup adapter for Supervisor. It never implements TLS or routing.
package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const uid = 65532

type options struct {
	Token    string `json:"cloudflare_api_token"`
	Config   string `json:"caddy_config"`
	ClientCA string `json:"client_ca_pem"`
}

// Matches the accepted formats of caddy-dns/cloudflare v0.2.4. That module
// includes malformed tokens in its errors: reject them here without echoing.
var tokenPattern = regexp.MustCompile(`^(?:[A-Za-z0-9_-]{35,50}|cf(?:ut|at)_[A-Za-z0-9_-]{32,256})$`)

func readOptions(r io.Reader) (options, error) {
	var o options
	d := json.NewDecoder(io.LimitReader(r, 1024*1024+1))
	d.DisallowUnknownFields()
	if err := d.Decode(&o); err != nil {
		return options{}, errors.New("invalid options JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return options{}, errors.New("unexpected trailing options data")
	}
	if !tokenPattern.MatchString(o.Token) {
		return options{}, errors.New("invalid Cloudflare token format")
	}
	if len(o.Config) > 128*1024 {
		return options{}, errors.New("Caddy configuration exceeds 128 KiB")
	}
	if !strings.HasPrefix(strings.TrimSpace(o.Config), "{") || !json.Valid([]byte(o.Config)) {
		return options{}, errors.New("caddy_config must be a JSON object")
	}
	if o.ClientCA != "" {
		if err := validateCA(o.ClientCA); err != nil {
			return options{}, err
		}
	}
	return o, nil
}

// Accept public CA certificates only, including bundles for CA rotation.
func validateCA(value string) error {
	invalid := errors.New("client_ca_pem must contain only public CA certificates (max 128 KiB)")
	if len(value) > 128*1024 {
		return invalid
	}
	rest := bytes.TrimSpace([]byte(value))
	if len(rest) == 0 {
		return invalid
	}
	for len(rest) > 0 {
		if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return invalid
		}
		block, tail := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return invalid
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.BasicConstraintsValid || !cert.IsCA {
			return invalid
		}
		rest = bytes.TrimSpace(tail)
	}
	return nil
}

// Only touch the dedicated state directory itself, never recursively chown
// user-controlled paths or follow a symlink. Existing Caddy state keeps its UID.
func prepareStorage(base string) error {
	fd, err := unix.Open(base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errors.New("cannot open data directory")
	}
	defer unix.Close(fd)
	if err = unix.Mkdirat(fd, "caddy", 0700); err != nil && err != unix.EEXIST {
		return errors.New("cannot create state directory")
	}
	sfd, err := unix.Openat(fd, "caddy", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errors.New("unsafe state directory")
	}
	defer unix.Close(sfd)
	if err = unix.Fchown(sfd, uid, uid); err != nil {
		return errors.New("cannot set state owner")
	}
	if err = unix.Fchmod(sfd, 0700); err != nil {
		return errors.New("cannot restrict state permissions")
	}
	return nil
}

func dropPrivileges() error {
	// prctl acts on the current thread. Keep that thread through exec. Go's
	// syscall.Set*id functions apply credentials across runtime threads.
	runtime.LockOSThread()
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return err
	}
	for cap := 0; ; cap++ {
		_, _, errno := unix.Syscall6(unix.SYS_PRCTL, unix.PR_CAPBSET_READ, uintptr(cap), 0, 0, 0, 0)
		if errno == unix.EINVAL {
			break
		}
		if errno != 0 {
			return errno
		}
		if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(cap), 0, 0, 0); err != nil {
			return err
		}
	}
	if err := syscall.Setgroups([]int{}); err != nil {
		return err
	}
	if err := syscall.Setgid(uid); err != nil {
		return err
	}
	if err := syscall.Setuid(uid); err != nil {
		return err
	}
	data := [2]unix.CapUserData{}
	return unix.Capset(&unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}, &data[0])
}

func childEnv(o options) []string {
	// Allowlist, rather than inherit Supervisor tokens or arbitrary variables.
	return []string{"CF_API_TOKEN=" + o.Token, "HOME=/data/caddy", "XDG_DATA_HOME=/data/caddy", "XDG_CONFIG_HOME=/data/caddy", "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt"}
}

// Called only AFTER dropping privileges. CreateTemp never follows a preexisting
// symlink, and rename replaces the destination entry without following it.
func writeConfig(dir, content string) (string, error) {
	return writePrivateFile(dir, "addon-config", content)
}

func writePrivateFile(dir, name, content string) (string, error) {
	f, err := os.CreateTemp(dir, ".addon-config-*")
	if err != nil {
		return "", errors.New("cannot create Caddy configuration")
	}
	defer os.Remove(f.Name())
	if _, err = io.WriteString(f, content); err != nil {
		f.Close()
		return "", errors.New("cannot write Caddy configuration")
	}
	if err = f.Close(); err != nil {
		return "", errors.New("cannot close Caddy configuration")
	}
	path := dir + "/" + name
	if err = os.Rename(f.Name(), path); err != nil {
		return "", errors.New("cannot replace Caddy configuration")
	}
	return path, nil
}

func run() error {
	if len(os.Args) != 1 {
		return errors.New("launcher takes no arguments")
	}
	if os.Geteuid() != 0 {
		return errors.New("launcher needs initial root to prepare Supervisor storage")
	}
	f, err := os.Open("/data/options.json")
	if err != nil {
		return errors.New("cannot open Supervisor options")
	}
	o, err := readOptions(f)
	f.Close()
	if err != nil {
		return err
	}
	unix.Umask(0077)
	if err = prepareStorage("/data"); err != nil {
		return err
	}
	if err = os.Chdir("/data/caddy"); err != nil {
		return errors.New("cannot enter state directory")
	}
	if err = dropPrivileges(); err != nil {
		return errors.New("cannot drop privileges; refusing to start")
	}
	if o.ClientCA != "" {
		if _, err := writePrivateFile("/data/caddy", "client-ca.pem", o.ClientCA); err != nil {
			return errors.New("cannot write client CA")
		}
	} else {
		if err := os.Remove("/data/caddy/client-ca.pem"); err != nil && !os.IsNotExist(err) {
			return errors.New("cannot remove stale client CA")
		}
	}
	path, err := writeConfig("/data/caddy", o.Config)
	if err != nil {
		return err
	}
	args := []string{"caddy", "run", "--config", path}
	if err = syscall.Exec("/usr/bin/caddy", args, childEnv(o)); err != nil {
		return errors.New("cannot execute Caddy")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "startup:", err)
		os.Exit(1)
	}
}
