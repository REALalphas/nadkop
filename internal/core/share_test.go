package core

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestMixedShareLinks(t *testing.T) {
	extra := `{"host":"cdn.example","path":"/inside","mode":"packet-up","noGRPCHeader":true,"xPaddingBytes":"100-200"}`
	links := []string{
		"vless://00000000-0000-4000-8000-000000000001@vpn.example:443?type=xhttp&security=reality&pbk=public-key&mode=auto&path=%2Foutside&extra=" + url.QueryEscape(extra) + "#xhttp",
		"trojan://secret%3Apassword@vpn.example:443?type=ws&security=tls&sni=cdn.example&path=%2Fws&cs=TLS_AES_128_GCM_SHA256&pcs=certificate-hash#trojan",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:secret:pass")) + "@[2001:db8::1]:8388#ss",
	}
	nodes, err := ParseSubscription("provider", []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 || nodes[0].Protocol != "vless" || nodes[1].Protocol != "trojan" || nodes[2].Protocol != "shadowsocks" {
		t.Fatal("mixed subscription protocols were lost")
	}
	var stream struct {
		Network string `json:"network"`
		XHTTP   struct {
			Host  string `json:"host"`
			Path  string `json:"path"`
			Mode  string `json:"mode"`
			Extra struct {
				NoGRPCHeader bool `json:"noGRPCHeader"`
			} `json:"extra"`
		} `json:"xhttpSettings"`
	}
	if err := json.Unmarshal(nodes[0].StreamSettings, &stream); err != nil {
		t.Fatal(err)
	}
	if stream.Network != "xhttp" || stream.XHTTP.Host != "cdn.example" || stream.XHTTP.Path != "/outside" || stream.XHTTP.Mode != "auto" || !stream.XHTTP.Extra.NoGRPCHeader {
		t.Fatal("XHTTP extra or explicit query overrides were lost")
	}
	var tls struct {
		TLS struct {
			CipherSuites string `json:"cipherSuites"`
			Pin          string `json:"pinnedPeerCertSha256"`
		} `json:"tlsSettings"`
	}
	if err := json.Unmarshal(nodes[1].StreamSettings, &tls); err != nil {
		t.Fatal(err)
	}
	if tls.TLS.CipherSuites != "TLS_AES_128_GCM_SHA256" || tls.TLS.Pin != "certificate-hash" {
		t.Fatal("Trojan TLS verification settings were lost")
	}
	for i, protocol := range []string{"trojan", "shadowsocks"} {
		var settings struct {
			Servers []struct {
				Password string `json:"password"`
				Address  string `json:"address"`
			} `json:"servers"`
		}
		if err := json.Unmarshal(nodes[i+1].Settings, &settings); err != nil {
			t.Fatal(err)
		}
		want := "secret:password"
		if protocol == "shadowsocks" {
			want = "secret:pass"
		}
		if len(settings.Servers) != 1 || settings.Servers[0].Password != want {
			t.Fatal("credentials were changed")
		}
	}
}

func TestShadowsocksEncodings(t *testing.T) {
	for _, link := range []string{
		"ss://aes-128-gcm:secret%3Apass@vpn.example:8388#node",
		"ss://" + base64.RawStdEncoding.EncodeToString([]byte("aes-128-gcm:secret:pass@vpn.example:8388")) + "#node",
		"ss://" + base64.RawStdEncoding.EncodeToString([]byte("aes-128-gcm:pass@word@vpn.example:8388")) + "#node",
	} {
		nodes, err := ParseSubscription("provider", []byte(link))
		if err != nil || len(nodes) != 1 || nodes[0].Tag != "provider/node" {
			t.Fatalf("Shadowsocks encoding: %v", err)
		}
	}
}

func TestRejectBrokenShareOptions(t *testing.T) {
	for _, link := range []string{
		"vless://id@host:443?security=tls&type=xhttp&extra=null",
		"vless://id@host:443?security=tls&type=xhttp&extra=%5B%5D",
		"vless://id@host:443?security=tls&type=ws&extra=%7B%7D",
		"vless://id@host:443?security=tls&security=none",
		"trojan://secret@host:443?allowInsecure=1",
		"ss://broken@host:8388",
		"ss://aes-128-gcm:secret@host:8388?plugin=unsupported",
	} {
		if _, err := ParseSubscription("provider", []byte(link)); err == nil {
			t.Fatal("invalid or unsupported share options were accepted")
		}
	}
}
