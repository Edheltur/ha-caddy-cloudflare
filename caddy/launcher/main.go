// Linux-only startup adapter for Supervisor. It never implements TLS or routing.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

const uid = 65532

type options struct {
	Token  string `json:"cloudflare_api_token"`
	Format string `json:"config_format"`
}

// Matches the accepted formats of caddy-dns/cloudflare v0.2.4. That module
// includes malformed tokens in its errors: reject them here without echoing.
var tokenPattern = regexp.MustCompile(`^(?:[A-Za-z0-9_-]{35,50}|cf(?:ut|at)_[A-Za-z0-9_-]{32,256})$`)

func readOptions(r io.Reader) (options, error) {
	var o options
	d := json.NewDecoder(io.LimitReader(r, 8193))
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
	if o.Format != "caddyfile" && o.Format != "json" {
		return options{}, errors.New("invalid config format")
	}
	return o, nil
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
	args := []string{"caddy", "run", "--config", "/config/Caddyfile", "--adapter", "caddyfile"}
	if o.Format == "json" {
		args = []string{"caddy", "run", "--config", "/config/caddy.json"}
	}
	if err = os.Chdir("/data/caddy"); err != nil {
		return errors.New("cannot enter state directory")
	}
	if err = dropPrivileges(); err != nil {
		return errors.New("cannot drop privileges; refusing to start")
	}
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
