package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// Sources use explicit node selection so a provider cannot silently change the exit.
type Source struct {
	Title   string            `json:"title,omitempty"`
	Name    string            `json:"name"`
	URL     string            `json:"url,omitempty"`
	File    string            `json:"file,omitempty"`
	Format  string            `json:"format,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	HWID    *HWIDSettings     `json:"hwid,omitempty"`
}

type SelectionSettings struct {
	Mode string `json:"mode"`
	URL  string `json:"url,omitempty"`
}

type Rule struct {
	Conditions *RuleConditions    `json:"conditions,omitempty"`
	Name       string             `json:"name,omitempty"`
	IPs        []string           `json:"ips,omitempty"`
	Sets       []string           `json:"sets,omitempty"`
	Domains    []string           `json:"domains"`
	Action     string             `json:"action"`
	Node       string             `json:"node,omitempty"`
	NodeRef    string             `json:"nodeRef,omitempty"`
	NodeRefs   []string           `json:"nodeRefs,omitempty"`
	Selection  *SelectionSettings `json:"selection,omitempty"`
}

type PickerSettings struct {
	Pinned     []string `json:"pinned,omitempty"`
	Order      []string `json:"order,omitempty"`
	EmojiStyle string   `json:"emojiStyle,omitempty"`
}

type Config struct {
	Transparent         TransparentSettings `json:"transparent,omitempty"`
	UpdateIntervalHours int                 `json:"updateIntervalHours,omitempty"`
	Geodata             GeoSettings         `json:"geodata,omitempty"`
	Sources             []Source            `json:"sources"`
	Rules               []Rule              `json:"rules"`
	Default             string              `json:"default"`
	SOCKSPort           int                 `json:"socksPort"`
	ZapretArgs          []string            `json:"zapretArgs"`
	WANInterface        string              `json:"wanInterface"`
	Picker              PickerSettings      `json:"picker,omitempty"`
}

var namePattern = regexp.MustCompile(`^[\p{L}\p{N}_-]+$`)

func ReadConfig(r io.Reader) (Config, error) {
	var c Config
	body, err := io.ReadAll(io.LimitReader(r, maxSubscription+1))
	if err != nil || len(body) > maxSubscription {
		return c, fmt.Errorf("configuration exceeds 4 MiB or cannot be read")
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || trimmed[0] != '{' {
		return c, fmt.Errorf("expected a JSON configuration object")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("expected exactly one JSON object")
	}
	if c.Default == "" {
		c.Default = "direct"
	}
	if c.Default != "direct" && c.Default != "block" {
		return c, fmt.Errorf("default must be direct or block")
	}
	if c.SOCKSPort == 0 {
		c.SOCKSPort = 1080
	}
	if c.SOCKSPort < 1 || c.SOCKSPort > 65535 {
		return c, fmt.Errorf("invalid socksPort")
	}
	if err := c.normalizeNetwork(); err != nil {
		return c, err
	}
	if len(c.Sources) > 16 || len(c.Rules) > 1024 {
		return c, fmt.Errorf("at most 16 subscriptions and 1024 rules are supported")
	}
	if c.UpdateIntervalHours < 0 || c.UpdateIntervalHours > 168 {
		return c, fmt.Errorf("updateIntervalHours must be between 0 and 168")
	}
	seen := map[string]bool{}
	for _, refs := range [][]string{c.Picker.Pinned, c.Picker.Order} {
		if len(refs) > 4096 {
			return c, fmt.Errorf("too many picker references")
		}
		unique := map[string]bool{}
		for _, ref := range refs {
			if !serverRefPattern.MatchString(ref) || unique[ref] {
				return c, fmt.Errorf("invalid or duplicate picker reference")
			}
			unique[ref] = true
		}
	}
	if c.Picker.EmojiStyle != "" && c.Picker.EmojiStyle != "apple" && c.Picker.EmojiStyle != "system" {
		return c, fmt.Errorf("emojiStyle must be apple or system")
	}
	for _, s := range c.Sources {
		if utf8.RuneCountInString(s.Title) > 80 || strings.IndexFunc(s.Title, unicode.IsControl) >= 0 {
			return c, fmt.Errorf("source title must be at most 80 characters without control characters")
		}
		if !namePattern.MatchString(s.Name) || seen[s.Name] {
			return c, fmt.Errorf("invalid or duplicate source name %q", s.Name)
		}
		seen[s.Name] = true
		if (s.URL == "") == (s.File == "") {
			return c, fmt.Errorf("source %s requires exactly one of url/file", s.Name)
		}
		if s.URL != "" {
			u, err := url.Parse(s.URL)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(s.URL) > 4096 || strings.ContainsAny(s.URL, "\r\n\t ") {
				return c, fmt.Errorf("source %s requires an HTTPS URL without userinfo or fragment", s.Name)
			}
		}
		if err := s.validateFormat(); err != nil {
			return c, fmt.Errorf("source %s: %w", s.Name, err)
		}
		if err := s.validateHWID(); err != nil {
			return c, fmt.Errorf("source %s: %w", s.Name, err)
		}
	}
	for _, address := range []string{c.Geodata.GeositeURL, c.Geodata.GeoIPURL} {
		if address == "" {
			continue
		}
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || len(address) > 2048 {
			return c, fmt.Errorf("geodata URL must be HTTPS")
		}
	}
	hasZapret := false
	multiGroups := 0
	groupRefs := map[string]bool{}
	for i := range c.Rules {
		stored := &c.Rules[i]
		if len(stored.Domains)+len(stored.IPs) > 16384 || len(stored.Sets) > 64 {
			return c, ruleError(i, "matchers", "Too many addresses or sets.")
		}
		active := stored.Active()
		r := &active
		r.Name = strings.TrimSpace(r.Name)
		if utf8.RuneCountInString(r.Name) > 80 || strings.IndexFunc(r.Name, unicode.IsControl) >= 0 {
			return c, ruleError(i, "name", "Name must be at most 80 characters without control characters.")
		}
		if len(r.Domains)+len(r.IPs)+len(r.Sets) == 0 {
			return c, ruleError(i, "matchers", "No action target specified. Add a domain, IP/CIDR, or set - one field is enough.")
		}
		if len(r.Domains)+len(r.IPs) > 16384 || len(r.Sets) > 64 {
			return c, ruleError(i, "matchers", "Too many addresses or sets.")
		}
		for j, value := range r.IPs {
			v, err := normalizeIP(strings.TrimSpace(value))
			if err != nil {
				return c, ruleError(i, "ips", "Invalid IP/CIDR \"%s\". Examples: 1.1.1.1 or 10.0.0.0/8.", value)
			}
			r.IPs[j] = v
		}
		r.IPs = uniqueStrings(r.IPs)
		for j, set := range r.Sets {
			set = strings.ToLower(strings.TrimSpace(set))
			if !validGeoSelector(set) {
				return c, ruleError(i, "sets", "Invalid set \"%s\". Select a set from the list.", set)
			}
			r.Sets[j] = set
		}
		r.Sets = uniqueStrings(r.Sets)
		switch r.Action {
		case "direct", "block", "zapret":
			if r.Action == "zapret" {
				hasZapret = true
			}
			if r.Node != "" || r.NodeRef != "" || len(r.NodeRefs) > 0 || r.Selection != nil {
				return c, ruleError(i, "action", "Selected servers are only used with the \"VPN\" action.")
			}
		case "proxy":
			selectors := 0
			if r.Node != "" {
				selectors++
			}
			if r.NodeRef != "" {
				selectors++
			}
			if len(r.NodeRefs) > 0 {
				selectors++
			}
			if selectors != 1 {
				return c, ruleError(i, "action", "For the \"VPN\" action, select a connection with the \"Select server\" button.")
			}
			if len(r.NodeRefs) > 16 {
				return c, ruleError(i, "action", "A single rule can contain at most 16 servers.")
			}
			seenRefs := map[string]bool{}
			for _, ref := range append(append([]string{}, r.NodeRefs...), r.NodeRef) {
				if ref == "" {
					continue
				}
				if !serverRefPattern.MatchString(ref) || seenRefs[ref] {
					return c, ruleError(i, "action", "Server list contains an invalid or duplicate reference. Select servers again.")
				}
				seenRefs[ref] = true
			}
			if len(r.NodeRefs) == 1 {
				r.NodeRef, r.NodeRefs = r.NodeRefs[0], nil
			}
			if len(r.NodeRefs) > 1 {
				multiGroups++
				for _, ref := range r.NodeRefs {
					groupRefs[ref] = true
				}
				if r.Selection == nil {
					r.Selection = &SelectionSettings{Mode: "priority"}
				}
				if r.Selection.Mode == "" {
					r.Selection.Mode = "priority"
				}
				switch r.Selection.Mode {
				case "priority", "ping":
					if r.Selection.URL != "" {
						return c, ruleError(i, "selection", "A custom URL can only be used in \"URL test\" mode.")
					}
				case "url":
					if r.Selection.URL == "" {
						r.Selection.URL = DefaultProbeURL
					}
					u, err := url.Parse(r.Selection.URL)
					if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" || len(r.Selection.URL) > 2048 || strings.ContainsAny(r.Selection.URL, "\r\n\t ") {
						return c, ruleError(i, "url", "Specify the full probe address starting with http:// or https://.")
					}
				default:
					return c, ruleError(i, "selection", "Select a server probing method.")
				}
			} else {
				r.Selection = nil
			}

		default:
			return c, ruleError(i, "action", "Select a rule action.")
		}
		for j, domain := range r.Domains {
			domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
			if !validDomain(domain) {
				return c, ruleError(i, "domains", "Invalid domain \"%s\". Specify a bare site name without a path; use punycode for non-ASCII names.", domain)
			}
			r.Domains[j] = domain
		}
		r.Domains = uniqueStrings(r.Domains)
		stored.Name = r.Name
		if stored.Conditions == nil || stored.Conditions.Domains {
			stored.Domains = r.Domains
		}
		if stored.Conditions == nil || stored.Conditions.IPs {
			stored.IPs = r.IPs
		}
		if stored.Conditions == nil || stored.Conditions.Sets {
			stored.Sets = r.Sets
		}
		if stored.Action == "proxy" {
			stored.Node, stored.NodeRef, stored.NodeRefs = r.Node, r.NodeRef, r.NodeRefs
			if len(r.NodeRefs) > 1 {
				if stored.Selection == nil {
					stored.Selection = r.Selection
				} else if stored.Selection.Mode == "" {
					stored.Selection.Mode = r.Selection.Mode
				}
			}
		}
	}
	if multiGroups > 16 || len(groupRefs) > 64 {
		return c, fmt.Errorf("at most 16 multi-server rules and 64 checked servers are supported")
	}
	if multiGroups > 0 && (c.SOCKSPort == HealthAPIPort || c.SOCKSPort == HealthSOCKSPort) {
		return c, fmt.Errorf("SOCKS port conflicts with server health monitoring")
	}
	if hasZapret && (!regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,15}$`).MatchString(c.WANInterface) || len(c.ZapretArgs) == 0) {
		return c, fmt.Errorf("zapret requires wanInterface (Linux device name) and zapretArgs")
	}
	// The marked Zapret outbound preserves ordered domain exceptions.
	if len(c.ZapretArgs) > 64 {
		return c, fmt.Errorf("at most 64 Zapret arguments are supported")
	}
	if !hasZapret {
		return c, nil
	}
	filters := map[string]bool{}
	for _, arg := range c.ZapretArgs {
		allowed := strings.HasPrefix(arg, "--filter-tcp=") || strings.HasPrefix(arg, "--filter-udp=") || strings.HasPrefix(arg, "--dpi-desync=") || strings.HasPrefix(arg, "--dpi-desync-")
		if !allowed || len(arg) > 8192 || strings.ContainsAny(arg, "\x00\r\n") || strings.HasPrefix(arg, "--dpi-desync-fwmark") {
			return c, fmt.Errorf("invalid or reserved zapret argument")
		}
		for _, protocol := range []string{"tcp", "udp"} {
			if strings.HasPrefix(arg, "--filter-"+protocol+"=") {
				if filters[protocol] {
					return c, fmt.Errorf("duplicate Zapret port filter")
				}
				filters[protocol] = true
				if _, err := portFilter(strings.TrimPrefix(arg, "--filter-"+protocol+"="), protocol); err != nil {
					return c, err
				}
			}
		}
	}
	return c, nil
}

