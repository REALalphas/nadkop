package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func (s Source) validateFormat() error {
	if s.Format != "" && s.Format != "xray-json" && s.Format != "links" {
		return errors.New("subscription format must be xray-json or links")
	}
	return nil
}

func subscriptionText(body []byte) string {
	s := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	if s != "" && !strings.Contains(s, "://") && s[0] != '[' && s[0] != '{' {
		if decoded, err := decodeBase64(strings.Join(strings.Fields(s), "")); err == nil {
			s = strings.TrimSpace(string(decoded))
		}
	}
	return s
}

func isJSONSubscription(body []byte) bool {
	s := subscriptionText(body)
	return strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{")
}

// Retain outbound fields supplied by the provider, including mux,
// proxySettings, sendThrough and future Xray options. The core validates them
// during preflight; converting through share links would lose these options.
func (n *Node) UnmarshalJSON(data []byte) error {
	type plain Node
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	location := locationFromFields(fields)
	for _, key := range locationKeys {
		delete(fields, key)
	}
	clean, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	var decoded plain
	if err := json.Unmarshal(clean, &decoded); err != nil {
		return err
	}
	for _, key := range []string{"tag", "protocol", "settings", "streamSettings"} {
		delete(fields, key)
	}
	*n = Node(decoded)
	n.Location, n.extra = location, fields
	return nil
}

func (n Node) MarshalJSON() ([]byte, error) {
	type plain Node
	data, err := json.Marshal(plain(n))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range n.extra {
		fields[key] = value
	}
	return json.Marshal(fields)
}

func parseXrayJSON(data []byte) ([]Node, error) {
	var entries []json.RawMessage
	if len(data) > 0 && data[0] == '{' {
		if !json.Valid(data) {
			return nil, errors.New("invalid Xray JSON")
		}
		entries = []json.RawMessage{data}
	} else if json.Unmarshal(data, &entries) != nil {
		return nil, errors.New("invalid Xray JSON array")
	}
	nodes := []Node{}
	used := map[string]bool{}
	profileMode := false
	for i, entry := range entries {
		var shape map[string]json.RawMessage
		if json.Unmarshal(entry, &shape) != nil || shape == nil {
			return nil, errors.New("expected Xray outbound objects or configuration profiles")
		}
		_, profile := shape["outbounds"]
		if i > 0 && profile != profileMode {
			return nil, errors.New("cannot mix Xray profiles and outbound objects")
		}
		profileMode = profile
		if !profile {
			var n Node
			if json.Unmarshal(entry, &n) != nil {
				return nil, errors.New("invalid Xray outbound object")
			}
			nodes = append(nodes, n)
			continue
		}
		var p struct {
			Remarks   string `json:"remarks"`
			Outbounds []Node `json:"outbounds"`
		}
		if json.Unmarshal(entry, &p) != nil || len(p.Outbounds) == 0 {
			return nil, fmt.Errorf("Xray profile %d has no valid outbounds", i+1)
		}
		profileLocation := locationFromFields(shape)
		for j := range p.Outbounds {
			p.Outbounds[j].Location = mergeLocation(p.Outbounds[j].Location, profileLocation)
		}
		selected, err := profileOutbounds(p.Outbounds, p.Remarks, i, len(entries), used)
		if err != nil {
			return nil, fmt.Errorf("Xray profile %d: %w", i+1, err)
		}
		nodes = append(nodes, selected...)
	}
	return nodes, nil
}

func profileOutbounds(out []Node, remarks string, index, count int, used map[string]bool) ([]Node, error) {
	byTag := map[string]Node{}
	for i := range out {
		if out[i].Tag == "" {
			out[i].Tag = fmt.Sprintf("outbound-%d", i+1)
		}
		if _, exists := byTag[out[i].Tag]; exists {
			return nil, errors.New("duplicate outbound tag")
		}
		byTag[out[i].Tag] = out[i]
	}
	// Import proxy outbounds and their dialer/fragmentation chain. Unreferenced
	// freedom/blackhole helpers and the provider's listeners, DNS and routing do
	// not replace nadkop's own local listener and user-defined rules.
	selected := map[string]bool{}
	var visit func(string) error
	visit = func(tag string) error {
		if selected[tag] {
			return nil
		}
		n, exists := byTag[tag]
		if !exists {
			return errors.New("outbound references an unknown dialer or proxy tag")
		}
		selected[tag] = true
		_, err := transformOutbound(n, func(ref string) (string, error) { return ref, visit(ref) })
		return err
	}
	for i, n := range out {
		if n.Protocol == "blackhole" || (n.Protocol == "freedom" && i != 0) {
			continue
		}
		if err := visit(n.Tag); err != nil {
			return nil, err
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("profile has no proxy outbounds")
	}
	label := cleanProfileTag(remarks)
	if label == "" && count > 1 {
		label = fmt.Sprintf("profile-%d", index+1)
	}
	tags := map[string]string{}
	for i, n := range out {
		if !selected[n.Tag] {
			continue
		}
		base := cleanProfileTag(n.Tag)
		if label != "" {
			base = label
			if i != 0 {
				base += " · " + cleanProfileTag(n.Tag)
			}
		}
		tag := base
		for suffix := 2; used[tag]; suffix++ {
			tag = fmt.Sprintf("%s (%d)", base, suffix)
		}
		used[tag], tags[n.Tag] = true, tag
	}
	result := []Node{}
	for i, n := range out {
		if !selected[n.Tag] {
			continue
		}
		n, err := renameOutbound(n, tags)
		if err != nil {
			return nil, err
		}
		n.auxiliary = i != 0
		if i == 0 && strings.TrimSpace(remarks) != "" {
			n.displayName = strings.TrimSpace(remarks)
		}
		result = append(result, n)
	}
	return result, nil
}

func cleanProfileTag(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r < 32 || r == 127 {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	return s
}

func renameOutbound(n Node, tags map[string]string) (Node, error) {
	n.Tag = tags[n.Tag]
	return transformOutbound(n, func(ref string) (string, error) {
		tag, ok := tags[ref]
		if !ok {
			return "", errors.New("outbound references an unknown dialer or proxy tag")
		}
		return tag, nil
	})
}

// UnprefixNodes prepares imported nodes for the local JSON file. References
// must lose the temporary import namespace along with their outbound tags.
func UnprefixNodes(source string, nodes []Node) ([]Node, error) {
	tags := map[string]string{}
	for _, n := range nodes {
		tags[n.Tag] = strings.TrimPrefix(n.Tag, source+"/")
	}
	result := make([]Node, len(nodes))
	for i, n := range nodes {
		var err error
		result[i], err = renameOutbound(n, tags)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func transformOutbound(n Node, transform func(string) (string, error)) (Node, error) {
	id, aliases, auxiliary, displayName := n.serverID, n.serverAliases, n.auxiliary, n.displayName
	data, err := json.Marshal(n)
	if err != nil {
		return n, err
	}
	var object any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return n, err
	}
	var walk func(any) error
	walk = func(value any) error {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "dialerProxy" {
					if ref, ok := child.(string); ok && ref != "" {
						tag, err := transform(ref)
						if err != nil {
							return err
						}
						value[key] = tag
					}
				}
				if key == "proxySettings" {
					if proxy, ok := child.(map[string]any); ok {
						if ref, ok := proxy["tag"].(string); ok && ref != "" {
							tag, err := transform(ref)
							if err != nil {
								return err
							}
							proxy["tag"] = tag
						}
					}
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
	if err := walk(object); err != nil {
		return n, err
	}
	data, err = json.Marshal(object)
	if err != nil {
		return n, err
	}
	err = json.Unmarshal(data, &n)
	n.serverID, n.serverAliases, n.auxiliary, n.displayName = id, aliases, auxiliary, displayName
	return n, err
}
