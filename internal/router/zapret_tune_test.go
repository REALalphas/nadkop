package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nadkop/internal/core"
)

const testTuneID = "0123456789abcdef0123456789abcdef"

func newTuneFixture(t *testing.T) (backend, tuneJob) {
	t.Helper()
	b := fixture(t)
	_, _, rev, _ := b.read()
	j := tuneJob{ID: testTuneID, Status: "running", Revision: rev, WAN: "eth0", Targets: []string{"one.example", "two.example"}, Protocols: []string{"http", "https"}, Strategies: tuneStrategies(), Results: []tuneProbe{}, StartedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Minute).Unix()}
	if err := os.MkdirAll(b.tuneDir(j.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := b.writeTune(j); err != nil {
		t.Fatal(err)
	}
	return b, j
}

func TestTuneInputBoundary(t *testing.T) {
	valid := request{WAN: "pppoe-wan", Targets: []string{"Example.COM", "other.example"}, Protocols: []string{"http", "https"}}
	targets, protocols, err := validateTune(valid)
	if err != nil || targets[0] != "example.com" || len(protocols) != 2 {
		t.Fatalf("valid request: %v %v", targets, err)
	}
	for _, hosts := range [][]string{nil, {"127.0.0.1"}, {"https://example.com"}, {"example.com:443"}, {"example.com/?secret=x"}, {"a.local"}, {"a.home.arpa"}, {"x..com"}, {"-x.com"}, {"x.com", "X.COM"}, {"a.com", "b.com", "c.com", "d.com", "e.com"}, {"x.com\n--debug=1"}, {"a.onion"}, {"exämple.test"}} {
		r := valid
		r.Targets = hosts
		if _, _, err := validateTune(r); err == nil {
			t.Errorf("accepted targets %q", hosts)
		}
	}
	for _, wan := range []string{"", "lo", "eth0\nflush ruleset", "../../etc/config", strings.Repeat("a", 16)} {
		r := valid
		r.WAN = wan
		if _, _, err := validateTune(r); err == nil {
			t.Errorf("accepted WAN %q", wan)
		}
	}
	for _, p := range [][]string{nil, {"quic"}, {"http", "http"}, {"https", "http", "https"}} {
		r := valid
		r.Protocols = p
		if _, _, err := validateTune(r); err == nil {
			t.Errorf("accepted protocols %q", p)
		}
	}
	b := fixture(t)
	if _, err := b.call(context.Background(), "zaprettune", request{Action: "run-shell"}); err == nil {
		t.Fatal("unknown action accepted")
	}
	if _, err := b.tuneStart(context.Background(), valid); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("missing revision was not rejected: %v", err)
	}
}

func TestTuneStartRejectsUnrestoredAndConcurrentJobs(t *testing.T) {
	for _, status := range []string{"queued", "running", "restoring", "recovery_failed"} {
		t.Run(status, func(t *testing.T) {
			b, j := newTuneFixture(t)
			j.Status = status
			j.StartedAt = time.Now().Unix()
			if err := b.writeTune(j); err != nil {
				t.Fatal(err)
			}
			if err := atomicWrite(filepath.Join(b.tuneRoot(), "active"), []byte(j.ID)); err != nil {
				t.Fatal(err)
			}
			_, err := b.tuneStart(context.Background(), request{WAN: j.WAN, Targets: j.Targets, Protocols: j.Protocols, Revision: j.Revision})
			if err == nil {
				t.Fatal("overlapping start accepted")
			}
			active, _ := os.ReadFile(filepath.Join(b.tuneRoot(), "active"))
			if strings.TrimSpace(string(active)) != j.ID {
				t.Fatal("unrestored job lost")
			}
		})
	}
	b, j := newTuneFixture(t)
	unlock, err := b.tuneLock("manager")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := b.tuneStart(context.Background(), request{WAN: j.WAN, Targets: j.Targets, Protocols: j.Protocols, Revision: j.Revision}); err == nil {
		t.Fatal("manager lock ignored")
	}
}

