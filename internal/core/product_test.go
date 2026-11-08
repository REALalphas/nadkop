package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func productConfig(t *testing.T, fields map[string]any) Config {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ReadConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestProductNetworkConfigurationBoundary(t *testing.T) {
	cases := []struct {
		name  string
		value any
		valid bool
	}{
		{"disabled", map[string]any{"enabled": false}, true},
		{"defaults", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}}, true},
		{"dual-stack", map[string]any{"enabled": true, "interfaces": []string{"br-lan", "guest"}, "ipv6": true, "dnsUpstream": "::1"}, true},
		{"bypass", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "bypass": []string{"192.0.2.3/24", "2001:db8::1/32"}}, true},
		{"missing-device", map[string]any{"enabled": true}, false},
		{"loopback", map[string]any{"enabled": true, "interfaces": []string{"lo"}}, false},
		{"wan", map[string]any{"enabled": true, "interfaces": []string{"eth0"}}, false},
		{"duplicate-device", map[string]any{"enabled": true, "interfaces": []string{"br-lan", "br-lan"}}, false},
		{"invalid-device", map[string]any{"enabled": true, "interfaces": []string{"br-lan\";flush ruleset"}}, false},
		{"long-device", map[string]any{"enabled": true, "interfaces": []string{strings.Repeat("a", 16)}}, false},
		{"too-many-devices", map[string]any{"enabled": true, "interfaces": strings.Fields(strings.Repeat("lan ", 17))}, false},
		{"dns-same-port", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "port": 1053}, false},
		{"socks-same-port", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "port": 1080}, false},
		{"reserved-api", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "port": 61080}, false},
		{"reserved-probe", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "dnsPort": 61081}, false},
		{"privileged-port", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "port": 53}, false},
		{"overflow-port", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "port": 65536}, false},
		{"negative-port", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "dnsPort": -1}, false},
		{"upstream-domain", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "dnsUpstream": "dns.example"}, false},
		{"upstream-port", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "dnsUpstream": "1.1.1.1:53"}, false},
		{"upstream-multicast", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "dnsUpstream": "224.0.0.1"}, false},
		{"upstream-unspecified", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "dnsUpstream": "0.0.0.0"}, false},
		{"upstream-fake", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "dnsUpstream": "198.18.0.1"}, false},
		{"upstream-v6-fake", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "ipv6": true, "dnsUpstream": "fc00::1"}, false},
		{"v6-upstream-with-v4", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "dnsUpstream": "::1"}, false},
		{"bypass-injection", map[string]any{"enabled": true, "interfaces": []string{"br-lan"}, "bypass": []string{"1.1.1.1; flush ruleset"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"wanInterface": "eth0", "transparent": tc.value})
			c, err := ReadConfig(bytes.NewReader(raw))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
			if err == nil && c.Transparent.Enabled && c.Transparent.Port == 0 {
				t.Fatal("missing defaults")
			}
		})
	}
	for _, interval := range []int{-1, 0, 1, 6, 168, 169} {
		t.Run(fmt.Sprintf("interval-%d", interval), func(t *testing.T) {
			_, err := ReadConfig(strings.NewReader(fmt.Sprintf(`{"updateIntervalHours":%d}`, interval)))
			if (err == nil) != (interval >= 0 && interval <= 168) {
				t.Fatal(err)
			}
		})
	}
	for _, raw := range []string{"null", "[]", "true", `{"socksPort":1.5}`, `{"unknown":true}`, `{} {}`, strings.Repeat(" ", maxSubscription+1)} {
		if _, err := ReadConfig(strings.NewReader(raw)); err == nil {
			t.Fatal("accepted invalid object")
		}
	}
}

