package core

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const s1Profile = `{"outbounds":[{"tag":"exit","protocol":"trojan","settings":{"servers":[{"address":"127.0.0.1","port":443,"password":"S1-SYNTHETIC-PASSWORD"}]},"streamSettings":{"network":"tcp","security":"tls"}}]}`
const s1HWID = "S1-SYNTHETIC-DEVICE-123"
const s1Auth = "Bearer S1-SYNTHETIC-AUTH"

func s1TLS(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(handler)
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func s1Transport(t *testing.T, servers ...*httptest.Server) *http.Transport {
	t.Helper()
	roots := x509.NewCertPool()
	for _, s := range servers {
		roots.AddCert(s.Certificate())
	}
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: time.Second, MaxResponseHeaderBytes: 64 << 10, DisableKeepAlives: true}
	t.Cleanup(tr.CloseIdleConnections)
	return tr
}

func s1Source(address string) Source {
	return Source{Name: "S1", URL: address, Headers: map[string]string{"Authorization": s1Auth, "X-Api-Key": "S1-SYNTHETIC-API-KEY"}, HWID: &HWIDSettings{Enabled: true, ID: s1HWID}}
}

func s1SafeError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected rejection")
	}
	for _, marker := range []string{s1HWID, s1Auth, "S1-SYNTHETIC-API-KEY", "S1-SYNTHETIC-TOKEN", "S1-SYNTHETIC-PASSWORD", "S1-SYNTHETIC-RESPONSE", "S1-SYNTHETIC-HOST"} {
		if strings.Contains(err.Error(), marker) {
			t.Fatalf("error leaked synthetic marker: %q", marker)
		}
	}
}

func TestS1HTTPSCertificateOriginAndRedirects(t *testing.T) {
	var foreignHits, plainHits atomic.Int32
	foreign := s1TLS(t, func(w http.ResponseWriter, r *http.Request) { foreignHits.Add(1); fmt.Fprint(w, s1Profile) })
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { plainHits.Add(1); fmt.Fprint(w, s1Profile) }))
	t.Cleanup(plain.Close)
	var sameHits atomic.Int32
	var origin *httptest.Server
	origin = s1TLS(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != s1Auth || r.Header.Get("X-HWID") != s1HWID || r.Header.Get("X-Api-Key") != "S1-SYNTHETIC-API-KEY" {
			t.Error("synthetic credentials missing at authorized origin")
		}
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/final?token=S1-SYNTHETIC-TOKEN", 302)
		case "/foreign-port":
			http.Redirect(w, r, foreign.URL+"/S1-SYNTHETIC-TOKEN", 307)
		case "/foreign-host":
			http.Redirect(w, r, strings.Replace(origin.URL, "127.0.0.1", "localhost", 1)+"/final", 302)
		case "/scheme-relative":
			http.Redirect(w, r, "//"+strings.TrimPrefix(foreign.URL, "https://")+"/final", 302)
		case "/downgrade":
			http.Redirect(w, r, plain.URL+"/S1-SYNTHETIC-TOKEN", 302)
		case "/loop":
			http.Redirect(w, r, "/loop", 302)
		default:
			sameHits.Add(1)
			fmt.Fprint(w, s1Profile)
		}
	})
	tr := s1Transport(t, origin, foreign)
	for _, path := range []string{"/final", "/same"} {
		nodes, err := loadContext(context.Background(), s1Source(origin.URL+path), tr, "", DeviceInfo{})
		if err != nil || len(nodes) != 1 {
			t.Fatalf("trusted TLS/same-origin failed: %v", err)
		}
	}
	if sameHits.Load() != 2 {
		t.Fatalf("expected 2 completed requests, got %d", sameHits.Load())
	}
	for _, path := range []string{"/foreign-port", "/foreign-host", "/scheme-relative", "/downgrade", "/loop"} {
		_, err := loadContext(context.Background(), s1Source(origin.URL+path), tr, "", DeviceInfo{})
		s1SafeError(t, err)
	}
	if foreignHits.Load() != 0 || plainHits.Load() != 0 {
		t.Fatal("redirect crossed origin or downgraded")
	}
	for _, config := range []*tls.Config{{RootCAs: x509.NewCertPool()}, {RootCAs: tr.TLSClientConfig.RootCAs, ServerName: "wrong.S1-SYNTHETIC-HOST.test"}} {
		rejected := &http.Transport{Proxy: nil, TLSClientConfig: config, TLSHandshakeTimeout: time.Second, DisableKeepAlives: true}
		t.Cleanup(rejected.CloseIdleConnections)
		_, err := loadContext(context.Background(), s1Source(origin.URL+"/S1-SYNTHETIC-TOKEN"), rejected, "", DeviceInfo{})
		s1SafeError(t, err)
	}
	t.Logf("trusted/same-origin completions=%d; blocked foreign TLS hits=%d; blocked HTTP hits=%d; untrusted CA and hostname mismatch rejected", sameHits.Load(), foreignHits.Load(), plainHits.Load())
}

