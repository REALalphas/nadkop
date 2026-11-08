package core

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestProductGeoHTTPSUpdatePreservesOldPairOnEveryFailure(t *testing.T) {
	source := geoFixture(t)
	site, err := os.ReadFile(filepath.Join(source, "current", "geosite.dat"))
	if err != nil {
		t.Fatal(err)
	}
	ip, err := os.ReadFile(filepath.Join(source, "current", "geoip.dat"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := InstallGeoFiles(root, site, ip); err != nil {
		t.Fatal(err)
	}
	var mu sync.RWMutex
	mode := "ok"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		current := mode
		mu.RUnlock()
		if current == "status" {
			w.WriteHeader(503)
			w.Write([]byte("private-provider-payload"))
			return
		}
		if current == "redirect" {
			http.Redirect(w, r, "/loop", http.StatusFound)
			return
		}
		if current == "truncated" {
			w.Header().Set("Content-Length", "500000")
			w.Write([]byte("short"))
			return
		}
		data := site
		if r.URL.Path == "/ip" {
			data = ip
		}
		if current == "invalid" {
			data = []byte("invalid protobuf")
		}
		if current == "missing-set" && r.URL.Path != "/ip" {
			data = bytes.ReplaceAll(site, []byte("SERVICE"), []byte("REMOVED"))
		}
		w.Write(data)
	}))
	defer server.Close()
	original := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = original }()
	cfg := Config{Geodata: GeoSettings{GeositeURL: server.URL + "/site", GeoIPURL: server.URL + "/ip"}, Rules: []Rule{{Action: "direct", Sets: []string{"geosite:service"}}}}
	if err := UpdateGeoData(context.Background(), cfg, root); err != nil {
		t.Fatal("valid TLS update failed", err)
	}
	good, _ := os.Readlink(filepath.Join(root, "current"))
	for _, failure := range []string{"status", "redirect", "truncated", "invalid", "missing-set"} {
		t.Run(failure, func(t *testing.T) {
			mu.Lock()
			mode = failure
			mu.Unlock()
			err := UpdateGeoData(context.Background(), cfg, root)
			if err == nil {
				t.Fatal("failed update accepted")
			}
			if strings.Contains(err.Error(), "private-provider-payload") {
				t.Fatal("response body exposed")
			}
			after, _ := os.Readlink(filepath.Join(root, "current"))
			if after != good {
				t.Fatal("failed update replaced old pair")
			}
			db, err := OpenGeoDatabase(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := db.Expand("geosite:service"); err != nil {
				t.Fatal("old database was damaged", err)
			}
		})
	}
	mu.Lock()
	mode = "ok"
	mu.Unlock()
	cfg.Rules = []Rule{{Action: "zapret", Sets: []string{"geosite:regex"}}}
	if err := UpdateGeoData(context.Background(), cfg, root); err == nil {
		t.Fatal("Zapret-incompatible replacement accepted")
	}
	after, _ := os.Readlink(filepath.Join(root, "current"))
	if after != good {
		t.Fatal("incompatible update replaced old pair")
	}
	cfg.Geodata.GeositeURL = "http://insecure.example/site"
	if err := UpdateGeoData(context.Background(), cfg, root); err == nil {
		t.Fatal("non-HTTPS geodata accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cfg.Geodata.GeositeURL = server.URL + "/site"
	if err := UpdateGeoData(cancelled, cfg, root); err == nil {
		t.Fatal("cancelled update accepted")
	}
}

func TestProductDefaultGeoURLsAndICMPNoOp(t *testing.T) {
	defaults := GeoURLs(Config{})
	if !strings.HasPrefix(defaults.GeositeURL, "https://") || !strings.HasPrefix(defaults.GeoIPURL, "https://") {
		t.Fatal(defaults)
	}
	custom := GeoSettings{GeositeURL: "https://geo.example/site", GeoIPURL: "https://geo.example/ip"}
	if got := GeoURLs(Config{Geodata: custom}); got != custom {
		t.Fatal(got)
	}
	if err := PrepareICMPDatagram(nil); err != nil {
		t.Fatal("ordinary nodes need no kernel change", err)
	}
}

// Independent reproduction after detecting a fixture defect in the broader test.
// This builds a new valid database explicitly rather than editing that failed test.
func TestProductGeoUpdateActuallyDisappearingSetIsRejected(t *testing.T) {
	fixture := geoFixture(t)
	site, err := os.ReadFile(filepath.Join(fixture, "current", "geosite.dat"))
	if err != nil {
		t.Fatal(err)
	}
	ip, err := os.ReadFile(filepath.Join(fixture, "current", "geoip.dat"))
	if err != nil {
		t.Fatal(err)
	}
	domain := append(pbInt(1, 2), pbString(2, []byte("another.test"))...)
	replacement := pbString(1, append(pbString(1, []byte("OTHER")), pbString(2, domain)...))
	if _, err := parseGeo(replacement, "geosite"); err != nil {
		t.Fatal("replacement is not a valid database", err)
	}
	if bytes.Contains(replacement, []byte("SERVICE")) {
		t.Fatal("test still contains the selected set")
	}
	root := t.TempDir()
	if err := InstallGeoFiles(root, site, ip); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Readlink(filepath.Join(root, "current"))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/site" {
			w.Write(replacement)
		} else {
			w.Write(ip)
		}
	}))
	defer server.Close()
	old := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = old }()
	cfg := Config{Geodata: GeoSettings{GeositeURL: server.URL + "/site", GeoIPURL: server.URL + "/ip"}, Rules: []Rule{{Action: "direct", Sets: []string{"geosite:service"}}}}
	if err := UpdateGeoData(context.Background(), cfg, root); err == nil {
		t.Fatal("database without the selected set was accepted")
	}
	after, _ := os.Readlink(filepath.Join(root, "current"))
	if before != after {
		t.Fatal("rejected database changed the active pair")
	}
}
