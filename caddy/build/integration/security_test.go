package integration

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

func request(address string, config *tls.Config, host string) (int, error) {
	conn, err := dialTLS(address, config)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host); err != nil {
		return 0, err
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}

func wantStatus(t *testing.T, address string, config *tls.Config, host string, want int) {
	t.Helper()
	code, err := request(address, config, host)
	if code != want || (want != 0 && err != nil) {
		t.Fatalf("SNI %q Host %q: got %d (%v), want %d", config.ServerName, host, code, err, want)
	}
}

func proxyBackend(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		_, _ = io.WriteString(w, "upstream-ok")
		return
	}
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n")
	_, _ = rw.Write([]byte{0x81, 2, 'o', 'k'})
	_ = rw.Flush()
}

func websocket(t *testing.T, address string, config *tls.Config) {
	t.Helper()
	conn, err := dialTLS(address, config)
	require(t, err)
	defer conn.Close()
	_, err = fmt.Fprintf(conn, "GET /api/websocket HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", haHost)
	require(t, err)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	require(t, err)
	defer response.Body.Close()
	if response.StatusCode != 101 {
		t.Fatalf("WebSocket upgrade returned %s", response.Status)
	}
	var frame [4]byte
	_, err = io.ReadFull(reader, frame[:])
	require(t, err)
	if frame != [4]byte{0x81, 2, 'o', 'k'} {
		t.Fatalf("wrong WebSocket frame: %x", frame)
	}
}

func sharedConnection(t *testing.T, address string, config *tls.Config) {
	t.Helper()
	config = config.Clone()
	config.NextProtos = []string{"h2"}
	conn, err := dialTLS(address, config)
	require(t, err)
	defer conn.Close()
	if conn.ConnectionState().NegotiatedProtocol != "h2" {
		t.Fatal("HTTP/2 ALPN not negotiated")
	}
	_, err = io.WriteString(conn, http2.ClientPreface)
	require(t, err)
	framer := http2.NewFramer(conn, conn)
	framer.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	require(t, framer.WriteSettings())
	var header bytes.Buffer
	encoder := hpack.NewEncoder(&header)
	for _, stream := range []struct {
		id           uint32
		host, status string
	}{{1, publicHost, "200"}, {3, haHost, "421"}} {
		header.Reset()
		for _, field := range []hpack.HeaderField{{Name: ":method", Value: "GET"}, {Name: ":scheme", Value: "https"}, {Name: ":authority", Value: stream.host}, {Name: ":path", Value: "/"}} {
			require(t, encoder.WriteField(field))
		}
		require(t, framer.WriteHeaders(http2.HeadersFrameParam{StreamID: stream.id, BlockFragment: header.Bytes(), EndHeaders: true, EndStream: true}))
		for {
			frame, err := framer.ReadFrame()
			require(t, err)
			switch frame := frame.(type) {
			case *http2.SettingsFrame:
				if !frame.IsAck() {
					require(t, framer.WriteSettingsAck())
				}
			case *http2.MetaHeadersFrame:
				if frame.StreamID != stream.id {
					continue
				}
				if got := frame.PseudoValue("status"); got != stream.status {
					t.Fatalf("HTTP/2 %s on shared connection: got %s, want %s", stream.host, got, stream.status)
				}
			}
			if headers, ok := frame.(*http2.MetaHeadersFrame); ok && headers.StreamID == stream.id {
				break
			}
		}
	}
}

