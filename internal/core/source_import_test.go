package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const sourceTestProfile = `{"title":"My VPN + EU!","outbounds":[{"tag":"proxy","protocol":"trojan","settings":{"servers":[{"address":"example.test","port":443,"password":"private-password"}]},"streamSettings":{"sockopt":{"dialerProxy":"helper"}}},{"tag":"helper","protocol":"freedom","settings":{}}]}`

func TestDiscoverTitleHeadersAndStableRefresh(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{"", "My_VPN_-_EU-"},
		{"Provider One / Premium", "Provider_One_-_Premium"},
		{"base64:" + base64.StdEncoding.EncodeToString([]byte("Probe 🚀 EU")), "Probe_-_EU"},
		{"base64:invalid!", "My_VPN_-_EU-"},
	} {
		t.Run(tc.want+tc.header, func(t *testing.T) {
			calls := 0
			identityPath := filepath.Join(t.TempDir(), "hwid")
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Accept") != "application/json" || !strings.HasPrefix(r.UserAgent(), "Happ/") {
					t.Error("JSON request defaults missing")
				}
				w.Header().Set("Profile-Title", tc.header)
				fmt.Fprint(w, sourceTestProfile)
			}))
			defer server.Close()
			s, nodes, err := discoverSource(context.Background(), Source{URL: server.URL}, nil, server.Client().Transport, identityPath, DeviceInfo{})
			if err != nil || s.Name != tc.want || calls != 1 {
				t.Fatalf("discover: name=%q calls=%d err=%v", s.Name, calls, err)
			}
			if len(nodes) != 2 || nodes[0].Tag != s.Name+"/proxy" || !nodes[1].auxiliary || !strings.Contains(string(nodes[0].StreamSettings), s.Name+"/helper") {
				t.Fatal("dependency namespace/metadata lost")
			}
			dir := t.TempDir()
			if _, err := ReadConfig(strings.NewReader(fmt.Sprintf(`{"sources":[{"name":%q,"url":%q}],"rules":[]}`, s.Name, s.URL))); err != nil {
				t.Fatal(err)
			}
			first, err := SyncServers(filepath.Join(dir, "servers.json"), nodes, Config{})
			if err != nil {
				t.Fatal(err)
			}
			// Future title changes do not rename the established namespace or its IDs.
			refresh, err := loadContext(context.Background(), s, server.Client().Transport, identityPath, DeviceInfo{})
			if err != nil {
				t.Fatal(err)
			}
			next, err := SyncServers(filepath.Join(dir, "servers.json"), refresh, Config{})
			if err != nil || first[0].ID != next[0].ID {
				t.Fatal("refresh changed reference", err)
			}
			s2, _, err := discoverSource(context.Background(), Source{URL: server.URL}, []Source{s}, server.Client().Transport, identityPath, DeviceInfo{})
			if err != nil || s2.Name != s.Name+"_2" {
				t.Fatal("name collision not resolved", err)
			}
		})
	}
}

func TestDiscoverLocalPathsAndFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local servers.json")
	profile := strings.Replace(sourceTestProfile, `"title":"My VPN + EU!",`, "", 1)
	if err := os.WriteFile(path, []byte(profile), 0600); err != nil {
		t.Fatal(err)
	}
	s, _, err := DiscoverSourceContext(context.Background(), Source{File: path}, nil, "")
	if err != nil || s.Name != "local_servers" {
		t.Fatal("filename fallback", s.Name, err)
	}
	if err := os.Symlink(path, filepath.Join(dir, "symlink.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo.json"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative.json", "/etc/shadow", dir, filepath.Join(dir, "symlink.json"), filepath.Join(dir, "fifo.json")} {
		if _, _, err := DiscoverSourceContext(context.Background(), Source{File: path}, nil, ""); err == nil {
			t.Fatalf("invalid file accepted: %s", path)
		}
	}
	for input, want := range map[string]string{"  My VPN / EU!  ": "My_VPN_-_EU-", "🔥!": "subscription", "Mój\tVPN": "Mój_VPN", "": "subscription"} {
		if got := SourceName(input); got != want {
			t.Fatalf("normalization: %q -> %q (want %q)", input, got, want)
		}
	}
}

func TestScopedSourceSyncKeepsOtherIDsAndAvailability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	first, _ := ParseSubscription("existing", []byte(sourceTestProfile))
	summaries, err := SyncServers(path, first, Config{})
	if err != nil {
		t.Fatal(err)
	}
	old := summaries[0]
	added, _ := ParseSubscription("new", []byte(sourceTestProfile))
	summaries, err = SyncSourceServers(path, "new", added, Config{})
	if err != nil || len(summaries) != 2 || summaries[0].ID != old.ID || !summaries[0].Available {
		t.Fatal("other source affected", err)
	}
	if _, err := SyncSourceServers(path, "wrong", added, Config{}); err == nil {
		t.Fatal("wrong scope accepted")
	}
	summaries, err = SyncSourceServers(path, "new", nil, Config{Picker: PickerSettings{Pinned: []string{summaries[1].ID}}})
	if err != nil || !summaries[0].Available || summaries[1].Available {
		t.Fatal("scoped missing update affected other source", err)
	}
}
