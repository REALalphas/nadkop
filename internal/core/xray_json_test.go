package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const profileFixture = `[
 {"remarks":"First / profile","dns":{"servers":["provider.example"]},"inbounds":[{"port":9999}],"routing":{"rules":[]},"outbounds":[
  {"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"first.example","port":443,"users":[{"id":"00000000-0000-4000-8000-000000000001"}]}]},"streamSettings":{"network":"xhttp","sockopt":{"dialerProxy":"fragment"},"xhttpSettings":{"extra":{"downloadSettings":{"sockopt":{"dialerProxy":"fragment"}}}}},"mux":{"enabled":true,"concurrency":8},"sendThrough":"0.0.0.0","targetStrategy":"AsIs"},
  {"tag":"fragment","protocol":"freedom","settings":{"fragment":{"packets":"tlshello","length":"10-20","interval":"1-2"}}},
  {"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]},
 {"remarks":"Second profile","outbounds":[
  {"tag":"proxy","protocol":"trojan","settings":{"servers":[{"address":"second.example","port":443,"password":"test-password"}]},"proxySettings":{"tag":"fragment","transportLayer":true}},
  {"tag":"fragment","protocol":"freedom"},{"tag":"direct","protocol":"freedom"}]}]`

func TestLoadRequestsXrayJSONAndRejectsLinkFallback(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.UserAgent(), "Happ/") && r.Header.Get("Accept") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(profileFixture))
			return
		}
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte("vless://test@host.example:443?security=tls#link"))))
	}))
	defer server.Close()
	source := Source{Name: "provider", URL: server.URL}
	load := func() ([]Node, error) {
		return loadContext(context.Background(), source, server.Client().Transport, filepath.Join(t.TempDir(), "hwid"), DeviceInfo{})
	}
	nodes, err := load()
	if err != nil || len(nodes) != 4 || nodes[0].Tag != "provider/First _ profile" {
		t.Fatalf("JSON negotiation failed: %v", err)
	}
	// Manual headers retain priority, but must not silently switch JSON to links.
	source.Headers = map[string]string{"user-agent": "LegacyClient"}
	if _, err := load(); err == nil || !strings.Contains(err.Error(), "instead of Xray JSON") || strings.Contains(err.Error(), "host.example") {
		t.Fatalf("link fallback was accepted or exposed secrets: %v", err)
	}
	source.Format = "links"
	nodes, err = load()
	if err != nil || len(nodes) != 1 || nodes[0].Tag != "provider/link" {
		t.Fatalf("explicit legacy format failed: %v", err)
	}
}

func TestXrayProfilesPreserveOutboundGraph(t *testing.T) {
	for _, body := range []string{profileFixture, base64.StdEncoding.EncodeToString([]byte(profileFixture))} {
		nodes, err := ParseSubscription("provider", []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 4 {
			t.Fatalf("expected two proxies and their helpers, got %d", len(nodes))
		}
		artifacts, err := Compile(Config{Default: "direct", SOCKSPort: 1080}, nodes)
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Outbounds []map[string]any `json:"outbounds"`
			Inbounds  []map[string]any `json:"inbounds"`
			DNS       any              `json:"dns"`
		}
		if err := json.Unmarshal(artifacts.Xray, &config); err != nil {
			t.Fatal(err)
		}
		if len(config.Outbounds) != 6 || config.DNS != nil || len(config.Inbounds) != 1 || config.Inbounds[0]["port"] != float64(1080) {
			t.Fatal("provider listeners or unrelated helpers replaced router settings")
		}
		first, second := config.Outbounds[2], config.Outbounds[4]
		stream := first["streamSettings"].(map[string]any)
		if stream["sockopt"].(map[string]any)["dialerProxy"] != nodes[1].Tag || first["mux"].(map[string]any)["concurrency"] != float64(8) || first["sendThrough"] != "0.0.0.0" || first["targetStrategy"] != "AsIs" {
			t.Fatal("outbound options or fragmentation chain lost")
		}
		download := stream["xhttpSettings"].(map[string]any)["extra"].(map[string]any)["downloadSettings"].(map[string]any)
		if download["sockopt"].(map[string]any)["dialerProxy"] != nodes[1].Tag || second["proxySettings"].(map[string]any)["tag"] != nodes[3].Tag {
			t.Fatal("nested XHTTP or proxySettings reference lost")
		}
		local, err := UnprefixNodes("provider", nodes)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(local)
		reloaded, err := ParseSubscription("provider", encoded)
		if err != nil {
			t.Fatalf("local JSON round trip failed: %v", err)
		}
		roundTrip, _ := Compile(Config{Default: "direct", SOCKSPort: 1080}, reloaded)
		if string(roundTrip.Xray) != string(artifacts.Xray) {
			t.Fatal("saving and reloading changed outbound fields or references")
		}
	}
}

func TestXraySingleConfigAndRepeatedProfileNames(t *testing.T) {
	profile := `{"remarks":"Same","outbounds":[{"tag":"fragment","protocol":"freedom","streamSettings":{"sockopt":{"dialerProxy":"proxy"}}},{"tag":"proxy","protocol":"socks","settings":{}}]}`
	for _, input := range []string{profile, "[" + profile + "," + profile + "]"} {
		nodes, err := ParseSubscription("test", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		tags := map[string]bool{}
		for _, node := range nodes {
			if tags[node.Tag] {
				t.Fatal("repeated profile labels produced duplicate tags")
			}
			tags[node.Tag] = true
		}
		for i := 0; i < len(nodes); i += 2 {
			var stream struct{ Sockopt struct{ DialerProxy string } }
			if err := json.Unmarshal(nodes[i].StreamSettings, &stream); err != nil || stream.Sockopt.DialerProxy != nodes[i+1].Tag {
				t.Fatal("freedom -> proxy chain was not retained")
			}
		}
	}
}

func TestRejectInvalidXrayProfiles(t *testing.T) {
	for _, input := range []string{
		`{"outbounds":[]}`, `[{"outbounds":[]},{"protocol":"socks","settings":{}}]`,
		`[{"outbounds":[{"protocol":"socks","settings":{}}]},{"protocol":"socks","settings":{}}]`,
		`{"outbounds":[{"tag":"a","protocol":"socks","settings":{},"streamSettings":{"sockopt":{"dialerProxy":"missing-secret-tag"}}}]}`,
		`{"outbounds":[{"tag":"a","protocol":"socks","settings":{}},{"tag":"a","protocol":"socks","settings":{}}]}`,
		`{"outbounds":[{"tag":"a","protocol":"socks","settings":{}}]} {}`,
		`[null]`,
	} {
		if _, err := ParseSubscription("test", []byte(input)); err == nil || strings.Contains(err.Error(), "missing-secret-tag") {
			t.Fatalf("bad profile accepted or secret exposed: %v", err)
		}
	}
	if _, err := ReadConfig(strings.NewReader(`{"sources":[{"name":"test","url":"https://example.test","format":"typo"}]}`)); err == nil {
		t.Fatal("invalid format accepted")
	}
}
