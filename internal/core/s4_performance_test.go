package core

// S4 opt-in resource measurements. All fixtures are synthetic and local.
// The runner compiles only production Go files and this file, then runs one
// scenario in each process. No test uses t.Parallel or the global HTTP transport.
import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

type s4IO struct{ WChar, SysCW, WriteBytes int64 }
type s4Stamp struct {
	Exists  bool
	Size    int64
	Inode   uint64
	MtimeNS int64
	SHA256  string
}
type s4Metric struct {
	Case            string         `json:"case"`
	Operation       string         `json:"operation"`
	Iterations      int            `json:"iterations"`
	StartNS         int64          `json:"start_unix_ns"`
	EndNS           int64          `json:"end_unix_ns"`
	WallNS          int64          `json:"wall_ns"`
	CPUUserNS       int64          `json:"cpu_user_ns"`
	CPUSystemNS     int64          `json:"cpu_system_ns"`
	AllocBytes      uint64         `json:"alloc_bytes"`
	Allocs          uint64         `json:"allocs"`
	GC              uint32         `json:"gc_cycles"`
	HeapBefore      uint64         `json:"heap_before_bytes"`
	HeapAfter       uint64         `json:"heap_after_bytes"`
	RSSBefore       int64          `json:"rss_before_kib"`
	RSSAfter        int64          `json:"rss_after_kib"`
	ProcessHWM      int64          `json:"process_high_water_rss_kib"`
	SysCW           int64          `json:"write_syscalls"`
	WChar           int64          `json:"write_chars"`
	WriteBytes      int64          `json:"kernel_write_bytes"`
	Events          map[string]int `json:"inotify_events,omitempty"`
	CatalogueBefore *s4Stamp       `json:"catalogue_before,omitempty"`
	CatalogueAfter  *s4Stamp       `json:"catalogue_after,omitempty"`
	Dimensions      map[string]any `json:"dimensions"`
	Error           string         `json:"error,omitempty"`
}

func s4ReadIO() s4IO {
	data, _ := os.ReadFile("/proc/self/io")
	var v s4IO
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		n, _ := strconv.ParseInt(f[1], 10, 64)
		switch f[0] {
		case "wchar:":
			v.WChar = n
		case "syscw:":
			v.SysCW = n
		case "write_bytes:":
			v.WriteBytes = n
		}
	}
	return v
}
func s4RSS() int64 {
	b, _ := os.ReadFile("/proc/self/statm")
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.ParseInt(f[1], 10, 64)
	return n * int64(os.Getpagesize()) / 1024
}
func s4FileStamp(path string) *s4Stamp {
	s := &s4Stamp{}
	st, err := os.Stat(path)
	if err != nil {
		return s
	}
	s.Exists = true
	s.Size = st.Size()
	s.MtimeNS = st.ModTime().UnixNano()
	if raw, ok := st.Sys().(*syscall.Stat_t); ok {
		s.Inode = raw.Ino
	}
	b, err := os.ReadFile(path)
	if err == nil {
		h := sha256.Sum256(b)
		s.SHA256 = hex.EncodeToString(h[:])
	}
	return s
}
func s4Watch(dir string) (int, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		return -1, err
	}
	_, err = syscall.InotifyAddWatch(fd, dir, syscall.IN_MODIFY|syscall.IN_CREATE|syscall.IN_MOVED_FROM|syscall.IN_MOVED_TO|syscall.IN_DELETE|syscall.IN_CLOSE_WRITE|syscall.IN_ATTRIB)
	if err != nil {
		syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}
