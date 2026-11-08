package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func UpdateGeoData(ctx context.Context, c Config, root string) error {
	urls := GeoURLs(c)
	settings := []string{urls.GeositeURL, urls.GeoIPURL}
	payload := make([][]byte, 2)
	failures := make([]error, 2)
	client := &http.Client{Timeout: 16 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" || len(via) > 5 {
			return errors.New("invalid geo redirect")
		}
		return nil
	}}
	var wg sync.WaitGroup
	for i, address := range settings {
		wg.Add(1)
		go func(i int, address string) {
			defer wg.Done()
			u, err := url.Parse(address)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				failures[i] = errors.New("Geodata URL must be HTTPS")
				return
			}
			req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
			if err != nil {
				failures[i] = err
				return
			}
			req.Header.Set("User-Agent", "Nadkop/0.2")
			response, err := client.Do(req)
			if err != nil {
				failures[i] = errors.New("Failed to download geodatabase")
				return
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				failures[i] = fmt.Errorf("Geodatabase download: HTTP %d", response.StatusCode)
				return
			}
			data, err := io.ReadAll(io.LimitReader(response.Body, maxGeoFile+1))
			if err != nil || len(data) > maxGeoFile {
				failures[i] = errors.New("Geodatabase read error or 64 MiB size limit exceeded")
				return
			}
			payload[i] = data
		}(i, address)
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			return err
		}
	}
	site, err := parseGeo(payload[0], "geosite")
	if err != nil {
		return err
	}
	ip, err := parseGeo(payload[1], "geoip")
	if err != nil {
		return err
	}
	candidate := &GeoDatabase{site: site, ip: ip}
	for _, r := range c.Rules {
		domains, _, err := ruleMatches(r, candidate)
		if err != nil {
			return fmt.Errorf("Databases were not updated: %w", err)
		}
		if r.Action == "zapret" {
			for _, d := range domains {
				if !strings.HasPrefix(d, "domain:") && !strings.HasPrefix(d, "full:") {
					return fmt.Errorf("Databases were not updated: Zapret set contains regexp/keyword")
				}
			}
		}
	}
	return InstallGeoFiles(root, payload[0], payload[1])
}
