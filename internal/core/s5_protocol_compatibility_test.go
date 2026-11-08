package core

// S5 owns this isolated integration harness. It changes no product configuration.
import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const s5UUID = "025009c3-6a69-4128-8de5-4656cb3c82c1"
const s5UUIDNext = "a07af8d7-4a67-4a85-90dd-a93d6b148c25"
const s5Name = "s5.localhost"
const s5Password = "S5:synthetic@password+%/only"

type s5Result struct {
	Name            string   `json:"name"`
	Expectation     string   `json:"expectation"`
	Parser          string   `json:"parser,omitempty"`
	Compile         string   `json:"compile,omitempty"`
	Preflight       string   `json:"preflight,omitempty"`
	HTTP            string   `json:"http,omitempty"`
	UDP             string   `json:"udp,omitempty"`
	HTTPOriginDelta int64    `json:"http_origin_delta,omitempty"`
	UDPEchoDelta    int64    `json:"udp_echo_delta,omitempty"`
	Millis          int64    `json:"millis"`
	Notes           []string `json:"notes,omitempty"`
}

type s5Proc struct {
	cmd     *exec.Cmd
	done    chan error
	log     *os.File
	stopped bool
	name    string
}

type s5Harness struct {
	t                                     *testing.T
	dir, engine, cert, key, ca, untrusted string
	pin, realityPrivate, realityPublic    string
	httpPort, udpPort, coverPort          int
	httpCount, udpCount                   atomic.Int64
	origin, cover                         *http.Server
	udp                                   *net.UDPConn
	ports                                 map[int]bool
	results                               []s5Result
	procs                                 []*s5Proc
	serial                                int
	started                               time.Time
}

func s5JSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}
func s5Clone(v map[string]any) map[string]any {
	var out map[string]any
	_ = json.Unmarshal(s5JSON(v), &out)
	return out
}
func s5Write(t *testing.T, path string, data []byte) {
	t.Helper()
	tmp := path + ".partial"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}
func s5Obj(v any) map[string]any { return v.(map[string]any) }
func s5Port(c net.Addr) int      { return c.(*net.TCPAddr).Port }

func s5New(t *testing.T) *s5Harness {
	t.Helper()
	engine := os.Getenv("S5_XRAY")
	if engine == "" {
		t.Skip("set S5_XRAY to the S5-owned Xray wrapper")
	}
	dir := os.Getenv("S5_RUN_DIR")
	if dir == "" {
		t.Fatal("S5_RUN_DIR is required to retain private evidence")
	}
	dir, _ = filepath.Abs(dir)
	engine, _ = filepath.Abs(engine)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	h := &s5Harness{t: t, dir: dir, engine: engine, ports: map[int]bool{}, started: time.Now()}
	h.cert = filepath.Join(dir, "server.pem")
	h.key = filepath.Join(dir, "server-key.pem")
	h.ca = filepath.Join(dir, "ca.pem")
	h.untrusted = filepath.Join(dir, "untrusted.pem")
	h.s5MakeCertificate()
	rkey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h.realityPrivate = base64.RawURLEncoding.EncodeToString(rkey.Bytes())
	h.realityPublic = base64.RawURLEncoding.EncodeToString(rkey.PublicKey().Bytes())
	s5Write(t, filepath.Join(dir, "reality-private.json"), s5JSON(map[string]string{"privateKey": h.realityPrivate, "publicKey": h.realityPublic}))
	originListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.httpPort = s5Port(originListener.Addr())
	h.ports[h.httpPort] = true
	h.origin = &http.Server{ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.httpCount.Add(1)
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "S5-HTTP-"+r.URL.Path+"\n"+strings.Repeat("synthetic-", 384))
	})}
	go func() { _ = h.origin.Serve(originListener) }()
	uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	h.udp = uc
	h.udpPort = uc.LocalAddr().(*net.UDPAddr).Port
	h.ports[h.udpPort] = true
	go func() {
		buf := make([]byte, 2048)
		for {
			n, a, e := uc.ReadFromUDP(buf)
			if e != nil {
				return
			}
			h.udpCount.Add(1)
			_, _ = uc.WriteToUDP(buf[:n], a)
		}
	}()
	cl, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.coverPort = s5Port(cl.Addr())
	h.ports[h.coverPort] = true
	h.cover = &http.Server{ReadHeaderTimeout: 20 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "S5 REALITY loopback target") })}
	go func() { _ = h.cover.ServeTLS(cl, h.cert, h.key) }()
	t.Cleanup(func() {
		for _, p := range h.procs {
			h.s5Stop(p)
		}
		_ = h.origin.Close()
		_ = h.cover.Close()
		_ = h.udp.Close()
		h.s5Save()
		var ps []map[string]any
		for _, p := range h.procs {
			ps = append(ps, map[string]any{"name": p.name, "pid": p.cmd.Process.Pid, "waited": p.stopped})
		}
		s5Write(t, filepath.Join(h.dir, "process-cleanup.json"), s5JSON(ps))
	})
	return h
}

func (h *s5Harness) s5MakeCertificate() {
	t := h.t
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(501), Subject: pkix.Name{CommonName: "S5 synthetic CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(502), Subject: pkix.Name{CommonName: s5Name}, DNSNames: []string{s5Name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	s5Write(t, h.cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	s5Write(t, h.key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}))
	s5Write(t, h.ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	other := *ca
	other.SerialNumber = big.NewInt(503)
	other.Subject.CommonName = "S5 unrelated CA"
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherDER, err := x509.CreateCertificate(rand.Reader, &other, &other, &otherKey.PublicKey, otherKey)
	if err != nil {
		t.Fatal(err)
	}
	s5Write(t, h.untrusted, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: otherDER}))
	sum := sha256.Sum256(der)
	h.pin = hex.EncodeToString(sum[:])
}