func s4Events(fd int) map[string]int {
	result := map[string]int{}
	b := make([]byte, 65536)
	for {
		n, err := syscall.Read(fd, b)
		if n <= 0 || err != nil {
			break
		}
		for pos := 0; pos+syscall.SizeofInotifyEvent <= n; {
			e := (*syscall.InotifyEvent)(unsafe.Pointer(&b[pos]))
			start := pos + syscall.SizeofInotifyEvent
			end := start + int(e.Len)
			if end > n {
				break
			}
			name := strings.TrimRight(string(b[start:end]), "\x00")
			if strings.HasPrefix(name, ".nadkop-servers-") {
				name = ".nadkop-servers-*"
			}
			for _, x := range []struct {
				bit   uint32
				label string
			}{{syscall.IN_MODIFY, "modify"}, {syscall.IN_CREATE, "create"}, {syscall.IN_MOVED_FROM, "move_from"}, {syscall.IN_MOVED_TO, "move_to"}, {syscall.IN_DELETE, "delete"}, {syscall.IN_CLOSE_WRITE, "close_write"}, {syscall.IN_ATTRIB, "attrib"}, {syscall.IN_Q_OVERFLOW, "overflow"}} {
				if e.Mask&x.bit != 0 {
					result[name+":"+x.label]++
				}
			}
			pos = end
		}
	}
	return result
}
func s4Measure(t *testing.T, operation string, iterations int, dims map[string]any, watchDir string, fn func() error) s4Metric {
	t.Helper()
	if dims == nil {
		dims = map[string]any{}
	}
	begin, _ := json.Marshal(map[string]any{"case": os.Getenv("S4_CASE"), "operation": operation, "dimensions": dims})
	fmt.Printf("S4_BEGIN %s\n", begin)
	var fd = -1
	var beforeStamp *s4Stamp
	if watchDir != "" {
		var err error
		fd, err = s4Watch(watchDir)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Close(fd)
		beforeStamp = s4FileStamp(filepath.Join(watchDir, "servers.json"))
	}
	runtime.GC()
	rssBefore := s4RSS()
	ioBefore := s4ReadIO()
	var before, after runtime.MemStats
	var ruBefore, ruAfter syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ruBefore)
	runtime.ReadMemStats(&before)
	start := time.Now()
	var runErr error
	done := 0
	for i := 0; i < iterations; i++ {
		runErr = fn()
		done++
		if runErr != nil {
			break
		}
	}
	end := time.Now()
	runtime.ReadMemStats(&after)
	syscall.Getrusage(syscall.RUSAGE_SELF, &ruAfter)
	ioAfter := s4ReadIO()
	rssAfter := s4RSS()
	m := s4Metric{Case: os.Getenv("S4_CASE"), Operation: operation, Iterations: done, StartNS: start.UnixNano(), EndNS: end.UnixNano(), WallNS: end.Sub(start).Nanoseconds(), CPUUserNS: ruAfter.Utime.Nano() - ruBefore.Utime.Nano(), CPUSystemNS: ruAfter.Stime.Nano() - ruBefore.Stime.Nano(), AllocBytes: after.TotalAlloc - before.TotalAlloc, Allocs: after.Mallocs - before.Mallocs, GC: after.NumGC - before.NumGC, HeapBefore: before.HeapAlloc, HeapAfter: after.HeapAlloc, RSSBefore: rssBefore, RSSAfter: rssAfter, ProcessHWM: ruAfter.Maxrss, SysCW: ioAfter.SysCW - ioBefore.SysCW, WChar: ioAfter.WChar - ioBefore.WChar, WriteBytes: ioAfter.WriteBytes - ioBefore.WriteBytes, Dimensions: dims, CatalogueBefore: beforeStamp}
	if fd >= 0 {
		m.Events = s4Events(fd)
		m.CatalogueAfter = s4FileStamp(filepath.Join(watchDir, "servers.json"))
	}
	if runErr != nil {
		m.Error = runErr.Error()
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("S4_METRIC %s\n", b)
	if runErr != nil {
		t.Fatal(runErr)
	}
	return m
}
func s4NoWrites(t *testing.T, m s4Metric) {
	t.Helper()
	// /proc/self/io also includes runtime eventfd/socket writes. File writes
	// are checked by write_bytes, inotify and unchanged inode/mtime/hash.
	if m.WriteBytes != 0 {
		t.Errorf("unexpected writes in %s: syscw=%d wchar=%d bytes=%d", m.Operation, m.SysCW, m.WChar, m.WriteBytes)
	}
	if m.CatalogueBefore != nil && *m.CatalogueBefore != *m.CatalogueAfter {
		t.Errorf("catalogue changed in %s", m.Operation)
	}
	for event, n := range m.Events {
		if strings.HasSuffix(event, ":modify") || strings.HasSuffix(event, ":move_to") || strings.HasSuffix(event, ":create") || event == ":overflow" {
			t.Errorf("unexpected catalogue event %s=%d", event, n)
		}
	}
}
func s4Fixture(n int, format string) []byte {
	var out bytes.Buffer
	if format == "links" || format == "base64" {
		for i := 0; i < n; i++ {
			fmt.Fprintf(&out, "vless://11111111-1111-4111-8111-%012d@node-%05d.s4.invalid:443?security=tls&type=ws&host=s4.invalid&path=%%2Fs4#S4-%05d\n", i, i, i)
		}
		if format == "base64" {
			return []byte(base64.StdEncoding.EncodeToString(out.Bytes()))
		}
		return out.Bytes()
	}
	out.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			out.WriteByte(',')
		}
		if format == "profiles" {
			fmt.Fprintf(&out, `{"remarks":"S4 server %05d","outbounds":[`, i)
		}
		fmt.Fprintf(&out, `{"tag":"node-%05d","protocol":"trojan","settings":{"servers":[{"address":"node-%05d.s4.invalid","port":443,"password":"synthetic-s4-%05d"}]},"streamSettings":{"network":"ws","security":"tls","tlsSettings":{"serverName":"s4.invalid"},"wsSettings":{"path":"/s4"}}}`, i, i, i)
		if format == "profiles" {
			out.WriteString("]}")
		}
	}
	out.WriteByte(']')
	return out.Bytes()
}
func s4BaseConfig() Config { return Config{Default: "direct", SOCKSPort: 1080, UpdateIntervalHours: 6} }
func s4Rules(n int, nodes []Node, groups bool) []Rule {
	rules := make([]Rule, n)
	for i := range rules {
		rules[i] = Rule{Name: fmt.Sprintf("S4 rule %04d", i), Domains: []string{fmt.Sprintf("r%04d.s4.invalid", i)}, IPs: []string{fmt.Sprintf("198.51.%d.%d/32", (i/256)%256, i%256)}, Action: "block"}
		if i%4 == 0 && len(nodes) > 0 {
			rules[i].Action = "proxy"
			rules[i].NodeRef = nodes[i%len(nodes)].serverID
		}
	}
	if groups {
		for g := 0; g < 16; g++ {
			r := &rules[g]
			r.Action = "proxy"
			r.NodeRef = ""
			r.Node = ""
			r.Selection = &SelectionSettings{Mode: "priority"}
			for j := 0; j < 16; j++ {
				r.NodeRefs = append(r.NodeRefs, nodes[(g%4)*16+j].serverID)
			}
		}
	}
	return rules
}
func s4CheckedConfig(t *testing.T, c Config) Config {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	c, err = ReadConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func s4ArtifactsSize(a Artifacts) int {
	n := 0
	for _, b := range a.Files() {
		n += len(b)
	}
	return n
}

func s4Scale(t *testing.T, n int, format string, maximum bool) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")
	payload := s4Fixture(n, format)
	sourceFile := filepath.Join(dir, "subscription.json")
	if err := os.WriteFile(sourceFile, payload, 0600); err != nil {
		t.Fatal(err)
	}
	source := Source{Name: "S4", File: sourceFile}
	if format == "links" || format == "base64" {
		source.Format = "links"
	}
	cfg := s4BaseConfig()
	cfg.Sources = []Source{source}
	dims := map[string]any{"nodes": n, "format": format, "subscription_bytes": len(payload), "sources": 1}
	var nodes []Node
	s4Measure(t, "parse_subscription", 3, dims, "", func() error {
		var err error
		nodes, err = ParseSubscription(source.Name, payload)
		if err == nil && len(nodes) != n {
			return fmt.Errorf("node count %d", len(nodes))
		}
		return err
	})
	s4Measure(t, "load_local_subscription", 3, dims, "", func() error {
		var err error
		nodes, err = loadContext(context.Background(), source, nil, filepath.Join(dir, "synthetic-hwid"), DeviceInfo{})
		return err
	})
	if maximum {
		nodes = nil
		cfg.Sources = nil
		for i := 0; i < 16; i++ {
			b := s4Fixture(256, "json")
			f := filepath.Join(dir, fmt.Sprintf("subscription-%02d.json", i))
			if err := os.WriteFile(f, b, 0600); err != nil {
				t.Fatal(err)
			}
			s := Source{Name: fmt.Sprintf("S4_%02d", i), File: f}
			cfg.Sources = append(cfg.Sources, s)
			ns, err := ParseSubscription(s.Name, b)
			if err != nil {
				t.Fatal(err)
			}
			nodes = append(nodes, ns...)
		}
		dims["sources"] = 16
		dims["subscription_bytes_total"] = len(s4Fixture(256, "json")) * 16
	}
	var summaries []ServerSummary
	s4Measure(t, "sync_first", 1, dims, dir, func() error {
		var err error
		summaries, err = SyncServers(path, nodes, cfg)
		if err == nil && len(summaries) != n {
			return fmt.Errorf("catalogue size %d", len(summaries))
		}
		return err
	})
	ruleCount := map[int]int{16: 8, 256: 64, 1024: 256, 2048: 512, 4096: 1024}[n]
	if ruleCount == 0 {
		ruleCount = 64
	}
	cfg.Rules = s4Rules(ruleCount, nodes, maximum)
	cfg = s4CheckedConfig(t, cfg)
	configBytes, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), configBytes, 0600); err != nil {
		t.Fatal(err)
	}
	dims["rules"] = ruleCount
	dims["config_bytes"] = len(configBytes)
	dims["catalogue_bytes"] = s4FileStamp(path).Size
	if maximum {
		dims["groups"] = 16
		dims["checked_refs"] = 64
		dims["members_per_group"] = 16
	}
	m := s4Measure(t, "sync_unchanged", 3, dims, dir, func() error { _, err := SyncServers(path, nodes, cfg); return err })
	s4NoWrites(t, m)
	var a Artifacts
	m = s4Measure(t, "compile", 3, dims, dir, func() error {
		var err error
		a, err = CompileWithGeo(cfg, nodes, filepath.Join(dir, "no-geodata"))
		return err
	})
	s4NoWrites(t, m)
	dims["artifact_bytes"] = s4ArtifactsSize(a)
	dims["xray_bytes"] = len(a.Xray)
	if maximum {
		var h HealthManifest
		if err := json.Unmarshal(a.Health, &h); err != nil {
			t.Fatal(err)
		}
		refs := map[string]bool{}
		for _, g := range h.Groups {
			for _, m := range g.Members {
				refs[m.ID] = true
			}
		}
		if len(h.Groups) != 16 || len(refs) != 64 {
			t.Fatal("wrong maximum health manifest")
		}
		ordered := make([]string, 0, 64)
		for ref := range refs {
			ordered = append(ordered, ref)
		}
		sort.Strings(ordered)
		s4Measure(t, "compile_probe_64", 3, dims, dir, func() error {
			p, err := CompileProbe(nodes, ordered, 29876)
			if err == nil && len(p.Members) != 64 {
				return fmt.Errorf("probe members %d", len(p.Members))
			}
			return err
		})
	}
	m = s4Measure(t, "refresh_local_load_sync_compile", 3, dims, dir, func() error {
		var err error
		var fresh []Node
		for _, s := range cfg.Sources {
			ns, e := loadContext(context.Background(), s, nil, filepath.Join(dir, "synthetic-hwid"), DeviceInfo{})
			if e != nil {
				return e
			}
			fresh = append(fresh, ns...)
		}
		if _, err = SyncServers(path, fresh, cfg); err != nil {
			return err
		}
		a, err = CompileWithGeo(cfg, fresh, filepath.Join(dir, "no-geodata"))
		return err
	})
	s4NoWrites(t, m)
	// Rotation changes private matching metadata once while retaining stable refs.
	oldRef := nodes[0].serverID
	nodes[0].Settings = bytes.Replace(nodes[0].Settings, []byte("synthetic-s4-00000"), []byte("synthetic-s4-rotated"), 1)
	if format == "json" || format == "profiles" || maximum {
		s4Measure(t, "sync_one_credential_rotation", 1, dims, dir, func() error { _, err := SyncServers(path, nodes, cfg); return err })
		if nodes[0].serverID != oldRef {
			t.Fatal("credential rotation changed stable ref")
		}
		m = s4Measure(t, "sync_unchanged_after_rotation", 2, dims, dir, func() error { _, err := SyncServers(path, nodes, cfg); return err })
		s4NoWrites(t, m)
	}
	runtime.KeepAlive(a)
}

