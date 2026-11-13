package router

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"nadkop/internal/core"
)

// Insertion commits only the new source. Other unsaved editor changes stay in
// the browser and can later be saved against the returned revision.
func (b backend) addSource(ctx context.Context, r request) (any, error) {
	return b.addSourceWith(ctx, r, atomicWrite)
}

func (b backend) addSourceWith(ctx context.Context, r request, write func(string, []byte) error) (any, error) {
	unlock, err := b.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	data, _, revision, err := b.read()
	if err != nil {
		return nil, err
	}
	if r.Revision != revision {
		return nil, errors.New("Configuration changed: reload the page before adding a subscription")
	}
	c, err := core.ReadConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("Invalid saved configuration")
	}
	location := strings.TrimSpace(r.Content)
	s := r.Options
	if s.Title != "" || s.Name != "" || s.URL != "" || s.File != "" {
		return nil, errors.New("Source options may only contain headers, format and HWID")
	}
	if strings.HasPrefix(location, "/") {
		if err := core.ValidateSourceFile(location); err != nil {
			return nil, err
		}
		s = core.Source{File: location}
	} else {
		u, err := url.Parse(location)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(location) > 4096 || strings.ContainsAny(location, "\r\n\t ") {
			return nil, errors.New("Enter an HTTPS URL or an absolute path to a .json file")
		}
		s.URL = location
		if s.HWID == nil {
			s.HWID = &core.HWIDSettings{Enabled: true}
		}
	}
	path := filepath.Join(filepath.Dir(b.config), "servers.json")
	servers, err := core.ServerSummaries(path)
	if err != nil {
		return nil, err
	}
	for _, existing := range c.Sources {
		if (s.URL != "" && existing.URL == s.URL) || (s.File != "" && existing.File == s.File) {
			return map[string]any{"source": existing, "revision": revision, "servers": servers, "existing": true}, nil
		}
	}
	if len(c.Sources) >= 16 {
		return nil, errors.New("At most 16 subscriptions are supported")
	}
	// Reserve namespaces from retained missing records too: a new provider with
	// the same title must never inherit old rule references by accident.
	reserved := append([]core.Source(nil), c.Sources...)
	for _, server := range servers {
		reserved = append(reserved, core.Source{Name: server.Source})
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	s, nodes, err := core.DiscoverSourceContext(fetchCtx, s, reserved, filepath.Join(filepath.Dir(b.config), "hwid"))
	if err != nil {
		return nil, err
	}
	c.Sources = append(c.Sources, s)
	if err := fetchCtx.Err(); err != nil {
		return nil, err
	}
	encoded, err := encodeSavedJSON(c)
	if err != nil {
		return nil, err
	}
	// Validate the complete future configuration before publishing catalogue IDs.
	if _, err := core.ReadConfig(bytes.NewReader(append(encoded, '\n'))); err != nil {
		return nil, errors.New("Imported source would make the saved configuration invalid")
	}
	if err := core.CheckImportedSourceServers(path, s.Name, nodes, c); err != nil {
		return nil, err
	}
	// Publish the source before its catalogue records. If catalogue publication
	// fails or the process exits, the configured source can be refreshed without
	// leaving an orphan namespace that changes IDs on the next import.
	if err := write(b.config, encoded); err != nil {
		return nil, errors.New("Cannot save subscription")
	}
	servers, err = core.SyncImportedSourceServers(path, s.Name, nodes, c)
	if err != nil {
		return nil, errors.New("Subscription saved, but server catalogue update failed; reload and refresh subscriptions")
	}
	_, _, revision, err = b.read()
	if err != nil {
		return nil, err
	}
	return map[string]any{"source": s, "revision": revision, "servers": servers}, nil
}