func (h *s5Harness) s5FreePort() int {
	h.t.Helper()
	for i := 0; i < 100; i++ {
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			h.t.Fatal(e)
		}
		p := s5Port(l.Addr())
		u, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p})
		if e == nil {
			_ = u.Close()
		}
		_ = l.Close()
		if e == nil && !h.ports[p] {
			h.ports[p] = true
			return p
		}
	}
	h.t.Fatal("could not reserve a free S5 port")
	return 0
}
func (h *s5Harness) s5Env(trust string) []string {
	if trust == "" {
		trust = h.ca
	}
	return append(os.Environ(), "GOMAXPROCS=2", "GOMEMLIMIT=128MiB", "SSL_CERT_FILE="+trust, "SSL_CERT_DIR="+filepath.Join(h.dir, "empty-ca-dir"))
}
func (h *s5Harness) s5Save() {
	s5Write(h.t, filepath.Join(h.dir, "results.json"), s5JSON(map[string]any{"started_utc": h.started.UTC().Format(time.RFC3339Nano), "elapsed_ms": time.Since(h.started).Milliseconds(), "http_origin_hits": h.httpCount.Load(), "udp_echo_hits": h.udpCount.Load(), "results": h.results}))
}
func (h *s5Harness) s5Add(r s5Result) {
	h.results = append(h.results, r)
	h.s5Save()
	h.t.Logf("S5 %s parser=%s compile=%s preflight=%s HTTP=%s UDP=%s (%dms)", r.Name, r.Parser, r.Compile, r.Preflight, r.HTTP, r.UDP, r.Millis)
}
func (h *s5Harness) s5Preflight(name string, config []byte) bool {
	path := filepath.Join(h.dir, name+".json")
	s5Write(h.t, path, config)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.engine, "run", "-test", "-config", path)
	cmd.Env = h.s5Env("")
	b, err := cmd.CombinedOutput()
	s5Write(h.t, filepath.Join(h.dir, name+".preflight.log"), b)
	return err == nil
}
func (h *s5Harness) s5Start(name string, config []byte, port int, trust string) *s5Proc {
	h.t.Helper()
	path := filepath.Join(h.dir, name+".json")
	s5Write(h.t, path, config)
	f, err := os.OpenFile(filepath.Join(h.dir, name+".runtime.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		h.t.Fatal(err)
	}
	cmd := exec.Command(h.engine, "run", "-config", path)
	cmd.Env = h.s5Env(trust)
	cmd.Stdout = f
	cmd.Stderr = f
	p := &s5Proc{cmd: cmd, log: f, done: make(chan error, 1), name: name}
	if err = cmd.Start(); err != nil {
		_ = f.Close()
		h.t.Fatal(err)
	}
	h.procs = append(h.procs, p)
	go func() { p.done <- cmd.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, e := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 40*time.Millisecond)
		if e == nil {
			_ = c.Close()
			var configObj map[string]any
			_ = json.Unmarshal(config, &configObj)
			for _, rawInbound := range configObj["inbounds"].([]any) {
				inbound := s5Obj(rawInbound)
				if stream, ok := inbound["streamSettings"].(map[string]any); ok && stream["security"] == "reality" {
					time.Sleep(5300 * time.Millisecond)
				}
			}
			return p
		}
		select {
		case e = <-p.done:
			p.stopped = true
			_ = f.Close()
			h.t.Fatalf("%s exited before listening: %v (see runtime log)", name, e)
		default:
		}
		time.Sleep(15 * time.Millisecond)
	}
	h.s5Stop(p)
	h.t.Fatalf("%s did not listen in 3s", name)
	return nil
}
func (h *s5Harness) s5Stop(p *s5Proc) {
	if p == nil || p.stopped {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	p.stopped = true
	_ = p.log.Close()
}

type s5Spec struct {
	name, protocol, network, security, mode, method, encoding, credential string
	query                                                                 url.Values
	jsonOnly                                                              bool
}

func (h *s5Harness) s5Stream(s s5Spec, server bool) map[string]any {
	network := s.network
	if network == "tcp" || network == "" {
		network = "raw"
	}
	if network == "splithttp" {
		network = "xhttp"
	}
	stream := map[string]any{"network": network}
	switch network {
	case "ws":
		stream["wsSettings"] = map[string]any{"path": "/s5-ws", "headers": map[string]any{"Host": s5Name}}
	case "grpc":
		stream["grpcSettings"] = map[string]any{"serviceName": "s5grpc"}
	case "xhttp":
		mode := s.mode
		if mode == "" {
			mode = "auto"
		}
		stream["xhttpSettings"] = map[string]any{"host": s5Name, "path": "/s5-xhttp", "mode": mode, "extra": map[string]any{"noGRPCHeader": true, "xPaddingBytes": "32-64", "scMinPostsIntervalMs": 5}}
	}
	sec := s.security
	if sec == "implicit" {
		sec = "tls"
	}
	if sec == "" || sec == "none" {
		return stream
	}
	stream["security"] = sec
	if sec == "tls" {
		alpn := []string{"http/1.1"}
		if network == "grpc" || network == "xhttp" {
			alpn = []string{"h2", "http/1.1"}
		}
		c := map[string]any{"alpn": alpn}
		if server {
			c["certificates"] = []any{map[string]any{"certificateFile": h.cert, "keyFile": h.key}}
		} else {
			c["serverName"] = s5Name
			c["fingerprint"] = "unsafe"
		}
		stream["tlsSettings"] = c
	}
	if sec == "reality" {
		c := map[string]any{}
		if server {
			c["target"] = fmt.Sprintf("127.0.0.1:%d", h.coverPort)
			c["serverNames"] = []string{s5Name}
			c["privateKey"] = h.realityPrivate
			c["shortIds"] = []string{"d421a17b09c3e8f5"}
		} else {
			c["serverName"] = s5Name
			c["fingerprint"] = "chrome"
			c["publicKey"] = h.realityPublic
			c["shortId"] = "d421a17b09c3e8f5"
			c["spiderX"] = "/s5-spider"
		}
		stream["realitySettings"] = c
	}
	return stream
}
func (s s5Spec) s5Credential() string {
	if s.credential != "" {
		return s.credential
	}
	if s.protocol == "vless" {
		return s5UUID
	}
	return s5Password
}
func (h *s5Harness) s5Server(s s5Spec, port int) map[string]any {
	settings := map[string]any{}
	switch s.protocol {
	case "vless":
		u := map[string]any{"id": s.s5Credential()}
		if s.query.Get("flow") != "" {
			u["flow"] = s.query.Get("flow")
		}
		settings["clients"] = []any{u}
		settings["decryption"] = "none"
	case "trojan":
		settings["clients"] = []any{map[string]any{"password": s.s5Credential()}}
	case "shadowsocks":
		settings["method"] = s.method
		settings["password"] = s.s5Credential()
		settings["network"] = "tcp,udp"
	}
	ob := map[string]any{"tag": "s5-server", "listen": "127.0.0.1", "port": port, "protocol": s.protocol, "settings": settings}
	if s.protocol != "shadowsocks" || s.jsonOnly {
		ob["streamSettings"] = h.s5Stream(s, true)
	}
	return map[string]any{"log": map[string]any{"loglevel": "debug"}, "inbounds": []any{ob}, "outbounds": []any{map[string]any{"tag": "s5-forward", "protocol": "freedom"}}}
}
func (h *s5Harness) s5Input(s s5Spec, port int) []byte {
	if s.jsonOnly {
		settings := map[string]any{}
		switch s.protocol {
		case "vless":
			settings["vnext"] = []any{map[string]any{"address": "127.0.0.1", "port": port, "users": []any{map[string]any{"id": s.s5Credential(), "encryption": "none"}}}}
		case "trojan":
			settings["servers"] = []any{map[string]any{"address": "127.0.0.1", "port": port, "password": s.s5Credential()}}
		case "shadowsocks":
			settings["servers"] = []any{map[string]any{"address": "127.0.0.1", "port": port, "method": s.method, "password": s.s5Credential()}}
		}
		return s5JSON(map[string]any{"tag": s.name, "protocol": s.protocol, "settings": settings, "streamSettings": h.s5Stream(s, false)})
	}
	if s.protocol == "shadowsocks" {
		endpoint := fmt.Sprintf("127.0.0.1:%d", port)
		creds := s.method + ":" + s.s5Credential()
		var link string
		switch s.encoding {
		case "legacy":
			link = "ss://" + base64.StdEncoding.EncodeToString([]byte(creds+"@"+endpoint))
		case "legacy-url":
			link = "ss://" + base64.RawURLEncoding.EncodeToString([]byte(creds+"@"+endpoint))
		case "std":
			link = "ss://" + base64.StdEncoding.EncodeToString([]byte(creds)) + "@" + endpoint
		case "url":
			link = "ss://" + base64.RawURLEncoding.EncodeToString([]byte(creds)) + "@" + endpoint
		default:
			link = "ss://" + url.UserPassword(s.method, s.s5Credential()).String() + "@" + endpoint
		}
		return []byte(link + "#" + url.PathEscape(s.name))
	}
	q := url.Values{"type": {s.network}, "sni": {s5Name}}
	if s.security != "implicit" {
		q.Set("security", s.security)
	}
	if s.security == "reality" {
		q.Set("pbk", h.realityPublic)
		q.Set("sid", "d421a17b09c3e8f5")
		q.Set("spx", "/s5-spider")
	} else {
		q.Set("fp", "unsafe")
		alpn := "http/1.1"
		if s.network == "grpc" || s.network == "xhttp" || s.network == "splithttp" {
			alpn = "h2,http/1.1"
		}
		q.Set("alpn", alpn)
	}
	switch s.network {
	case "ws":
		q.Set("host", s5Name)
		q.Set("path", "/s5-ws")
	case "grpc":
		q.Set("serviceName", "s5grpc")
	case "xhttp", "splithttp":
		mode := s.mode
		if mode == "" {
			mode = "auto"
		}
		q.Set("host", s5Name)
		q.Set("path", "/s5-xhttp")
		q.Set("mode", mode)
		q.Set("extra", `{"noGRPCHeader":true,"xPaddingBytes":"32-64","scMinPostsIntervalMs":5}`)
	}
	for k, v := range s.query {
		q[k] = v
	}
	u := url.URL{Scheme: s.protocol, User: url.User(s.s5Credential()), Host: fmt.Sprintf("127.0.0.1:%d", port), RawQuery: q.Encode(), Fragment: s.name}
	return []byte(u.String())
}

func (h *s5Harness) s5Compile(name string, input []byte) ([]byte, []Node, int, error) {
	s5Write(h.t, filepath.Join(h.dir, name+".subscription.txt"), input)
	nodes, err := ParseSubscription("S5", input)
	if err != nil {
		return nil, nil, 0, err
	}
	cat := filepath.Join(h.dir, name+".servers.json")
	_, err = SyncServers(cat, nodes, Config{})
	if err != nil {
		return nil, nodes, 0, err
	}
	return h.s5CompileNodes(name, nodes, nodes[0].serverID)
}
func (h *s5Harness) s5CompileNodes(name string, nodes []Node, ref string) ([]byte, []Node, int, error) {
	p := h.s5FreePort()
	c := Config{Default: "block", SOCKSPort: p, Rules: []Rule{{Name: "S5 loopback only", IPs: []string{"127.0.0.1/32"}, Action: "proxy", NodeRef: ref}}}
	a, err := Compile(c, nodes)
	if err != nil {
		return nil, nodes, p, err
	}
	s5Write(h.t, filepath.Join(h.dir, name+".compiled.json"), a.Xray)
	var m map[string]any
	_ = json.Unmarshal(a.Xray, &m)
	m["log"] = map[string]any{"loglevel": "debug"}
	return s5JSON(m), nodes, p, nil
}

func s5ReadSocksAddr(c io.Reader) (string, int, error) {
	b := make([]byte, 4)
	if _, e := io.ReadFull(c, b); e != nil {
		return "", 0, e
	}
	if b[0] != 5 || b[1] != 0 {
		return "", 0, fmt.Errorf("SOCKS response %x", b)
	}
	var ip string
	switch b[3] {
	case 1:
		d := make([]byte, 4)
		if _, e := io.ReadFull(c, d); e != nil {
			return "", 0, e
		}
		ip = net.IP(d).String()
	case 4:
		d := make([]byte, 16)
		if _, e := io.ReadFull(c, d); e != nil {
			return "", 0, e
		}
		ip = net.IP(d).String()
	case 3:
		l := []byte{0}
		if _, e := io.ReadFull(c, l); e != nil {
			return "", 0, e
		}
		d := make([]byte, int(l[0]))
		if _, e := io.ReadFull(c, d); e != nil {
			return "", 0, e
		}
		ip = string(d)
	default:
		return "", 0, fmt.Errorf("bad SOCKS ATYP %d", b[3])
	}
	pb := make([]byte, 2)
	_, e := io.ReadFull(c, pb)
	return ip, int(binary.BigEndian.Uint16(pb)), e
}
func s5Socks(port int, cmd byte, target int, timeout time.Duration) (net.Conn, string, int, error) {
	c, e := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if e != nil {
		return nil, "", 0, e
	}
	_ = c.SetDeadline(time.Now().Add(timeout))
	fail := func(e error) (net.Conn, string, int, error) { _ = c.Close(); return nil, "", 0, e }
	if _, e = c.Write([]byte{5, 1, 0}); e != nil {
		return fail(e)
	}
	b := make([]byte, 2)
	if _, e = io.ReadFull(c, b); e != nil {
		return fail(e)
	}
	if !bytes.Equal(b, []byte{5, 0}) {
		return fail(fmt.Errorf("SOCKS auth %x", b))
	}
	req := []byte{5, cmd, 0, 1, 127, 0, 0, 1, byte(target >> 8), byte(target)}
	if cmd == 3 {
		req = []byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}
	}
	if _, e = c.Write(req); e != nil {
		return fail(e)
	}
	host, p, e := s5ReadSocksAddr(c)
	if e != nil {
		return fail(e)
	}
	return c, host, p, nil
}
func (h *s5Harness) s5HttpExchange(port int, name string, timeout time.Duration) error {
	c, _, _, e := s5Socks(port, 1, h.httpPort, timeout)
	if e != nil {
		return e
	}
	defer c.Close()
	path := "/" + name
	if _, e = fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nConnection: close\r\n\r\n", path, h.httpPort); e != nil {
		return e
	}
	resp, e := http.ReadResponse(bufio.NewReader(c), nil)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if e != nil {
		return e
	}
	want := "S5-HTTP-" + path + "\n" + strings.Repeat("synthetic-", 384)
	if resp.StatusCode != 200 || string(b) != want {
		return fmt.Errorf("HTTP payload/status mismatch (%d, %d bytes)", resp.StatusCode, len(b))
	}
	return nil
}
func (h *s5Harness) s5UdpExchange(port int, name string, timeout time.Duration) error {
	c, host, p, e := s5Socks(port, 3, 0, timeout)
	if e != nil {
		return e
	}
	defer c.Close()
	if host == "0.0.0.0" || host == "" {
		host = "127.0.0.1"
	}
	if host != "127.0.0.1" {
		return fmt.Errorf("non-loopback SOCKS UDP relay %s", host)
	}
	u, e := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(host), Port: p})
	if e != nil {
		return e
	}
	defer u.Close()
	_ = u.SetDeadline(time.Now().Add(timeout))
	payload := []byte("S5-UDP-" + name + "-" + strings.Repeat("packet", 24))
	d := []byte{0, 0, 0, 1, 127, 0, 0, 1, byte(h.udpPort >> 8), byte(h.udpPort)}
	d = append(d, payload...)
	if _, e = u.Write(d); e != nil {
		return e
	}
	buf := make([]byte, 2048)
	n, e := u.Read(buf)
	if e != nil {
		return e
	}
	if n < 10 || !bytes.Equal(buf[0:4], []byte{0, 0, 0, 1}) || binary.BigEndian.Uint16(buf[8:10]) != uint16(h.udpPort) || !bytes.Equal(buf[10:n], payload) {
		return fmt.Errorf("UDP payload/header mismatch: %d bytes", n)
	}
	return nil
}
func s5Status(e error) string {
	if e == nil {
		return "pass"
	}
	return "fail: " + e.Error()
}