func s4ExpectRead(t *testing.T, name string, c Config, wantError bool) {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	d := map[string]any{"input_bytes": len(b), "expected_rejection": wantError}
	s4Measure(t, name, 1, d, "", func() error {
		_, err := ReadConfig(bytes.NewReader(b))
		if (err != nil) != wantError {
			return fmt.Errorf("rejection=%v, want=%v: %v", err != nil, wantError, err)
		}
		if err != nil {
			d["rejection"] = err.Error()
		}
		return nil
	})
}
func s4Boundaries(t *testing.T) {
	dir := t.TempDir()
	base := s4BaseConfig()
	refs := make([]Node, 65)
	for i := range refs {
		refs[i].serverID = fmt.Sprintf("srv_%032x", i+1)
	}
	for _, n := range []int{1024, 1025} {
		c := base
		c.Rules = s4Rules(n, refs, false)
		s4ExpectRead(t, fmt.Sprintf("rules_%d", n), c, n > 1024)
	}
	for _, n := range []int{16, 17} {
		c := base
		for i := 0; i < n; i++ {
			c.Sources = append(c.Sources, Source{Name: fmt.Sprintf("S4_%d", i), File: filepath.Join(dir, fmt.Sprintf("s%d.json", i))})
		}
		s4ExpectRead(t, fmt.Sprintf("sources_%d", n), c, n > 16)
	}
	for _, n := range []int{16, 17} {
		c := base
		c.Rules = []Rule{{Action: "proxy", Domains: []string{"s4.invalid"}}}
		for i := 0; i < n; i++ {
			c.Rules[0].NodeRefs = append(c.Rules[0].NodeRefs, refs[i].serverID)
		}
		s4ExpectRead(t, fmt.Sprintf("group_members_%d", n), c, n > 16)
	}
	for _, n := range []int{16, 17} {
		c := base
		c.Rules = s4Rules(n, refs, false)
		for i := range c.Rules {
			c.Rules[i].Action = "proxy"
			c.Rules[i].NodeRef = ""
			c.Rules[i].NodeRefs = []string{refs[0].serverID, refs[1].serverID}
		}
		s4ExpectRead(t, fmt.Sprintf("groups_%d", n), c, n > 16)
	}
	for _, n := range []int{64, 65} {
		c := base
		c.Rules = s4Rules(5, refs, false)
		for g := range c.Rules {
			r := &c.Rules[g]
			r.Action = "proxy"
			r.NodeRef = ""
			for j := g * 13; j < (g+1)*13 && j < n; j++ {
				r.NodeRefs = append(r.NodeRefs, refs[j].serverID)
			}
		}
		s4ExpectRead(t, fmt.Sprintf("checked_refs_%d", n), c, n > 64)
	}
	for _, n := range []int{4096, 4097} {
		c := base
		for i := 0; i < n; i++ {
			c.Picker.Pinned = append(c.Picker.Pinned, fmt.Sprintf("srv_%032x", i+1))
		}
		s4ExpectRead(t, fmt.Sprintf("picker_refs_%d", n), c, n > 4096)
	}
	for _, n := range []int{16384, 16385} {
		c := base
		c.Rules = []Rule{{Action: "block"}}
		for i := 0; i < n; i++ {
			c.Rules[0].Domains = append(c.Rules[0].Domains, fmt.Sprintf("d%d.s4.invalid", i))
		}
		s4ExpectRead(t, fmt.Sprintf("matchers_%d", n), c, n > 16384)
	}
	for _, n := range []int{64, 65} {
		c := base
		c.Rules = []Rule{{Action: "block"}}
		for i := 0; i < n; i++ {
			c.Rules[0].Sets = append(c.Rules[0].Sets, fmt.Sprintf("geosite:s4-%d", i))
		}
		s4ExpectRead(t, fmt.Sprintf("rule_sets_%d", n), c, n > 64)
	}
	config, _ := json.Marshal(base)
	for _, size := range []int{maxSubscription, maxSubscription + 1} {
		b := append(append([]byte{}, config...), bytes.Repeat([]byte(" "), size-len(config))...)
		d := map[string]any{"input_bytes": size, "expected_rejection": size > maxSubscription}
		s4Measure(t, fmt.Sprintf("config_bytes_%d", size), 1, d, "", func() error {
			_, err := ReadConfig(bytes.NewReader(b))
			if (err != nil) != (size > maxSubscription) {
				return fmt.Errorf("unexpected size result: %v", err)
			}
			if err != nil {
				d["rejection"] = err.Error()
			}
			return nil
		})
	}
	one := s4Fixture(1, "json")
	source := Source{Name: "S4", File: filepath.Join(dir, "s4.json")}
	for _, size := range []int{maxSubscription, maxSubscription + 1} {
		b := append(append([]byte{}, one...), bytes.Repeat([]byte(" "), size-len(one))...)
		if err := os.WriteFile(source.File, b, 0600); err != nil {
			t.Fatal(err)
		}
		d := map[string]any{"input_bytes": size, "expected_rejection": size > maxSubscription}
		s4Measure(t, fmt.Sprintf("subscription_bytes_%d", size), 1, d, "", func() error {
			_, err := loadContext(context.Background(), source, nil, filepath.Join(dir, "synthetic-hwid"), DeviceInfo{})
			if (err != nil) != (size > maxSubscription) {
				return fmt.Errorf("unexpected size result: %v", err)
			}
			if err != nil {
				d["rejection"] = err.Error()
			}
			return nil
		})
	}
	for _, depth := range []int{10000, 10001} {
		b := []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
		d := map[string]any{"depth": depth, "input_bytes": len(b)}
		s4Measure(t, fmt.Sprintf("nested_json_%d", depth), 1, d, "", func() error {
			_, err := ParseSubscription("S4", b)
			if err == nil {
				return fmt.Errorf("nested arrays accepted as nodes")
			}
			d["rejection"] = err.Error()
			return nil
		})
	}
}
func s4CatalogueLimit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "servers.json")
	payload := s4Fixture(4096, "json")
	nodes, err := ParseSubscription("S4", payload)
	if err != nil {
		t.Fatal(err)
	}
	c := s4BaseConfig()
	d := map[string]any{"nodes": 4096, "subscription_bytes": len(payload)}
	s4Measure(t, "catalogue_4096", 1, d, dir, func() error { _, err := SyncServers(p, nodes, c); return err })
	before := s4FileStamp(p)
	over, err := ParseSubscription("S4", s4Fixture(4097, "json"))
	if err != nil {
		t.Fatal(err)
	}
	d = map[string]any{"nodes": 4097, "expected_rejection": true}
	m := s4Measure(t, "catalogue_4097_rejected", 1, d, dir, func() error {
		_, err := SyncServers(p, over, c)
		if err == nil {
			return fmt.Errorf("4097 entries accepted")
		}
		d["rejection"] = err.Error()
		return nil
	})
	s4NoWrites(t, m)
	if *before != *s4FileStamp(p) {
		t.Fatal("rejected catalogue expansion changed disk")
	}
}
func s4PB(n int, b []byte) []byte {
	out := binary.AppendUvarint(nil, uint64(n<<3|2))
	out = binary.AppendUvarint(out, uint64(len(b)))
	return append(out, b...)
}
func s4PBInt(n int, v uint64) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(nil, uint64(n<<3)), v)
}
func s4TinyGeo() ([]byte, []byte) {
	domain := append(s4PBInt(1, 2), s4PB(2, []byte("s4.invalid"))...)
	site := s4PB(1, append(s4PB(1, []byte("S4")), s4PB(2, domain)...))
	cidr := append(s4PB(1, []byte{192, 0, 2, 1}), s4PBInt(2, 32)...)
	ip := s4PB(1, append(s4PB(1, []byte("S4")), s4PB(2, cidr)...))
	return site, ip
}
func s4GeoBytes(t *testing.T) {
	dir := t.TempDir()
	site, ip := s4TinyGeo()
	// Unknown length-delimited top-level field is valid protobuf padding.
	pad := maxGeoFile - len(site) - 5
	large := append(append([]byte{}, site...), s4PB(15, make([]byte, pad))...)
	if len(large) != maxGeoFile {
		t.Fatalf("geo fixture size=%d", len(large))
	}
	d := map[string]any{"geosite_bytes": len(large), "geoip_bytes": len(ip), "padding": "valid unknown protobuf field; not a dense real dataset"}
	s4Measure(t, "install_geo_exact_64MiB", 1, d, "", func() error { return InstallGeoFiles(dir, large, ip) })
	var db *GeoDatabase
	s4Measure(t, "open_geo_exact_64MiB", 1, d, "", func() error { var err error; db, err = OpenGeoDatabase(dir); return err })
	runtime.KeepAlive(db)
	before, _ := os.Readlink(filepath.Join(dir, "current"))
	large = append(large, 0)
	d2 := map[string]any{"geosite_bytes": len(large), "expected_rejection": true}
	s4Measure(t, "install_geo_over_64MiB", 1, d2, "", func() error {
		err := InstallGeoFiles(dir, large, ip)
		if err == nil {
			return fmt.Errorf("oversize geo accepted")
		}
		d2["rejection"] = err.Error()
		return nil
	})
	after, _ := os.Readlink(filepath.Join(dir, "current"))
	if before != after {
		t.Fatal("oversize install published")
	}
	f, err := os.OpenFile(filepath.Join(dir, before, "geosite.dat"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate(maxGeoFile + 1)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	s4Measure(t, "open_geo_over_64MiB", 1, d2, "", func() error {
		_, err := OpenGeoDatabase(dir)
		if err == nil {
			return fmt.Errorf("oversize open accepted")
		}
		d2["open_rejection"] = err.Error()
		return nil
	})
}
func s4GeoEntries(t *testing.T) {
	_, ip := s4TinyGeo()
	domain := append(s4PBInt(1, 2), s4PB(2, []byte("s4.invalid"))...)
	for _, n := range []int{20000, 20001} {
		var data []byte
		for i := 0; i < n; i++ {
			e := append(s4PB(1, []byte(fmt.Sprintf("S4-%05d", i))), s4PB(2, domain)...)
			data = append(data, s4PB(1, e)...)
		}
		d := map[string]any{"entries": n, "input_bytes": len(data), "expected_rejection": n > 20000}
		s4Measure(t, fmt.Sprintf("geo_entries_%d", n), 1, d, "", func() error {
			_, err := parseGeo(data, "geosite")
			if (err != nil) != (n > 20000) {
				return fmt.Errorf("unexpected count result: %v", err)
			}
			if err != nil {
				d["rejection"] = err.Error()
			}
			return nil
		})
	}
	for i, b := range [][]byte{{0x0a, 0xff, 0xff, 0xff, 0xff, 0x7f}, {0x0a, 0x08, 0x0a, 0x7f}, {0x00}, {0x0a, 0x00}} {
		d := map[string]any{"input_bytes": len(b), "expected_rejection": true}
		s4Measure(t, fmt.Sprintf("geo_malformed_%d", i), 1, d, "", func() error {
			_, err := parseGeo(b, "geosite")
			if err == nil {
				return fmt.Errorf("invalid protobuf accepted")
			}
			d["rejection"] = err.Error()
			return nil
		})
	}
	runtime.KeepAlive(ip)
}
func s4RealGeoRoot(t *testing.T) string {
	t.Helper()
	source := os.Getenv("S4_GEO_DIR")
	if source == "" {
		t.Skip("S4_GEO_DIR not set")
	}
	root := t.TempDir()
	site, err := os.ReadFile(filepath.Join(source, "geosite.dat"))
	if err != nil {
		t.Fatal(err)
	}
	ip, err := os.ReadFile(filepath.Join(source, "geoip.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if err := InstallGeoFiles(root, site, ip); err != nil {
		t.Fatal(err)
	}
	return root
}
func s4Largest(db *GeoDatabase) (string, string, int, int) {
	site, ip := "", ""
	ns, ni := 0, 0
	for k, v := range db.site {
		if v.count > ns || (v.count == ns && k < site) {
			site = k
			ns = v.count
		}
	}
	for k, v := range db.ip {
		if !v.inverse && (v.count > ni || (v.count == ni && k < ip)) {
			ip = k
			ni = v.count
		}
	}
	return "geosite:" + site, "geoip:" + ip, ns, ni
}
func s4GeoReal(t *testing.T, install bool) {
	source := os.Getenv("S4_GEO_DIR")
	root := s4RealGeoRoot(t)
	siteSt, err := os.Stat(filepath.Join(source, "geosite.dat"))
	if err != nil {
		t.Fatal(err)
	}
	ipSt, err := os.Stat(filepath.Join(source, "geoip.dat"))
	if err != nil {
		t.Fatal(err)
	}
	d := map[string]any{"geosite_bytes": siteSt.Size(), "geoip_bytes": ipSt.Size()}
	var db *GeoDatabase
	if install {
		site, err := os.ReadFile(filepath.Join(source, "geosite.dat"))
		if err != nil {
			t.Fatal(err)
		}
		ip, err := os.ReadFile(filepath.Join(source, "geoip.dat"))
		if err != nil {
			t.Fatal(err)
		}
		root = t.TempDir()
		s4Measure(t, "install_real_geo", 1, d, "", func() error { return InstallGeoFiles(root, site, ip) })
		before, _ := os.Readlink(filepath.Join(root, "current"))
		s4Measure(t, "install_identical_real_geo", 1, d, "", func() error { return InstallGeoFiles(root, site, ip) })
		after, _ := os.Readlink(filepath.Join(root, "current"))
		d["identical_install_changed_generation"] = before != after
		dirs, _ := filepath.Glob(filepath.Join(root, "generation.*"))
		d["retained_generations"] = len(dirs)
	}
	s4Measure(t, "open_real_geo", 2, d, "", func() error { var err error; db, err = OpenGeoDatabase(root); return err })
	var catalog []GeoItem
	s4Measure(t, "geo_catalogue", 2, d, "", func() error { catalog = db.Catalogue(); d["catalogue_items"] = len(catalog); return nil })
	d["catalogue_items"] = len(catalog)
	site, ip, ns, ni := s4Largest(db)
	d["largest_geosite"] = site
	d["largest_geosite_count"] = ns
	d["largest_geoip"] = ip
	d["largest_geoip_count"] = ni
	for _, selector := range []string{"geosite:github", "geosite:youtube", "mixed:telegram", "geoip:ru", site, ip} {
		sel := selector
		dim := map[string]any{"selector": sel}
		var domains, ips []string
		s4Measure(t, "expand_real_geo", 2, dim, "", func() error {
			var err error
			domains, ips, err = db.Expand(sel)
			if err == nil {
				dim["domains"] = len(domains)
				dim["ips"] = len(ips)
			}
			return err
		})
		runtime.KeepAlive(domains)
		runtime.KeepAlive(ips)
	}
}
func s4GeoFanout(t *testing.T, n int) {
	root := s4RealGeoRoot(t)
	db, err := OpenGeoDatabase(root)
	if err != nil {
		t.Fatal(err)
	}
	site, ip, ns, ni := s4Largest(db)
	cfg := s4BaseConfig()
	for i := 0; i < n; i++ {
		cfg.Rules = append(cfg.Rules, Rule{Name: fmt.Sprintf("S4 geo %d", i), Action: "block", Sets: []string{site, ip}})
	}
	cfg = s4CheckedConfig(t, cfg)
	b, _ := json.Marshal(cfg)
	d := map[string]any{"rules": n, "config_bytes": len(b), "selectors_per_rule": 2, "geosite": site, "geosite_entries_per_rule": ns, "geoip": ip, "geoip_entries_per_rule": ni, "expanded_input_entries_total": int64(n) * int64(ns+ni)}
	db = nil
	runtime.GC()
	var a Artifacts
	s4Measure(t, "compile_geo_fanout", 1, d, "", func() error {
		var err error
		a, err = CompileWithGeo(cfg, nil, root)
		if err == nil {
			d["artifact_bytes"] = s4ArtifactsSize(a)
			d["xray_bytes"] = len(a.Xray)
		}
		return err
	})
	runtime.KeepAlive(a)
}

// A loopback-only SOCKS5/auth + HTTP 204 responder. It does no DNS lookup,
// does not dial any destination, and is not an Xray/VPN compatibility test.
func s4SOCKS(t *testing.T) (string, func(), *atomic.Int64, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	active := new(atomic.Int64)
	peak := new(atomic.Int64)
	requests := new(atomic.Int64)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				a := active.Add(1)
				defer active.Add(-1)
				for {
					p := peak.Load()
					if a <= p || peak.CompareAndSwap(p, a) {
						break
					}
				}
				var h [4]byte
				if _, err := io.ReadFull(c, h[:2]); err != nil || h[0] != 5 {
					return
				}
				methods := make([]byte, int(h[1]))
				if _, err := io.ReadFull(c, methods); err != nil {
					return
				}
				if _, err := c.Write([]byte{5, 2}); err != nil {
					return
				}
				if _, err := io.ReadFull(c, h[:2]); err != nil || h[0] != 1 {
					return
				}
				if _, err := io.CopyN(io.Discard, c, int64(h[1])); err != nil {
					return
				}
				if _, err := io.ReadFull(c, h[:1]); err != nil {
					return
				}
				if _, err := io.CopyN(io.Discard, c, int64(h[0])); err != nil {
					return
				}
				if _, err := c.Write([]byte{1, 0}); err != nil {
					return
				}
				if _, err := io.ReadFull(c, h[:4]); err != nil || h[0] != 5 || h[1] != 1 {
					return
				}
				size := 0
				switch h[3] {
				case 1:
					size = 4
				case 4:
					size = 16
				case 3:
					if _, err := io.ReadFull(c, h[:1]); err != nil {
						return
					}
					size = int(h[0])
				default:
					return
				}
				if _, err := io.CopyN(io.Discard, c, int64(size+2)); err != nil {
					return
				}
				if _, err := c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
					return
				}
				req, err := http.ReadRequest(bufio.NewReader(c))
				if err != nil {
					return
				}
				req.Body.Close()
				requests.Add(1)
				time.Sleep(2 * time.Millisecond)
				io.WriteString(c, "HTTP/1.1 204 No Content\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
			}()
		}
	}()
	return ln.Addr().String(), func() { ln.Close(); wg.Wait() }, active, peak, requests
}
func s4Monitor(t *testing.T) {
	addr, stop, _, peak, requests := s4SOCKS(t)
	defer stop()
	manifest := HealthManifest{API: "synthetic-apply-stub", SOCKS: addr, Password: "synthetic-s4"}
	for g := 0; g < 16; g++ {
		group := HealthGroup{Tag: fmt.Sprintf("s4-group-%d", g), Mode: "priority", URL: "http://127.0.0.1/generate_204"}
		for j := 0; j < 16; j++ {
			i := (g%4)*16 + j
			group.Members = append(group.Members, HealthMember{ID: fmt.Sprintf("srv_%032x", i+1), Tag: fmt.Sprintf("S4/node-%d", i)})
		}
		manifest.Groups = append(manifest.Groups, group)
	}
	for _, unique := range []bool{false, true} {
		if unique {
			for g := range manifest.Groups {
				manifest.Groups[g].URL = fmt.Sprintf("http://127.0.0.1/generate_204?group=%d", g)
			}
		}
		d := map[string]any{"groups": 16, "checked_refs": 64, "members_per_group": 16, "distinct_urls": unique, "responder_delay_ms": 2, "network": "real loopback SOCKS5/auth and HTTP; synthetic responder, apply callback stub"}
		startRequests := requests.Load()
		peak.Store(0)
		applied := 0
		var probeActive, probePeak atomic.Int64
		countedProbe := func(ctx context.Context, m HealthManifest, member HealthMember, target string) ProbeResult {
			n := probeActive.Add(1)
			defer probeActive.Add(-1)
			for {
				old := probePeak.Load()
				if n <= old || probePeak.CompareAndSwap(old, n) {
					break
				}
			}
			return Probe(ctx, m, member, target)
		}
		s4Measure(t, "monitor_cycle_loopback", 1, d, "", func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := MonitorCycle(ctx, manifest, countedProbe, func(_ context.Context, _, _, tag string) error {
				applied++
				if tag == "block" {
					return fmt.Errorf("all local probes failed")
				}
				return nil
			})
			d["http_requests"] = requests.Load() - startRequests
			d["peak_responder_handlers"] = peak.Load()
			d["peak_active_probe_callbacks"] = probePeak.Load()
			d["applied_groups"] = applied
			return err
		})
		expected := int64(64)
		if unique {
			expected = 256
		}
		if requests.Load()-startRequests != expected || applied != 16 || probePeak.Load() > 4 {
			t.Fatalf("wrong monitor counts: requests=%d applied=%d peak=%d", requests.Load()-startRequests, applied, probePeak.Load())
		}
	}
}

