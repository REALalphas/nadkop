package router

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nadkop/internal/core"
)

func closureC2Generation(t *testing.T, b backend, name string, enabled, ipv6 bool) string {
	t.Helper()
	dir := filepath.Join(b.runtime, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	plan := core.RuntimeSettings{SOCKSPort: 1080, Transparent: core.TransparentSettings{Enabled: enabled, IPv6: ipv6, Interfaces: []string{"br-c2"}, Port: 12345, DNSPort: 1053}}
	data, _ := json.Marshal(plan)
	for name, body := range map[string][]byte{"runtime.json": data, "xray.json": []byte(`{}`), "rules.nft": nil, "input.nft": nil} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestClosureC2JournalBoundaries(t *testing.T) {
	b := fixture(t)
	gen := closureC2Generation(t, b, "generation.new", false, false)
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte(`[]`), nil }
	plan, err := readRuntime(gen)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.beginNetwork(context.Background(), plan, gen, run); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"rollback", "commit"} {
		var err error
		if action == "rollback" {
			err = b.rollbackNetwork(context.Background(), gen+"-wrong", run)
		} else {
			err = b.commitNetwork(gen + "-wrong")
		}
		if err == nil {
			t.Fatal("accepted mismatched transaction", action)
		}
	}
	if err = b.commitNetwork(gen); err == nil {
		t.Fatal("committed unpublished generation")
	}
	if err = os.Symlink(gen, filepath.Join(b.runtime, "current")); err != nil {
		t.Fatal(err)
	}
	if err = b.rollbackNetwork(context.Background(), gen, run); err == nil {
		t.Fatal("rolled back published generation")
	}
	if err = b.commitNetwork(filepath.Join(b.runtime, "current")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(b.networkJournal()); !os.IsNotExist(err) {
		t.Fatal("committed journal retained", err)
	}
}

func TestClosureC2CorruptCurrentApplyFailsBeforeMutation(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{true: "missing", false: "corrupt"}[missing], func(t *testing.T) {
			b := fixture(t)
			old := closureC2Generation(t, b, "generation.old", true, true)
			if err := os.Symlink(old, filepath.Join(b.runtime, "current")); err != nil {
				t.Fatal(err)
			}
			if missing {
				os.Remove(filepath.Join(old, "runtime.json"))
			} else {
				os.WriteFile(filepath.Join(old, "runtime.json"), []byte("{"), 0600)
			}
			calls := 0
			run := func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil }
			err := b.applyNetwork(context.Background(), core.RuntimeSettings{}, filepath.Join(b.runtime, "candidate", "apply.nft"), run)
			if err == nil || calls != 0 {
				t.Fatal("invalid previous plan mutated network", err, calls)
			}
		})
	}
}

func TestClosureC2RecoveryRejectsMalformedJournal(t *testing.T) {
	for _, raw := range []string{`{`, `null`, `{"Generation":"relative"}`, `{"Generation":"/valid","Policies":[{"Family":"-4"},{"Family":"-4"}]}`, `{"Generation":"/valid","Policies":[{"Family":"-5"}]}`} {
		b := fixture(t)
		os.MkdirAll(b.runtime, 0700)
		os.WriteFile(b.networkJournal(), []byte(raw), 0600)
		calls := 0
		err := b.recoverNetwork(context.Background(), func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil })
		if err == nil || calls != 0 {
			t.Fatal("unsafe journal accepted", raw, err, calls)
		}
		if _, err = os.Stat(b.networkJournal()); err != nil {
			t.Fatal("evidence journal removed", err)
		}
	}
}