func validDomain(s string) bool {
	if len(s) > 253 || net.ParseIP(s) != nil {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if len(l) == 0 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// ErrInterruptedSave identifies a saved-pair recovery failure under the state lock.
var ErrInterruptedSave = errors.New("configuration recovery required")

// interruptedSaveError carries ErrInterruptedSave for errors.Is without
// leaking the sentinel text into the operator-facing message.
type interruptedSaveError struct{ cause error }

func (e *interruptedSaveError) Error() string   { return e.cause.Error() }
func (e *interruptedSaveError) Unwrap() []error { return []error{e.cause, ErrInterruptedSave} }

// LockConfiguration covers reading config.json through catalogue reconciliation.
// Use a separate inode because saving replaces config.json by rename. The lock
// order is router ui.lock, configuration lock, then catalogue lock; core sync
// never reacquires the configuration lock.
func LockConfiguration(configPath string, nonblocking bool) (func(), error) {
	f, err := os.OpenFile(filepath.Join(filepath.Dir(configPath), ".state.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("configuration lock must be a regular file")
	}
	flags := syscall.LOCK_EX
	if nonblocking {
		flags |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), flags); err != nil {
		f.Close()
		return nil, err
	}
	if err := resolveSavedPair(configPath); err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, &interruptedSaveError{cause: err}
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
