package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestXICMPMissingIPsResolvedOnlyInGeneratedCopy(t *testing.T) {
	n := Node{Tag: "service/kcp", Protocol: "vless", Settings: json.RawMessage(`{"vnext":[{"address":"endpoint.test","port":1234,"users":[{"id":"private-id"}]}]}`), StreamSettings: json.RawMessage(`{"network":"kcp","kcpSettings":{"mtu":900},"finalmask":{"udp":[{"type":"xicmp","settings":{"dgram":true}}]}}`)}
	original := string(n.StreamSettings)
	resolved := resolveFinalMaskNode(n, func(host string) []string {
		if host != "endpoint.test" {
			t.Fatal(host)
		}
		return []string{"192.0.2.5"}
	})
	if !strings.Contains(string(resolved.StreamSettings), `"ips":["192.0.2.5"]`) || string(n.StreamSettings) != original || string(resolved.Settings) != string(n.Settings) {
		t.Fatal("resolver lost settings or mutated source")
	}
	preserved := resolveFinalMaskNode(resolved, func(string) []string { t.Fatal("explicit addresses re-resolved"); return nil })
	if string(preserved.StreamSettings) != string(resolved.StreamSettings) {
		t.Fatal("explicit IPs changed")
	}
	failed := resolveFinalMaskNode(n, func(string) []string { return nil })
	if string(failed.StreamSettings) != original {
		t.Fatal("DNS failure changed outbound")
	}
	ordinary := n
	ordinary.StreamSettings = json.RawMessage(`{"network":"kcp","kcpSettings":{"seed":"private-seed"}}`)
	result := resolveFinalMaskNode(ordinary, func(string) []string { t.Fatal("ordinary KCP was modified"); return nil })
	if string(result.StreamSettings) != string(ordinary.StreamSettings) {
		t.Fatal("plain KCP changed")
	}
}

func TestICMPDatagramRequirement(t *testing.T) {
	nodes := []Node{{StreamSettings: json.RawMessage(`{"network":"kcp","finalmask":{"udp":[{"type":"xicmp","settings":{"dgram":true}}]}}`)}}
	if !NeedsICMPDatagram(nodes) {
		t.Fatal("missing ICMP datagram requirement")
	}
	nodes[0].StreamSettings = json.RawMessage(`{"network":"kcp"}`)
	if NeedsICMPDatagram(nodes) {
		t.Fatal("plain KCP should not change socket permissions")
	}
}