func (h *s5Harness) s5Positive(s s5Spec) {
	h.t.Helper()
	start := time.Now()
	r := s5Result{Name: s.name, Expectation: "parser+preflight+HTTP+UDP success"}
	sp := h.s5FreePort()
	input := h.s5Input(s, sp)
	client, nodes, cp, err := h.s5Compile(s.name, input)
	if len(nodes) > 0 {
		r.Parser = "pass"
	} else {
		r.Parser = s5Status(err)
	}
	if err != nil {
		r.Compile = s5Status(err)
		h.s5Add(r)
		h.t.Errorf("%s: %v", s.name, err)
		return
	}
	r.Compile = "pass"
	server := s5JSON(h.s5Server(s, sp))
	a := h.s5Preflight(s.name+"-server", server)
	b := h.s5Preflight(s.name+"-client", client)
	if !a || !b {
		r.Preflight = fmt.Sprintf("fail: server=%v client=%v", a, b)
		r.Millis = time.Since(start).Milliseconds()
		h.s5Add(r)
		h.t.Errorf("%s: %s", s.name, r.Preflight)
		return
	}
	r.Preflight = "pass"
	ps := h.s5Start(s.name+"-server", server, sp, "")
	defer h.s5Stop(ps)
	trust := ""
	if strings.HasPrefix(s.name, "tls-pin-") {
		trust = h.untrusted
	}
	pc := h.s5Start(s.name+"-client", client, cp, trust)
	defer h.s5Stop(pc)
	before := h.httpCount.Load()
	err = h.s5HttpExchange(cp, s.name, 4*time.Second)
	r.HTTP = s5Status(err)
	r.HTTPOriginDelta = h.httpCount.Load() - before
	if err != nil {
		h.t.Errorf("%s HTTP: %v", s.name, err)
	}
	before = h.udpCount.Load()
	err = h.s5UdpExchange(cp, s.name, 4*time.Second)
	r.UDP = s5Status(err)
	r.UDPEchoDelta = h.udpCount.Load() - before
	if err != nil {
		h.t.Errorf("%s UDP: %v", s.name, err)
	}
	r.Millis = time.Since(start).Milliseconds()
	h.s5Add(r)
}

