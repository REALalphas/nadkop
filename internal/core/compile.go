package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Artifacts struct {
	Xray    []byte
	Hosts   []byte
	Zapret  []byte
	NFT     []byte
	ICMP    []byte
	IPs     []byte
	Health  []byte
	Input   []byte
	Runtime []byte
}

func (a Artifacts) Files() map[string][]byte {
	var restore []byte
	if len(a.NFT) > 0 {
		restore = append([]byte("add table inet nadkop\nflush table inet nadkop\n"), a.NFT...)
	}
	return map[string][]byte{"xray.json": a.Xray, "zapret-hosts.txt": a.Hosts, "zapret-ips.txt": a.IPs, "icmp-dgram": a.ICMP, "nfqws-args.json": a.Zapret, "rules.nft": a.NFT, "health.json": a.Health, "input.nft": a.Input, "runtime.json": a.Runtime, "restore.nft": restore}
}

func Compile(c Config, nodes []Node) (Artifacts, error) {
	return CompileWithGeo(c, nodes, GeoDataPath)
}
func CompileWithGeo(c Config, nodes []Node, geoPath string) (Artifacts, error) {
	c = c.Active()
	if c.Default == "" {
		c.Default = "direct"
	}
	if c.SOCKSPort == 0 {
		c.SOCKSPort = 1080
	}
	if err := c.normalizeNetwork(); err != nil {
		return Artifacts{}, err
	}
	hasZapret := false
	for _, r := range c.Rules {
		hasZapret = hasZapret || r.Action == "zapret"
	}
	nodes = resolveFinalMaskNodes(nodes)
	var db *GeoDatabase
	for _, r := range c.Rules {
		if len(r.Sets) > 0 {
			var err error
			db, err = OpenGeoDatabase(geoPath)
			if err != nil {
				return Artifacts{}, err
			}
			break
		}
	}

	references := map[string]string{}
	aliases := map[string]string{}
	for _, n := range nodes {
		if _, _, separator := serverDetails(n); separator {
			continue
		}
		if n.serverID != "" {
			references[n.serverID] = n.Tag
		}
		for _, alias := range n.serverAliases {
			if previous, ok := aliases[alias]; ok && previous != n.Tag {
				aliases[alias] = ""
			} else if !ok {
				aliases[alias] = n.Tag
			}
		}
	}
	tags := map[string]bool{"direct": true, "block": true}
	out := []any{map[string]any{"tag": c.Default, "protocol": map[string]string{"direct": "freedom", "block": "blackhole"}[c.Default]}}
	other := "block"
	if c.Default == "block" {
		other = "direct"
	}
	out = append(out, map[string]any{"tag": other, "protocol": map[string]string{"direct": "freedom", "block": "blackhole"}[other]})
	if hasZapret {
		for _, raw := range out {
			ob := raw.(map[string]any)
			if ob["protocol"] == "freedom" {
				ob["streamSettings"] = map[string]any{"sockopt": map[string]any{"mark": BypassZapretMark}}
			}
		}
		out = append(out, map[string]any{"tag": "__nadkop:zapret", "protocol": "freedom", "streamSettings": map[string]any{"sockopt": map[string]any{"mark": ZapretMark}}})
		tags["__nadkop:zapret"] = true
	}
	for _, n := range nodes {
		if _, _, separator := serverDetails(n); separator {
			continue
		}
		if tags[n.Tag] {
			return Artifacts{}, fmt.Errorf("duplicate outbound tag")
		}
		tags[n.Tag] = true
		n.Location = nil // Subscription display metadata is not an Xray outbound option.
		if hasZapret || c.Transparent.Enabled {
			var err error
			mark := uint32(0)
			if hasZapret {
				mark = BypassZapretMark
			}
			n, err = markOutbound(n, mark)
			if err != nil {
				return Artifacts{}, err
			}
		}
		out = append(out, n)
	}
	rules := []any{}
	balancers := []any{}
	inbounds := []any{map[string]any{"tag": "socks", "listen": "127.0.0.1", "port": c.SOCKSPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}, "sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}}}
	monitor := HealthManifest{API: fmt.Sprintf("127.0.0.1:%d", HealthAPIPort), SOCKS: fmt.Sprintf("127.0.0.1:%d", HealthSOCKSPort)}
	probeRefs := map[string]bool{}
	for _, r := range c.Rules {
		if len(r.NodeRefs) > 1 {
			for _, ref := range r.NodeRefs {
				probeRefs[ref] = true
			}
		}
	}
	if len(probeRefs) > 0 {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return Artifacts{}, fmt.Errorf("cannot create monitoring credentials")
		}
		monitor.Password = hex.EncodeToString(token[:])
		out = append(out, map[string]any{"tag": "__nadkop:block", "protocol": "blackhole"})
		accounts := []any{}
		refKeys := []string{}
		for ref := range probeRefs {
			refKeys = append(refKeys, ref)
		}
		sort.Strings(refKeys)
		for _, ref := range refKeys {
			accounts = append(accounts, map[string]any{"user": ref, "pass": monitor.Password})
			if tag := references[ref]; tag != "" {
				rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{"__nadkop:probe"}, "user": []string{ref}, "outboundTag": tag})
			}
		}
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{"__nadkop:probe"}, "outboundTag": "block"})
		inbounds = append(inbounds, map[string]any{"tag": "__nadkop:probe", "listen": "127.0.0.1", "port": HealthSOCKSPort, "protocol": "socks", "settings": map[string]any{"auth": "password", "accounts": accounts, "udp": false}})
	}
	hosts := []string{}
	zapretIPs := []string{}
	hasIPRules := false
	addMatch := func(domains, ips []string, key, target string) {
		if len(domains) > 0 {
			rules = append(rules, map[string]any{"type": "field", "domain": domains, key: target})
		}
		if len(ips) > 0 {
			hasIPRules = true
			rules = append(rules, map[string]any{"type": "field", "ip": ips, key: target})
		}
	}

	expand := compileExpander(db, c.Rules)
	for ruleIndex, r := range c.Rules {
		domains, ips, err := ruleMatchesWith(r, expand)
		if err != nil {
			return Artifacts{}, err
		}
		tag := r.Action
		if tag == "proxy" && len(r.NodeRefs) > 1 {
			group := HealthGroup{Tag: fmt.Sprintf("__nadkop:rule-%d", ruleIndex), Mode: "priority", URL: DefaultProbeURL}
			if r.Selection != nil {
				group.Mode = r.Selection.Mode
				if group.Mode == "url" && r.Selection.URL != "" {
					group.URL = r.Selection.URL
				}
			}
			for _, ref := range r.NodeRefs {
				group.Members = append(group.Members, HealthMember{ID: ref, Tag: references[ref]})
			}
			monitor.Groups = append(monitor.Groups, group)
			balancers = append(balancers, map[string]any{"tag": group.Tag, "selector": []string{"__nadkop:block"}, "strategy": map[string]any{"type": "random"}})
			addMatch(domains, ips, "balancerTag", group.Tag)
			continue
		}
		if tag == "proxy" {
			tag = r.Node
			if r.NodeRef != "" {
				var found bool
				tag, found = references[r.NodeRef]
				if !found {
					return Artifacts{}, fmt.Errorf("selected server is missing from the subscription; its reference is retained")
				}
			} else if !tags[tag] && aliases[tag] != "" {
				tag = aliases[tag]
			}
		}
		if tag == "zapret" {
			tag = "__nadkop:zapret"
			for _, d := range domains {
				switch {
				case strings.HasPrefix(d, "domain:"):
					hosts = append(hosts, strings.TrimPrefix(d, "domain:"))
				case strings.HasPrefix(d, "full:"):
					hosts = append(hosts, "^"+strings.TrimPrefix(d, "full:"))
				default:
					return Artifacts{}, fmt.Errorf("Rule %s: Zapret does not support regexp/keyword in the selected geosite; select Proxy/Direct/Block or a set without regexp", r.Name)
				}
			}
			zapretIPs = append(zapretIPs, ips...)
		}
		if !tags[tag] {
			return Artifacts{}, fmt.Errorf("unknown node %q", tag)
		}
		addMatch(domains, ips, "outboundTag", tag)
	}
	routing := map[string]any{"domainStrategy": "AsIs", "rules": rules}
	if hasIPRules {
		routing["domainStrategy"] = "IPOnDemand"
	}
	config := map[string]any{"log": map[string]string{"loglevel": "warning"}, "inbounds": inbounds, "outbounds": out, "routing": routing}
	addTransparent(config, c)
	var health []byte
	if len(monitor.Groups) > 0 {
		routing["balancers"] = balancers
		config["api"] = map[string]any{"tag": "__nadkop:api", "listen": monitor.API, "services": []string{"RoutingService"}}
		health, _ = json.Marshal(monitor)
	}
	x, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Artifacts{}, err
	}
	hosts = uniqueStrings(hosts)
	zapretIPs = uniqueStrings(zapretIPs)
	sort.Strings(hosts)
	sort.Strings(zapretIPs)
	hostText := ""
	if len(hosts) > 0 {
		hostText = strings.Join(hosts, "\n") + "\n"
	}
	args := []string{"--qnum=200", "--uid=2147483647:2147483647", "--dpi-desync-fwmark=0x40000000"}
	if len(hosts) > 0 {
		args = append(args, "--hostlist=/var/run/nadkop/current/zapret-hosts.txt")
		args = append(args, c.ZapretArgs...)
	}
	if len(zapretIPs) > 0 {
		if len(hosts) > 0 {
			args = append(args, "--new")
		}
		args = append(args, "--ipset=/var/run/nadkop/current/zapret-ips.txt")
		args = append(args, c.ZapretArgs...)
	}
	ipText := ""
	if len(zapretIPs) > 0 {
		ipText = strings.Join(zapretIPs, "\n") + "\n"
	}
	z, _ := json.MarshalIndent(args, "", "  ")
	zapret := len(hosts)+len(zapretIPs) > 0
	nft, input := networkNFT(c, zapret)
	runtime, err := json.Marshal(RuntimeSettings{Transparent: c.Transparent, SOCKSPort: c.SOCKSPort, Health: len(monitor.Groups) > 0, Zapret: zapret, WANInterface: c.WANInterface, UpdateIntervalHours: c.UpdateIntervalHours})
	if err != nil {
		return Artifacts{}, err
	}
	icmp := []byte{}
	if NeedsICMPDatagram(nodes) {
		icmp = []byte("1\n")
	}
	return Artifacts{ICMP: icmp, Xray: append(x, '\n'), Hosts: []byte(hostText), Zapret: append(z, '\n'), NFT: []byte(nft), Health: health, IPs: []byte(ipText), Input: []byte(input), Runtime: append(runtime, '\n')}, nil
}
