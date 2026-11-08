package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func TestS5ProtocolEdges(t *testing.T) {
	h := s5New(t)
	for _, s := range []s5Spec{
		{name: "tls-chrome-fingerprint", protocol: "vless", network: "raw", security: "tls", query: url.Values{"fp": {"chrome"}}},
		{name: "tls-default-fingerprint", protocol: "vless", network: "raw", security: "tls", query: url.Values{"fp": {""}}},
		{name: "tls-pin-chrome", protocol: "trojan", network: "ws", security: "tls", query: url.Values{"fp": {"chrome"}, "pcs": {h.pin}}},
		{name: "xhttp-extra-fallback", protocol: "vless", network: "xhttp", security: "tls", mode: "packet-up", query: url.Values{"path": {""}, "host": {""}, "mode": {""}, "extra": {`{"host":"s5.localhost","path":"/s5-xhttp","mode":"packet-up","noGRPCHeader":true,"xPaddingBytes":"32-64","scMinPostsIntervalMs":5}`}}},
		{name: "vless-tls-vision", protocol: "vless", network: "raw", security: "tls", query: url.Values{"flow": {"xtls-rprx-vision"}, "fp": {"chrome"}}},
	} {
		t.Run(s.name, func(t *testing.T) { h.t = t; h.s5Positive(s) })
	}
	h.t = t
	for _, proto := range []string{"trojan", "shadowsocks"} {
		s := s5Spec{name: "missing", protocol: proto, network: "raw", security: "tls", method: "aes-128-gcm", encoding: "url"}
		t.Run("unavailable-"+proto, func(t *testing.T) { h.t = t; h.s5NegativeMissing("unavailable-"+proto, s) })
	}
	h.t = t
	t.Run("chain-protocol-layer", func(t *testing.T) { h.t = t; h.s5ProtocolChain() })
	h.t = t
	t.Run("chain-graph-rejections", func(t *testing.T) { h.t = t; h.s5GraphRejections() })
	h.t = t
	t.Run("ambiguous-rotation", func(t *testing.T) { h.t = t; h.s5AmbiguousRotation() })
	h.t = t
	t.Run("incompatible-vision-websocket", func(t *testing.T) { h.t = t; h.s5VisionWebsocket() })
	h.t = t
}

func (h *s5Harness) s5NegativeMissing(name string, s s5Spec) {
	start := time.Now()
	p := h.s5FreePort()
	client, _, cp, err := h.s5Compile(name, h.s5Input(s, p))
	if err != nil {
		h.t.Fatal(err)
	}
	r := s5Result{Name: name, Expectation: "upstream absent; preflight succeeds, HTTP+UDP cannot reach origin", Parser: "pass", Compile: "pass"}
	if !h.s5Preflight(name+"-client", client) {
		h.t.Fatal("missing upstream preflight should succeed")
	}
	r.Preflight = "pass"
	pc := h.s5Start(name+"-client", client, cp, "")
	defer h.s5Stop(pc)
	before := h.httpCount.Load()
	e := h.s5HttpExchange(cp, name, 1800*time.Millisecond)
	r.HTTPOriginDelta = h.httpCount.Load() - before
	if e == nil || r.HTTPOriginDelta != 0 {
		h.t.Errorf("%s HTTP unexpected delivery", name)
		r.HTTP = "unexpected success"
	} else {
		r.HTTP = "rejected as expected: " + e.Error()
	}
	before = h.udpCount.Load()
	e = h.s5UdpExchange(cp, name, 800*time.Millisecond)
	r.UDPEchoDelta = h.udpCount.Load() - before
	if e == nil || r.UDPEchoDelta != 0 {
		h.t.Errorf("%s UDP unexpected delivery", name)
		r.UDP = "unexpected success"
	} else {
		r.UDP = "rejected as expected: " + e.Error()
	}
	r.Millis = time.Since(start).Milliseconds()
	h.s5Add(r)
}