func TestClosureC2PolicySelectorBoundaries(t *testing.T) {
	for _, field := range []string{"ipproto", "uidrange", "sport", "dport", "not", "tos", "suppress_prefixlength", "l3mdev", "action", "goto", "iif", "oif"} {
		t.Run(field, func(t *testing.T) {
			entry := map[string]any{"priority": 10000, "table": 100, "src": "all", "fwmark": "0x10000", "fwmask": "0x10000", field: 1}
			data, _ := json.Marshal([]any{entry})
			deletes := 0
			run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if strings.Contains(strings.Join(args, " "), " del ") {
					deletes++
				}
				return data, nil
			}
			if err := cleanupPolicy(context.Background(), "-4", run); err == nil || deletes != 0 {
				t.Fatal("foreign selector deleted", field, err)
			}
		})
	}
	for _, tc := range []struct {
		family, raw string
		ok          bool
	}{
		{"-6", `[{"table":100,"type":"local","dst":"default","dev":"lo","protocol":"boot","metric":1024,"pref":"medium","flags":[]}]`, true},
		{"-4", `[{"table":100,"type":"local","dst":"default","dev":"lo","protocol":"boot","scope":"host","flags":[]}]`, true},
		{"-6", `[{"table":100,"type":"local","dst":"default","dev":"lo","metric":99,"pref":"medium"}]`, false},
		{"-4", `[{"table":100,"type":"local","dst":"default","dev":"lo","metric":1024}]`, false},
		{"-4", `[{"table":100,"type":"local","dst":"default","dev":"lo","flags":["onlink"]}]`, false},
	} {
		run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if strings.Contains(strings.Join(args, " "), "rule show") {
				return []byte(`[]`), nil
			}
			return []byte(tc.raw), nil
		}
		_, _, err := policyState(context.Background(), tc.family, run)
		if (err == nil) != tc.ok {
			t.Fatal(tc, err)
		}
	}
}

func TestClosureC2PolicyRollbackErrorsVisible(t *testing.T) {
	b := fixture(t)
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, " show") {
			return []byte(`[]`), nil
		}
		if strings.Contains(joined, " del ") {
			return nil, errors.New("injected cleanup error")
		}
		if name == "/usr/sbin/nft" {
			return nil, errors.New("injected nft error")
		}
		return nil, nil
	}
	err := b.applyNetwork(context.Background(), core.RuntimeSettings{Transparent: core.TransparentSettings{Enabled: true}}, filepath.Join(b.runtime, "new", "apply.nft"), run)
	if err == nil || !strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatal("rollback error hidden", err)
	}
	if _, err = os.Stat(b.networkJournal()); err != nil {
		t.Fatal("missing recovery journal", err)
	}
}

func TestClosureC2PreparePreflightRecognizesPendingOwnership(t *testing.T) {
	b := fixture(t)
	gen := closureC2Generation(t, b, "generation.new", false, false)
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "list table inet nadkop" {
			return []byte("table inet nadkop {}\n"), nil
		}
		if name == "/usr/sbin/nft" {
			return []byte("chain input {}"), nil
		}
		return []byte(`[]`), nil
	}
	if _, err := b.nftTransaction(context.Background(), gen, run); err == nil {
		t.Fatal("unowned table accepted")
	}
	if _, err := b.beginNetwork(context.Background(), core.RuntimeSettings{}, gen, run); err != nil {
		t.Fatal(err)
	}
	if _, err := b.nftTransaction(context.Background(), gen, run); err != nil {
		t.Fatal("pending first-start table blocked prepare preflight", err)
	}
	if err := b.recoverNetwork(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.networkJournal()); !os.IsNotExist(err) {
		t.Fatal("recovery did not finish", err)
	}
}

func TestClosureC2ForeignPolicyAppearingDuringRollback(t *testing.T) {
	b := fixture(t)
	conflicted, deletes := false, 0
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "rule show") {
			if conflicted {
				return []byte(`[{"priority":10000,"table":200,"src":"all"}]`), nil
			}
			return []byte(`[]`), nil
		}
		if strings.Contains(joined, "route show") {
			return []byte(`[]`), nil
		}
		if strings.Contains(joined, " del ") {
			deletes++
		}
		if name == "/usr/sbin/nft" {
			conflicted = true
			return nil, errors.New("injected nft error and foreign replacement")
		}
		return nil, nil
	}
	err := b.applyNetwork(context.Background(), core.RuntimeSettings{Transparent: core.TransparentSettings{Enabled: true}}, filepath.Join(b.runtime, "new", "apply.nft"), run)
	if err == nil || deletes != 0 {
		t.Fatal("foreign policies mutated during rollback", err, deletes)
	}
	if _, err := os.Stat(b.networkJournal()); err != nil {
		t.Fatal("missing recovery journal", err)
	}
}