func TestS5Compatibility(t *testing.T) {
	h := s5New(t)
	for _, s := range []s5Spec{
		{name: "vless-tls-raw", protocol: "vless", network: "raw", security: "tls"},
		{name: "vless-tls-tcp-alias", protocol: "vless", network: "tcp", security: "tls"},
		{name: "vless-tls-ws", protocol: "vless", network: "ws", security: "tls"},
		{name: "vless-tls-grpc", protocol: "vless", network: "grpc", security: "tls"},
		{name: "vless-tls-xhttp-auto", protocol: "vless", network: "xhttp", security: "tls"},
		{name: "vless-tls-xhttp-stream-one", protocol: "vless", network: "xhttp", security: "tls", mode: "stream-one"},
		{name: "vless-tls-xhttp-stream-up", protocol: "vless", network: "xhttp", security: "tls", mode: "stream-up"},
		{name: "vless-tls-xhttp-packet-up", protocol: "vless", network: "xhttp", security: "tls", mode: "packet-up"},
		{name: "vless-tls-splithttp-alias", protocol: "vless", network: "splithttp", security: "tls"},
		{name: "vless-reality-raw", protocol: "vless", network: "raw", security: "reality"},
		{name: "vless-reality-vision", protocol: "vless", network: "tcp", security: "reality", query: url.Values{"flow": {"xtls-rprx-vision"}}},
		{name: "vless-reality-grpc", protocol: "vless", network: "grpc", security: "reality"},
		{name: "vless-reality-xhttp", protocol: "vless", network: "xhttp", security: "reality"},
		{name: "vless-reality-xhttp-stream-one", protocol: "vless", network: "xhttp", security: "reality", mode: "stream-one"},
		{name: "trojan-implicit-tls-raw", protocol: "trojan", network: "raw", security: "implicit"},
		{name: "trojan-tls-ws", protocol: "trojan", network: "ws", security: "tls"},
		{name: "trojan-tls-grpc", protocol: "trojan", network: "grpc", security: "tls"},
		{name: "trojan-tls-xhttp", protocol: "trojan", network: "xhttp", security: "tls"},
		{name: "trojan-reality-raw", protocol: "trojan", network: "raw", security: "reality"},
		{name: "trojan-reality-grpc", protocol: "trojan", network: "grpc", security: "reality"},
		{name: "trojan-reality-xhttp", protocol: "trojan", network: "xhttp", security: "reality"},
		{name: "vless-json-plain-raw", protocol: "vless", network: "raw", security: "none", jsonOnly: true},
		{name: "ss-sip002-plain", protocol: "shadowsocks", method: "aes-128-gcm"},
		{name: "ss-sip002-base64", protocol: "shadowsocks", method: "aes-256-gcm", encoding: "std"},
		{name: "ss-sip002-base64url", protocol: "shadowsocks", method: "chacha20-ietf-poly1305", encoding: "url"},
		{name: "ss-legacy-base64", protocol: "shadowsocks", method: "aes-128-gcm", encoding: "legacy"},
		{name: "ss-legacy-base64url", protocol: "shadowsocks", method: "aes-256-gcm", encoding: "legacy-url"},
		{name: "ss-2022-aes128", protocol: "shadowsocks", method: "2022-blake3-aes-128-gcm", encoding: "url", credential: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x15}, 16))},
		{name: "ss-2022-aes256", protocol: "shadowsocks", method: "2022-blake3-aes-256-gcm", encoding: "std", credential: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x25}, 32))},
		{name: "ss-2022-chacha", protocol: "shadowsocks", method: "2022-blake3-chacha20-poly1305", encoding: "url", credential: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x35}, 32))},
	} {
		t.Run(s.name, func(t *testing.T) { h.t = t; h.s5Positive(s) })
	}
	h.t = t
	t.Run("parser-and-preflight-negatives", func(t *testing.T) { h.t = t; h.s5ParserNegatives() })
	h.t = t
	t.Run("parameter-preservation", func(t *testing.T) { h.t = t; h.s5Preservation() })
	h.t = t
	t.Run("runtime-negatives", func(t *testing.T) { h.t = t; h.s5Negatives() })
	h.t = t
	t.Run("chains", func(t *testing.T) { h.t = t; h.s5Chains() })
	h.t = t
	t.Run("credential-rotation", func(t *testing.T) { h.t = t; h.s5Rotation() })
	h.t = t
}