func (h *s5Harness) s5ProtocolChain() {
	name := "chain-proxySettings-protocol"
	start := time.Now()
	main := s5Spec{name: "entry", protocol: "vless", network: "raw", security: "none", jsonOnly: true}
	hop := s5Spec{name: "hop", protocol: "shadowsocks", method: "aes-128-gcm", encoding: "url"}
	mp, hp := h.s5FreePort(), h.s5FreePort()
	mn, err := ParseSubscription("S5", h.s5Input(main, mp))
	if err != nil {
		h.t.Fatal(err)
	}
	hn, err := ParseSubscription("S5", h.s5Input(hop, hp))
	if err != nil {
		h.t.Fatal(err)
	}
	var mo, ho map[string]any
	_ = json.Unmarshal(s5JSON(mn[0]), &mo)
	_ = json.Unmarshal(s5JSON(hn[0]), &ho)
	mo["tag"] = "entry"
	ho["tag"] = "hop"
	mo["proxySettings"] = map[string]any{"tag": "hop", "transportLayer": false}
	mo["sendThrough"] = "127.0.0.1"
	mo["targetStrategy"] = "AsIs"
	mo["mux"] = map[string]any{"enabled": false, "concurrency": 2}
	profile := s5JSON(map[string]any{"remarks": "S5 protocol chain", "outbounds": []any{mo, ho}})
	client, nodes, cp, err := h.s5Compile(name, profile)
	if err != nil {
		h.t.Fatal(err)
	}
	var cfg map[string]any
	_ = json.Unmarshal(client, &cfg)
	co := s5Obj(cfg["outbounds"].([]any)[2])
	if co["sendThrough"] != "127.0.0.1" || co["targetStrategy"] != "AsIs" || !bytes.Equal(s5JSON(co["mux"]), s5JSON(mo["mux"])) || s5Obj(co["proxySettings"])["transportLayer"] != false || s5Obj(co["proxySettings"])["tag"] != nodes[1].Tag {
		h.t.Error("extra fields or dependency reference lost")
	}
	ms, hs := s5JSON(h.s5Server(main, mp)), s5JSON(h.s5Server(hop, hp))
	if !h.s5Preflight(name+"-client", client) || !h.s5Preflight(name+"-entry", ms) || !h.s5Preflight(name+"-hop", hs) {
		h.t.Fatal("protocol chain preflight")
	}
	pm := h.s5Start(name+"-entry", ms, mp, "")
	defer h.s5Stop(pm)
	ph := h.s5Start(name+"-hop", hs, hp, "")
	defer h.s5Stop(ph)
	pc := h.s5Start(name+"-client", client, cp, "")
	defer h.s5Stop(pc)
	r := s5Result{Name: name, Expectation: "VLESS RAW wrapped by Shadowsocks at protocol layer; HTTP+UDP, no bypass on hop failure", Parser: "pass", Compile: "pass", Preflight: "pass"}
	before := h.httpCount.Load()
	e := h.s5HttpExchange(cp, name, 4*time.Second)
	r.HTTP = s5Status(e)
	r.HTTPOriginDelta = h.httpCount.Load() - before
	before = h.udpCount.Load()
	e = h.s5UdpExchange(cp, name, 4*time.Second)
	r.UDP = s5Status(e)
	r.UDPEchoDelta = h.udpCount.Load() - before
	if r.HTTP != "pass" || r.UDP != "pass" {
		h.t.Errorf("protocol chain HTTP=%s UDP=%s", r.HTTP, r.UDP)
	}
	h.s5Stop(pc)
	h.s5Stop(ph)
	p2 := h.s5FreePort()
	s5Obj(cfg["inbounds"].([]any)[0])["port"] = p2
	pc = h.s5Start(name+"-hop-down", s5JSON(cfg), p2, "")
	defer h.s5Stop(pc)
	bh, bu := h.httpCount.Load(), h.udpCount.Load()
	he := h.s5HttpExchange(p2, name+"-down", 1200*time.Millisecond)
	ue := h.s5UdpExchange(p2, name+"-down", 700*time.Millisecond)
	dh, du := h.httpCount.Load()-bh, h.udpCount.Load()-bu
	if he == nil || ue == nil || dh != 0 || du != 0 {
		h.t.Error("protocol layer chain bypassed missing hop")
	}
	r.Notes = []string{fmt.Sprintf("hop outage: HTTP error=%v, UDP error=%v, origin deltas=%d/%d", he, ue, dh, du), "mux, sendThrough, targetStrategy and proxySettings.transportLayer=false preserved"}
	r.Millis = time.Since(start).Milliseconds()
	h.s5Add(r)
	// The exact same ID must still be resolvable by the production probe compiler.
	setup, e := CompileProbe(nodes, []string{nodes[0].serverID}, h.s5FreePort())
	if e != nil {
		h.t.Fatal(e)
	}
	if !h.s5Preflight(name+"-probe", setup.Xray) {
		h.t.Error("protocol chain CompileProbe preflight")
	}
}

