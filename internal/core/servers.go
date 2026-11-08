package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

var serverRefPattern = regexp.MustCompile(`^srv_[0-9a-f]{32}$`)

// The catalogue contains references and matching metadata, never Xray objects
// or credentials. Only missing records used by saved rules/pins are retained.
type ServerSummary struct {
	ID        string          `json:"id"`
	Source    string          `json:"source"`
	Name      string          `json:"name"`
	Protocol  string          `json:"protocol"`
	Transport string          `json:"transport,omitempty"`
	Port      int             `json:"port,omitempty"`
	Position  int             `json:"position"`
	Separator bool            `json:"separator,omitempty"`
	Tag       string          `json:"tag"`
	Aliases   []string        `json:"aliases,omitempty"`
	Available bool            `json:"available"`
	Location  *ServerLocation `json:"location,omitempty"`
}

type serverRecord struct {
	ServerSummary
	Fingerprint string `json:"fingerprint"`
	Endpoint    string `json:"endpoint"`
}

type serverCatalog struct {
	Version int            `json:"version"`
	Servers []serverRecord `json:"servers"`
}

func readCatalog(path string) (serverCatalog, error) {
	c := serverCatalog{Version: 1, Servers: []serverRecord{}}
	f, err := openPrivateState(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, errors.New("cannot open server catalogue")
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return c, errors.New("server catalogue must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSubscription+1))
	if err != nil || len(data) > maxSubscription {
		return c, errors.New("cannot read server catalogue")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return c, errors.New("empty server catalogue; restore it before refreshing")
	}
	var stored serverCatalog
	if json.Unmarshal(data, &stored) != nil || stored.Version != 1 || stored.Servers == nil || len(stored.Servers) > 4096 {
		return c, errors.New("invalid server catalogue; repair it without replacing saved references")
	}
	c = stored
	seen := map[string]bool{}
	for _, s := range c.Servers {
		if !serverRefPattern.MatchString(s.ID) || seen[s.ID] {
			return c, errors.New("invalid server catalogue references")
		}
		seen[s.ID] = true
	}
	return c, nil
}

func ServerSummaries(path string) ([]ServerSummary, error) {
	c, err := readCatalog(path)
	if err != nil {
		return nil, err
	}
	result := make([]ServerSummary, 0, len(c.Servers))
	for _, s := range c.Servers {
		result = append(result, s.ServerSummary)
	}
	return result, nil
}

// SyncServers serializes catalogue updates from rpcd and the procd generator.
// Identical refreshes do not write flash. The Xray payload stays in RAM.
func SyncServers(path string, nodes []Node, config Config) ([]ServerSummary, error) {
	return syncServers(path, nodes, "", config, true, true, false)
}

// SyncServersContext checks cancellation again at the catalogue publication
// boundary. A completed rename is a commit and is never rolled back on cancel.
func SyncServersContext(ctx context.Context, path string, nodes []Node, config Config) ([]ServerSummary, error) {
	return syncServersContext(ctx, path, nodes, "", config, true, true, false)
}

// SyncSourceServers updates one imported source without marking other sources missing.
func SyncSourceServers(path, source string, nodes []Node, config Config) ([]ServerSummary, error) {
	return syncSourceServers(path, source, nodes, config, false)
}

// SyncImportedSourceServers also cleans removed source records in the same
// candidate publication. Import must not prune before validating future config.
func SyncImportedSourceServers(path, source string, nodes []Node, config Config) ([]ServerSummary, error) {
	return syncSourceServers(path, source, nodes, config, true)
}

func syncSourceServers(path, source string, nodes []Node, config Config, pruneRemoved bool) ([]ServerSummary, error) {
	if !namePattern.MatchString(source) {
		return nil, errors.New("invalid source name")
	}
	for _, n := range nodes {
		if !strings.HasPrefix(n.Tag, source+"/") {
			return nil, errors.New("node belongs to another source")
		}
	}
	return syncServers(path, nodes, source, config, true, true, pruneRemoved)
}

// PruneServers removes unbound missing records without fetching subscriptions.
// Removed sources become missing; configured sources keep their last availability.
func PruneServers(path string, config Config) ([]ServerSummary, error) {
	return syncServers(path, nil, "", config, false, true, false)
}

// CheckServerPrune validates bindings and the exact pruned catalogue before a
// caller starts publishing related config/nodes files. Callers hold the outer
// configuration lock through this check, those writes, and PruneServers.
func CheckServerPrune(path string, config Config) error {
	_, err := syncServers(path, nil, "", config, false, false, false)
	return err
}

func pruneServers(c *serverCatalog, config Config) {
	refs, tags := map[string]bool{}, map[string]bool{}
	for _, rule := range config.Rules {
		if rule.Node != "" || rule.NodeRef != "" || len(rule.NodeRefs) > 0 {
			refs[rule.NodeRef] = true
			for _, ref := range rule.NodeRefs {
				refs[ref] = true
			}
			if rule.Node != "" {
				tags[rule.Node] = true
			}
		}
	}
	for _, ref := range config.Picker.Pinned {
		refs[ref] = true
	}
	kept := make([]serverRecord, 0, len(c.Servers))
	for _, server := range c.Servers {
		keep := server.Available || refs[server.ID] || tags[server.Tag]
		for _, alias := range server.Aliases {
			keep = keep || tags[alias]
		}
		if keep {
			kept = append(kept, server)
		}
	}
	c.Servers = kept
}

func syncServers(path string, nodes []Node, source string, config Config, refresh, publish, pruneRemoved bool) ([]ServerSummary, error) {
	return syncServersContext(context.Background(), path, nodes, source, config, refresh, publish, pruneRemoved)
}

func syncServersContext(ctx context.Context, path string, nodes []Node, source string, config Config, refresh, publish, pruneRemoved bool) ([]ServerSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return nil, errors.New("cannot create server catalogue directory")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("cannot open server catalogue lock")
	}
	defer lock.Close()
	if st, err := lock.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, errors.New("server catalogue lock must be a regular file")
	}
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return nil, errors.New("cannot lock server catalogue")
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := readCatalog(path)
	if err != nil {
		return nil, err
	}
	// Every stored binding, including inactive rule fields and pins, requires
	// its original record. Never create replacement IDs for a partial restore.
	known := map[string]bool{}
	for _, record := range c.Servers {
		known[record.ID] = true
	}
	refs := append([]string(nil), config.Picker.Pinned...)
	for _, rule := range config.Rules {
		if rule.NodeRef != "" {
			refs = append(refs, rule.NodeRef)
		}
		refs = append(refs, rule.NodeRefs...)
	}
	for _, ref := range refs {
		if !known[ref] {
			return nil, errors.New("server catalogue lacks saved references; restore it before refreshing")
		}
	}
	before, _ := json.Marshal(c)
	if !refresh || pruneRemoved {
		sources := map[string]bool{}
		for _, s := range config.Sources {
			sources[s.Name] = true
		}
		for i := range c.Servers {
			if !sources[c.Servers[i].Source] {
				c.Servers[i].Available = false
			}
		}
	}
	if refresh && source == "" {
		if err := reconcileServers(&c, nodes); err != nil {
			return nil, err
		}
	} else if refresh {
		subset := serverCatalog{Version: 1, Servers: []serverRecord{}}
		for _, record := range c.Servers {
			if record.Source == source {
				subset.Servers = append(subset.Servers, record)
			}
		}
		if err := reconcileServers(&subset, nodes); err != nil {
			return nil, err
		}
		updated := map[string]serverRecord{}
		for _, record := range subset.Servers {
			updated[record.ID] = record
		}
		for i, record := range c.Servers {
			if next, ok := updated[record.ID]; ok {
				c.Servers[i] = next
				delete(updated, record.ID)
			}
		}
		for _, record := range subset.Servers {
			if _, ok := updated[record.ID]; ok {
				c.Servers = append(c.Servers, record)
			}
		}
	}
	pruneServers(&c, config)
	// Catalogue insertion order must not override a provider's current ordering.
	sourceOrder := map[string]int{}
	for _, s := range config.Sources {
		if _, ok := sourceOrder[s.Name]; !ok {
			sourceOrder[s.Name] = len(sourceOrder)
		}
	}
	for _, s := range c.Servers {
		if _, ok := sourceOrder[s.Source]; !ok {
			sourceOrder[s.Source] = len(sourceOrder)
		}
	}
	sort.SliceStable(c.Servers, func(i, j int) bool {
		a, b := c.Servers[i], c.Servers[j]
		if a.Source != b.Source {
			return sourceOrder[a.Source] < sourceOrder[b.Source]
		}
		if a.Available != b.Available {
			return a.Available
		}
		return a.Position < b.Position
	})
	if len(c.Servers) > 4096 {
		return nil, errors.New("server catalogue limit reached; bound references are retained")
	}
	after, _ := json.Marshal(c)
	// Reader and writer share the same budget, including the final newline.
	if len(after)+1 > maxSubscription {
		return nil, errors.New("server catalogue exceeds 4 MiB; previous references are retained")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if publish && !bytes.Equal(before, after) {
		f, err := os.CreateTemp(filepath.Dir(path), ".nadkop-servers-*")
		if err != nil {
			return nil, errors.New("cannot save server catalogue")
		}
		defer os.Remove(f.Name())
		defer f.Close()
		if _, err = f.Write(append(after, '\n')); err != nil {
			return nil, errors.New("cannot save server catalogue")
		}
		if f.Sync() != nil || f.Close() != nil {
			return nil, errors.New("cannot publish server catalogue")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if os.Rename(f.Name(), path) != nil {
			return nil, errors.New("cannot publish server catalogue")
		}
		dir, err := os.Open(filepath.Dir(path))
		if err != nil {
			return nil, errors.New("cannot sync server catalogue directory")
		}
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return nil, errors.New("cannot sync server catalogue directory")
		}
	}
	result := make([]ServerSummary, 0, len(c.Servers))
	for _, s := range c.Servers {
		result = append(result, s.ServerSummary)
	}
	return result, nil
}

type serverGroup struct {
	source, fingerprint, endpoint string
	indices                       []int
	matched                       int
}

func reconcileServers(c *serverCatalog, nodes []Node) error {
	byTag := map[string]Node{}
	for _, n := range nodes {
		byTag[n.Tag] = n
	}
	fullCache, endpointCache := map[string]string{}, map[string]string{}
	groups := []*serverGroup{}
	byFingerprint := map[string]*serverGroup{}
	for i, n := range nodes {
		nodes[i].serverID = ""
		nodes[i].serverAliases = nil
		if n.auxiliary || n.Protocol == "blackhole" {
			continue
		}
		fp, err := outboundIdentity(n.Tag, byTag, fullCache, map[string]bool{}, false)
		if err != nil {
			return err
		}
		endpoint, err := outboundIdentity(n.Tag, byTag, endpointCache, map[string]bool{}, true)
		if err != nil {
			return err
		}
		source, _, ok := strings.Cut(n.Tag, "/")
		if !ok {
			return errors.New("server has no source namespace")
		}
		_, _, separator := serverDetails(n)
		if separator {
			// Equal placeholder endpoints are distinct headings in the provider list.
			hash := sha256.Sum256([]byte(fp + ":separator:" + n.Tag))
			fp = hex.EncodeToString(hash[:])
		}
		key := source + ":" + fp
		g := byFingerprint[key]
		if g == nil {
			g = &serverGroup{source: source, fingerprint: fp, endpoint: endpoint, matched: -1}
			byFingerprint[key] = g
			groups = append(groups, g)
		}
		g.indices = append(g.indices, i)
	}
	used := map[int]bool{}
	// Match exact connection graphs first. Display tags, ordering and remarks
	// are excluded, including tags inside nested dialerProxy chains.
	for _, g := range groups {
		for i, s := range c.Servers {
			if s.Source == g.source && s.Fingerprint == g.fingerprint && !used[i] {
				g.matched = i
				used[i] = true
				break
			}
		}
	}
	// Credential/option rotation can retain a unique endpoint. A provider's
	// stable tag can retain a unique record when the endpoint itself changes.
	// Match the whole unmatched set: never guess between two candidates.
	for _, endpointMode := range []bool{true, false} {
		candidates := map[*serverGroup][]int{}
		claims := map[int]int{}
		for _, g := range groups {
			if g.matched >= 0 {
				continue
			}
			for i, s := range c.Servers {
				if used[i] || s.Source != g.source || s.Protocol != nodes[g.indices[0]].Protocol {
					continue
				}
				match := endpointMode && s.Endpoint == g.endpoint
				if !endpointMode {
					for _, idx := range g.indices {
						if nodes[idx].Tag == s.Tag || contains(s.Aliases, nodes[idx].Tag) {
							match = true
							break
						}
					}
				}
				if match {
					candidates[g] = append(candidates[g], i)
					claims[i]++
				}
			}
		}
		for _, g := range groups {
			if options := candidates[g]; len(options) == 1 && claims[options[0]] == 1 {
				g.matched = options[0]
				used[g.matched] = true
			}
		}
	}
	for i := range c.Servers {
		c.Servers[i].Available = false
	}
	for _, g := range groups {
		if g.matched < 0 {
			var random [16]byte
			if _, err := rand.Read(random[:]); err != nil {
				return errors.New("cannot create server reference")
			}
			g.matched = len(c.Servers)
			c.Servers = append(c.Servers, serverRecord{ServerSummary: ServerSummary{ID: "srv_" + hex.EncodeToString(random[:])}})
		}
		s := &c.Servers[g.matched]
		chosen := g.indices[len(g.indices)-1]
		for _, idx := range g.indices {
			if nodes[idx].Tag == s.Tag {
				chosen = idx
				break
			}
		}
		aliases := append([]string{}, s.Aliases...)
		if s.Tag != "" && !contains(aliases, s.Tag) {
			aliases = append(aliases, s.Tag)
		}
		for _, idx := range g.indices {
			if !contains(aliases, nodes[idx].Tag) {
				aliases = append(aliases, nodes[idx].Tag)
			}
		}
		// Keep recent aliases for legacy rules without growing on every rename.
		if len(aliases) > 32 {
			aliases = aliases[len(aliases)-32:]
		}
		sort.Strings(aliases)
		n := &nodes[chosen]
		_, name, _ := strings.Cut(n.Tag, "/")
		if n.displayName != "" {
			name = n.displayName
		}
		s.Source, s.Name, s.Tag, s.Protocol = g.source, name, n.Tag, n.Protocol
		s.Transport, s.Port, s.Separator = serverDetails(*n)
		s.Position = g.indices[0]
		s.Location = mergeLocation(n.Location, nil)
		s.Fingerprint, s.Endpoint, s.Available, s.Aliases = g.fingerprint, g.endpoint, true, aliases
		n.serverID, n.serverAliases = s.ID, aliases
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func outboundIdentity(tag string, nodes map[string]Node, cache map[string]string, visiting map[string]bool, endpoint bool) (string, error) {
	if value := cache[tag]; value != "" {
		return value, nil
	}
	if visiting[tag] {
		return "", errors.New("cyclic server connection chain")
	}
	n, ok := nodes[tag]
	if !ok {
		return "", errors.New("unknown server connection dependency")
	}
	visiting[tag] = true
	defer delete(visiting, tag)
	n, err := transformOutbound(n, func(ref string) (string, error) { return outboundIdentity(ref, nodes, cache, visiting, endpoint) })
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(n)
	if err != nil {
		return "", errors.New("cannot identify server")
	}
	var object map[string]any
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.UseNumber()
	if d.Decode(&object) != nil {
		return "", errors.New("cannot identify server")
	}
	delete(object, "tag")
	delete(object, "location")
	if endpoint {
		addresses := []any{}
		var walk func(any)
		walk = func(value any) {
			switch v := value.(type) {
			case map[string]any:
				if address, ok := v["address"].(string); ok {
					addresses = append(addresses, []any{strings.ToLower(address), v["port"]})
				}
				// Iterate named settings in a stable order.
				keys := make([]string, 0, len(v))
				for k := range v {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					walk(v[k])
				}
			case []any:
				for _, child := range v {
					walk(child)
				}
			}
		}
		walk(object["settings"])
		stream, _ := object["streamSettings"].(map[string]any)
		refs := []string{}
		_, err = transformOutbound(n, func(ref string) (string, error) { refs = append(refs, ref); return ref, nil })
		if err != nil {
			return "", err
		}
		sort.Strings(refs)
		object = map[string]any{"protocol": n.Protocol, "addresses": addresses, "network": stream["network"], "security": stream["security"], "chain": refs}
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return "", errors.New("cannot identify server")
	}
	digest := sha256.Sum256(canonical)
	key := hex.EncodeToString(digest[:])
	cache[tag] = key
	return key, nil
}
