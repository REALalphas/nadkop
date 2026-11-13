package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func x2Receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("channel did not progress")
		var zero T
		return zero
	}
}

func TestX2MonitorReadyGroupsAndSingleApply(t *testing.T) {
	manifest := HealthManifest{Groups: []HealthGroup{
		{Tag: "slow", URL: "slow", Mode: "priority", Members: []HealthMember{{"s", "s"}}},
		{Tag: "fast", URL: "fast", Mode: "priority", Members: []HealthMember{{"f", "f"}}},
		{Tag: "shared", URL: "fast", Mode: "url", Members: []HealthMember{{"f", "f"}}},
	}}
	release := make(chan struct{})
	slowStarted := make(chan struct{})
	defer close(release)
	applies := make(chan string, 3)
	done := make(chan error, 1)
	var count atomic.Int32
	go func() {
		done <- MonitorCycle(context.Background(), manifest, func(_ context.Context, _ HealthManifest, _ HealthMember, url string) ProbeResult {
			count.Add(1)
			if url == "slow" {
				close(slowStarted)
				<-release
			}
			return ProbeResult{Available: true}
		}, func(_ context.Context, _, group, _ string) error { applies <- group; return nil })
	}()
	first := x2Receive(t, applies)
	second := x2Receive(t, applies)
	// Fast results can be applied before the slow worker is scheduled.
	x2Receive(t, slowStarted)
	if first != "fast" || second != "shared" || count.Load() != 2 {
		t.Fatal(first, second, count.Load())
	}
	// Finish only after verifying the unrelated slow probe remains blocked.
	release <- struct{}{}
	if x2Receive(t, applies) != "slow" || x2Receive(t, done) != nil {
		t.Fatal("missing slow result")
	}
}

func TestX2MonitorFairQueueCeilingAndSlowApply(t *testing.T) {
	manifest := HealthManifest{}
	for i := 0; i < 8; i++ {
		manifest.Groups = append(manifest.Groups, HealthGroup{Tag: string(rune('a' + i)), URL: "same", Mode: "priority", Members: []HealthMember{{string(rune('a' + i)), "out"}, {"shared", "shared"}}})
	}
	started := make(chan string, 32)
	probeGate := make(chan struct{})
	defer close(probeGate)
	applyGate := make(chan struct{})
	defer close(applyGate)
	var active, max, applies atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- MonitorCycle(context.Background(), manifest, func(_ context.Context, _ HealthManifest, m HealthMember, _ string) ProbeResult {
			n := active.Add(1)
			for {
				old := max.Load()
				if old >= n || max.CompareAndSwap(old, n) {
					break
				}
			}
			started <- m.ID
			<-probeGate
			active.Add(-1)
			return ProbeResult{Available: true}
		}, func(context.Context, string, string, string) error {
			if applies.Add(1) == 1 {
				<-applyGate
			}
			return nil
		})
	}()
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		seen[x2Receive(t, started)] = true
	}
	if !seen["a"] || !seen["b"] || !seen["c"] || !seen["d"] {
		t.Fatal("not round robin", seen)
	}
	for i := 0; i < 9; i++ {
		probeGate <- struct{}{}
		if i < 5 {
			seen[x2Receive(t, started)] = true
		}
	}
	if max.Load() != 4 || len(seen) != 9 {
		t.Fatal("probe limit/dedup", max.Load(), seen)
	}
	applyGate <- struct{}{}
	if x2Receive(t, done) != nil || applies.Load() != 8 {
		t.Fatal("lost groups", applies.Load())
	}
}

func TestX2MonitorCancellationDoesNotApplyLateResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	gate := make(chan struct{})
	var calls atomic.Int32
	done := make(chan error, 1)
	manifest := HealthManifest{Groups: []HealthGroup{{Tag: "g", URL: "url", Members: []HealthMember{{"a", "a"}, {"b", "b"}}}}}
	var once sync.Once
	go func() {
		done <- MonitorCycle(ctx, manifest, func(context.Context, HealthManifest, HealthMember, string) ProbeResult {
			once.Do(func() { close(started) })
			<-gate
			return ProbeResult{Available: true}
		}, func(context.Context, string, string, string) error { calls.Add(1); return nil })
	}()
	x2Receive(t, started)
	cancel()
	close(gate)
	if !errors.Is(x2Receive(t, done), context.Canceled) || calls.Load() != 0 {
		t.Fatal("late apply", calls.Load())
	}
}

func TestX2CompileExpansionPreservesSemantics(t *testing.T) {
	root := geoFixture(t)
	db, err := OpenGeoDatabase(root)
	if err != nil {
		t.Fatal(err)
	}
	rules := []Rule{{Domains: []string{"explicit.test"}, Sets: []string{"mixed:service", "geosite:service@extra"}}, {Sets: []string{"geosite:service@extra", "mixed:service"}}, {Sets: []string{"geosite:regex"}}, {Sets: []string{"missing:selector"}}}
	expand := compileExpander(db, rules)
	for _, rule := range rules {
		d, ip, e := ruleMatches(rule, db)
		nextD, nextIP, nextErr := ruleMatchesWith(rule, expand)
		if !reflect.DeepEqual(d, nextD) || !reflect.DeepEqual(ip, nextIP) || (e == nil) != (nextErr == nil) || e != nil && e.Error() != nextErr.Error() {
			t.Fatal("matcher changed", rule)
		}
	}
	c := Config{Rules: []Rule{{Action: "direct", Sets: []string{"geosite:regex"}}, {Action: "zapret", Sets: []string{"geosite:regex"}}}}
	var issue *RuleValidationError
	if err := ValidateRuleSets(c, root); !errors.As(err, &issue) || issue.RuleIndex != 1 {
		t.Fatal("cached compatibility lost Zapret rejection", err)
	}
	// The public database still returns independent mutable slices.
	d, _, _ := db.Expand("geosite:service")
	d[0] = "changed"
	next, _, _ := db.Expand("geosite:service")
	if next[0] == "changed" {
		t.Fatal("public slices shared")
	}
}