func (h *s5Harness) s5GraphRejections() {
	base := map[string]any{"tag": "entry", "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": h.s5FreePort(), "users": []any{map[string]any{"id": s5UUID, "encryption": "none"}}}}}, "streamSettings": map[string]any{"network": "raw", "sockopt": map[string]any{"dialerProxy": "absent"}}}
	r := s5Result{Name: "graph-missing-reference", Expectation: "parser rejects missing proxy dependency"}
	_, err := ParseSubscription("S5", s5JSON(base))
	if err == nil {
		h.t.Error("missing graph dependency accepted")
	}
	r.Parser = s5Status(err)
	r.Notes = []string{"expected rejection"}
	h.s5Add(r)
	cyclic := s5Clone(base)
	s5Obj(s5Obj(cyclic["streamSettings"])["sockopt"])["dialerProxy"] = "entry"
	nodes, err := ParseSubscription("S5", s5JSON(cyclic))
	if err != nil {
		h.t.Fatal(err)
	}
	_, err = SyncServers(filepath.Join(h.dir, "graph-cyclic.servers.json"), nodes, Config{})
	if err == nil {
		h.t.Error("cycle accepted by catalogue")
	}
	h.s5Add(s5Result{Name: "graph-self-cycle", Expectation: "catalogue rejects cyclic connection before runtime", Parser: "pass", Compile: s5Status(err), Notes: []string{"expected catalogue rejection; no Xray process started"}})
	conflict := s5Clone(base)
	conflict["proxySettings"] = map[string]any{"tag": "hop", "transportLayer": true}
	s5Obj(s5Obj(conflict["streamSettings"])["sockopt"])["dialerProxy"] = "hop"
	hop := map[string]any{"tag": "hop", "protocol": "freedom", "settings": map[string]any{}}
	client, _, _, err := h.s5Compile("graph-conflicting-chain-options", s5JSON([]any{conflict, hop}))
	if err != nil {
		h.t.Fatal(err)
	}
	ok := h.s5Preflight("graph-conflicting-chain-options-client", client)
	if ok {
		h.t.Error("conflicting proxySettings/dialerProxy accepted by Xray")
	}
	h.s5Add(s5Result{Name: "graph-conflicting-chain-options", Expectation: "parser keeps both options; Xray preflight rejects conflict", Parser: "pass", Compile: "pass", Preflight: fmt.Sprintf("rejected=%v", !ok)})
}