func TestTunePublicDNSAndPinning(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.1.1", "198.18.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "255.255.255.255", "::1", "2001:4860:4860::8888"} {
		if publicTuneIP(net.ParseIP(s)) {
			t.Errorf("accepted nonpublic IPv4 %s", s)
		}
	}
	if !publicTuneIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public IPv4 rejected")
	}
	for _, ips := range [][]net.IP{nil, {net.ParseIP("10.0.0.1")}, {net.ParseIP("1.1.1.1"), net.ParseIP("127.0.0.1")}} {
		_, err := resolveTune(context.Background(), []string{"example.com"}, func(context.Context, string, string) ([]net.IP, error) { return ips, nil })
		if err == nil {
			t.Fatalf("unsafe DNS response accepted %v", ips)
		}
	}
	lookups := 0
	got, err := resolveTune(context.Background(), []string{"example.com"}, func(_ context.Context, network, host string) ([]net.IP, error) {
		lookups++
		if network != "ip4" {
			t.Fatal(network)
		}
		return []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")}, nil
	})
	if err != nil || lookups != 1 || got["example.com"] != "1.1.1.1" {
		t.Fatalf("DNS pin failed: %v %v", got, err)
	}
	_, err = resolveTune(context.Background(), []string{"example.com"}, func(context.Context, string, string) ([]net.IP, error) {
		return nil, errors.New("DNS token-secret-error")
	})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("raw DNS error leaked")
	}
}

func TestTuneSearchNeedsImprovementAndRepeat(t *testing.T) {
	cases := []struct {
		name, outcome                    string
		baseline, recheck                bool
		failSecond, failOther, queueLost bool
		wantBegins                       int
	}{
		{"already accessible", "unneeded", true, true, false, false, false, 0},
		{"confirmed", "completed", false, false, false, false, false, 1},
		{"intermittent direct", "inconclusive", false, true, false, false, false, 1},
		{"second attempt fails", "no_strategy", false, false, true, false, false, 4},
		{"regression on another site", "no_strategy", false, false, false, true, false, 4},
		{"process lost despite HTTP success", "no_strategy", false, false, false, false, true, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, j := newTuneFixture(t)
			begins, ends, persist := 0, 0, 0
			err := searchTune(context.Background(), &j, tuneSearchOps{
				begin: func(context.Context, tuneStrategy) error { begins++; return nil }, end: func() error { ends++; return nil }, persist: func() error { persist++; return nil },
				probe: func(_ context.Context, host, protocol, stage, strategy string, round int, candidate bool) tuneProbe {
					ok := true
					if stage == "baseline" {
						ok = tc.baseline || host == "two.example"
					}
					if stage == "baseline_recheck" {
						ok = tc.recheck || host == "two.example"
					}
					if candidate && ((tc.failSecond && round == 2) || (tc.failOther && host == "two.example")) {
						ok = false
					}
					return tuneProbe{Host: host, Protocol: protocol, Stage: stage, Strategy: strategy, Round: round, OK: ok, Queued: candidate && !tc.queueLost}
				},
			})
			if err != nil || j.Outcome != tc.outcome || begins != tc.wantBegins || ends != begins {
				t.Fatalf("outcome=%s err=%v begins/ends=%d/%d", j.Outcome, err, begins, ends)
			}
			if persist != len(j.Results) || j.Completed != persist {
				t.Fatal("progress did not match actual network results")
			}
			if (j.Winner != "") != (tc.outcome == "completed") {
				t.Fatalf("unverified winner %q", j.Winner)
			}
			if tc.outcome == "completed" {
				if len(j.Results) != 20 {
					t.Fatalf("missing repeat or direct comparison: %d results", len(j.Results))
				}
			}
		})
	}
}

func TestTuneSearchCancellationAndFailures(t *testing.T) {
	for _, where := range []string{"baseline", "first trial", "second trial", "preflight", "write", "stop"} {
		t.Run(where, func(t *testing.T) {
			_, j := newTuneFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, ends := 0, 0
			err := searchTune(ctx, &j, tuneSearchOps{
				begin: func(context.Context, tuneStrategy) error {
					if where == "preflight" {
						return errors.New("engine failed")
					}
					return nil
				},
				end: func() error {
					ends++
					if where == "stop" {
						return errors.New("cannot stop engine")
					}
					return nil
				},
				persist: func() error {
					if where == "write" {
						return errors.New("disk full")
					}
					return nil
				},
				probe: func(_ context.Context, host, p, stage, s string, n int, candidate bool) tuneProbe {
					calls++
					if (where == "baseline" && stage == "baseline") || (where == "first trial" && candidate) || (where == "second trial" && n == 2) {
						cancel()
					}
					return tuneProbe{Host: host, Protocol: p, Stage: stage, Strategy: s, Round: n, OK: candidate, Queued: candidate}
				},
			})
			if err == nil || j.Winner != "" || j.Outcome == "completed" {
				t.Fatalf("failed run selected strategy: %#v %v", j, err)
			}
			if where == "baseline" && calls != 1 {
				t.Fatal("cancellation did not stop probes")
			}
			if strings.Contains(where, "trial") && ends != 1 {
				t.Fatal("candidate cancellation skipped engine stop")
			}
		})
	}
}

