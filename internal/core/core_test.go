package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVLESSFormats(t *testing.T) {
	uri := "vless://00000000-0000-4000-8000-000000000001@vpn.example:443?security=reality&pbk=test&type=tcp&sni=example.com#test"
	for _, input := range []string{uri, base64.StdEncoding.EncodeToString([]byte(uri)), base64.RawURLEncoding.EncodeToString([]byte(uri))} {
		n, err := ParseSubscription("provider", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if len(n) != 1 || n[0].Tag != "provider/test" {
			t.Fatalf("wrong nodes: %+v", n)
		}
		var stream map[string]any
		if err := json.Unmarshal(n[0].StreamSettings, &stream); err != nil {
			t.Fatal(err)
		}
		if stream["network"] != "raw" || stream["security"] != "reality" {
			t.Fatalf("wrong transport: %v", stream)
		}
	}
}

func TestRejectUnsupportedSubscription(t *testing.T) {
	for _, s := range []string{"", "happ://crypto/test", "vless://id@host:443?security=tls&type=unknown", "vless://id@host:443?security=tls&allowInsecure=1", `[{"tag":"a","protocol":"unsupported","settings":{}}]`, `[{"tag":"a","protocol":"vless","settings":{}},{"tag":"a","protocol":"vless","settings":{}}]`} {
		if _, err := ParseSubscription("test", []byte(s)); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}

func TestConfigRejectsAmbiguousRules(t *testing.T) {
	for _, s := range []string{
		`{"rules":[{"domains":["example.org"],"action":"proxy"}]}`,
		`{"rules":[{"domains":["example.org"],"action":"zapret"},{"domains":["a.example.org"],"action":"direct"}]}`,
		`{"rules":[{"domains":["bad\n.example"],"action":"zapret"}]}`,
		`{"sources":[{"name":"a","url":"https://a"},{"name":"a","url":"https://b"}]}`,
		`{"wanInterface":"eth0","rules":[{"domains":["dpi.test"],"action":"zapret"}],"zapretArgs":["--hostlist=/tmp/foreign"]}`,
		`{} {}`,
		`{"typo":true}`,
	} {
		if _, err := ReadConfig(strings.NewReader(s)); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}

func TestCompileSplitAndDefault(t *testing.T) {
	c, err := ReadConfig(strings.NewReader(`{"default":"block","wanInterface":"pppoe-wan","zapretArgs":["--dpi-desync=fake"],"rules":[{"domains":["dpi.example"],"action":"zapret"},{"domains":["proxy.example"],"action":"proxy","node":"test/exit"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	n, err := ParseSubscription("test", []byte(`[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"localhost","port":2080}]}}]`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Compile(c, n)
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Hosts) != "dpi.example\n" {
		t.Fatalf("wrong hostlist: %s", a.Hosts)
	}
	if !strings.Contains(string(a.NFT), `oifname "pppoe-wan"`) || !strings.Contains(string(a.NFT), "queue num 200 bypass") {
		t.Fatalf("wrong nft rules: %s", a.NFT)
	}
	var x struct {
		Outbounds []struct{ Tag string }
		Routing   struct {
			Rules []struct{ OutboundTag string }
		}
	}
	if err := json.Unmarshal(a.Xray, &x); err != nil {
		t.Fatal(err)
	}
	if x.Outbounds[0].Tag != "block" || x.Routing.Rules[0].OutboundTag != "__nadkop:zapret" || x.Routing.Rules[1].OutboundTag != "test/exit" {
		t.Fatalf("wrong routing: %s", a.Xray)
	}
	c.Rules[1].Node = "missing"
	if _, err := Compile(c, n); err == nil {
		t.Fatal("accepted missing node")
	}
}

func TestLoadLocalAndLimits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nodes")
	if err := os.WriteFile(p, []byte(`[{"tag":"exit","protocol":"socks","settings":{}}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(Source{Name: "local", File: p}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, maxSubscription+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(Source{Name: "local", File: p}); err == nil {
		t.Fatal("accepted oversized source")
	}
	if _, err := Load(Source{Name: "remote", URL: "http://example.org/token"}); err == nil || strings.Contains(err.Error(), "token") {
		t.Fatal("HTTP accepted or secret leaked")
	}
}

func TestRuleNamesPersistWithoutChangingRouting(t *testing.T) {
	c, err := ReadConfig(strings.NewReader(`{"sources":[],"rules":[{"name":"  Video and work  ","domains":["test.example"],"action":"direct"}]}`))
	if err != nil || c.Rules[0].Name != "Video and work" {
		t.Fatalf("rule name: %+v %v", c, err)
	}
	named, err := Compile(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(c)
	restored, err := ReadConfig(bytes.NewReader(encoded))
	if err != nil || restored.Rules[0].Name != c.Rules[0].Name {
		t.Fatal("name did not survive reload")
	}
	c.Rules[0].Name = ""
	unnamed, err := Compile(c, nil)
	if err != nil || !bytes.Equal(named.Xray, unnamed.Xray) {
		t.Fatal("display name changed Xray")
	}
	for _, name := range []string{strings.Repeat("é", 81), "bad\nname"} {
		c.Rules[0].Name = name
		data, _ := json.Marshal(c)
		if _, err := ReadConfig(bytes.NewReader(data)); err == nil {
			t.Fatal("invalid rule name accepted")
		}
	}
}
