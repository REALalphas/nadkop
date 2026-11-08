package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func x2GeoFixture(tb testing.TB) string {
	tb.Helper()
	var site, ip []byte
	for i := 0; i < 8; i++ {
		entry := pbString(1, []byte(fmt.Sprintf("set%d", i)))
		ipEntry := pbString(1, []byte(fmt.Sprintf("set%d", i)))
		for n := 0; n < 2048; n++ {
			domain := append(pbInt(1, 2), pbString(2, []byte(fmt.Sprintf("host%d.set%d.example", n, i)))...)
			entry = append(entry, pbString(2, domain)...)
			cidr := append(pbString(1, []byte{10, byte(i), byte(n / 256), byte(n % 256)}), pbInt(2, 32)...)
			ipEntry = append(ipEntry, pbString(2, cidr)...)
		}
		site = append(site, pbString(1, entry)...)
		ip = append(ip, pbString(1, ipEntry)...)
	}
	root := tb.TempDir()
	if err := InstallGeoFiles(root, site, ip); err != nil {
		tb.Fatal(err)
	}
	return root
}

func x2GeoConfig(different bool) Config {
	c := Config{Default: "direct", SOCKSPort: 1080}
	for i := 0; i < 8; i++ {
		set := 0
		if different {
			set = i
		}
		c.Rules = append(c.Rules, Rule{Action: "direct", Sets: []string{fmt.Sprintf("mixed:set%d", set)}})
	}
	return c
}

func BenchmarkX2Geo(b *testing.B) {
	root := x2GeoFixture(b)
	for _, different := range []bool{false, true} {
		c := x2GeoConfig(different)
		b.Run(fmt.Sprintf("different=%v/validate", different), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := ValidateRuleSets(c, root); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("different=%v/compile", different), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				a, err := CompileWithGeo(c, nil, root)
				if err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(a.Xray)))
			}
		})
	}
}

func BenchmarkX2Reconcile(b *testing.B) {
	for _, n := range []int{256, 1024} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			profiles := make([]map[string]any, n)
			for i := range profiles {
				profiles[i] = profile(fmt.Sprint(i), fmt.Sprintf("host%d.example", i), "synthetic")
			}
			data, _ := json.Marshal(profiles)
			nodes, err := ParseSubscription("test", data)
			if err != nil {
				b.Fatal(err)
			}
			path := filepath.Join(b.TempDir(), "servers.json")
			if _, err := SyncServers(path, nodes, Config{}); err != nil {
				b.Fatal(err)
			}
			catalog, err := readCatalog(path)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c := serverCatalog{Version: 1, Servers: append([]serverRecord(nil), catalog.Servers...)}
				if err := reconcileServers(&c, nodes); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestX2ProfileFixture(t *testing.T) {
	if os.Getenv("X2_PROFILE_FIXTURE") == "" {
		t.Skip("explicit profiling fixture only")
	}
	root := x2GeoFixture(t)
	c := x2GeoConfig(os.Getenv("X2_PROFILE_DIFFERENT") == "1")
	operation := os.Getenv("X2_PROFILE_OPERATION")
	runtime.GC()
	var before, after, live runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	var kept any
	var output int
	var err error
	switch operation {
	case "validate":
		err = ValidateRuleSets(c, root)
	case "open":
		kept, err = OpenGeoDatabase(root)
	case "expand":
		var db *GeoDatabase
		db, err = OpenGeoDatabase(root)
		if err == nil {
			var d, ip []string
			d, ip, err = db.Expand("mixed:set0")
			kept = []any{db, d, ip}
		}
	default:
		var a Artifacts
		a, err = CompileWithGeo(c, nil, root)
		kept = a
		output = len(a.Xray)
		if path := os.Getenv("X2_PROFILE_XRAY"); path != "" {
			if e := os.WriteFile(path, a.Xray, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	runtime.GC()
	runtime.ReadMemStats(&live)
	runtime.KeepAlive(kept)
	var usage syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	data, _ := json.Marshal(map[string]any{"operation": operation, "different": os.Getenv("X2_PROFILE_DIFFERENT") == "1", "elapsedNS": elapsed.Nanoseconds(), "totalAlloc": after.TotalAlloc - before.TotalAlloc, "heapAfter": after.HeapAlloc, "heapBaseline": before.HeapAlloc, "liveHeap": live.HeapAlloc, "peakRSSKiB": usage.Maxrss, "outputBytes": output})
	fmt.Println("X2_METRIC " + string(data))
}

func BenchmarkX2Monitor(b *testing.B) {
	manifest := HealthManifest{}
	for i := 0; i < 8; i++ {
		id := fmt.Sprint(i)
		manifest.Groups = append(manifest.Groups, HealthGroup{Tag: id, URL: id, Mode: "priority", Members: []HealthMember{{id, id}}})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := MonitorCycle(context.Background(), manifest, func(context.Context, HealthManifest, HealthMember, string) ProbeResult {
			time.Sleep(time.Millisecond)
			return ProbeResult{Available: true}
		}, func(context.Context, string, string, string) error { time.Sleep(time.Millisecond); return nil })
		if err != nil {
			b.Fatal(err)
		}
	}
}