func (h *s5Harness) s5AmbiguousRotation() {
	p := h.s5FreePort()
	a := s5Spec{name: "ambiguous-A", protocol: "vless", network: "raw", security: "tls"}
	b := a
	b.name = "ambiguous-B"
	b.credential = s5UUIDNext
	old, err := ParseSubscription("S5", append(append(h.s5Input(a, p), '\n'), h.s5Input(b, p)...))
	if err != nil {
		h.t.Fatal(err)
	}
	cat := filepath.Join(h.dir, "ambiguous.servers.json")
	_, err = SyncServers(cat, old, Config{})
	if err != nil {
		h.t.Fatal(err)
	}
	ra, rb := old[0].serverID, old[1].serverID
	saved := Config{Picker: PickerSettings{Pinned: []string{ra, rb}}, Rules: []Rule{{Action: "proxy", NodeRefs: []string{ra, rb}}}}
	a.name = "ambiguous-renamed-X"
	a.credential = "2d287ee2-505d-4b89-9431-210caab5f681"
	b.name = "ambiguous-renamed-Y"
	b.credential = "468f52bf-a3d5-4542-9ac7-2bd762b27b14"
	next, err := ParseSubscription("S5", append(append(h.s5Input(a, p), '\n'), h.s5Input(b, p)...))
	if err != nil {
		h.t.Fatal(err)
	}
	summaries, err := SyncServers(cat, next, saved)
	if err != nil {
		h.t.Fatal(err)
	}
	retained := 0
	for _, s := range summaries {
		if (s.ID == ra || s.ID == rb) && !s.Available {
			retained++
		}
	}
	if retained != 2 || next[0].serverID == ra || next[0].serverID == rb || next[1].serverID == ra || next[1].serverID == rb {
		h.t.Error("ambiguous endpoint reassigned a bound reference")
	}
	_, _, _, err = h.s5CompileNodes("ambiguous-bound", next, ra)
	if err == nil {
		h.t.Error("missing retained ID compiled as a live route")
	}
	h.s5Add(s5Result{Name: "ambiguous-credential-rotation", Expectation: "never guess between two rotated credentials at identical endpoint", Parser: "pass", Compile: s5Status(err), Notes: []string{fmt.Sprintf("retained unavailable bound IDs=%d/2; fresh credentials receive distinct fresh IDs; Compile with old singleton reference rejects", retained)}})
}

func (h *s5Harness) s5VisionWebsocket() {
	s := s5Spec{name: "vision-ws-incompatible", protocol: "vless", network: "ws", security: "tls", query: url.Values{"flow": {"xtls-rprx-vision"}}}
	sp := h.s5FreePort()
	client, _, cp, err := h.s5Compile(s.name, h.s5Input(s, sp))
	if err != nil {
		h.t.Fatal(err)
	}
	server := s5JSON(h.s5Server(s, sp))
	if !h.s5Preflight(s.name+"-client", client) || !h.s5Preflight(s.name+"-server", server) {
		h.t.Fatal("vision websocket unexpectedly rejected before runtime; see preflight")
	}
	ps := h.s5Start(s.name+"-server", server, sp, "")
	defer h.s5Stop(ps)
	pc := h.s5Start(s.name+"-client", client, cp, "")
	defer h.s5Stop(pc)
	bh, bu := h.httpCount.Load(), h.udpCount.Load()
	he := h.s5HttpExchange(cp, s.name, 1800*time.Millisecond)
	ue := h.s5UdpExchange(cp, s.name, 800*time.Millisecond)
	if he == nil || ue == nil || h.httpCount.Load() != bh || h.udpCount.Load() != bu {
		h.t.Error("XTLS Vision unexpectedly worked through WebSocket")
	}
	h.s5Add(s5Result{Name: s.name, Expectation: "documented engine restriction: Vision needs direct TLS/REALITY, cannot wrap WS", Parser: "pass", Compile: "pass", Preflight: "pass", HTTP: "rejected as expected: " + fmt.Sprint(he), UDP: "rejected as expected: " + fmt.Sprint(ue), Notes: []string{"no HTTP/UDP origin hits; parser acceptance/preflight do not imply connection compatibility", "engine runtime log contains XTLS only supports TLS and REALITY directly for now"}})
}