func TestS1HTTPSFailuresBodiesLimitsAndCancellation(t *testing.T) {
	prefix := []byte(s1Profile)
	exact := append(append([]byte{}, prefix...), bytes.Repeat([]byte(" "), maxSubscription-len(prefix))...)
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, _ = zw.Write(append(exact, ' '))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	server := s1TLS(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exact":
			_, _ = w.Write(exact)
		case "/over":
			_, _ = w.Write(append(exact, ' '))
		case "/gzip-over":
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(compressed.Bytes())
		case "/status":
			w.WriteHeader(403)
			fmt.Fprint(w, "S1-SYNTHETIC-RESPONSE S1-SYNTHETIC-PASSWORD")
		case "/hwid":
			w.Header().Set("X-Hwid-Limit", "true")
			w.WriteHeader(403)
			fmt.Fprint(w, "S1-SYNTHETIC-RESPONSE")
		case "/malformed":
			fmt.Fprint(w, `{"outbounds":[{"protocol":"trojan","settings":[],"password":"S1-SYNTHETIC-PASSWORD"}]}`)
		case "/links":
			fmt.Fprint(w, "trojan://S1-SYNTHETIC-PASSWORD@127.0.0.1:443?security=tls#exit")
		case "/body-hwid":
			fmt.Fprint(w, "#subscription-always-hwid-enable: true\ntrojan://S1-SYNTHETIC-PASSWORD@127.0.0.1:443?security=tls#exit")
		case "/hang":
			<-r.Context().Done()
		}
	})
	tr := s1Transport(t, server)
	nodes, err := loadContext(context.Background(), s1Source(server.URL+"/exact"), tr, "", DeviceInfo{})
	if err != nil || len(nodes) != 1 {
		t.Fatalf("exact 4 MiB rejected: %v", err)
	}
	for _, path := range []string{"/over", "/gzip-over", "/status", "/hwid", "/malformed", "/links"} {
		t.Run(path, func(t *testing.T) {
			_, err := loadContext(context.Background(), s1Source(server.URL+path+"?token=S1-SYNTHETIC-TOKEN"), tr, "", DeviceInfo{})
			s1SafeError(t, err)
		})
	}
	s := s1Source(server.URL + "/links")
	s.Format = "links"
	if n, err := loadContext(context.Background(), s, tr, "", DeviceInfo{}); err != nil || len(n) != 1 {
		t.Fatalf("explicit links failed: %v", err)
	}
	s = s1Source(server.URL + "/body-hwid")
	s.Format = "links"
	s.HWID = &HWIDSettings{Enabled: false}
	_, err = loadContext(context.Background(), s, tr, "", DeviceInfo{})
	s1SafeError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = loadContext(ctx, s1Source(server.URL+"/hang?token=S1-SYNTHETIC-TOKEN"), tr, "", DeviceInfo{})
	s1SafeError(t, err)
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancellation exceeded 2 s")
	}
	t.Logf("raw limit=%d bytes; gzip fixture=%d bytes expands to %d and is rejected; cancellation=%s", maxSubscription, compressed.Len(), maxSubscription+1, time.Since(start))
}