func (h *s5Harness) s5ParserNegatives() {
	base := s5Spec{name: "base", protocol: "vless", network: "raw", security: "tls"}
	p := h.s5FreePort()
	good := string(h.s5Input(base, p))
	alter := func(q url.Values) string {
		u, _ := url.Parse(good)
		old := u.Query()
		for k, v := range q {
			old[k] = v
		}
		u.RawQuery = old.Encode()
		return u.String()
	}
	cases := []struct{ name, input, stage string }{
		{"reject-unknown-transport", alter(url.Values{"type": {"kcp"}}), "parser"},
		{"reject-security-none-uri", alter(url.Values{"security": {"none"}}), "parser"},
		{"reject-allow-insecure", alter(url.Values{"allowInsecure": {"1"}}), "parser"},
		{"reject-duplicate-query", good + "&security=tls&security=tls", "parser"},
		{"reject-unknown-option", alter(url.Values{"s5Unknown": {"true"}}), "parser"},
		{"reject-xhttp-null-extra", alter(url.Values{"type": {"xhttp"}, "extra": {"null"}}), "parser"},
		{"reject-xhttp-array-extra", alter(url.Values{"type": {"xhttp"}, "extra": {"[]"}}), "parser"},
		{"reject-xhttp-bad-type", alter(url.Values{"type": {"xhttp"}, "extra": {"{\"path\":15}"}}), "parser"},
		{"reject-non-xhttp-extra", alter(url.Values{"type": {"ws"}, "extra": {"{}"}}), "parser"},
		{"reject-reality-without-key", alter(url.Values{"security": {"reality"}}), "parser"},
		{"reject-trojan-flow", strings.Replace(alter(url.Values{"flow": {"xtls-rprx-vision"}}), "vless://", "trojan://", 1), "parser"},
		{"reject-ss-plugin", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:synthetic")) + fmt.Sprintf("@127.0.0.1:%d/?plugin=v2ray-plugin", p), "parser"},
		{"reject-xhttp-invalid-mode", alter(url.Values{"type": {"xhttp"}, "mode": {"s5-invalid"}}), "preflight"},
		{"reject-reality-ws", alter(url.Values{"type": {"ws"}, "security": {"reality"}, "pbk": {h.realityPublic}}), "preflight"},
		{"reject-reality-bad-pbk", alter(url.Values{"security": {"reality"}, "pbk": {"s5-invalid-key"}}), "preflight"},
		{"reject-bad-pin", alter(url.Values{"pcs": {"abcdef"}}), "preflight"},
		{"reject-ss-method", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("s5-invalid:synthetic")) + fmt.Sprintf("@127.0.0.1:%d", p), "preflight"},
		{"reject-ss2022-key-length", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:c2hvcnQ=")) + fmt.Sprintf("@127.0.0.1:%d", p), "preflight"},
		{"reject-xhttp-stream-one-download", alter(url.Values{"type": {"xhttp"}, "mode": {"stream-one"}, "extra": {"{\"downloadSettings\":{\"network\":\"raw\"}}"}}), "preflight"},
	}
	// Append query before the fragment so this is an actual duplicate parameter.
	for i := range cases {
		if cases[i].name == "reject-duplicate-query" {
			u, _ := url.Parse(good)
			u.RawQuery += "&security=tls"
			cases[i].input = u.String()
		}
	}
	for _, c := range cases {
		r := s5Result{Name: c.name, Expectation: "rejection at " + c.stage}
		start := time.Now()
		client, nodes, _, err := h.s5Compile(c.name, []byte(c.input))
		if len(nodes) > 0 {
			r.Parser = "pass"
		} else {
			r.Parser = s5Status(err)
		}
		if c.stage == "parser" {
			if err == nil {
				h.t.Errorf("%s unexpectedly accepted", c.name)
				r.Notes = []string{"unexpected acceptance"}
			} else {
				r.Notes = []string{"expected parser rejection"}
			}
		} else if err != nil {
			h.t.Errorf("%s failed before preflight: %v", c.name, err)
			r.Compile = s5Status(err)
		} else {
			r.Compile = "pass"
			if h.s5Preflight(c.name+"-client", client) {
				r.Preflight = "pass (unexpected)"
				h.t.Errorf("%s preflight accepted incompatible config", c.name)
			} else {
				r.Preflight = "rejected as expected"
			}
		}
		r.Millis = time.Since(start).Milliseconds()
		h.s5Add(r)
	}
}

func (h *s5Harness) s5Preservation() {
	p := h.s5FreePort()
	extra := `{"host":"` + s5Name + `","path":"/extra-path","mode":"packet-up","noGRPCHeader":true,"xPaddingBytes":"17-23","scMaxEachPostBytes":"10000-20000","xmux":{"maxConnections":2},"downloadSettings":{"address":"127.0.0.1","port":` + fmt.Sprint(p) + `,"network":"xhttp","security":"tls","tlsSettings":{"serverName":"` + s5Name + `","alpn":["h2"]},"sockopt":{"dialerProxy":"hop"},"xhttpSettings":{"host":"` + s5Name + `","path":"/download"}}}`
	s := s5Spec{name: "preserve", protocol: "vless", network: "xhttp", security: "tls", query: url.Values{"extra": {extra}, "path": {"/query-path"}, "mode": {"stream-up"}, "cs": {"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"}, "pcs": {h.pin}, "alpn": {"h2,http/1.1"}}}
	// A URI cannot refer to another outbound; test URI extra without the dependency first.
	var e map[string]any
	_ = json.Unmarshal([]byte(extra), &e)
	delete(s5Obj(e["downloadSettings"]), "sockopt")
	s.query.Set("extra", string(s5JSON(e)))
	input := h.s5Input(s, p)
	client, nodes, _, err := h.s5Compile(s.name, input)
	r := s5Result{Name: "parameter-preservation", Expectation: "URI/JSON parameters retained after import, unprefix, catalogue and Compile"}
	if err != nil {
		h.t.Fatal(err)
	}
	r.Parser = "pass"
	r.Compile = "pass"
	var n map[string]any
	_ = json.Unmarshal(s5JSON(nodes[0]), &n)
	stream := s5Obj(n["streamSettings"])
	x := s5Obj(stream["xhttpSettings"])
	tlsSettings := s5Obj(stream["tlsSettings"])
	if x["path"] != "/query-path" || x["mode"] != "stream-up" || x["host"] != s5Name || !bytes.Equal(s5JSON(x["extra"]), s5JSON(e)) {
		h.t.Error("XHTTP query precedence or extra content lost")
	}
	if tlsSettings["pinnedPeerCertSha256"] != h.pin || tlsSettings["cipherSuites"] != s.query.Get("cs") || !bytes.Equal(s5JSON(tlsSettings["alpn"]), s5JSON([]string{"h2", "http/1.1"})) {
		h.t.Error("TLS parameters lost")
	}
	unprefixed, err := UnprefixNodes("S5", nodes)
	if err != nil {
		h.t.Fatal(err)
	}
	again, err := ParseSubscription("S5", s5JSON(unprefixed))
	if err != nil {
		h.t.Fatal(err)
	}
	if !bytes.Equal(s5JSON(nodes), s5JSON(again)) {
		h.t.Error("Unprefix + Parse round trip changed outbound")
	}
	var cfg map[string]any
	_ = json.Unmarshal(client, &cfg)
	compiled := cfg["outbounds"].([]any)[2]
	delete(n, "location")
	if !bytes.Equal(s5JSON(n), s5JSON(compiled)) {
		h.t.Error("Compile changed provider outbound")
	}
	if h.s5Preflight(s.name+"-client", client) {
		r.Preflight = "pass"
	} else {
		r.Preflight = "fail"
		h.t.Error("preserved parameters fail xray -test")
	}
	// Test default extraction from extra when host/path/mode have no query values.
	u, _ := url.Parse(string(input))
	q := u.Query()
	q.Del("host")
	q.Del("path")
	q.Del("mode")
	u.RawQuery = q.Encode()
	ns, err := ParseSubscription("S5", []byte(u.String()))
	if err != nil {
		h.t.Fatal(err)
	}
	var st map[string]any
	_ = json.Unmarshal(ns[0].StreamSettings, &st)
	xx := s5Obj(st["xhttpSettings"])
	if xx["host"] != s5Name || xx["path"] != "/extra-path" || xx["mode"] != "packet-up" {
		h.t.Error("XHTTP extra fallback extraction lost")
	}
	// All 4 base64 encodings of a mixed synthetic subscription must retain three nodes.
	lines := string(h.s5Input(s5Spec{name: "b64-vless", protocol: "vless", network: "raw", security: "tls"}, p)) + "\n" + string(h.s5Input(s5Spec{name: "b64-trojan", protocol: "trojan", network: "ws", security: "tls"}, p)) + "\n" + string(h.s5Input(s5Spec{name: "b64-ss", protocol: "shadowsocks", method: "aes-128-gcm", encoding: "url"}, p))
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		b := enc.EncodeToString([]byte(lines))
		mid := len(b) / 2
		ns, err = ParseSubscription("S5", []byte(" \n"+b[:mid]+"\n"+b[mid:]+"\n"))
		if err != nil || len(ns) != 3 {
			h.t.Errorf("base64 subscription: %d nodes %v", len(ns), err)
		}
	}
	r.Notes = []string{"asserted query precedence; extra fallback; nested downloadSettings; ALPN; cipherSuites; SHA256 pin; Unprefix+Parse roundtrip; exact compiled outbound; 4 mixed-list base64 encodings"}
	h.s5Add(r)
}