func TestTuneSearchRequiresStableControlForEverySiteAndProtocol(t *testing.T) {
	for _, unstable := range []string{"one.example/http", "one.example/https", "two.example/http", "two.example/https"} {
		t.Run(unstable, func(t *testing.T) {
			_, j := newTuneFixture(t)
			err := searchTune(context.Background(), &j, tuneSearchOps{
				begin: func(context.Context, tuneStrategy) error { return nil }, end: func() error { return nil }, persist: func() error { return nil },
				probe: func(_ context.Context, host, p, stage, s string, n int, candidate bool) tuneProbe {
					ok := candidate || host == "two.example"
					if stage == "baseline_recheck" && host+"/"+p == unstable {
						ok = !ok
					}
					return tuneProbe{Host: host, Protocol: p, Stage: stage, Strategy: s, Round: n, OK: ok, Queued: candidate}
				},
			})
			if err != nil || j.Outcome != "inconclusive" || j.Winner != "" {
				t.Fatalf("mixed control accepted: %s %s %v", j.Outcome, j.Winner, err)
			}
		})
	}
}

func TestTuneHTTPAndCertificateErrorsAreInconclusive(t *testing.T) {
	for _, code := range []string{"tls_certificate", "http_status_or_body"} {
		t.Run(code, func(t *testing.T) {
			_, j := newTuneFixture(t)
			begins := 0
			err := searchTune(context.Background(), &j, tuneSearchOps{
				begin: func(context.Context, tuneStrategy) error { begins++; return nil }, end: func() error { return nil }, persist: func() error { return nil },
				probe: func(_ context.Context, h, p, stage, s string, n int, candidate bool) tuneProbe {
					return tuneProbe{Host: h, Protocol: p, Stage: stage, Code: code, OK: false}
				},
			})
			if err != nil || j.Outcome != "inconclusive" || j.Winner != "" || begins != 0 {
				t.Fatalf("server error treated as DPI: %s %v", j.Outcome, err)
			}
		})
	}
}

func nftListTune(id, comment string) []byte {
	data, _ := json.Marshal(map[string]any{"nftables": []any{map[string]any{"table": map[string]any{"family": "inet", "name": tuneTable(id), "comment": comment}}}})
	return data
}

func TestTuneCleanupOwnershipAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		marker, owned, removeFails, listFails bool
		wantErr                               bool
		deletes                               int
	}{
		{"nothing installed", false, false, false, false, false, 0},
		{"owned table", true, true, false, false, false, 1},
		{"table removed by firewall reload", true, false, false, false, false, 0},
		{"foreign table", true, false, false, false, true, 0},
		{"delete failed", true, true, true, false, true, 1},
		{"inspection failed", true, true, false, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, j := newTuneFixture(t)
			deletes := 0
			if tc.marker {
				if err := os.WriteFile(filepath.Join(b.tuneDir(j.ID), "owns-table"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "/usr/sbin/nft" {
					t.Fatal("unexpected command", name)
				}
				if args[0] == "delete" {
					deletes++
					if tc.removeFails {
						return nil, errors.New("busy")
					}
					return nil, nil
				}
				if tc.listFails {
					return nil, errors.New("netlink unavailable")
				}
				if tc.name == "foreign table" {
					return nftListTune(j.ID, "somebody else"), nil
				}
				if tc.owned {
					return nftListTune(j.ID, "nadkop-zapret-tune:"+j.ID), nil
				}
				return []byte(`{"nftables":[]}`), nil
			}
			err := b.tuneCleanup(context.Background(), j, run)
			if (err != nil) != tc.wantErr || deletes != tc.deletes {
				t.Fatalf("cleanup error=%v deletes=%d", err, deletes)
			}
		})
	}
}

func TestTuneGuardPublishesOnlyAfterCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, outcome, want string
		cancel, cleanupFail bool
	}{
		{"success", "completed", "completed", false, false},
		{"worker killed", "", "failed", false, false},
		{"cancelled after final probe", "completed", "cancelled", true, false},
		{"cleanup cannot be verified", "completed", "recovery_failed", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, j := newTuneFixture(t)
			j.WorkerPID = 0
			j.Outcome = tc.outcome
			j.Winner = "multisplit"
			j.Status = "restoring"
			if err := b.writeTune(j); err != nil {
				t.Fatal(err)
			}
			if tc.cancel {
				_, err := b.tuneCancel(request{Job: j.ID})
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc.cleanupFail {
				_ = os.WriteFile(filepath.Join(b.tuneDir(j.ID), "owns-table"), nil, 0600)
			}
			run := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("nft unavailable") }
			if err := b.tuneGuard(j.ID, run); err != nil {
				t.Fatal(err)
			}
			got, err := b.readTune(j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want || got.Restored == tc.cleanupFail {
				t.Fatalf("guard published %#v", got)
			}
			if tc.want != "completed" && got.Winner != "" {
				t.Fatalf("failure retained winner: %#v", got)
			}
		})
	}
}

func TestTuneFailedRecoveryCanBeRetriedWithoutRevivingWinner(t *testing.T) {
	b, j := newTuneFixture(t)
	j.Status = "restoring"
	j.Outcome = "completed"
	j.Winner = "multisplit"
	_ = b.writeTune(j)
	_ = os.WriteFile(filepath.Join(b.tuneDir(j.ID), "owns-table"), nil, 0600)
	if err := b.tuneGuard(j.ID, func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("netlink unavailable")
	}); err != nil {
		t.Fatal(err)
	}
	j, _ = b.readTune(j.ID)
	if j.Status != "recovery_failed" || j.Restored {
		t.Fatal("cleanup failure hidden")
	}
	if err := b.tuneGuard(j.ID, func(context.Context, string, ...string) ([]byte, error) { return []byte(`{"nftables":[]}`), nil }); err != nil {
		t.Fatal(err)
	}
	j, _ = b.readTune(j.ID)
	if j.Status != "failed" || !j.Restored || j.Winner != "" {
		t.Fatalf("retry revived unconfirmed success: %#v", j)
	}
}

func TestTuneSelectionRejectsConfigurationWithoutZapret(t *testing.T) {
	b, j := newTuneFixture(t)
	saved, _, _, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	c, err := core.ReadConfig(bytes.NewReader(saved))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range c.Active().Rules {
		if rule.Action == "zapret" {
			t.Fatal("fixture unexpectedly contains Zapret")
		}
	}
	j.Status, j.Restored, j.Winner = "completed", true, "multisplit"
	if err := b.writeTune(j); err != nil {
		t.Fatal(err)
	}
	_, err = b.tuneSelect(request{Job: j.ID, Strategy: j.Winner, Revision: j.Revision, WAN: j.WAN})
	if err == nil || err.Error() != "No rules with Zapret action; add a rule and run tuning again" {
		t.Fatalf("expected missing Zapret error, got %v", err)
	}
}

func TestTuneStartReplacesUnreadableActiveJob(t *testing.T) {
	for _, old := range []string{"garbage", testTuneID} {
		t.Run(old, func(t *testing.T) {
			b := fixture(t)
			_, _, rev, err := b.read()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(b.tuneRoot(), 0700); err != nil {
				t.Fatal(err)
			}
			activePath := filepath.Join(b.tuneRoot(), "active")
			if err := atomicWrite(activePath, []byte(old)); err != nil {
				t.Fatal(err)
			}
			started := false
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "/sbin/ip" || strings.Join(args, " ") != "link show dev eth0" {
					t.Fatalf("unexpected command %s %v", name, args)
				}
				return nil, nil
			}
			start := func(cmd *exec.Cmd) error {
				if cmd.Path != "/usr/bin/nadkop" || len(cmd.Args) != 3 || cmd.Args[1] != "zapret-worker" {
					t.Fatalf("unexpected worker %v", cmd.Args)
				}
				started = true
				return nil
			}
			_, err = b.tuneStartWith(context.Background(), request{Revision: rev, WAN: "eth0", Targets: []string{"one.example"}, Protocols: []string{"https"}}, run, start)
			if err != nil {
				t.Fatal(err)
			}
			active, err := os.ReadFile(activePath)
			if err != nil {
				t.Fatal(err)
			}
			id := strings.TrimSpace(string(active))
			if !started || id == old || !tuneIDPattern.MatchString(id) {
				t.Fatalf("active job not replaced: %q, started=%v", id, started)
			}
			j, err := b.readTune(id)
			if err != nil || j.Status != "queued" || j.Revision != rev {
				t.Fatalf("new job not saved: %#v, %v", j, err)
			}
		})
	}
}

