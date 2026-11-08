package core

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

const TransparentMark = 0x00010000
const ZapretMark = 0x10000000
const BypassZapretMark = 0x20000000
const RoutingTable = 100
const RoutingPriority = 10000

type TransparentSettings struct {
	Enabled     bool     `json:"enabled"`
	Interfaces  []string `json:"interfaces,omitempty"`
	Port        int      `json:"port,omitempty"`
	DNSPort     int      `json:"dnsPort,omitempty"`
	DNSUpstream string   `json:"dnsUpstream,omitempty"`
	IPv6        bool     `json:"ipv6"`
	Bypass      []string `json:"bypass,omitempty"`
}

type RuntimeSettings struct {
	Transparent         TransparentSettings `json:"transparent"`
	SOCKSPort           int                 `json:"socksPort"`
	Health              bool                `json:"health"`
	Zapret              bool                `json:"zapret"`
	WANInterface        string              `json:"wanInterface,omitempty"`
	UpdateIntervalHours int                 `json:"updateIntervalHours"`
}

var interfacePattern = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,15}$`)

func (c *Config) normalizeNetwork() error {
	t := &c.Transparent
	if !t.Enabled {
		return nil
	}
	if len(t.Interfaces) == 0 || len(t.Interfaces) > 16 {
		return fmt.Errorf("transparent routing requires 1–16 LAN device names")
	}
	seen := map[string]bool{}
	for _, name := range t.Interfaces {
		if !interfacePattern.MatchString(name) || name == "lo" || name == c.WANInterface || seen[name] {
			return fmt.Errorf("invalid, duplicate or WAN transparent interface %q", name)
		}
		seen[name] = true
	}
	if t.Port == 0 {
		t.Port = 12345
	}
	if t.DNSPort == 0 {
		t.DNSPort = 1053
	}
	ports := map[int]bool{c.SOCKSPort: true, HealthAPIPort: true, HealthSOCKSPort: true, 53: true}
	for _, port := range []int{t.Port, t.DNSPort} {
		if port < 1024 || port > 65535 || ports[port] {
			return fmt.Errorf("transparent/DNS port is invalid or conflicts with another listener")
		}
		ports[port] = true
	}
	if t.DNSUpstream == "" {
		t.DNSUpstream = "127.0.0.1"
	}
	addr, err := netip.ParseAddr(t.DNSUpstream)
	if err != nil || addr.Zone() != "" || addr.Is4In6() || addr.IsUnspecified() || addr.IsMulticast() || (!t.IPv6 && addr.Is6()) {
		return fmt.Errorf("dnsUpstream must be a literal, reachable DNS server IP")
	}
	if netip.MustParsePrefix("198.18.0.0/15").Contains(addr) || netip.MustParsePrefix("fc00::/18").Contains(addr) {
		return fmt.Errorf("dnsUpstream overlaps the FakeDNS address pool")
	}
	if len(t.Bypass) > 256 {
		return fmt.Errorf("at most 256 bypass prefixes are supported")
	}
	for i, value := range t.Bypass {
		value, err = normalizeIP(strings.TrimSpace(value))
		if err != nil {
			return err
		}
		t.Bypass[i] = value
	}
	t.Bypass = uniqueStrings(t.Bypass)
	return nil
}

// FakeDNS is used only for intercepted client queries. Xray's ordinary IP
// lookups skip FakeDNS, so earlier CIDR rules see real addresses (IPOnDemand).
func addTransparent(config map[string]any, c Config) {
	if !c.Transparent.Enabled {
		return
	}
	t := c.Transparent
	listen := "0.0.0.0"
	if t.IPv6 {
		listen = "::"
	}
	inbounds := config["inbounds"].([]any)
	transparent := map[string]any{"tag": "__nadkop:tproxy", "listen": listen, "port": t.Port, "protocol": "dokodemo-door",
		"settings":       map[string]any{"network": "tcp,udp", "followRedirect": true},
		"streamSettings": map[string]any{"sockopt": map[string]any{"tproxy": "tproxy"}},
		"sniffing":       map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic", "fakedns"}, "routeOnly": false}}
	dns := map[string]any{"tag": "__nadkop:dns-in", "listen": listen, "port": t.DNSPort, "protocol": "dokodemo-door",
		"settings":       map[string]any{"address": t.DNSUpstream, "port": 53, "network": "tcp,udp", "followRedirect": true},
		"streamSettings": map[string]any{"sockopt": map[string]any{"tproxy": "tproxy"}}}
	config["inbounds"] = append(inbounds, transparent, dns)
	config["outbounds"] = append(config["outbounds"].([]any), map[string]any{"tag": "__nadkop:dns", "protocol": "dns", "settings": map[string]any{"address": t.DNSUpstream, "port": 53, "nonIPQuery": "skip"}})
	routing := config["routing"].(map[string]any)
	routing["rules"] = append([]any{
		map[string]any{"type": "field", "inboundTag": []string{"__nadkop:dns-in"}, "outboundTag": "__nadkop:dns"},
		map[string]any{"type": "field", "inboundTag": []string{"__nadkop:dns-upstream"}, "outboundTag": "direct"},
	}, routing["rules"].([]any)...)
	pools := []any{map[string]any{"ipPool": "198.18.0.0/15", "poolSize": 65535}}
	strategy := "UseIPv4"
	if t.IPv6 {
		pools = append(pools, map[string]any{"ipPool": "fc00::/18", "poolSize": 65535})
		strategy = "UseIP"
	}
	config["fakedns"] = pools
	config["dns"] = map[string]any{"tag": "__nadkop:dns-upstream", "queryStrategy": strategy, "servers": []any{
		map[string]any{"address": t.DNSUpstream, "domains": []string{"domain:lan", "domain:local", "domain:home.arpa"}, "skipFallback": true, "finalQuery": true},
		"fakedns", t.DNSUpstream,
	}}
}

func networkNFT(c Config, zapret bool) (string, string) {
	if !c.Transparent.Enabled && !zapret {
		return "", ""
	}
	var rules strings.Builder
	rules.WriteString("table inet nadkop {\n")
	input := ""
	if c.Transparent.Enabled {
		t := c.Transparent
		quoted := make([]string, len(t.Interfaces))
		for i, name := range t.Interfaces {
			quoted[i] = fmt.Sprintf("%q", name)
		}
		interfaces := "{ " + strings.Join(quoted, ", ") + " }"
		fmt.Fprintf(&rules, " chain prerouting {\n  type filter hook prerouting priority mangle; policy accept;\n  iifname != %s return\n", interfaces)
		if !t.IPv6 {
			rules.WriteString("  meta nfproto ipv6 meta l4proto { tcp, udp } drop\n")
		}
		// DNS precedes the local-address bypass: clients may use any resolver,
		// including the router. DHCP, SSH, LuCI, local services and multicast stay local.
		fmt.Fprintf(&rules, "  meta l4proto { tcp, udp } th dport 53 tproxy to :%d meta mark set meta mark | 0x10000 accept\n", t.DNSPort)
		rules.WriteString("  fib daddr type local return\n")
		fmt.Fprintf(&rules, "  ip daddr 198.18.0.0/15 meta l4proto { tcp, udp } tproxy ip to :%d meta mark set meta mark | 0x10000 accept\n", t.Port)
		if t.IPv6 {
			fmt.Fprintf(&rules, "  ip6 daddr fc00::/18 meta l4proto { tcp, udp } tproxy ip6 to :%d meta mark set meta mark | 0x10000 accept\n", t.Port)
		}
		rules.WriteString("  ip daddr { 0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4, 240.0.0.0/4 } return\n")
		if t.IPv6 {
			rules.WriteString("  ip6 daddr { ::/128, ::1/128, fc00::/7, fe80::/10, ff00::/8 } return\n")
		}
		for _, prefix := range t.Bypass {
			family := "ip"
			if strings.Contains(prefix, ":") {
				family = "ip6"
			}
			fmt.Fprintf(&rules, "  %s daddr %s return\n", family, prefix)
		}
		fmt.Fprintf(&rules, "  meta l4proto { tcp, udp } tproxy to :%d meta mark set meta mark | 0x10000 accept\n }\n", t.Port)
		fmt.Fprintf(&rules, " chain input_guard {\n  type filter hook input priority -10; policy accept;\n  iifname != \"lo\" meta l4proto { tcp, udp } th dport { %d, %d } meta mark & 0x10000 == 0 drop\n }\n", t.Port, t.DNSPort)
		// A base-chain accept would still meet fw4's later input reject. This
		// narrow exception is inserted into fw4 and restored by its include.
		input = fmt.Sprintf("iifname %s meta mark & 0x10000 == 0x10000 accept comment \"nadkop-tproxy\"\n", interfaces)
	}
	if zapret {
		rules.WriteString(" chain postrouting {\n  type filter hook postrouting priority mangle; policy accept;\n")
		filters := map[string]string{}
		for _, arg := range c.ZapretArgs {
			for _, protocol := range []string{"tcp", "udp"} {
				if strings.HasPrefix(arg, "--filter-"+protocol+"=") {
					filters[protocol] = strings.TrimPrefix(arg, "--filter-"+protocol+"=")
				}
			}
		}
		if len(filters) == 0 {
			filters = map[string]string{"tcp": "80,443", "udp": "443"}
		}
		for _, protocol := range []string{"tcp", "udp"} {
			if value, ok := filters[protocol]; ok {
				match, _ := portFilter(value, protocol)
				fmt.Fprintf(&rules, "  oifname %q meta mark & 0x60000000 == 0 %s queue num 200 bypass\n", c.WANInterface, match)
			}
		}
		rules.WriteString(" }\n")
	}
	rules.WriteString("}\n")
	return rules.String(), input
}

func portFilter(value, protocol string) (string, error) {
	if value == "*" {
		return "meta l4proto " + protocol, nil
	}
	operator := ""
	if strings.HasPrefix(value, "~") {
		operator = "!= "
		value = strings.TrimPrefix(value, "~")
	}
	if len(value) == 0 || len(value) > 1024 {
		return "", fmt.Errorf("invalid Zapret port filter")
	}
	parts := strings.Split(value, ",")
	for _, part := range parts {
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return "", fmt.Errorf("invalid Zapret port range")
		}
		last := 0
		for _, bound := range bounds {
			port, err := strconv.Atoi(bound)
			if err != nil || port < 1 || port > 65535 || port < last || strconv.Itoa(port) != bound {
				return "", fmt.Errorf("invalid Zapret port filter")
			}
			last = port
		}
	}
	return protocol + " dport " + operator + "{ " + strings.Join(parts, ", ") + " }", nil
}

func markOutbound(n Node, mark uint32) (Node, error) {
	var stream map[string]any
	if len(n.StreamSettings) > 0 && json.Unmarshal(n.StreamSettings, &stream) != nil {
		return n, fmt.Errorf("invalid outbound stream settings")
	}
	if stream == nil {
		stream = map[string]any{}
	}
	if stream["sockopt"] == nil {
		stream["sockopt"] = map[string]any{}
	}
	var walk func(any) error
	walk = func(value any) error {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "sockopt" {
					sockopt, ok := child.(map[string]any)
					if !ok {
						return fmt.Errorf("invalid outbound socket settings")
					}
					bits := mark
					if value, ok := sockopt["mark"]; ok {
						number, ok := value.(float64)
						if !ok || number < 0 || number > 4294967295 || number != float64(uint32(number)) || uint32(number)&(TransparentMark|ZapretMark|BypassZapretMark|0x40000000) != 0 {
							return fmt.Errorf("outbound socket mark conflicts with Nadkop routing/Zapret marks")
						}
						bits |= uint32(number)
					}
					sockopt["mark"] = bits
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range value {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(stream); err != nil {
		return n, err
	}
	n.StreamSettings, _ = json.Marshal(stream)
	return n, nil
}
