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

func TestDiagnosticStepsSuccessFailureAndCleanup(t *testing.T) {
	for _, failure := range []string{"", "xray", "nfqws", "nft"} {
		t.Run(failure, func(t *testing.T) {
			b := fixture(t)
			if err := os.MkdirAll(b.runtime, 0700); err != nil {
				t.Fatal(err)
			}
			cfg := `{"sources":[],"rules":[{"name":"Video","action":"zapret","domains":["test.example"]}],"wanInterface":"eth0","zapretArgs":["--dpi-desync=fake"]}`
			if err := os.WriteFile(b.config, []byte(cfg), 0600); err != nil {
				t.Fatal(err)
			}
			var commands []string
			report := b.diagnose(context.Background(), func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if filepath.Base(name) == "ubus" {
					return []byte(`{}`), nil
				}
				if filepath.Base(name) == "nft" && !(len(args) > 0 && args[0] == "-c") {
					return nil, nil
				}
				commands = append(commands, filepath.Base(name))
				if filepath.Base(name) == failure {
					return []byte("private-password private-host"), errors.New("secret-error")
				}
				return nil, nil
			})
			if len(report.Steps) != 7 || report.OK != (failure == "") {
				t.Fatalf("bad report: %+v", report)
			}
			failed := -1
			for i, s := range report.Steps {
				if s.State == "error" {
					failed = i
				}
				if failed < 0 && s.State != "ok" {
					t.Fatalf("incorrect early step: %+v", s)
				}
				if failed >= 0 && i > failed && s.State != "skipped" {
					t.Fatalf("dependent check was not skipped: %+v", s)
				}
			}
			if failure != "" && (failed < 0 || len(commands) != failed-2) {
				t.Fatalf("wrong failed stage/calls: %+v %v", report, commands)
			}
			if failure == "" && strings.Join(commands, ",") != "xray,nfqws,nft,ip,ip,uci,sh" {
				t.Fatal(commands)
			}
			data, _ := json.Marshal(report)
			if strings.Contains(string(data), "private-") || strings.Contains(string(data), "secret-error") {
				t.Fatal("diagnostics exposed command output")
			}
			files, _ := filepath.Glob(filepath.Join(b.runtime, "generation.*"))
			if len(files) != 0 {
				t.Fatal("diagnostic generation was not removed")
			}
		})
	}
}

func TestDiagnosticInvalidConfigSourceAndRoutes(t *testing.T) {
	for _, tc := range []struct{ cfg, failed string }{
		{`{"password":"private-password"}`, "config"},
		{`{"sources":[{"name":"provider","file":"/missing-provider.json"}],"rules":[]}`, "sources"},
		// Missing catalogue bindings now fail before reconciliation, in load.
		{`{"sources":[],"rules":[{"domains":["test.example"],"action":"proxy","nodeRef":"srv_00000000000000000000000000000000"}]}`, "sources"},
		// A legacy tag has no stored ID binding; compilation still rejects it.
		{`{"sources":[],"rules":[{"domains":["test.example"],"action":"proxy","node":"provider/missing"}]}`, "routes"},
	} {
		b := fixture(t)
		if err := os.WriteFile(b.config, []byte(tc.cfg), 0600); err != nil {
			t.Fatal(err)
		}
		report := b.diagnose(context.Background(), func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("command ran after an earlier failure")
			return nil, nil
		})
		if report.OK || report.Error == "" {
			t.Fatal("invalid setup passed")
		}
		found := false
		for _, s := range report.Steps {
			if s.ID == tc.failed && s.State == "error" {
				found = true
			}
		}
		if !found {
			t.Fatalf("wrong failed stage: %+v", report)
		}
		data, _ := json.Marshal(report)
		if strings.Contains(string(data), "private-password") {
			t.Fatal("config leaked")
		}
	}
}

func TestDiagnosticDirectOnlySkipsZapret(t *testing.T) {
	b := fixture(t)
	if err := os.MkdirAll(b.runtime, 0700); err != nil {
		t.Fatal(err)
	}
	report := b.diagnose(context.Background(), func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// Coexistence detection runs its own nft/ip/uci/sh probes even for a
		// direct-only configuration; it never fails the check.
		switch filepath.Base(name) {
		case "xray", "ubus", "nft", "ip", "uci", "sh":
		default:
			t.Fatal("unnecessary Zapret check", name)
		}
		if filepath.Base(name) == "nft" && len(args) > 0 && args[0] == "-c" {
			t.Fatal("unnecessary Zapret check")
		}
		return nil, nil
	})
	if !report.OK || report.Steps[4].State != "skipped" || report.Steps[5].State != "skipped" {
		t.Fatalf("direct-only: %+v", report)
	}
}
