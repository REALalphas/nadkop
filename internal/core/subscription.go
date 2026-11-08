package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxSubscription = 4 << 20

type Node struct {
	Tag            string          `json:"tag"`
	Location       *ServerLocation `json:"location,omitempty"`
	Protocol       string          `json:"protocol"`
	Settings       json.RawMessage `json:"settings"`
	StreamSettings json.RawMessage `json:"streamSettings,omitempty"`
	extra          map[string]json.RawMessage
	serverID       string
	serverAliases  []string
	displayName    string
	auxiliary      bool
}

func Load(s Source) ([]Node, error) {
	return LoadContext(context.Background(), s)
}

func LoadContext(ctx context.Context, s Source) ([]Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return loadContext(ctx, s, nil, HWIDPath, ReadDeviceInfo())
}

func loadContext(ctx context.Context, s Source, transport http.RoundTripper, identityPath string, info DeviceInfo) ([]Node, error) {
	data, _, err := readSubscription(ctx, s, transport, identityPath, info)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nodes, err := ParseSubscription(s.Name, data)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nodes, err
}

func readSubscription(ctx context.Context, s Source, transport http.RoundTripper, identityPath string, info DeviceInfo) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	var r io.ReadCloser
	var sentHWID bool
	var title string
	if err := s.validateFormat(); err != nil {
		return nil, "", err
	}
	if err := s.validateHWID(); err != nil {
		return nil, "", err
	}
	if s.File != "" {
		f, err := openPrivateState(s.File)
		if err != nil {
			return nil, "", fmt.Errorf("cannot open subscription file")
		}
		r = f
	} else {
		u, err := url.Parse(s.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return nil, "", fmt.Errorf("subscription requires an HTTPS URL without userinfo")
		}
		req, err := http.NewRequestWithContext(ctx, "GET", s.URL, nil)
		if err != nil {
			return nil, "", fmt.Errorf("invalid subscription request")
		}
		if err := applySourceHeaders(req, s, identityPath, info); err != nil {
			return nil, "", err
		}
		sentHWID = req.Header.Get("X-HWID") != ""
		client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("redirect outside source origin")
			}
			return nil
		}}
		resp, err := client.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("subscription request failed (URL and headers redacted)")
		}
		if err := hwidResponseError(resp.Header, sentHWID); err != nil {
			resp.Body.Close()
			return nil, "", err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, "", fmt.Errorf("subscription HTTP status %d", resp.StatusCode)
		}
		title = resp.Header.Get("Profile-Title")
		r = resp.Body
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(subscriptionReader{ctx, r}, maxSubscription+1))
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}
	if err != nil {
		return nil, "", fmt.Errorf("cannot read subscription")
	}
	if len(b) > maxSubscription {
		return nil, "", fmt.Errorf("subscription exceeds 4 MiB")
	}
	if s.File == "" && !sentHWID && bodyRequiresHWID(b) {
		return nil, "", fmt.Errorf("subscription requires HWID; enable HWID in this source's settings")
	}
	if s.File == "" && s.Format != "links" && !isJSONSubscription(b) {
		return nil, "", fmt.Errorf("provider returned links instead of Xray JSON; check this source's User-Agent or select the links format")
	}
	return b, title, nil
}

type subscriptionReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r subscriptionReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func ParseSubscription(source string, b []byte) ([]Node, error) {
	if !namePattern.MatchString(source) {
		return nil, fmt.Errorf("invalid source name")
	}
	s := subscriptionText(b)
	if s == "" {
		return nil, fmt.Errorf("empty subscription")
	}
	nodes := []Node{}
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
		var err error
		nodes, err = parseXrayJSON([]byte(s))
		if err != nil {
			return nil, err
		}
	} else {
		if !strings.Contains(s, "://") {
			decoded, err := decodeBase64(strings.Join(strings.Fields(s), ""))
			if err != nil {
				return nil, fmt.Errorf("expected outbound array, URI list or base64 URI list")
			}
			s = string(decoded)
		}
		for i, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			n, err := parseShareURI(line)
			if err != nil {
				return nil, fmt.Errorf("subscription line %d: %w", i+1, err)
			}
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 && !strings.HasPrefix(s, "[") {
		return nil, fmt.Errorf("subscription has no nodes")
	}
	seen := map[string]bool{}
	for i := range nodes {
		n := &nodes[i]
		if n.Tag == "" {
			n.Tag = strconv.Itoa(i + 1)
		}
		if strings.ContainsAny(n.Tag, "\x00\r\n/") || seen[n.Tag] {
			return nil, fmt.Errorf("invalid or duplicate node tag")
		}
		seen[n.Tag] = true
		if code := flagCountryCode(n.Tag); code != "" && (n.Location == nil || (n.Location.CountryCode == "" && n.Location.Country == "")) {
			n.Location = mergeLocation(n.Location, &ServerLocation{CountryCode: code})
		}
		switch n.Protocol {
		case "vless", "vmess", "trojan", "shadowsocks", "socks", "http", "freedom", "blackhole":
		default:
			return nil, fmt.Errorf("unsupported outbound protocol")
		}
		var settings map[string]any
		if (len(n.Settings) == 0 || strings.TrimSpace(string(n.Settings)) == "null") && (n.Protocol == "freedom" || n.Protocol == "blackhole") {
			n.Settings = json.RawMessage(`{}`)
		}
		if json.Unmarshal(n.Settings, &settings) != nil || settings == nil {
			return nil, fmt.Errorf("node settings must be an object")
		}
	}
	tags := map[string]string{}
	for _, n := range nodes {
		tags[n.Tag] = source + "/" + n.Tag
	}
	for i, n := range nodes {
		var err error
		nodes[i], err = renameOutbound(n, tags)
		if err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

func decodeBase64(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("invalid base64")
}

func parseShareURI(s string) (Node, error) {
	if strings.HasPrefix(s, "ss://") {
		return parseShadowsocks(s)
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "vless" && u.Scheme != "trojan") {
		return Node{}, fmt.Errorf("unsupported URI scheme: use VLESS, Trojan, Shadowsocks or Xray outbound JSON")
	}
	if u.User == nil || u.User.Username() == "" || u.Hostname() == "" {
		return Node{}, fmt.Errorf("invalid proxy endpoint")
	}
	if _, ok := u.User.Password(); ok {
		return Node{}, fmt.Errorf("unexpected userinfo password; escape colons in credentials")
	}
	port, err := endpointPort(u)
	if err != nil {
		return Node{}, err
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Node{}, fmt.Errorf("invalid URI query")
	}
	for k, values := range q {
		if len(values) != 1 {
			return Node{}, fmt.Errorf("duplicate URI parameter")
		}
		switch k {
		case "type", "security", "sni", "fp", "pbk", "sid", "spx", "flow", "encryption", "host", "path", "serviceName", "mode", "extra", "alpn", "cs", "pcs":
		default:
			return Node{}, fmt.Errorf("unsupported URI parameter %q", k)
		}
	}
	if e := q.Get("encryption"); e != "" && e != "none" {
		return Node{}, fmt.Errorf("unsupported VLESS encryption")
	}
	if u.Scheme == "trojan" && q.Get("flow") != "" {
		return Node{}, fmt.Errorf("flow only applies to VLESS")
	}
	transport := q.Get("type")
	if transport == "" || transport == "tcp" {
		transport = "raw"
	}
	stream := map[string]any{"network": transport}
	switch transport {
	case "raw":
	case "ws":
		stream["wsSettings"] = map[string]any{"path": q.Get("path"), "headers": map[string]string{"Host": q.Get("host")}}
	case "grpc":
		stream["grpcSettings"] = map[string]string{"serviceName": q.Get("serviceName")}
	case "xhttp", "splithttp":
		stream["network"] = "xhttp"
		xhttp := map[string]any{}
		var extra map[string]json.RawMessage
		if q.Get("extra") != "" {
			if json.Unmarshal([]byte(q.Get("extra")), &extra) != nil || extra == nil {
				return Node{}, fmt.Errorf("XHTTP extra must be a JSON object")
			}
			xhttp["extra"] = extra
		}
		for _, key := range []string{"host", "path", "mode"} {
			value := q.Get(key)
			if value == "" && extra[key] != nil {
				if json.Unmarshal(extra[key], &value) != nil {
					return Node{}, fmt.Errorf("XHTTP host, path and mode must be strings")
				}
			}
			xhttp[key] = value
		}
		stream["xhttpSettings"] = xhttp
	default:
		return Node{}, fmt.Errorf("unsupported URI transport")
	}
	if transport != "xhttp" && transport != "splithttp" && (q.Get("extra") != "" || q.Get("mode") != "") {
		return Node{}, fmt.Errorf("extra and mode require XHTTP")
	}
	security := q.Get("security")
	if security == "" && u.Scheme == "trojan" {
		security = "tls"
	}
	switch security {
	case "tls":
		stream["security"] = "tls"
		tls := map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp")}
		if q.Get("alpn") != "" {
			tls["alpn"] = strings.Split(q.Get("alpn"), ",")
		}
		if q.Get("cs") != "" {
			tls["cipherSuites"] = q.Get("cs")
		}
		if q.Get("pcs") != "" {
			tls["pinnedPeerCertSha256"] = q.Get("pcs")
		}
		stream["tlsSettings"] = tls
	case "reality":
		if q.Get("pbk") == "" {
			return Node{}, fmt.Errorf("REALITY requires pbk")
		}
		fp := q.Get("fp")
		if fp == "" {
			fp = "chrome"
		}
		stream["security"] = "reality"
		stream["realitySettings"] = map[string]string{"serverName": q.Get("sni"), "fingerprint": fp, "publicKey": q.Get("pbk"), "shortId": q.Get("sid"), "spiderX": q.Get("spx")}
	default:
		return Node{}, fmt.Errorf("URI requires TLS or REALITY")
	}
	var settings []byte
	if u.Scheme == "vless" {
		settings, _ = json.Marshal(map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": port, "users": []any{map[string]string{"id": u.User.Username(), "encryption": "none", "flow": q.Get("flow")}}}}})
	} else {
		settings, _ = json.Marshal(map[string]any{"servers": []any{map[string]any{"address": u.Hostname(), "port": port, "password": u.User.Username()}}})
	}
	ss, _ := json.Marshal(stream)
	return Node{Tag: u.Fragment, Protocol: u.Scheme, Settings: settings, StreamSettings: ss}, nil
}

func endpointPort(u *url.URL) (int, error) {
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || u.Hostname() == "" {
		return 0, fmt.Errorf("invalid proxy host or port")
	}
	return port, nil
}

func parseShadowsocks(s string) (Node, error) {
	// Legacy links encode the entire method:password@host:port authority.
	body, fragment, _ := strings.Cut(strings.TrimPrefix(s, "ss://"), "#")
	authority, query, _ := strings.Cut(body, "?")
	if !strings.Contains(authority, "@") {
		decoded, err := decodeBase64(authority)
		if err != nil {
			return Node{}, fmt.Errorf("invalid Shadowsocks base64 authority")
		}
		separator := strings.LastIndexByte(string(decoded), '@')
		if separator < 0 {
			return Node{}, fmt.Errorf("invalid Shadowsocks credentials")
		}
		userinfo, endpoint := string(decoded[:separator]), string(decoded[separator+1:])
		method, password, credentials := strings.Cut(userinfo, ":")
		if !credentials {
			return Node{}, fmt.Errorf("invalid Shadowsocks credentials")
		}
		s = "ss://" + url.UserPassword(method, password).String() + "@" + endpoint
		if query != "" {
			s += "?" + query
		}
		if fragment != "" {
			s += "#" + fragment
		}
	}
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return Node{}, fmt.Errorf("invalid Shadowsocks endpoint")
	}
	port, err := endpointPort(u)
	if err != nil {
		return Node{}, err
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 0 {
		return Node{}, fmt.Errorf("Shadowsocks plugins and query options are unsupported")
	}
	method := u.User.Username()
	password, plain := u.User.Password()
	if !plain {
		decoded, err := decodeBase64(method)
		if err != nil {
			return Node{}, fmt.Errorf("invalid Shadowsocks base64 userinfo")
		}
		method, password, plain = strings.Cut(string(decoded), ":")
	}
	if !plain || method == "" || password == "" {
		return Node{}, fmt.Errorf("invalid Shadowsocks credentials")
	}
	settings, _ := json.Marshal(map[string]any{"servers": []any{map[string]any{"address": u.Hostname(), "port": port, "method": method, "password": password}}})
	return Node{Tag: u.Fragment, Protocol: "shadowsocks", Settings: settings}, nil
}
