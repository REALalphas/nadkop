package router

import (
	"nadkop/internal/core"
	"strings"
	"testing"
)

func TestProbeRequestValidation(t *testing.T) {
	ref := "srv_" + strings.Repeat("a", 32)
	for _, mode := range []string{"priority", "ping"} {
		got, err := validateProbe(request{Refs: []string{ref}, Mode: mode})
		if err != nil || got != core.DefaultProbeURL {
			t.Fatal(got, err)
		}
	}
	for _, r := range []request{
		{Mode: "ping"}, {Refs: []string{"unknown"}, Mode: "ping"}, {Refs: []string{ref, ref}, Mode: "ping"},
		{Refs: []string{ref}, Mode: "unknown"}, {Refs: []string{ref}, Mode: "url", URL: "file:///etc/passwd"},
		{Refs: []string{ref}, Mode: "url", URL: "https://user:pass@example.org/"},
		{Refs: []string{ref}, Mode: "url", URL: "https://example.org/#fragment"},
	} {
		if _, err := validateProbe(r); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
	got, err := validateProbe(request{Refs: []string{ref}, Mode: "url", URL: "http://127.0.0.1:18081/204?ok=1"})
	if err != nil || got != "http://127.0.0.1:18081/204?ok=1" {
		t.Fatal(got, err)
	}
}
