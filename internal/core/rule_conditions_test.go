package core

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDormantRuleFieldsRoundTripAndNeverCompile(t *testing.T) {
	raw := `{"rules":[{"conditions":{"domains":true,"ips":false,"sets":false},"domains":["active.test"],"ips":["not-an-ip"],"sets":["geosite:missing"],"action":"block","nodeRef":"srv_11111111111111111111111111111111","selection":{"mode":"url","url":"https://probe.test/"}}]}`
	c, err := ReadConfig(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if c.Rules[0].IPs[0] != "not-an-ip" || c.Rules[0].Sets[0] != "geosite:missing" || c.Rules[0].NodeRef == "" || c.Rules[0].Selection == nil {
		t.Fatal("dormant data erased", c.Rules[0])
	}
	if err = ValidateRuleSets(c, t.TempDir()); err != nil {
		t.Fatal("inactive dataset was validated", err)
	}
	a, err := Compile(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"not-an-ip", "geosite:missing", "srv_1111", "probe.test", "__nadkop:probe"} {
		if bytes.Contains(a.Xray, []byte(value)) {
			t.Fatal("dormant data compiled", value)
		}
	}
	encoded, _ := json.Marshal(c)
	restored, err := ReadConfig(bytes.NewReader(encoded))
	if err != nil || restored.Rules[0].IPs[0] != "not-an-ip" {
		t.Fatal("roundtrip lost data", err)
	}
	restored.Rules[0].Conditions.IPs = true
	encoded, _ = json.Marshal(restored)
	if _, err = ReadConfig(bytes.NewReader(encoded)); err == nil {
		t.Fatal("reactivated invalid IP accepted")
	}
	c.Rules[0].Conditions.Domains = false
	encoded, _ = json.Marshal(c)
	if _, err = ReadConfig(bytes.NewReader(encoded)); err == nil {
		t.Fatal("all conditions disabled accepted")
	}
}

func TestDormantVPNAndURLPreservedButIgnored(t *testing.T) {
	id := "srv_" + strings.Repeat("1", 32)
	other := "srv_" + strings.Repeat("2", 32)
	c, err := ReadConfig(strings.NewReader(`{"rules":[{"domains":["example.test"],"action":"direct","nodeRefs":["` + id + `","` + other + `"],"selection":{"mode":"url","url":"https://custom.test/check"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Rules[0].NodeRefs) != 2 || c.Rules[0].Selection.URL != "https://custom.test/check" {
		t.Fatal("inactive VPN erased")
	}
	a, err := Compile(c, nil)
	if err != nil || len(a.Health) > 0 {
		t.Fatal("inactive VPN started monitoring", err)
	}
	c.Rules[0].Action = "proxy"
	c.Rules[0].Selection.Mode = "ping"
	encoded, _ := json.Marshal(c)
	c, err = ReadConfig(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if c.Rules[0].Selection.URL != "https://custom.test/check" || c.Rules[0].Active().Selection.URL != "" {
		t.Fatal("ping erased or used dormant URL")
	}
}

func TestDisabledZapretSetsDoNotBlockSaveOrDatabaseUpdate(t *testing.T) {
	c := Config{WANInterface: "eth0", ZapretArgs: []string{"--filter-tcp=80,443", "--dpi-desync=fake"}, Rules: []Rule{{Conditions: &RuleConditions{Domains: true}, Domains: []string{"example.test"}, Sets: []string{"geosite:regex"}, Action: "zapret"}}}
	path := geoFixture(t)
	if err := ValidateRuleSets(c, path); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileWithGeo(c, nil, path); err != nil {
		t.Fatal("inactive incompatible set used", err)
	}
	c.Rules[0].Conditions.Sets = true
	if err := ValidateRuleSets(c, path); err == nil {
		t.Fatal("active incompatible set accepted")
	}
}

func TestDormantVPNReferencesSurviveCataloguePruning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	nodes, err := ParseSubscription("lab", []byte(`[{"tag":"vpn","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080}]}}]`))
	if err != nil {
		t.Fatal(err)
	}
	summary, err := SyncServers(path, nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Rules: []Rule{{Action: "direct", Domains: []string{"example.test"}, NodeRef: summary[0].ID}}}
	if _, err = SyncServers(path, nil, config); err != nil {
		t.Fatal(err)
	}
	kept, err := ServerSummaries(path)
	if err != nil || len(kept) != 1 || kept[0].ID != summary[0].ID {
		t.Fatal("inactive VPN reference discarded", kept, err)
	}
}

func TestInactiveZapretStrategyIsRetainedNotUsed(t *testing.T) {
	c, err := ReadConfig(strings.NewReader(`{"zapretArgs":["unfinished strategy"],"rules":[{"action":"direct","domains":["example.test"]}]}`))
	if err != nil || c.ZapretArgs[0] != "unfinished strategy" {
		t.Fatal(c, err)
	}
	c.Rules[0].Action = "zapret"
	c.WANInterface = "eth0"
	data, _ := json.Marshal(c)
	if _, err = ReadConfig(bytes.NewReader(data)); err == nil {
		t.Fatal("active invalid Zapret strategy accepted")
	}
}
