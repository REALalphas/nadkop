package core

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSubscriptionLocationMetadata(t *testing.T) {
	for _, tc := range []struct{ label, metadata, code, country, city, region, display string }{
		{"explicit country wins over name flag", `"location":{"countryCode":"de","country":"Germany","city":"Berlin"}`, "DE", "Germany", "Berlin", "", ""},
		{"flat location", `"country_code":"nl","city":"Amsterdam"`, "NL", "", "Amsterdam", "", ""},
		{"country name", `"location":{"country":"Netherlands","city":"Rotterdam","region":"Zuid-Holland"}`, "", "Netherlands", "Rotterdam", "Zuid-Holland", ""},
		{"free location label", `"location":"🇯🇵 Tokyo / premium"`, "JP", "", "", "", "🇯🇵 Tokyo / premium"},
		{"country code in country", `"country":"uk","city":"London"`, "GB", "", "London", "", ""},
	} {
		t.Run(tc.label, func(t *testing.T) {
			raw := `{"remarks":"🇺🇸 Provider name",` + tc.metadata + `,"outbounds":[{"tag":"main","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080}]}}]}`
			nodes, err := ParseSubscription("provider", []byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			l := nodes[0].Location
			if l == nil || l.CountryCode != tc.code || l.Country != tc.country || l.City != tc.city || l.Region != tc.region || l.Label != tc.display {
				t.Fatalf("location: %+v", l)
			}
			local, err := UnprefixNodes("provider", nodes)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(local)
			roundTrip, err := ParseSubscription("provider", encoded)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(l)
			b, _ := json.Marshal(roundTrip[0].Location)
			if !bytes.Equal(a, b) {
				t.Fatalf("local import lost metadata: %s / %s", a, b)
			}
		})
	}
}

func TestOutboundLocationOverridesProfileAndDoesNotAffectIDsOrXray(t *testing.T) {
	raw := `{"location":{"countryCode":"US","city":"New York"},"outbounds":[{"tag":"main","protocol":"socks","countryCode":"DE","city":"Berlin","settings":{"servers":[{"address":"127.0.0.1","port":2080}]}}]}`
	nodes, err := ParseSubscription("provider", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if nodes[0].Location.CountryCode != "DE" || nodes[0].Location.City != "Berlin" {
		t.Fatal("outbound location not preferred")
	}
	path := filepath.Join(t.TempDir(), "servers.json")
	first, err := SyncServers(path, nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Location.CountryCode != "DE" {
		t.Fatal("location missing from summary")
	}
	config := Config{Default: "direct", SOCKSPort: 1080, Rules: []Rule{{Action: "proxy", Domains: []string{"lab.example"}, NodeRef: first[0].ID}}}
	before, err := Compile(config, nodes)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`"location"`, `"countryCode"`, `"city"`, "Berlin", "New York"} {
		if bytes.Contains(before.Xray, []byte(marker)) {
			t.Fatalf("display metadata leaked to Xray: %s", marker)
		}
	}
	nodes[0].Location = &ServerLocation{CountryCode: "NL", City: "Amsterdam"}
	next, err := SyncServers(path, nodes, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if next[0].ID != first[0].ID || next[0].Location.CountryCode != "NL" {
		t.Fatal("location change changed ID or was not refreshed")
	}
	after, err := Compile(config, nodes)
	if err != nil || !bytes.Equal(before.Xray, after.Xray) {
		t.Fatal("location change affected Xray", err)
	}
	cached, _ := ServerSummaries(path)
	if cached[0].Location.City != "Amsterdam" {
		t.Fatal("cached metadata lost")
	}
	_, err = SyncServers(path, nil, config)
	if err != nil {
		t.Fatal(err)
	}
	cached, _ = ServerSummaries(path)
	if cached[0].Available || cached[0].Location.City != "Amsterdam" {
		t.Fatal("missing server lost location")
	}
}