func TestS1HeadersValidationAndPrecedence(t *testing.T) {
	cases := []map[string]string{
		{"Authorization": "a", "authorization": "b"}, {"X-HWID": "a", "x-hwid": "b"}, {"Host": "a"}, {"Content-Length": "1"}, {"transfer-encoding": "chunked"},
		{"Bad Name": "a"}, {"Authorization": "a\r\nX-Injection: S1-SYNTHETIC-AUTH"}, {"Authorization": "a\x00b"}, {"X": "a\x7fb"}, {"X": strings.Repeat("a", 8193)}, {strings.Repeat("X", 257): "a"},
	}
	many := map[string]string{}
	for i := 0; i < 65; i++ {
		many[fmt.Sprintf("X-S1-%d", i)] = "a"
	}
	cases = append(cases, many)
	for i, headers := range cases {
		s := s1Source("https://127.0.0.1:1")
		s.Headers = headers
		if err := s.validateHWID(); err == nil {
			t.Fatalf("invalid header case %d accepted", i)
		}
		data, _ := json.Marshal(Config{Sources: []Source{s}})
		if _, err := ReadConfig(bytes.NewReader(data)); err == nil {
			t.Fatalf("ReadConfig accepted header case %d", i)
		}
	}
	req, _ := http.NewRequest("GET", "https://127.0.0.1:1", nil)
	s := s1Source(req.URL.String())
	s.Headers = map[string]string{"x-hwid": "S1-MANUAL-DEVICE-123", "x-device-os": "S1ManualOS", "user-agent": "S1Manual/1", "X-Tab": "a\tb"}
	if err := applySourceHeaders(req, s, filepath.Join(t.TempDir(), "unused-hwid"), DeviceInfo{OS: "S1Fallback"}); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-HWID") != "S1-MANUAL-DEVICE-123" || req.Header.Get("X-Device-OS") != "S1ManualOS" || req.UserAgent() != "S1Manual/1" {
		t.Fatal("manual precedence lost")
	}
	t.Logf("rejected invalid/conflicting header maps=%d; manual case-insensitive overrides preserved", len(cases))
}

