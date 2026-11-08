package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type ProbeSetup struct {
	Xray     []byte
	Manifest HealthManifest
	Members  []HealthMember
}

// CompileProbe isolates draft checks from the running service and its listeners.
func CompileProbe(nodes []Node, refs []string, port int) (ProbeSetup, error) {
	// An unrelated broken outbound must not prevent checks of selected servers.
	// Keep the full dependency chain, including nested XHTTP dialerProxy links.
	selected, err := probeNodes(nodes, refs)
	if err != nil {
		return ProbeSetup{}, err
	}
	nodes = selected
	a, err := Compile(Config{Default: "direct", SOCKSPort: port}, nodes)
	if err != nil {
		return ProbeSetup{}, err
	}
	var config map[string]any
	if err = json.Unmarshal(a.Xray, &config); err != nil {
		return ProbeSetup{}, err
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return ProbeSetup{}, err
	}
	setup := ProbeSetup{Manifest: HealthManifest{SOCKS: fmt.Sprintf("127.0.0.1:%d", port), Password: hex.EncodeToString(token[:])}}
	accounts := []any{}
	rules := []any{}
	for _, ref := range refs {
		member := HealthMember{ID: ref}
		for _, node := range nodes {
			if node.serverID == ref {
				member.Tag = node.Tag
				break
			}
		}
		setup.Members = append(setup.Members, member)
		accounts = append(accounts, map[string]any{"user": ref, "pass": setup.Manifest.Password})
		if member.Tag != "" {
			rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{"probe"}, "user": []string{ref}, "outboundTag": member.Tag})
		}
	}
	rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{"probe"}, "outboundTag": "block"})
	config["inbounds"] = []any{map[string]any{"tag": "probe", "listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "password", "accounts": accounts, "udp": false}}}
	config["routing"] = map[string]any{"domainStrategy": "AsIs", "rules": rules}
	setup.Xray, err = json.Marshal(config)
	return setup, err
}

func probeNodes(nodes []Node, refs []string) ([]Node, error) {
	wanted := map[string]bool{}
	for _, ref := range refs {
		wanted[ref] = true
	}
	byTag := map[string]Node{}
	pending := []string{}
	for _, node := range nodes {
		byTag[node.Tag] = node
		if wanted[node.serverID] {
			pending = append(pending, node.Tag)
		}
	}
	included := map[string]bool{}
	for len(pending) > 0 {
		tag := pending[0]
		pending = pending[1:]
		if included[tag] {
			continue
		}
		node, ok := byTag[tag]
		if !ok {
			return nil, fmt.Errorf("probe outbound dependency is missing")
		}
		included[tag] = true
		if _, err := transformOutbound(node, func(dependency string) (string, error) {
			pending = append(pending, dependency)
			return dependency, nil
		}); err != nil {
			return nil, err
		}
	}
	result := []Node{}
	for _, node := range nodes {
		if included[node.Tag] {
			result = append(result, node)
		}
	}
	return result, nil
}