func (h *s5Harness) s5Negative(name string, serverSpec s5Spec, change func(map[string]any), trust string, wantUDP bool) {
	start := time.Now()
	sp := h.s5FreePort()
	in := h.s5Input(serverSpec, sp)
	client, nodes, cp, err := h.s5Compile(name, in)
	if err != nil {
		h.t.Fatal(err)
	}
	var cfg map[string]any
	_ = json.Unmarshal(client, &cfg)
	out := s5Obj(cfg["outbounds"].([]any)[2])
	change(out)
	client = s5JSON(cfg)
	r := s5Result{Name: name, Expectation: "preflight success; HTTP/UDP refusal with zero origin hits", Parser: "pass", Compile: "pass"}
	_ = nodes
	if !h.s5Preflight(name+"-client", client) {
		r.Preflight = "fail"
		h.s5Add(r)
		h.t.Errorf("%s unexpectedly failed preflight", name)
		return
	}
	r.Preflight = "pass"
	var ps *s5Proc
	if name != "unavailable-upstream" {
		server := s5JSON(h.s5Server(serverSpec, sp))
		if !h.s5Preflight(name+"-server", server) {
			h.t.Fatal("negative-test server preflight failed")
		}
		ps = h.s5Start(name+"-server", server, sp, "")
		defer h.s5Stop(ps)
	}
	pc := h.s5Start(name+"-client", client, cp, trust)
	defer h.s5Stop(pc)
	before := h.httpCount.Load()
	err = h.s5HttpExchange(cp, name, 1800*time.Millisecond)
	r.HTTPOriginDelta = h.httpCount.Load() - before
	if err == nil || r.HTTPOriginDelta != 0 {
		r.HTTP = "unexpected success/leak"
		h.t.Errorf("%s HTTP leaked: %v hits=%d", name, err, r.HTTPOriginDelta)
	} else {
		r.HTTP = "rejected as expected: " + err.Error()
	}
	if wantUDP {
		before = h.udpCount.Load()
		err = h.s5UdpExchange(cp, name, 800*time.Millisecond)
		r.UDPEchoDelta = h.udpCount.Load() - before
		if err == nil || r.UDPEchoDelta != 0 {
			r.UDP = "unexpected success/leak"
			h.t.Errorf("%s UDP leaked: %v hits=%d", name, err, r.UDPEchoDelta)
		} else {
			r.UDP = "rejected as expected: " + err.Error()
		}
	}
	r.Millis = time.Since(start).Milliseconds()
	h.s5Add(r)
}
func (h *s5Harness) s5Negatives() {
	base := s5Spec{name: "negative", protocol: "vless", network: "raw", security: "tls"}
	noop := func(map[string]any) {}
	h.s5Negative("unavailable-upstream", base, noop, "", true)
	h.s5Negative("tls-untrusted-ca", base, noop, h.untrusted, false)
	h.s5Negative("tls-wrong-name", base, func(o map[string]any) {
		s5Obj(s5Obj(o["streamSettings"])["tlsSettings"])["serverName"] = "wrong.s5.localhost"
	}, "", false)
	h.s5Negative("tls-wrong-pin", base, func(o map[string]any) {
		s5Obj(s5Obj(o["streamSettings"])["tlsSettings"])["pinnedPeerCertSha256"] = strings.Repeat("a1", 32)
	}, "", false)
	h.s5Negative("tls-alpn-mismatch", base, func(o map[string]any) {
		s5Obj(s5Obj(o["streamSettings"])["tlsSettings"])["alpn"] = []string{"s5-no-match"}
	}, "", false)
	ws := base
	ws.network = "ws"
	h.s5Negative("ws-wrong-path", ws, func(o map[string]any) { s5Obj(s5Obj(o["streamSettings"])["wsSettings"])["path"] = "/s5-wrong" }, "", false)
	grpc := base
	grpc.network = "grpc"
	h.s5Negative("grpc-wrong-service", grpc, func(o map[string]any) { s5Obj(s5Obj(o["streamSettings"])["grpcSettings"])["serviceName"] = "wronggrpc" }, "", false)
	xhttp := base
	xhttp.network = "xhttp"
	h.s5Negative("xhttp-wrong-path", xhttp, func(o map[string]any) { s5Obj(s5Obj(o["streamSettings"])["xhttpSettings"])["path"] = "/s5-wrong" }, "", false)
	reality := base
	reality.security = "reality"
	h.s5Negative("reality-wrong-shortid", reality, func(o map[string]any) {
		s5Obj(s5Obj(o["streamSettings"])["realitySettings"])["shortId"] = "aaaaaaaaaaaaaaaa"
	}, "", false)
	rkey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		h.t.Fatal(err)
	}
	h.s5Negative("reality-wrong-publickey", reality, func(o map[string]any) {
		s5Obj(s5Obj(o["streamSettings"])["realitySettings"])["publicKey"] = base64.RawURLEncoding.EncodeToString(rkey.PublicKey().Bytes())
	}, "", false)
	// A correct pin works without the test CA and supports a comma-separated set.
	for _, pinCase := range []struct{ name, pin string }{{"tls-pin-exact", h.pin}, {"tls-pin-list", strings.Repeat("a2", 32) + "," + h.pin}} {
		s := base
		s.name = pinCase.name
		s.query = url.Values{"pcs": {pinCase.pin}}
		h.s5Positive(s)
	}
}