func TestTLS(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	clientCA := issue(t, "client-ca", nil, 0, false)
	foreignCA := issue(t, "foreign-ca", nil, 0, false)
	serverCA := issue(t, "server-ca", nil, 0, false)
	valid := issue(t, "valid", clientCA, x509.ExtKeyUsageClientAuth, false)
	wrong := issue(t, "wrong", foreignCA, x509.ExtKeyUsageClientAuth, false)
	expired := issue(t, "expired", clientCA, x509.ExtKeyUsageClientAuth, true)
	roots := x509.NewCertPool()
	roots.AddCert(serverCA.cert)
	caFile := filepath.Join(dir, "client-ca.pem")
	write(t, caFile, clientCA.pem)
	backend := httptest.NewServer(http.HandlerFunc(proxyBackend))
	t.Cleanup(backend.Close)

	for _, public := range []bool{false, true} {
		name := "default"
		if public {
			name = "shared-wildcard"
		}
		t.Run(name, func(t *testing.T) {
			config := defaultConfig(t)
			tlsApp := app(config, "tls")
			if config["admin"].(object)["disabled"] != true || !reflect.DeepEqual(tlsApp["certificates"].(object)["automate"], []any{haHost}) {
				t.Fatal("unexpected default Admin API or certificate subjects")
			}
			issuer := tlsApp["automation"].(object)["policies"].([]any)[0].(object)["issuers"].([]any)[0].(object)
			provider := issuer["challenges"].(object)["dns"].(object)["provider"]
			if !reflect.DeepEqual(provider, object{"name": "cloudflare", "api_token": "{env.CF_API_TOKEN}"}) {
				t.Fatal("default does not use the Cloudflare token placeholder")
			}
			address := loopback(t)
			srv := server(config)
			srv["listen"] = []string{address}
			policies := srv["tls_connection_policies"].([]any)
			policies[0].(object)["client_authentication"].(object)["ca"].(object)["pem_files"] = []string{caFile}
			routes := srv["routes"].([]any)
			proxy := routes[0].(object)["handle"].([]any)[0].(object)
			proxy["upstreams"] = []object{{"dial": strings.TrimPrefix(backend.URL, "http://")}}
			config["storage"].(object)["root"] = dir
			path := filepath.Join(dir, name+".json")
			if !public {
				write(t, path, encode(t, config))
				cmd := exec.Command(bin, "validate", "--config", path)
				cmd.Env = append(os.Environ(), "CF_API_TOKEN="+testToken)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("validate defaults: %v\n%s", err, strings.ReplaceAll(string(out), testToken, "[redacted]"))
				}
			}
			certName := haHost
			if public {
				certName = "*.example.com"
				policies = append(policies, object{"match": object{"sni": []string{publicHost}}, "protocol_min": "tls1.2"})
				srv["tls_connection_policies"] = policies
				srv["routes"] = []any{routes[0], object{"match": []object{{"host": []string{publicHost}}}, "handle": []object{proxy}, "terminal": true}, routes[1]}
			}
			cert := issue(t, certName, serverCA, x509.ExtKeyUsageServerAuth, false)
			certFile, keyFile := filepath.Join(dir, name+".pem"), filepath.Join(dir, name+".key")
			write(t, certFile, cert.pem)
			write(t, keyFile, cert.key)
			config["apps"].(object)["tls"] = object{"certificates": object{"load_files": []object{{"certificate": certFile, "key": keyFile}}}}
			write(t, path, encode(t, config))
			stop := caddy(t, bin, path, testToken, false)
			protected := &tls.Config{RootCAs: roots, ServerName: haHost, Certificates: []tls.Certificate{valid.pair}, NextProtos: []string{"http/1.1"}}
			await(t, 10*time.Second, func() bool { code, _ := request(address, protected, haHost); return code == 200 })

			for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
				for _, client := range []struct {
					cert *certificate
					want int
				}{{nil, 0}, {wrong, 0}, {expired, 0}, {valid, 200}} {
					settings := protected.Clone()
					settings.MinVersion, settings.MaxVersion = version, version
					settings.Certificates = nil
					if client.cert != nil {
						settings.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &client.cert.pair, nil }
					}
					wantStatus(t, address, settings, haHost, client.want)
				}
			}
			for _, sni := range []string{"unknown.example.com", ""} {
				// Allow the wildcard certificate here so only server-side SNI policy can reject it.
				wantStatus(t, address, &tls.Config{ServerName: sni, InsecureSkipVerify: true}, "unknown.example.com", 0)
			}
			wantsWrongHost := "unknown.example.com"
			if public {
				wantsWrongHost = publicHost
			}
			wantStatus(t, address, protected, wantsWrongHost, 421)
			if public {
				open := &tls.Config{RootCAs: roots, ServerName: publicHost, NextProtos: []string{"http/1.1"}}
				for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
					open.MinVersion, open.MaxVersion = version, version
					wantStatus(t, address, open, publicHost, 200)
					wantStatus(t, address, open, haHost, 421)
				}
				sharedConnection(t, address, open)
			}
			websocket(t, address, protected)
			stop()

			for _, policy := range policies {
				policy.(object)["protocol_min"] = "tls1.3"
			}
			write(t, path, encode(t, config))
			caddy(t, bin, path, testToken, false)
			await(t, 10*time.Second, func() bool { code, _ := request(address, protected, haHost); return code == 200 })
			settings := protected.Clone()
			settings.MaxVersion = tls.VersionTLS12
			wantStatus(t, address, settings, haHost, 0)
			settings.MinVersion, settings.MaxVersion = tls.VersionTLS13, tls.VersionTLS13
			wantStatus(t, address, settings, haHost, 200)
		})
	}
}

func internalConfig(t *testing.T, address, storage string) object {
	t.Helper()
	config := defaultConfig(t)
	config["storage"].(object)["root"] = storage
	apps := config["apps"].(object)
	apps["pki"] = object{"certificate_authorities": object{"local": object{"install_trust": false}}}
	apps["tls"] = object{"certificates": object{"automate": []string{"localhost"}}, "automation": object{"policies": []object{{"subjects": []string{"localhost"}, "issuers": []object{{"module": "internal"}}}}}}
	srv := server(config)
	srv["listen"] = []string{address}
	srv["tls_connection_policies"] = []object{{"match": object{"sni": []string{"localhost"}}}}
	srv["routes"] = []object{{"match": []object{{"host": []string{"localhost"}}}, "handle": []object{{"handler": "static_response", "body": "ok"}}}}
	app(config, "http")["grace_period"] = "1s"
	return config
}

func TestInternalStorage(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	address := loopback(t)
	config := internalConfig(t, address, dir)
	path := filepath.Join(dir, "config.json")
	write(t, path, encode(t, config))
	settings := &tls.Config{ServerName: "localhost", InsecureSkipVerify: true}
	var previous map[string][32]byte
	for range 2 {
		stop := caddy(t, bin, path, testToken, false)
		await(t, 10*time.Second, func() bool { code, _ := request(address, settings, "localhost"); return code == 200 })
		current := hashes(t, dir, func(path string) bool { return filepath.Ext(path) == ".key" })
		if len(current) < 3 {
			t.Fatal("missing internal CA or server keys")
		}
		if previous != nil && !reflect.DeepEqual(previous, current) {
			t.Fatal("internal CA keys changed after restart")
		}
		previous = current
		stop()
	}
}
