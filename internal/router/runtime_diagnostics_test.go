package router

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeDiagnosticsReflectAppliedZapret(t *testing.T) {
	for _, tc := range []struct {
		name                                      string
		applied, required, nfqws, queue, firewall bool
		state                                     string
	}{
		{name: "not-applied", state: "info"},
		{name: "direct", applied: true, state: "info"},
		{name: "failed-process", applied: true, required: true, state: "error"},
		{name: "running", applied: true, required: true, nfqws: true, queue: true, firewall: true, state: "ok"},
		{name: "missing-queue", applied: true, required: true, nfqws: true, firewall: true, state: "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := fixture(t)
			current := filepath.Join(b.runtime, "current")
			if err := os.MkdirAll(current, 0700); err != nil {
				t.Fatal(err)
			}
			if tc.applied {
				if err := os.WriteFile(filepath.Join(current, "xray.json"), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			hosts := ""
			if tc.required {
				hosts = "test.example\n"
			}
			if err := os.WriteFile(filepath.Join(current, "zapret-hosts.txt"), []byte(hosts), 0600); err != nil {
				t.Fatal(err)
			}
			queue := filepath.Join(b.runtime, "queue")
			payload := ""
			if tc.queue {
				payload = " 200 123 0 2\n"
			}
			if err := os.WriteFile(queue, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			run := func(_ context.Context, name string, _ ...string) ([]byte, error) {
				if filepath.Base(name) == "ubus" {
					return []byte(`{"nadkop":{"instances":{"xray":{"running":true,"pid":1},"nfqws":{"running":` + map[bool]string{true: "true", false: "false"}[tc.nfqws] + `,"pid":2}}}}`), nil
				}
				if filepath.Base(name) == "nft" && !tc.firewall {
					return []byte("private-password"), errors.New("private-password")
				}
				return nil, nil
			}
			status := b.statusWith(context.Background(), run, queue).(map[string]any)
			if status["zapretRequired"] != tc.required {
				t.Fatal(status)
			}
			checks := status["runtimeChecks"].([]diagnosticStep)
			states := map[string]string{}
			for _, check := range checks {
				states[check.ID] = check.State
				if check.Detail == "" {
					t.Fatal(check)
				}
			}
			if states["runtime-zapret"] != tc.state {
				t.Fatal(checks)
			}
			if tc.required {
				want := "error"
				if tc.queue {
					want = "ok"
				}
				if states["runtime-queue"] != want {
					t.Fatal(checks)
				}
				want = "error"
				if tc.firewall {
					want = "ok"
				}
				if states["runtime-firewall"] != want {
					t.Fatal(checks)
				}
			}
			data, _ := json.Marshal(status)
			if strings.Contains(string(data), "private-password") {
				t.Fatal("raw command output leaked")
			}
		})
	}
}