func TestS1FileBoundaryAndDeviceID(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "allowed.json")
	if err := os.WriteFile(regular, []byte(s1Profile), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DiscoverSourceContext(context.Background(), Source{File: regular}, nil, ""); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "link.json")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo.json")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(dir, "directory.json")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for i, path := range []string{"relative.json", dir + "/../allowed.json", dir + "//allowed.json", regular + "\x00", regular + "\n", filepath.Join(dir, "no-extension"), symlink, fifo, directory} {
		start := time.Now()
		_, _, err := DiscoverSourceContext(context.Background(), Source{File: path}, nil, "")
		if err == nil {
			t.Fatalf("invalid path case %d accepted", i)
		}
		if time.Since(start) > time.Second {
			t.Fatal("invalid file blocked")
		}
	}
	// Parent symlinks are deliberately characterized, not treated as an ACL escape.
	parent := filepath.Join(dir, "parent")
	if err := os.Symlink(dir, parent); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DiscoverSourceContext(context.Background(), Source{File: filepath.Join(parent, "allowed.json")}, nil, ""); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(dir, "large.json")
	if err := os.WriteFile(large, append([]byte(s1Profile), bytes.Repeat([]byte(" "), maxSubscription+1-len(s1Profile))...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DiscoverSourceContext(context.Background(), Source{File: large}, nil, ""); err == nil {
		t.Fatal("oversized local file accepted")
	}
	idPath := filepath.Join(dir, "id")
	id, err := DeviceID(idPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DeviceID(idPath)
	if err != nil || id != second {
		t.Fatal("installation ID rotated")
	}
	st, err := os.Stat(idPath)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("identity not private")
	}
	for _, path := range []string{filepath.Join(dir, "id-link"), filepath.Join(dir, "id-lock-link")} {
		link := path
		if strings.HasSuffix(path, "lock-link") {
			link += ".lock"
		}
		if err := os.Symlink(idPath, link); err != nil {
			t.Fatal(err)
		}
		_, err := DeviceID(path)
		s1SafeError(t, err)
	}
	corrupt := filepath.Join(dir, "corrupt-id")
	if err := os.WriteFile(corrupt, []byte("S1-SYNTHETIC-PASSWORD invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = DeviceID(corrupt)
	s1SafeError(t, err)
	after, _ := os.ReadFile(corrupt)
	if string(after) != "S1-SYNTHETIC-PASSWORD invalid" {
		t.Fatal("corrupt identity overwritten")
	}
	t.Log("9 invalid file cases rejected promptly; terminal symlink/FIFO/directory rejected; explicit parent-symlink JSON allowed; ID and lock symlinks rejected; stable private ID")
}

func TestS1MalformedInputAndDependencyGraph(t *testing.T) {
	uri := "trojan://S1-SYNTHETIC-PASSWORD@127.0.0.1:443?security=tls#exit"
	for i, body := range []string{s1Profile, "\ufeff" + s1Profile, base64.StdEncoding.EncodeToString([]byte(s1Profile)), uri, base64.RawURLEncoding.EncodeToString([]byte(uri))} {
		if nodes, err := ParseSubscription("S1", []byte(body)); err != nil || len(nodes) != 1 {
			t.Fatalf("positive format %d: %v", i, err)
		}
	}
	invalid := []string{
		"!not-base64", base64.StdEncoding.EncodeToString([]byte("invalid URI S1-SYNTHETIC-PASSWORD")), `[{"tag":"x","protocol":"socks","settings":{}}] {}`, `[null]`, `[{"tag":"x","protocol":"socks","settings":[]}]`,
		`[{"tag":"x","protocol":"socks","settings":{}},{"tag":"x","protocol":"socks","settings":{}}]`, `[{"tag":"x/y","protocol":"socks","settings":{}}]`, `[{"tag":"x\nq","protocol":"socks","settings":{}}]`,
		"trojan://S1-SYNTHETIC-PASSWORD@127.0.0.1:443?security=tls&security=reality", "trojan://S1-SYNTHETIC-PASSWORD@127.0.0.1:0?security=tls", "trojan://S1-SYNTHETIC-PASSWORD@127.0.0.1:65536?security=tls", "trojan://S1-SYNTHETIC-PASSWORD@127.0.0.1:443?security=tls&path=%zz", "https://S1-SYNTHETIC-PASSWORD@127.0.0.1:443", "ss://invalid!@127.0.0.1:443",
		`{"outbounds":[{"tag":"x","protocol":"socks","settings":{},"streamSettings":{"sockopt":{"dialerProxy":"S1-SYNTHETIC-PASSWORD"}}}]}`,
		strings.Repeat("[", 10001) + strings.Repeat("]", 10001),
	}
	for i, body := range invalid {
		t.Run(fmt.Sprintf("reject-%02d", i), func(t *testing.T) { _, err := ParseSubscription("S1", []byte(body)); s1SafeError(t, err) })
	}
	graph := `{"inbounds":[{"listen":"0.0.0.0","port":9999}],"routing":{"domainStrategy":"AsIs"},"dns":{"servers":["S1-SYNTHETIC-HOST"]},"outbounds":[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":12345}]},"streamSettings":{"sockopt":{"dialerProxy":"helper"},"xhttpSettings":{"extra":{"downloadSettings":{"sockopt":{"dialerProxy":"helper"}}}}}},{"tag":"helper","protocol":"freedom","settings":{}}]}`
	nodes, err := ParseSubscription("S1", []byte(graph))
	if err != nil || len(nodes) != 2 {
		t.Fatalf("graph parse: %v", err)
	}
	a, err := Compile(Config{Default: "block", SOCKSPort: 1080}, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(a.Xray, []byte("S1-SYNTHETIC-HOST")) || bytes.Contains(a.Xray, []byte("9999")) || bytes.Contains(a.Xray, []byte(`"listen": "0.0.0.0"`)) {
		t.Fatal("profile replaced local listeners/DNS")
	}
	if bytes.Count(a.Xray, []byte(`"dialerProxy": "S1/helper"`)) != 2 {
		t.Fatal("nested dependencies not namespaced")
	}
	flat, err := UnprefixNodes("S1", nodes)
	if err != nil {
		t.Fatal(err)
	}
	serialized, _ := json.Marshal(flat)
	if bytes.Contains(serialized, []byte("S1/")) {
		t.Fatal("local graph keeps import namespace")
	}
	cycle := `[{"tag":"a","protocol":"socks","settings":{},"proxySettings":{"tag":"b"}},{"tag":"b","protocol":"socks","settings":{},"proxySettings":{"tag":"a"}}]`
	cyclic, err := ParseSubscription("S1", []byte(cycle))
	if err != nil {
		t.Fatal(err)
	}
	_, err = SyncServers(filepath.Join(t.TempDir(), "servers.json"), cyclic, Config{})
	if err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("cycle survived catalogue validation: %v", err)
	}
	t.Logf("positive formats=5; malformed cases=%d; nested graph preserved; provider listeners/DNS ignored; cycle parse accepts and catalogue rejects", len(invalid))
}

func TestS1ConfigBoundaryAndDuplicateKeyCharacterization(t *testing.T) {
	valid := `{"sources":[],"rules":[],"default":"block"}`
	exact := valid + strings.Repeat(" ", maxSubscription-len(valid))
	if _, err := ReadConfig(strings.NewReader(exact)); err != nil {
		t.Fatal(err)
	}
	cases := []string{exact + " ", "[]", "null", valid + " {}", `{"unknown":"S1-SYNTHETIC-PASSWORD"}`, `{"sources":[{"name":"S1","url":"http://127.0.0.1"}]}`, `{"sources":[{"name":"S1","url":"https://user:S1-SYNTHETIC-PASSWORD@127.0.0.1"}]}`, `{"sources":[{"name":"S1","url":"https://127.0.0.1/#S1-SYNTHETIC-TOKEN"}]}`}
	for _, raw := range cases {
		_, err := ReadConfig(strings.NewReader(raw))
		s1SafeError(t, err)
	}
	for _, s := range []Source{{Name: "S1", URL: "https://127.0.0.1/" + strings.Repeat("x", 4096)}, {Name: "S1", URL: "https://127.0.0.1", Title: strings.Repeat("T", 81)}, {Name: "S1", URL: "https://127.0.0.1", HWID: &HWIDSettings{Enabled: true, ID: "short"}}} {
		data, _ := json.Marshal(Config{Sources: []Source{s}})
		if _, err := ReadConfig(bytes.NewReader(data)); err == nil {
			t.Fatal("source limit bypassed")
		}
	}
	c, err := ReadConfig(strings.NewReader(`{"default":"direct","default":"block","sources":[],"rules":[]}`))
	if err != nil || c.Default != "block" {
		t.Fatal("duplicate-key characterization changed")
	}
	// Go's decoder is last-key-wins. No independent parser/ACL boundary was found.
	t.Log("4 MiB config boundary, object/single-value/unknown field and source URL/title/HWID validation pass; duplicate keys accepted, last value wins")
}

func TestS1ProbeSOCKS5AuthenticationErrorsAndRedirect(t *testing.T) {
	var redirectHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectHits.Add(1); w.WriteHeader(204) }))
	defer target.Close()
	for _, scenario := range []string{"ok", "redirect", "reject-auth", "malformed-response"} {
		t.Run(scenario, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				done <- func() error {
					conn, err := listener.Accept()
					if err != nil {
						return err
					}
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
					var greeting [2]byte
					if _, err = io.ReadFull(conn, greeting[:]); err != nil {
						return err
					}
					if greeting[0] != 5 {
						return fmt.Errorf("wrong SOCKS version")
					}
					methods := make([]byte, int(greeting[1]))
					if _, err = io.ReadFull(conn, methods); err != nil {
						return err
					}
					if !bytes.Contains(methods, []byte{2}) {
						return fmt.Errorf("SOCKS auth method absent")
					}
					if _, err = conn.Write([]byte{5, 2}); err != nil {
						return err
					}
					var auth [2]byte
					if _, err = io.ReadFull(conn, auth[:]); err != nil {
						return err
					}
					username := make([]byte, int(auth[1]))
					if _, err = io.ReadFull(conn, username); err != nil {
						return err
					}
					var plen [1]byte
					if _, err = io.ReadFull(conn, plen[:]); err != nil {
						return err
					}
					password := make([]byte, int(plen[0]))
					if _, err = io.ReadFull(conn, password); err != nil {
						return err
					}
					if auth[0] != 1 || string(username) != "srv_00000000000000000000000000000001" || string(password) != "S1-SYNTHETIC-PROBE-AUTH" {
						return fmt.Errorf("unexpected synthetic SOCKS credentials")
					}
					if scenario == "reject-auth" {
						_, err = conn.Write([]byte{1, 1})
						return err
					}
					if _, err = conn.Write([]byte{1, 0}); err != nil {
						return err
					}
					var connect [4]byte
					if _, err = io.ReadFull(conn, connect[:]); err != nil {
						return err
					}
					if connect[0] != 5 || connect[1] != 1 {
						return fmt.Errorf("bad SOCKS CONNECT")
					}
					addrLen := 4
					switch connect[3] {
					case 1:
					case 4:
						addrLen = 16
					case 3:
						var l [1]byte
						if _, err = io.ReadFull(conn, l[:]); err != nil {
							return err
						}
						addrLen = int(l[0])
					default:
						return fmt.Errorf("bad SOCKS address")
					}
					address := make([]byte, addrLen+2)
					if _, err = io.ReadFull(conn, address); err != nil {
						return err
					}
					if _, err = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1}); err != nil {
						return err
					}
					request, err := http.ReadRequest(bufio.NewReader(conn))
					if err != nil {
						return err
					}
					defer request.Body.Close()
					if request.URL.RawQuery != "token=S1-SYNTHETIC-TOKEN" {
						return fmt.Errorf("HTTP positive control missing")
					}
					if scenario == "malformed-response" {
						_, err = io.WriteString(conn, "S1-SYNTHETIC-PASSWORD invalid HTTP\r\n\r\n")
						return err
					}
					status, extra := "200 OK", ""
					if scenario == "redirect" {
						status = "302 Found"
						extra = "Location: " + target.URL + "/S1-SYNTHETIC-TOKEN\r\n"
					}
					_, err = fmt.Fprintf(conn, "HTTP/1.1 %s\r\n%sContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, extra, len("S1-SYNTHETIC-RESPONSE"), "S1-SYNTHETIC-RESPONSE")
					return err
				}()
			}()
			manifest := HealthManifest{SOCKS: listener.Addr().String(), Password: "S1-SYNTHETIC-PROBE-AUTH"}
			result := Probe(context.Background(), manifest, HealthMember{ID: "srv_00000000000000000000000000000001", Tag: "S1/exit"}, "http://127.0.0.1:1/check?token=S1-SYNTHETIC-TOKEN")
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("local SOCKS fixture did not finish")
			}
			if result.Available != (scenario == "ok") {
				t.Fatalf("wrong probe availability: %+v", result)
			}
			if scenario == "redirect" && result.Reason != "HTTP 302" {
				t.Fatal("redirect was not returned as HTTP 302")
			}
			data, _ := json.Marshal(result)
			for _, marker := range []string{"S1-SYNTHETIC-PROBE-AUTH", "S1-SYNTHETIC-TOKEN", "S1-SYNTHETIC-PASSWORD", "S1-SYNTHETIC-RESPONSE"} {
				if bytes.Contains(data, []byte(marker)) {
					t.Fatal("probe result reflected a synthetic secret")
				}
			}
		})
	}
	if redirectHits.Load() != 0 {
		t.Fatal("probe followed HTTP redirect")
	}
	t.Logf("4 real local SOCKS5 exchanges and 3 tunneled HTTP requests; synthetic auth verified; HTTP 302 not followed (target hits=%d); auth/parse errors redacted; no Xray process used", redirectHits.Load())
}