func TestX2PriorityAppliesBeforeLowerProbeButStillWaitsForCycle(t *testing.T) {
	manifest := HealthManifest{Groups: []HealthGroup{{Tag: "priority", URL: "same", Mode: "priority", Members: []HealthMember{{"first", "first"}, {"lower", "lower"}}}}}
	lower := make(chan struct{})
	defer close(lower)
	applied := make(chan string, 2)
	done := make(chan error, 1)
	go func() {
		done <- MonitorCycle(context.Background(), manifest, func(_ context.Context, _ HealthManifest, m HealthMember, _ string) ProbeResult {
			if m.ID == "lower" {
				<-lower
			}
			return ProbeResult{Available: true}
		}, func(_ context.Context, _, _, tag string) error { applied <- tag; return nil })
	}()
	if x2Receive(t, applied) != "first" {
		t.Fatal("priority changed")
	}
	select {
	case <-done:
		t.Fatal("cycle omitted lower probe")
	default:
	}
	lower <- struct{}{}
	if x2Receive(t, done) != nil || len(applied) != 0 {
		t.Fatal("reapplied group")
	}
}

func TestX2SavedPairDetectionAndLockResolution(t *testing.T) {
	oldC, oldN, newC, newN := []byte("old-config"), []byte("old-nodes"), []byte("new-config"), []byte("new-nodes")
	for _, tc := range []struct {
		name string
		c, n []byte
		ok   bool
	}{{"old", oldC, oldN, true}, {"new", newC, newN, true}, {"mixed-nodes", oldC, newN, false}, {"mixed-config", newC, oldN, false}, {"unknown", []byte("unexpected"), newN, false}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			config := filepath.Join(root, "config.json")
			marker := filepath.Join(root, SaveIntentFile)
			os.WriteFile(config, tc.c, 0600)
			os.WriteFile(filepath.Join(root, "nodes.json"), tc.n, 0600)
			os.WriteFile(marker, SaveIntentBytes(oldC, oldN, newC, newN), 0600)
			if (CheckSavedPair(config) == nil) != tc.ok {
				t.Fatal("verification")
			}
			unlock, err := LockConfiguration(config, false)
			if (err == nil) != tc.ok {
				t.Fatal(err)
			}
			if unlock != nil {
				unlock()
			}
			_, err = os.Lstat(marker)
			if tc.ok != os.IsNotExist(err) {
				t.Fatal("marker lifecycle", err)
			}
		})
	}
}

func TestX2StatusBoundaries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "status.json")
	s := ActionStatus{Generation: root, PID: os.Getpid(), ProcessStart: ProcessStart(os.Getpid()), State: "error", StartedAt: 1, FinishedAt: 2, Total: 4, Completed: 4, Failed: 1}
	if err := WriteActionStatus(path, s); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadActionStatus(path); err != nil || got != s {
		t.Fatal(got, err)
	}
	for _, data := range [][]byte{[]byte(`{}`), []byte(`[]`), []byte(`{"state":"ok","password":"canary"}`), bytesOfSize(2049), []byte(`{} {}`)} {
		os.WriteFile(path, data, 0600)
		if _, err := ReadActionStatus(path); err == nil {
			t.Fatal("invalid status accepted")
		}
	}
	os.Remove(path)
	os.Symlink(filepath.Join(root, "elsewhere"), path)
	if _, err := ReadActionStatus(path); err == nil {
		t.Fatal("symlink status")
	}
	data, _ := json.Marshal(s)
	if len(data) > 2048 {
		t.Fatal("status not bounded")
	}
}

func TestX2LeaseAndCatalogueLockCancellation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "ui.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if release, err := monitorLease(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatal(err)
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	release, err := monitorLease(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	release()
	catalog := filepath.Join(root, "servers.json")
	lock, err := os.OpenFile(catalog+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := SyncServersContext(ctx, catalog, nil, Config{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("catalogue did not cancel", err)
	}
	if _, err := os.Stat(catalog); !os.IsNotExist(err) {
		t.Fatal("cancelled catalogue published")
	}
}

func TestX2BackupKeepsMixedSaveDetection(t *testing.T) {
	state := s3NewState(t, "")
	root := state.root
	path := s3Path(root, "config.json")
	config := s3Read(t, path)
	nodes := s3Read(t, s3Path(root, "nodes.json"))
	marker := SaveIntentBytes(config, nodes, []byte("new-config"), []byte("new-nodes"))
	s3Write(t, s3Path(root, SaveIntentFile), marker, 0600)
	s3Write(t, s3Path(root, "nodes.json"), []byte("new-nodes"), 0600)
	archive, entries := s3Archive(t, root)
	if !contains(entries, "etc/nadkop/"+SaveIntentFile) {
		t.Fatal("backup omitted save marker")
	}
	restored := s3Restore(t, archive)
	if err := CheckSavedPair(s3Path(restored, "config.json")); err == nil {
		t.Fatal("mixed restored pair not detected")
	}
}

func bytesOfSize(n int) []byte { return make([]byte, n) }
