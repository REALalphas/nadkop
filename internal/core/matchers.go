package core

import (
	"fmt"
	"net/netip"
	"strings"
)

func normalizeIP(value string) (string, error) {
	if address, err := netip.ParseAddr(value); err == nil && !address.Is4In6() && address.Zone() == "" {
		return address.String(), nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || prefix.Addr().Is4In6() || prefix.Addr().Zone() != "" {
		return "", fmt.Errorf("invalid IP/CIDR: %s", value)
	}
	return prefix.Masked().String(), nil
}
func validGeoSelector(value string) bool {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return false
	}
	switch parts[0] {
	case "geoip", "mixed":
		return geoCode(parts[1])
	case "geosite":
		attr := strings.Split(parts[1], "@")
		if len(attr) > 2 {
			return false
		}
		for _, s := range attr {
			if !geoCode(s) {
				return false
			}
		}
		return true
	}
	return false
}
func ruleMatches(rule Rule, db *GeoDatabase) (domains, ips []string, err error) {
	return ruleMatchesWith(rule, func(set string) ([]string, []string, error) {
		if db == nil {
			return nil, nil, fmt.Errorf("geodata is not loaded")
		}
		return db.Expand(set)
	})
}

func ruleMatchesWith(rule Rule, expand func(string) ([]string, []string, error)) (domains, ips []string, err error) {
	rule = rule.Active()
	for _, domain := range rule.Domains {
		domains = append(domains, "domain:"+domain)
	}
	ips = append(ips, rule.IPs...)
	for _, set := range rule.Sets {
		d, ip, e := expand(set)
		if e != nil {
			return nil, nil, e
		}
		domains = append(domains, d...)
		ips = append(ips, ip...)
	}
	domains = uniqueStrings(domains)
	ips = uniqueStrings(ips)
	return
}

// Results are private to one compilation and only retained while a selector
// has remaining uses. Distinct selectors never populate the cache. ruleMatchesWith
// copies slice elements before deduplication, so cached results stay immutable.
func compileExpander(db *GeoDatabase, rules []Rule) func(string) ([]string, []string, error) {
	remaining := map[string]int{}
	for _, rule := range rules {
		for _, set := range rule.Active().Sets {
			remaining[set]++
		}
	}
	type expansion struct{ domains, ips []string }
	cache := map[string]expansion{}
	return func(set string) ([]string, []string, error) {
		if db == nil {
			return nil, nil, fmt.Errorf("geodata is not loaded")
		}
		remaining[set]--
		if result, ok := cache[set]; ok {
			if remaining[set] == 0 {
				delete(cache, set)
			}
			return result.domains, result.ips, nil
		}
		d, ip, err := db.Expand(set)
		if err == nil && remaining[set] > 0 {
			cache[set] = expansion{d, ip}
		}
		return d, ip, err
	}
}
func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