func (h *s5Harness) s5Chains() {
	for _, kind := range []string{"dialerProxy", "proxySettings", "nested-download"} {
		name := "chain-" + kind
		start := time.Now()
		main := s5Spec{name: "entry", protocol: "vless", network: "raw", security: "tls"}
		if kind == "nested-download" {
			main.network = "xhttp"
			main.mode = "stream-up"
		}
		hop := s5Spec{name: "hop", protocol: "shadowsocks", method: "aes-128-gcm", encoding: "url"}
		mp, hp := h.s5FreePort(), h.s5FreePort()
		mainNodes, err := ParseSubscription("S5", h.s5Input(main, mp))
		if err != nil {
			h.t.Fatal(err)
		}
		hopNodes, err := ParseSubscription("S5", h.s5Input(hop, hp))
		if err != nil {
			h.t.Fatal(err)
		}
		var ob, hopOb map[string]any
		_ = json.Unmarshal(s5JSON(mainNodes[0]), &ob)
		_ = json.Unmarshal(s5JSON(hopNodes[0]), &hopOb)
		ob["tag"] = "entry"
		hopOb["tag"] = "hop"
		stream := s5Obj(ob["streamSettings"])
		if kind == "proxySettings" {
			ob["proxySettings"] = map[string]any{"tag": "hop", "transportLayer": true}
		} else {
			stream["sockopt"] = map[string]any{"dialerProxy": "hop", "tcpKeepAliveIdle": 30}
		}
		if kind == "nested-download" {
			download := h.s5Stream(main, false)
			download["address"] = "127.0.0.1"
			download["port"] = mp
			download["sockopt"] = map[string]any{"dialerProxy": "hop"}
			s5Obj(download["xhttpSettings"])["mode"] = "stream-up"
			s5Obj(s5Obj(stream["xhttpSettings"])["extra"])["downloadSettings"] = download
		}
		profile := s5JSON(map[string]any{"remarks": "S5 profile / " + kind, "inbounds": []any{map[string]any{"listen": "0.0.0.0", "port": 1}}, "routing": map[string]any{"rules": []any{map[string]any{"outboundTag": "hop"}}}, "outbounds": []any{ob, hopOb, map[string]any{"tag": "unused-direct", "protocol": "freedom", "settings": map[string]any{}}}})
		client, nodes, cp, err := h.s5Compile(name, profile)
		if err != nil {
			h.t.Fatal(err)
		}
		if len(nodes) != 2 || !nodes[1].auxiliary {
			h.t.Error("profile graph did not retain exactly 2 dependent outbounds")
		}
		unprefix, err := UnprefixNodes("S5", nodes)
		if err != nil {
			h.t.Fatal(err)
		}
		again, err := ParseSubscription("S5", s5JSON(unprefix))
		if err != nil || !bytes.Equal(s5JSON(nodes), s5JSON(again)) {
			h.t.Errorf("chain Unprefix round trip: %v", err)
		}
		r := s5Result{Name: name, Expectation: "two real Xray hops; HTTP+UDP; hop outage refuses", Parser: "pass", Compile: "pass"}
		ms, hs := s5JSON(h.s5Server(main, mp)), s5JSON(h.s5Server(hop, hp))
		if !h.s5Preflight(name+"-client", client) || !h.s5Preflight(name+"-entry", ms) || !h.s5Preflight(name+"-hop", hs) {
			r.Preflight = "fail"
			h.s5Add(r)
			h.t.Errorf("%s preflight", name)
			continue
		}
		r.Preflight = "pass"
		pm := h.s5Start(name+"-entry", ms, mp, "")
		ph := h.s5Start(name+"-hop", hs, hp, "")
		pc := h.s5Start(name+"-client", client, cp, "")
		before := h.httpCount.Load()
		err = h.s5HttpExchange(cp, name, 4*time.Second)
		r.HTTP = s5Status(err)
		r.HTTPOriginDelta = h.httpCount.Load() - before
		if err != nil {
			h.t.Errorf("%s HTTP %v", name, err)
		}
		before = h.udpCount.Load()
		err = h.s5UdpExchange(cp, name, 4*time.Second)
		r.UDP = s5Status(err)
		r.UDPEchoDelta = h.udpCount.Load() - before
		if err != nil {
			h.t.Errorf("%s UDP %v", name, err)
		}
		h.s5Stop(pc)
		// CompileProbe must retain the same dependency graph and perform a real authenticated SOCKS HTTP probe.
		probePort := h.s5FreePort()
		setup, e := CompileProbe(nodes, []string{nodes[0].serverID}, probePort)
		if e != nil {
			h.t.Fatal(e)
		}
		if h.s5Preflight(name+"-probe", setup.Xray) {
			pp := h.s5Start(name+"-probe", setup.Xray, probePort, "")
			pr := Probe(context.Background(), setup.Manifest, setup.Members[0], fmt.Sprintf("http://127.0.0.1:%d/%s-probe", h.httpPort, name))
			h.s5Stop(pp)
			if !pr.Available {
				h.t.Errorf("%s real Probe did not connect", name)
			}
			r.Notes = append(r.Notes, fmt.Sprintf("CompileProbe dependency closure: real HTTP alive=%v", pr.Available))
		} else {
			h.t.Errorf("%s probe preflight", name)
		}
		h.s5Stop(ph)
		h.s5Stop(pc)
		clientPort := h.s5FreePort()
		var cc map[string]any
		_ = json.Unmarshal(client, &cc)
		s5Obj(cc["inbounds"].([]any)[0])["port"] = clientPort
		pc = h.s5Start(name+"-client-hop-down", s5JSON(cc), clientPort, "")
		before = h.httpCount.Load()
		err = h.s5HttpExchange(clientPort, name+"-hop-down", 1200*time.Millisecond)
		delta := h.httpCount.Load() - before
		if err == nil || delta != 0 {
			h.t.Errorf("%s hop outage leaked", name)
			r.Notes = append(r.Notes, "hop outage unexpectedly reached HTTP origin")
		} else {
			r.Notes = append(r.Notes, "hop outage refused HTTP; origin delta 0; entry server remained running")
		}
		h.s5Stop(pc)
		h.s5Stop(pm)
		r.Millis = time.Since(start).Milliseconds()
		h.s5Add(r)
	}
}