func TestTuneSelectionNeverWritesConfiguration(t *testing.T) {
	b, j := newTuneFixture(t)
	if err := os.WriteFile(b.config, []byte(`{"wanInterface":"eth0","rules":[{"action":"zapret","domains":["one.example"]}],"zapretArgs":["--dpi-desync=multisplit"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	beforeC, beforeN, rev, _ := b.read()
	j.Revision = rev
	j.Status, j.Restored, j.Winner = "completed", true, "multisplit"
	if err := b.writeTune(j); err != nil {
		t.Fatal(err)
	}
	valid := request{Job: j.ID, Strategy: j.Winner, Revision: rev, WAN: j.WAN}
	result, err := b.tuneSelect(valid)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.(map[string]any)["args"].([]string)) == 0 {
		t.Fatal("missing verified strategy")
	}
	for _, mutate := range []func(*tuneJob, *request){
		func(j *tuneJob, r *request) { j.Restored = false }, func(j *tuneJob, r *request) { j.Status = "no_strategy" }, func(j *tuneJob, r *request) { j.Status = "restoring" }, func(j *tuneJob, r *request) { j.Winner = "" }, func(j *tuneJob, r *request) { r.Strategy = "shell" }, func(j *tuneJob, r *request) { r.Revision = "stale" }, func(j *tuneJob, r *request) { j.Revision = "stale" }, func(j *tuneJob, r *request) { r.WAN = "different" }, func(j *tuneJob, r *request) { j.StartedAt = time.Now().Add(-2 * time.Hour).Unix() },
	} {
		copyJ, copyR := j, valid
		mutate(&copyJ, &copyR)
		if err := b.writeTune(copyJ); err != nil {
			t.Fatal(err)
		}
		if _, err := b.tuneSelect(copyR); err == nil {
			t.Fatal("invalid selection accepted", copyJ, copyR)
		}
	}
	afterC, afterN, _, _ := b.read()
	if !bytes.Equal(beforeC, afterC) || !bytes.Equal(beforeN, afterN) {
		t.Fatal("tuner changed saved settings")
	}
}

func TestTunePrivateStateAndSocketIsolation(t *testing.T) {
	b, j := newTuneFixture(t)
	j.Addresses = map[string]string{"one.example": "1.1.1.1"}
	j.WorkerPID = 123
	j.WorkerTick = "private"
	j.EnginePID = 456
	j.EngineTick = "private"
	j.Outcome = "completed"
	if err := b.writeTune(j); err != nil {
		t.Fatal(err)
	}
	result, err := b.tuneStatus(request{Job: j.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	for _, s := range []string{"workerPID", "enginePID", "addresses", "private", "outcome"} {
		if bytes.Contains(raw, []byte(s)) {
			t.Fatalf("private state leaked: %s", raw)
		}
	}
	info, _ := os.Stat(filepath.Join(b.tuneDir(j.ID), "job.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("public job file")
	}
	info, _ = os.Stat(b.tuneDir(j.ID))
	if info.Mode().Perm() != 0700 {
		t.Fatal("public workspace")
	}
	nft := tuneNft(j)
	for _, s := range []string{"create table inet " + tuneTable(j.ID), "oifname \"eth0\"", "meta mark == 0x28000000", "ip daddr { 1.1.1.1 }", "tcp dport { 80, 443 }", "queue num 201 bypass"} {
		if !strings.Contains(nft, s) {
			t.Fatalf("missing isolation %q in %s", s, nft)
		}
	}
	for _, s := range []string{"flush ruleset", "table inet nadkop {", "queue num 200", "prerouting"} {
		if strings.Contains(nft, s) {
			t.Fatalf("production state touched: %s", nft)
		}
	}
	for _, id := range []string{"../config.json", strings.Repeat("a", 31), strings.Repeat("a", 33), ""} {
		if _, err := b.readTune(id); err == nil {
			t.Fatalf("accepted job path %q", id)
		}
	}
}

func TestTuneQueueStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queues")
	for _, tc := range []struct {
		data       string
		present    bool
		seq, drops uint64
		bad        bool
	}{
		{" 200 1 0 2 65531 0 0 40 1\n", false, 0, 0, false},
		{" 201 22 0 2 65531 3 4 99 1\n", true, 99, 7, false},
		{" 201 22 0 2 65531 x 4 99 1\n", false, 0, 0, true},
	} {
		_ = os.WriteFile(path, []byte(tc.data), 0600)
		p, s, d, e := tuneQueueStats(path)
		if p != tc.present || s != tc.seq || d != tc.drops || (e != nil) != tc.bad {
			t.Fatalf("queue stats %q => %v %d %d %v", tc.data, p, s, d, e)
		}
	}
}

func TestTuneCandidateCatalogueIsAcceptedConfiguration(t *testing.T) {
	for _, s := range tuneStrategies() {
		t.Run(s.ID, func(t *testing.T) {
			args, _ := json.Marshal(s.Args)
			raw := fmt.Sprintf(`{"wanInterface":"eth0","rules":[{"action":"zapret","domains":["example.com"]}],"zapretArgs":%s}`, args)
			_, err := core.ReadConfig(strings.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTuneCandidateVerdict(t *testing.T) {
	healthy := tuneQueueObservation{present: true, ownedBefore: true, ownedAfter: true, alive: true, before: 10, after: 11}
	for _, tc := range []struct {
		name   string
		change func(*tuneQueueObservation)
		owned  bool
		err    error
		queued bool
		code   string
		calls  int
	}{
		{"verified queue skips nft", nil, false, errors.New("must not inspect"), true, "", 0},
		{"counters unchanged", func(q *tuneQueueObservation) { q.after = q.before }, true, nil, false, "queue_not_used", 1},
		{"table absent", func(q *tuneQueueObservation) { q.after = q.before }, false, nil, false, "table_lost", 1},
		{"ownership error", func(q *tuneQueueObservation) { q.after = q.before }, false, errors.New("foreign or unavailable"), false, "queue_not_verified", 1},
		{"queue absent", func(q *tuneQueueObservation) { q.present = false }, true, nil, false, "queue_not_used", 1},
		{"previous owner differs", func(q *tuneQueueObservation) { q.ownedBefore = false }, true, nil, false, "queue_not_used", 1},
		{"current owner differs", func(q *tuneQueueObservation) { q.ownedAfter = false }, true, nil, false, "queue_not_used", 1},
		{"drops increased", func(q *tuneQueueObservation) { q.dropsAfter++ }, true, nil, false, "queue_not_used", 1},
		{"engine exited", func(q *tuneQueueObservation) { q.alive = false }, true, nil, false, "queue_not_used", 1},
		{"initial counters unavailable", func(q *tuneQueueObservation) { q.beforeErr = errors.New("read") }, true, nil, false, "queue_not_used", 1},
		{"final counters unavailable", func(q *tuneQueueObservation) { q.afterErr = errors.New("read") }, true, nil, false, "queue_not_used", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := healthy
			if tc.change != nil {
				tc.change(&q)
			}
			calls := 0
			queued, code := tuneCandidateVerdict(q, func() (bool, error) { calls++; return tc.owned, tc.err })
			if queued != tc.queued || code != tc.code || calls != tc.calls {
				t.Fatalf("queued=%v code=%q inspections=%d", queued, code, calls)
			}
		})
	}
}

func TestTuneSearchPreservesCandidateCodes(t *testing.T) {
	for _, code := range []string{"table_lost", "queue_not_verified", ""} {
		for _, lostRound := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/round%d", code, lostRound), func(t *testing.T) {
				_, j := newTuneFixture(t)
				ends := 0
				err := searchTune(context.Background(), &j, tuneSearchOps{
					begin:   func(context.Context, tuneStrategy) error { return nil },
					end:     func() error { ends++; return nil },
					persist: func() error { return nil },
					probe: func(_ context.Context, h, p, stage, s string, n int, candidate bool) tuneProbe {
						r := tuneProbe{Host: h, Protocol: p, Stage: stage, Strategy: s, Round: n, OK: candidate, Queued: candidate}
						if candidate && n == lostRound {
							r.OK, r.Queued, r.Code = false, false, code
						}
						return r
					},
				})
				if code == "table_lost" {
					if err == nil || err.Error() != tuneTableLostMessage || j.Outcome != "" || j.Winner != "" || ends != 1 {
						t.Fatalf("lost rules: outcome=%q winner=%q ends=%d err=%v", j.Outcome, j.Winner, ends, err)
					}
				} else if err != nil || j.Outcome != "no_strategy" || j.Winner != "" {
					t.Fatalf("unexpected result: outcome=%q winner=%q err=%v", j.Outcome, j.Winner, err)
				}
				want := code
				if want == "" {
					want = "queue_not_used"
				}
				for _, r := range j.Results {
					if r.Stage == "strategy" && r.Round == lostRound && r.Code != want {
						t.Fatalf("candidate code overwritten: got %q want %q", r.Code, want)
					}
				}
			})
		}
	}
}
