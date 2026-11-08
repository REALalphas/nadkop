package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Xray's xICMP finalmask requires literal IPs: unlike ordinary KCP it does not
// resolve a destination hostname. Fill only absent xICMP IP lists in generated
// outbounds. Subscription payload, credentials, explicit IPs and catalogue IDs
// remain unchanged. DNS failure leaves the original outbound usable on retry.
func resolveFinalMaskNodes(nodes []Node) []Node {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cache := map[string][]string{}
	lookup := func(host string) []string {
		if ips, ok := cache[host]; ok {
			return ips
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			cache[host] = nil
			return nil
		}
		var v4, v6 []string
		for _, addr := range addresses {
			if addr.IP.To4() != nil {
				v4 = append(v4, addr.IP.String())
			} else if addr.Zone == "" {
				v6 = append(v6, addr.IP.String())
			}
		}
		ips := v4
		if len(ips) == 0 {
			ips = v6
		}
		cache[host] = uniqueStrings(ips)
		return cache[host]
	}
	result := make([]Node, len(nodes))
	for i, n := range nodes {
		result[i] = resolveFinalMaskNode(n, lookup)
	}
	return result
}
func resolveFinalMaskNode(n Node, lookup func(string) []string) Node {
	if !strings.Contains(string(n.StreamSettings), "xicmp") {
		return n
	}
	var stream map[string]any
	if json.Unmarshal(n.StreamSettings, &stream) != nil {
		return n
	}
	maskConfig, ok := stream["finalmask"].(map[string]any)
	if !ok {
		return n
	}
	masks, ok := maskConfig["udp"].([]any)
	if !ok {
		return n
	}
	var settings struct {
		VNext   []struct{ Address string }
		Servers []struct{ Address string }
	}
	if json.Unmarshal(n.Settings, &settings) != nil {
		return n
	}
	host := ""
	if len(settings.VNext) > 0 {
		host = settings.VNext[0].Address
	} else if len(settings.Servers) > 0 {
		host = settings.Servers[0].Address
	}
	if host == "" || net.ParseIP(host) != nil {
		return n
	}
	changed := false
	for _, raw := range masks {
		mask, ok := raw.(map[string]any)
		if !ok || mask["type"] != "xicmp" {
			continue
		}
		opts, _ := mask["settings"].(map[string]any)
		if opts == nil {
			opts = map[string]any{}
		}
		if ips, ok := opts["ips"].([]any); ok && len(ips) > 0 {
			continue
		}
		addresses := lookup(host)
		if len(addresses) == 0 {
			continue
		}
		opts["ips"] = addresses
		mask["settings"] = opts
		changed = true
	}
	if changed {
		n.StreamSettings, _ = json.Marshal(stream)
	}
	return n
}

func NeedsICMPDatagram(nodes []Node) bool {
	for _, node := range nodes {
		var stream struct {
			FinalMask struct {
				UDP []struct {
					Type     string
					Settings struct{ Dgram bool }
				}
			}
		}
		if json.Unmarshal(node.StreamSettings, &stream) != nil {
			continue
		}
		for _, mask := range stream.FinalMask.UDP {
			if mask.Type == "xicmp" && mask.Settings.Dgram {
				return true
			}
		}
	}
	return false
}

// ping_group_range controls unprivileged ICMP datagram sockets even for a
// process running as root. Include GID 0 while preserving the upper bound.
func PrepareICMPDatagram(nodes []Node) error {
	if !NeedsICMPDatagram(nodes) {
		return nil
	}
	const path = "/proc/sys/net/ipv4/ping_group_range"
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to check ICMP socket permission")
	}
	var low, high uint64
	if _, err = fmt.Sscan(string(data), &low, &high); err != nil {
		return fmt.Errorf("failed to read ping_group_range")
	}
	if low == 0 {
		return nil
	}
	if err = os.WriteFile(path, []byte(fmt.Sprintf("0 %d\n", high)), 0600); err != nil {
		return fmt.Errorf("failed to allow ICMP sockets for KCP/xICMP")
	}
	return nil
}
