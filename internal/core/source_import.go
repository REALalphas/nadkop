package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
)

// ValidateSourceFile limits the admin import API to explicit absolute JSON paths.
// The reader additionally rejects symlinks and non-regular files before reading.
func ValidateSourceFile(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.EqualFold(filepath.Ext(path), ".json") || strings.ContainsAny(path, "\x00\r\n") {
		return fmt.Errorf("local source requires an absolute path to a .json file")
	}
	return nil
}

// DiscoverSourceContext loads once and names a new source from its metadata.
// Names are assigned only on insertion; refresh never renames a source namespace.
func DiscoverSourceContext(ctx context.Context, s Source, existing []Source, identityPath string) (Source, []Node, error) {
	return discoverSource(ctx, s, existing, nil, identityPath, ReadDeviceInfo())
}

func discoverSource(ctx context.Context, s Source, existing []Source, transport http.RoundTripper, identityPath string, info DeviceInfo) (Source, []Node, error) {
	if s.File != "" {
		if err := ValidateSourceFile(s.File); err != nil {
			return s, nil, err
		}
	}
	data, title, err := readSubscription(ctx, s, transport, identityPath, info)
	if err != nil {
		return s, nil, err
	}
	if s.File != "" && !isJSONSubscription(data) {
		return s, nil, fmt.Errorf("local source must contain Xray JSON")
	}
	title = strings.TrimSpace(title)
	if strings.HasPrefix(strings.ToLower(title), "base64:") {
		decoded, err := decodeBase64(strings.TrimSpace(title[len("base64:"):]))
		title = ""
		if err == nil {
			title = strings.TrimSpace(string(decoded))
		}
	}
	if title == "" {
		var metadata struct {
			Title   string `json:"title"`
			Remarks string `json:"remarks"`
		}
		if json.Unmarshal([]byte(subscriptionText(data)), &metadata) == nil {
			title = metadata.Title
			if strings.TrimSpace(title) == "" {
				title = metadata.Remarks
			}
		}
	}
	if strings.TrimSpace(title) == "" {
		if s.File != "" {
			title = strings.TrimSuffix(filepath.Base(s.File), filepath.Ext(s.File))
		} else if u, err := url.Parse(s.URL); err == nil {
			title = u.Hostname()
		}
	}
	base := SourceName(title)
	used := map[string]bool{}
	for _, other := range existing {
		used[other.Name] = true
	}
	s.Name = base
	for i := 2; used[s.Name]; i++ {
		s.Name = fmt.Sprintf("%s_%d", base, i)
	}
	nodes, err := ParseSubscription(s.Name, data)
	if ctx.Err() != nil {
		return s, nil, ctx.Err()
	}
	if err != nil {
		return s, nil, err
	}
	selectable := false
	for _, n := range nodes {
		if !n.auxiliary && n.Protocol != "blackhole" {
			selectable = true
			break
		}
	}
	if !selectable {
		return s, nil, fmt.Errorf("subscription has no selectable servers")
	}
	return s, nodes, nil
}

// SourceName keeps letters (including Cyrillic), numbers, '-' and '_'.
func SourceName(title string) string {
	var out []rune
	for _, r := range strings.TrimSpace(title) {
		switch {
		case unicode.IsSpace(r):
			out = append(out, '_')
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
		if len(out) >= 128 {
			break
		}
	}
	name := string(out)
	if strings.Trim(name, "_-") == "" {
		return "subscription"
	}
	return name
}