// A legal sub-4MiB source may expand into a larger serialized catalogue because
// each display tag is retained in name, tag and aliases. Require rejection
// before publication, preserving the previous readable catalogue and IDs.
func s4CatalogueByteBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")
	payload := s4Fixture(4096, "json")
	payload = bytes.ReplaceAll(payload, []byte(`"tag":"node-`), []byte(`"tag":"`+strings.Repeat("s4", 256)+"-node-"))
	if len(payload) > maxSubscription {
		t.Fatalf("fixture exceeds source budget: %d", len(payload))
	}
	source := Source{Name: "S4", File: filepath.Join(dir, "source.json")}
	if err := os.WriteFile(source.File, payload, 0600); err != nil {
		t.Fatal(err)
	}
	var nodes []Node
	dims := map[string]any{"nodes": 4096, "subscription_bytes": len(payload), "tag_prefix_bytes": 513, "source_budget_bytes": maxSubscription}
	s4Measure(t, "load_legal_long_tags", 1, dims, "", func() error {
		var err error
		nodes, err = loadContext(context.Background(), source, nil, filepath.Join(dir, "synthetic-hwid"), DeviceInfo{})
		return err
	})
	c := s4BaseConfig()
	c.Sources = []Source{source}
	// Seed a small readable catalogue, then require a budget failure to preserve it.
	seed, err := ParseSubscription("S4", s4Fixture(1, "json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = SyncServers(path, seed, c); err != nil {
		t.Fatal(err)
	}
	before := s4FileStamp(path)
	s4Measure(t, "reject_oversized_legal_long_tags", 1, dims, dir, func() error {
		_, err := SyncServers(path, nodes, c)
		if err == nil || !strings.Contains(err.Error(), "exceeds 4 MiB") {
			return fmt.Errorf("expected catalogue byte budget rejection: %v", err)
		}
		dims["expected_rejection"] = err.Error()
		return nil
	})
	if after := s4FileStamp(path); *after != *before {
		t.Fatal("rejected refresh changed previous catalogue")
	}
	s4Measure(t, "catalogue_reopen_and_valid_retry", 1, dims, dir, func() error {
		if _, err := ServerSummaries(path); err != nil {
			return err
		}
		_, err := SyncServers(path, seed, c)
		return err
	})
	if after := s4FileStamp(path); *after != *before {
		t.Fatal("valid identical retry changed catalogue")
	}

}
func s4DenseRules(t *testing.T) {
	cfg := s4BaseConfig()
	const rules = 1024
	const domains = 128
	for i := 0; i < rules; i++ {
		r := Rule{Name: fmt.Sprintf("S4 dense %d", i), Action: "block", IPs: []string{fmt.Sprintf("198.51.%d.%d/32", i/256, i%256)}}
		for j := 0; j < domains; j++ {
			r.Domains = append(r.Domains, fmt.Sprintf("r%04dd%03d.s4.invalid", i, j))
		}
		cfg.Rules = append(cfg.Rules, r)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxSubscription {
		t.Fatalf("dense config too large: %d", len(b))
	}
	d := map[string]any{"rules": rules, "domains_per_rule": domains, "domain_matchers": rules * domains, "ip_matchers": rules, "config_bytes": len(b)}
	s4Measure(t, "read_dense_config", 2, d, "", func() error { var err error; cfg, err = ReadConfig(bytes.NewReader(b)); return err })
	var a Artifacts
	s4Measure(t, "compile_dense_config", 2, d, "", func() error {
		var err error
		a, err = CompileWithGeo(cfg, nil, "")
		if err == nil {
			d["artifact_bytes"] = s4ArtifactsSize(a)
			d["xray_bytes"] = len(a.Xray)
		}
		return err
	})
	runtime.KeepAlive(a)
}

// Measure the aggregate wire-byte budget with only two concurrent loaders.
// Each source has 256 nodes and JSON whitespace padding to exactly 4MiB;
// this is a wire-size boundary, not 64MiB of dense outbound objects.
func s4HTTPSBudget(t *testing.T) {
	dir := t.TempDir()
	catalogue := filepath.Join(dir, "servers.json")
	body := s4Fixture(256, "json")
	body = append(body, bytes.Repeat([]byte(" "), maxSubscription-len(body))...)
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()
	client := server.Client()
	defer client.CloseIdleConnections()
	cfg := s4BaseConfig()
	for i := 0; i < 16; i++ {
		cfg.Sources = append(cfg.Sources, Source{Name: fmt.Sprintf("S4_%02d", i), URL: fmt.Sprintf("%s/source-%02d", server.URL, i)})
	}
	var nodes []Node
	load := func() error {
		results := make([][]Node, len(cfg.Sources))
		failures := make([]error, len(cfg.Sources))
		jobs := make(chan int, len(cfg.Sources))
		for i := range cfg.Sources {
			jobs <- i
		}
		close(jobs)
		var wg sync.WaitGroup
		for worker := 0; worker < 2; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					results[i], failures[i] = loadContext(context.Background(), cfg.Sources[i], client.Transport, filepath.Join(dir, "synthetic-hwid"), DeviceInfo{})
				}
			}()
		}
		wg.Wait()
		nodes = nil
		for i, result := range results {
			if failures[i] != nil {
				return failures[i]
			}
			nodes = append(nodes, result...)
		}
		if len(nodes) != 4096 {
			return fmt.Errorf("wrong aggregate count %d", len(nodes))
		}
		return nil
	}
	d := map[string]any{"sources": 16, "nodes": 4096, "nodes_per_source": 256, "input_bytes_per_source": maxSubscription, "input_bytes_total": 16 * maxSubscription, "load_workers": 2, "input_shape": "Xray JSON plus whitespace padding", "network": "real loopback HTTPS with per-test trusted TLS transport; no global HTTP transport changes"}
	s4Measure(t, "load_16_https_max_bytes", 1, d, "", load)
	if _, err := SyncServers(catalogue, nodes, cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Rules = s4Rules(1024, nodes, true)
	cfg = s4CheckedConfig(t, cfg)
	b, _ := json.Marshal(cfg)
	d["config_bytes"] = len(b)
	d["rules"] = 1024
	d["groups"] = 16
	d["checked_refs"] = 64
	d["catalogue_bytes"] = s4FileStamp(catalogue).Size
	var a Artifacts
	beforeRequests := requests.Load()
	m := s4Measure(t, "refresh_16_https_max_bytes_unchanged", 2, d, dir, func() error {
		if err := load(); err != nil {
			return err
		}
		if _, err := SyncServers(catalogue, nodes, cfg); err != nil {
			return err
		}
		var err error
		a, err = CompileWithGeo(cfg, nodes, "")
		if err == nil {
			d["artifact_bytes"] = s4ArtifactsSize(a)
		}
		return err
	})
	s4NoWrites(t, m)
	if requests.Load()-beforeRequests != 32 {
		t.Fatalf("wrong HTTP request count %d", requests.Load()-beforeRequests)
	}
	runtime.KeepAlive(a)
}