func TestProductOrderedRulesAndZapretIsolation(t *testing.T) {
	c := productConfig(t, map[string]any{"wanInterface": "eth0", "zapretArgs": []string{"--filter-tcp=80,443", "--dpi-desync=fake"}, "rules": []any{
		map[string]any{"domains": []string{"private.example.org"}, "ips": []string{"203.0.113.0/24"}, "action": "direct"},
		map[string]any{"domains": []string{"example.org"}, "action": "zapret"},
		map[string]any{"domains": []string{"example.org"}, "action": "block"},
	}})
	a, err := Compile(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var x struct {
		Outbounds []Node
		Routing   struct {
			DomainStrategy string
			Rules          []struct {
				Domain      []string
				IP          []string
				OutboundTag string
			}
		}
	}
	if err = json.Unmarshal(a.Xray, &x); err != nil {
		t.Fatal(err)
	}
	if x.Routing.DomainStrategy != "IPOnDemand" || len(x.Routing.Rules) != 4 {
		t.Fatal(string(a.Xray))
	}
	want := []string{"direct", "direct", "__nadkop:zapret", "block"}
	for i, r := range x.Routing.Rules {
		if r.OutboundTag != want[i] || len(r.Domain) > 0 && len(r.IP) > 0 {
			t.Fatal("first-match / OR contract violated", string(a.Xray))
		}
	}
	for _, n := range x.Outbounds {
		if n.Protocol == "freedom" {
			var stream struct{ Sockopt struct{ Mark uint32 } }
			json.Unmarshal(n.StreamSettings, &stream)
			expected := uint32(BypassZapretMark)
			if n.Tag == "__nadkop:zapret" {
				expected = ZapretMark
			}
			if stream.Sockopt.Mark != expected {
				t.Fatal(n.Tag, stream)
			}
		}
	}
	if !strings.Contains(string(a.NFT), "meta mark & 0x60000000 == 0") {
		t.Fatal("Zapret can capture bypass/recursive packets")
	}
}

func TestProductTransparentDNSAndFirewallContract(t *testing.T) {
	for _, v6 := range []bool{false, true} {
		t.Run(fmt.Sprint(v6), func(t *testing.T) {
			c := productConfig(t, map[string]any{"transparent": map[string]any{"enabled": true, "interfaces": []string{"br-lan", "guest"}, "ipv6": v6, "bypass": []string{"203.0.113.0/24"}}, "default": "block"})
			a, err := Compile(c, nil)
			if err != nil {
				t.Fatal(err)
			}
			var x map[string]any
			if err = json.Unmarshal(a.Xray, &x); err != nil {
				t.Fatal(err)
			}
			if len(x["inbounds"].([]any)) != 3 {
				t.Fatal("missing listeners")
			}
			routing := x["routing"].(map[string]any)["rules"].([]any)
			if routing[0].(map[string]any)["outboundTag"] != "__nadkop:dns" || routing[1].(map[string]any)["outboundTag"] != "direct" {
				t.Fatal("DNS is subject to ordinary default block")
			}
			if len(x["fakedns"].([]any)) != map[bool]int{false: 1, true: 2}[v6] {
				t.Fatal("wrong fake pools")
			}
			nft := string(a.NFT)
			dns := strings.Index(nft, "th dport 53 tproxy")
			local := strings.Index(nft, "fib daddr type local return")
			fake := strings.Index(nft, "ip daddr 198.18.0.0/15")
			bypass := strings.Index(nft, "ip daddr { 0.0.0.0/8")
			custom := strings.Index(nft, "ip daddr 203.0.113.0/24 return")
			if dns < 0 || local < dns || fake < local || bypass < fake || custom < bypass {
				t.Fatal("DNS/FakeDNS bypass order", nft)
			}
			if !strings.Contains(nft, `iifname != { "br-lan", "guest" } return`) || !strings.Contains(nft, "input_guard") || strings.Contains(nft, "hook output") {
				t.Fatal("LAN isolation violated", nft)
			}
			if strings.Contains(nft, "meta nfproto ipv6 meta l4proto { tcp, udp } drop") == v6 {
				t.Fatal("IPv6 bypass policy")
			}
			if !strings.Contains(string(a.Input), `comment "nadkop-tproxy"`) {
				t.Fatal("fw4 exception absent")
			}
			var plan RuntimeSettings
			if err = json.Unmarshal(a.Runtime, &plan); err != nil || !plan.Transparent.Enabled || plan.Transparent.IPv6 != v6 {
				t.Fatal(plan, err)
			}
			files := a.Files()
			if len(files) != 10 || !bytes.Contains(files["restore.nft"], []byte("flush table inet nadkop")) {
				t.Fatal("incomplete publication")
			}
			for name := range files {
				if filepath.Base(name) != name {
					t.Fatal("unsafe artifact name")
				}
			}
		})
	}
}

func TestProductPortFilterFollowsStrategy(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		want, absent string
	}{
		{[]string{"--filter-tcp=8080,8443-8445", "--dpi-desync=fake"}, "tcp dport { 8080, 8443-8445 }", "udp dport"},
		{[]string{"--filter-udp=*", "--dpi-desync=fake"}, "meta l4proto udp queue", "tcp dport"},
		{[]string{"--filter-tcp=~443", "--dpi-desync=fake"}, "tcp dport != { 443 }", "udp dport"},
		{[]string{"--dpi-desync=fake"}, "udp dport { 443 }", "never-present"},
	} {
		raw, _ := json.Marshal(map[string]any{"wanInterface": "eth0", "zapretArgs": tc.args, "rules": []Rule{{Domains: []string{"dpi.test"}, Action: "zapret"}}})
		c, err := ReadConfig(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		a, err := Compile(c, nil)
		if err != nil || !bytes.Contains(a.NFT, []byte(tc.want)) || bytes.Contains(a.NFT, []byte(tc.absent)) {
			t.Fatal(string(a.NFT), err)
		}
	}
	for _, arg := range []string{"--filter-tcp=0", "--filter-tcp=65536", "--filter-tcp=443-80", "--filter-tcp=80;drop", "--filter-tcp=", "--filter-udp=1-2-3", "--filter-tcp=080", "--dpi-desync-fwmark=1", "--hostlist=/tmp/x", "--new"} {
		raw, _ := json.Marshal(map[string]any{"wanInterface": "eth0", "rules": []Rule{{Action: "zapret", Domains: []string{"example.test"}}}, "zapretArgs": []string{arg}})
		if _, err := ReadConfig(bytes.NewReader(raw)); err == nil {
			t.Fatal("accepted", arg)
		}
	}
}

func TestProductOutboundMarksPreserveOptionsAndRejectConflicts(t *testing.T) {
	for _, mark := range []uint32{0, 1, 255, 65535} {
		n := Node{StreamSettings: json.RawMessage(fmt.Sprintf(`{"network":"xhttp","sockopt":{"mark":%d,"tcpFastOpen":true},"xhttpSettings":{"extra":{"downloadSettings":{"sockopt":{"mark":%d}}}}}`, mark, mark))}
		got, err := markOutbound(n, BypassZapretMark)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(got.StreamSettings, []byte(`"tcpFastOpen":true`)) {
			t.Fatal("lost provider option")
		}
		if bytes.Count(got.StreamSettings, []byte(fmt.Sprintf(`"mark":%d`, BypassZapretMark|mark))) != 2 {
			t.Fatal("nested mark not preserved", string(got.StreamSettings))
		}
		if !bytes.Contains(n.StreamSettings, []byte(fmt.Sprintf(`"mark":%d`, mark))) {
			t.Fatal("input mutated")
		}
	}
	for _, value := range []string{"65536", "268435456", "536870912", "1073741824", "-1", "1.1", "4294967296", `"1"`} {
		n := Node{StreamSettings: json.RawMessage(`{"sockopt":{"mark":` + value + `}}`)}
		if _, err := markOutbound(n, 0); err == nil {
			t.Fatal("accepted conflicting socket mark", value)
		}
	}
}

func TestProductHeaderAndURLBoundary(t *testing.T) {
	for _, headers := range []map[string]string{{"Bad Name": "x"}, {"User-Agent": "bad\r\nInjected:1"}, {"Host": "other.test"}, {"Authorization": "bad\x00secret"}, {"Content-Length": "1"}, {"x": "bad\x7f"}} {
		raw, _ := json.Marshal(map[string]any{"sources": []Source{{Name: "p", URL: "https://example.test/sub", Headers: headers}}})
		if _, err := ReadConfig(bytes.NewReader(raw)); err == nil {
			t.Fatal("invalid headers accepted")
		}
	}
	for _, u := range []string{"http://example.test", "https://user:secret@example.test", "https://example.test/#secret", "https://example.test/\nsecret"} {
		raw, _ := json.Marshal(map[string]any{"sources": []Source{{Name: "p", URL: u}}})
		if _, err := ReadConfig(bytes.NewReader(raw)); err == nil {
			t.Fatal("invalid URL accepted")
		}
	}
}

func TestProductConcurrentGeoUpdatesPublishCompletePairs(t *testing.T) {
	fixture := geoFixture(t)
	site, err := os.ReadFile(filepath.Join(fixture, "current", "geosite.dat"))
	if err != nil {
		t.Fatal(err)
	}
	ip, err := os.ReadFile(filepath.Join(fixture, "current", "geoip.dat"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- InstallGeoFiles(root, site, ip) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := OpenGeoDatabase(root); err != nil {
		t.Fatal(err)
	}
	dirs, _ := filepath.Glob(filepath.Join(root, "generation.*"))
	if len(dirs) != 2 {
		t.Fatal("generation retention", len(dirs))
	}
}

func FuzzProductConfigBoundary(f *testing.F) {
	for _, raw := range []string{`{}`, `null`, `{"transparent":{"enabled":true,"interfaces":["br-lan"],"ipv6":true}}`, `{"rules":[{"action":"block","domains":["example.org"]}]}`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 8192 {
			return
		}
		c, err := ReadConfig(strings.NewReader(raw))
		if err == nil {
			data, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ReadConfig(bytes.NewReader(data)); err != nil {
				t.Fatalf("valid config cannot round-trip: %v", err)
			}
		}
	})
}

func FuzzProductSubscriptionBoundary(f *testing.F) {
	for _, raw := range []string{`[]`, `[{"protocol":"freedom","settings":{}}]`, `vless://id@proxy.test:443?security=tls&type=tcp#name`, `{"outbounds":[]}`, "not-base64"} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 8192 {
			return
		}
		nodes, err := ParseSubscription("fuzz", []byte(raw))
		if err != nil {
			return
		}
		for _, node := range nodes {
			if node.Tag == "" || !strings.HasPrefix(node.Tag, "fuzz/") {
				t.Fatal("unscoped outbound")
			}
			if _, err := json.Marshal(node); err != nil {
				t.Fatal(err)
			}
		}
	})
}