func (h *s5Harness) s5Rotation() {
	for _, proto := range []string{"vless", "trojan", "shadowsocks"} {
		name := "rotation-" + proto
		start := time.Now()
		s := s5Spec{name: name, protocol: proto, network: "raw", security: "tls", method: "aes-128-gcm", encoding: "url"}
		sp := h.s5FreePort()
		client, nodes, cp, err := h.s5Compile(name, h.s5Input(s, sp))
		if err != nil {
			h.t.Fatal(err)
		}
		ref := nodes[0].serverID
		cat := filepath.Join(h.dir, name+".servers.json")
		server := s5JSON(h.s5Server(s, sp))
		if !h.s5Preflight(name+"-old-client", client) || !h.s5Preflight(name+"-old-server", server) {
			h.t.Fatal("rotation preflight")
		}
		ps := h.s5Start(name+"-old-server", server, sp, "")
		pc := h.s5Start(name+"-old-client", client, cp, "")
		if e := h.s5HttpExchange(cp, name+"-before", 3*time.Second); e != nil {
			h.t.Errorf("%s initial HTTP %v", name, e)
		}
		h.s5Stop(pc)
		h.s5Stop(ps)
		s.credential = s5Password + "-rotated"
		if proto == "vless" {
			s.credential = s5UUIDNext
		}
		s.name = name + "-renamed"
		nextNodes, err := ParseSubscription("S5", h.s5Input(s, sp))
		if err != nil {
			h.t.Fatal(err)
		}
		saved := Config{Rules: []Rule{{NodeRef: ref, Action: "proxy"}}, Picker: PickerSettings{Pinned: []string{ref}, Order: []string{ref}}}
		_, err = SyncServers(cat, nextNodes, saved)
		if err != nil {
			h.t.Fatal(err)
		}
		stable := nextNodes[0].serverID == ref
		if !stable {
			h.t.Errorf("%s stable ID changed after unambiguous credential rotation", name)
		}
		newServer := s5JSON(h.s5Server(s, sp))
		if !h.s5Preflight(name+"-new-server", newServer) {
			h.t.Fatal("rotated server preflight")
		}
		ps = h.s5Start(name+"-new-server", newServer, sp, "")
		pc = h.s5Start(name+"-stale-client", client, cp, "")
		before := h.httpCount.Load()
		e := h.s5HttpExchange(cp, name+"-stale", 1500*time.Millisecond)
		if e == nil || h.httpCount.Load() != before {
			h.t.Errorf("%s stale credential reached HTTP origin", name)
		}
		beforeUDP := h.udpCount.Load()
		ue := h.s5UdpExchange(cp, name+"-stale", 700*time.Millisecond)
		if ue == nil || h.udpCount.Load() != beforeUDP {
			h.t.Errorf("%s stale credential reached UDP echo", name)
		}
		h.s5Stop(pc)
		newClient, _, np, err := h.s5CompileNodes(name+"-updated", nextNodes, ref)
		if err != nil {
			h.t.Fatal(err)
		}
		if !h.s5Preflight(name+"-updated-client", newClient) {
			h.t.Fatal("updated client preflight")
		}
		pc = h.s5Start(name+"-updated-client", newClient, np, "")
		r := s5Result{Name: name, Expectation: "stable catalogue ID; old credential rejected, new credential exchanges HTTP+UDP", Parser: "pass", Compile: "pass", Preflight: "pass"}
		before = h.httpCount.Load()
		r.HTTP = s5Status(h.s5HttpExchange(np, name+"-updated", 3*time.Second))
		r.HTTPOriginDelta = h.httpCount.Load() - before
		beforeUDP = h.udpCount.Load()
		r.UDP = s5Status(h.s5UdpExchange(np, name+"-updated", 3*time.Second))
		r.UDPEchoDelta = h.udpCount.Load() - beforeUDP
		if r.HTTP != "pass" || r.UDP != "pass" {
			h.t.Errorf("%s after rotation HTTP=%s UDP=%s", name, r.HTTP, r.UDP)
		}
		r.Notes = []string{fmt.Sprintf("stableID=%v; source and endpoint unchanged; tag and credentials rotated", stable), fmt.Sprintf("stale HTTP error=%v; stale UDP error=%v; both origin deltas 0 expected", e, ue)}
		h.s5Stop(pc)
		h.s5Stop(ps)
		r.Millis = time.Since(start).Milliseconds()
		h.s5Add(r)
	}
}