func TestS4Performance(t *testing.T) {
	c := os.Getenv("S4_CASE")
	if c == "" {
		t.Skip("opt-in: scripts/test-s4-performance.py")
	}
	if runtime.GOMAXPROCS(0) > 2 {
		t.Fatal("S4 requires GOMAXPROCS<=2")
	}
	switch {
	case strings.HasPrefix(c, "scale-"):
		n, err := strconv.Atoi(strings.TrimPrefix(c, "scale-"))
		if err != nil || n > 4096 || n < 1 {
			t.Fatal("invalid scale")
		}
		s4Scale(t, n, "json", false)
	case c == "maximum":
		s4Scale(t, 4096, "json", true)
	case c == "profiles-4096":
		s4Scale(t, 4096, "profiles", false)
	case c == "links-4096":
		s4Scale(t, 4096, "links", false)
	case c == "base64-4096":
		s4Scale(t, 4096, "base64", false)
	case c == "boundaries":
		s4Boundaries(t)
	case c == "catalogue-limit":
		s4CatalogueLimit(t)
	case c == "catalogue-byte-budget":
		s4CatalogueByteBudget(t)
	case c == "rules-dense":
		s4DenseRules(t)
	case c == "https-byte-budget":
		s4HTTPSBudget(t)
	case c == "geo-bytes":
		s4GeoBytes(t)
	case c == "geo-entries":
		s4GeoEntries(t)
	case c == "geo-real":
		s4GeoReal(t, false)
	case c == "geo-install":
		s4GeoReal(t, true)
	case strings.HasPrefix(c, "geo-fanout-"):
		n, err := strconv.Atoi(strings.TrimPrefix(c, "geo-fanout-"))
		if err != nil || n > 1024 || n < 1 {
			t.Fatal("invalid geo scale")
		}
		s4GeoFanout(t, n)
	case c == "monitor-loopback":
		s4Monitor(t)
	default:
		t.Fatalf("unknown S4_CASE %q", c)
	}
}
