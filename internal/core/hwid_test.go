package core

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDeviceIDConcurrentAndPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etc", "nadkop", "hwid")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	// This is the empty conffile installed by an OpenWrt package.
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	errs := make(chan error, 16)
	for i := 0; i < cap(ids); i++ {
		wg.Add(1)
		go func() { defer wg.Done(); id, err := DeviceID(path); ids <- id; errs <- err }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for id := range ids {
		if !hwidPattern.MatchString(id) || len(id) != 32 {
			t.Fatal("invalid generated device ID")
		}
		if first == "" {
			first = id
		}
		if first != id {
			t.Fatal("concurrent requests registered different devices")
		}
	}
	if id, err := DeviceID(path); err != nil || id != first {
		t.Fatal("device ID changed on reload")
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("device ID is not private")
	}
}

func TestInvalidDeviceIDIsNeverRotated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hwid")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := DeviceID(path); err == nil {
		t.Fatal("corrupt ID silently replaced")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "broken" {
		t.Fatal("corrupt ID overwritten")
	}
	before, _ := os.Stat(dir)
	if _, err := DeviceID(dir); err == nil {
		t.Fatal("directory used as an ID file")
	}
	stat, _ := os.Stat(dir)
	if stat.Mode().Perm() != before.Mode().Perm() {
		t.Fatal("invalid ID path changed directory permissions")
	}
}

func TestHWIDHeadersAndManualPrecedence(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "hwid")
	info := DeviceInfo{OS: "OpenWrt", Version: "25.12.6", Model: "Test Router"}
	received := make(chan http.Header, 4)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.Write([]byte(`[{"tag":"exit","protocol":"socks","settings":{}}]`))
	}))
	defer server.Close()
	s := Source{Name: "provider", URL: server.URL, HWID: &HWIDSettings{Enabled: true}}
	for i := 0; i < 2; i++ {
		if _, err := loadContext(context.Background(), s, server.Client().Transport, identityPath, info); err != nil {
			t.Fatal(err)
		}
	}
	a, b := <-received, <-received
	if a.Get("X-HWID") == "" || a.Get("X-HWID") != b.Get("X-HWID") {
		t.Fatal("HWID changed across subscription updates")
	}
	if a.Get("X-Device-OS") != info.OS || a.Get("X-Ver-OS") != info.Version || a.Get("X-Device-Model") != info.Model {
		t.Fatal("device metadata not sent")
	}
	s.HWID = &HWIDSettings{Enabled: true, ID: "profile-device-123", OS: "CustomOS", Version: "v1", Model: "Custom model"}
	s.Headers = map[string]string{"x-hwid": "manual-device-123", "x-device-os": "ManualOS", "User-Agent": "MyClient/1.0"}
	if _, err := loadContext(context.Background(), s, server.Client().Transport, identityPath, info); err != nil {
		t.Fatal(err)
	}
	manual := <-received
	if manual.Get("X-HWID") != "manual-device-123" || manual.Get("X-Device-OS") != "ManualOS" || manual.Get("User-Agent") != "MyClient/1.0" {
		t.Fatal("manual identification was replaced")
	}
	if manual.Get("X-Ver-OS") != "v1" || manual.Get("X-Device-Model") != "Custom model" {
		t.Fatal("source metadata override ignored")
	}
}

func TestHWIDDisabledAndCustomDoesNotCreateIdentity(t *testing.T) {
	for _, settings := range []*HWIDSettings{{Enabled: false}, {Enabled: true, ID: "custom-device-123"}} {
		path := filepath.Join(t.TempDir(), "hwid")
		req, _ := http.NewRequest("GET", "https://example.test", nil)
		s := Source{HWID: settings}
		if err := applySourceHeaders(req, s, path, DeviceInfo{}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("unused automatic ID created")
		}
		if settings == nil || !settings.Enabled {
			if req.Header.Get("X-HWID") != "" || req.Header.Get("X-Device-OS") != "" {
				t.Fatal("disabled HWID sent")
			}
		} else if req.Header.Get("X-HWID") != settings.ID {
			t.Fatal("custom ID ignored")
		}
	}
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	if err := applySourceHeaders(req, Source{Headers: map[string]string{"X-HWID": "legacy-device-123"}}, "", DeviceInfo{}); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-HWID") != "legacy-device-123" {
		t.Fatal("legacy manual HWID stopped working")
	}
	// Advanced headers retain their historical behavior for other provider formats.
	if err := applySourceHeaders(req, Source{Headers: map[string]string{"X-HWID": "legacy_device_id_123"}}, "", DeviceInfo{}); err != nil {
		t.Fatal("legacy provider-specific HWID was rejected")
	}
}

func TestHWIDProviderFeedbackAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		header, value, body, reason string
		enabled                     bool
	}{
		{"X-Hwid-Not-Supported", "true", "secret-provider-body", "requires HWID", false},
		{"X-Hwid-Not-Supported", "true", "secret-provider-body", "rejected HWID", true},
		{"X-Hwid-Max-Devices-Reached", "true", "secret-provider-body", "device limit", true},
		{"X-Hwid-Limit", "1", "secret-provider-body", "device limit", false},
		{"Subscription-Always-Hwid-Enable", "1", "[]", "requires HWID", false},
		{"", "", "#subscription-always-hwid-enable: true\n" + `[]`, "requires HWID", false},
		{"", "", base64.StdEncoding.EncodeToString([]byte("#subscription-always-hwid-enable: 1\nvless://private-credential@private-host:443")), "requires HWID", false},
	} {
		t.Run(tc.header+tc.reason+tc.value, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					w.Header().Set(tc.header, tc.value)
				}
				if strings.HasPrefix(tc.body, "secret") {
					w.WriteHeader(http.StatusForbidden)
				}
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			s := Source{Name: "provider", URL: server.URL + "/secret-token", HWID: &HWIDSettings{Enabled: tc.enabled, ID: "secret-device-id"}}
			_, err := loadContext(context.Background(), s, server.Client().Transport, filepath.Join(t.TempDir(), "hwid"), DeviceInfo{})
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("wrong HWID feedback: %v", err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
				t.Fatal("HWID error exposed credentials or provider response")
			}
		})
	}
}

func TestHWIDNeverFollowsCrossOriginRedirect(t *testing.T) {
	var contacted atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted.Add(1) }))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer server.Close()
	_, err := loadContext(context.Background(), Source{Name: "provider", URL: server.URL, HWID: &HWIDSettings{Enabled: true, ID: "custom-device-123"}}, server.Client().Transport, "", DeviceInfo{})
	if err == nil || contacted.Load() != 0 {
		t.Fatal("HWID leaked through a cross-origin redirect")
	}
}

func TestHWIDConfigValidation(t *testing.T) {
	for _, value := range []string{
		`{"sources":[{"name":"a","url":"https://example.test","hwid":{"enabled":true,"id":"short"}}]}`,
		`{"sources":[{"name":"a","file":"/etc/nadkop/nodes.json","hwid":{"enabled":true}}]}`,
		`{"sources":[{"name":"a","url":"https://example.test","hwid":{"enabled":true,"model":"bad\r\nheader"}}]}`,
		`{"sources":[{"name":"a","url":"https://example.test","headers":{"X-HWID":"device-one-123","x-hwid":"device-two-123"}}]}`,
	} {
		if _, err := ReadConfig(strings.NewReader(value)); err == nil {
			t.Fatal("invalid HWID settings accepted")
		}
	}
}

func TestHWIDEnabledByDefaultAndExplicitOptOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hwid")
	var previous string
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("GET", "https://example.test", nil)
		if err := applySourceHeaders(req, Source{}, path, DeviceInfo{OS: "OpenWrt"}); err != nil {
			t.Fatal(err)
		}
		id := req.Header.Get("X-HWID")
		if id == "" || (previous != "" && id != previous) || req.Header.Get("X-Device-OS") != "OpenWrt" {
			t.Fatal("default identity missing or rotated")
		}
		previous = id
	}
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	if err := applySourceHeaders(req, Source{HWID: &HWIDSettings{Enabled: false}}, path, DeviceInfo{OS: "OpenWrt"}); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-HWID") != "" || req.Header.Get("X-Device-OS") != "" {
		t.Fatal("explicit opt-out ignored")
	}
	id, err := DeviceID(path)
	if err != nil || id != previous {
		t.Fatal("opt-out changed persistent ID")
	}
}
