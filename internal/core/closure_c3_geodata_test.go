package core

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func closureC3Pair(domain string, octet byte) ([]byte, []byte) {
	d := append(pbInt(1, 2), pbString(2, []byte(domain))...)
	site := pbString(1, append(pbString(1, []byte("SERVICE")), pbString(2, d)...))
	cidr := append(pbString(1, []byte{192, 0, 2, octet}), pbInt(2, 32)...)
	ip := pbString(1, append(pbString(1, []byte("SERVICE")), pbString(2, cidr)...))
	return site, ip
}

func closureC3Write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func closureC3Link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func TestClosureC3GeodataSymlinkBoundary(t *testing.T) {
	for _, variant := range []string{
		"absolute-current", "relative-escape-current", "absolute-internal-current",
		"nested-current", "generation-symlink", "internal-generation-symlink",
		"geosite-absolute-symlink", "geoip-relative-symlink", "internal-dat-symlink",
		"root-symlink", "dat-directory", "dat-fifo", "current-directory", "empty-generation-name",
	} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			site, ip := closureC3Pair("inside.c3.test", 1)
			if err := InstallGeoFiles(root, site, ip); err != nil {
				t.Fatal(err)
			}
			generation, err := os.Readlink(filepath.Join(root, "current"))
			if err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			extSite, extIP := closureC3Pair("outside.c3.test", 2)
			closureC3Write(t, filepath.Join(outside, "geosite.dat"), extSite)
			closureC3Write(t, filepath.Join(outside, "geoip.dat"), extIP)
			current := filepath.Join(root, "current")
			dat := filepath.Join(root, generation, "geosite.dat")
			switch variant {
			case "absolute-current":
				closureC3Link(t, outside, current)
			case "relative-escape-current":
				rel, err := filepath.Rel(root, outside)
				if err != nil {
					t.Fatal(err)
				}
				closureC3Link(t, rel, current)
			case "absolute-internal-current":
				closureC3Link(t, filepath.Join(root, generation), current)
			case "nested-current":
				closureC3Link(t, "./"+generation, current)
			case "generation-symlink", "internal-generation-symlink":
				target := outside
				if variant == "internal-generation-symlink" {
					target = generation
				}
				closureC3Link(t, target, filepath.Join(root, "generation.link"))
				closureC3Link(t, "generation.link", current)
			case "geosite-absolute-symlink":
				closureC3Link(t, filepath.Join(outside, "geosite.dat"), dat)
			case "geoip-relative-symlink":
				dat = filepath.Join(root, generation, "geoip.dat")
				rel, err := filepath.Rel(filepath.Dir(dat), filepath.Join(outside, "geoip.dat"))
				if err != nil {
					t.Fatal(err)
				}
				closureC3Link(t, rel, dat)
			case "internal-dat-symlink":
				closureC3Write(t, filepath.Join(root, generation, "other.dat"), site)
				closureC3Link(t, "other.dat", dat)
			case "root-symlink":
				link := filepath.Join(t.TempDir(), "geodata")
				closureC3Link(t, root, link)
				root = link
			case "dat-directory", "dat-fifo":
				if err := os.Remove(dat); err != nil {
					t.Fatal(err)
				}
				if variant == "dat-directory" {
					err = os.Mkdir(dat, 0700)
				} else {
					err = syscall.Mkfifo(dat, 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "current-directory":
				if err := os.Remove(current); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(root, generation), current); err != nil {
					t.Fatal(err)
				}
			case "empty-generation-name":
				if err := os.Rename(filepath.Join(root, generation), filepath.Join(root, "generation.")); err != nil {
					t.Fatal(err)
				}
				closureC3Link(t, "generation.", current)
			}
			if db, err := OpenGeoDatabase(root); err == nil {
				domains, ips, expandErr := db.Expand("mixed:service")
				t.Errorf("unsafe restored pair accepted: domains=%v ips=%v err=%v", domains, ips, expandErr)
			}
			cfg := Config{Rules: []Rule{{Action: "block", Sets: []string{"mixed:service"}}}}
			if _, err := CompileWithGeo(cfg, nil, root); err == nil {
				t.Error("compile accepted unsafe restored pair")
			}
			for name, expected := range map[string][]byte{"geosite.dat": extSite, "geoip.dat": extIP} {
				actual, err := os.ReadFile(filepath.Join(outside, name))
				if err != nil || !bytes.Equal(actual, expected) {
					t.Fatal("external fixture changed", name, err)
				}
			}
		})
	}
}

func TestClosureC3RelativeGenerationUpdateCompile(t *testing.T) {
	root := t.TempDir()
	for i := byte(1); i <= 3; i++ {
		site, ip := closureC3Pair("valid.c3.test", i)
		previous, _ := os.Readlink(filepath.Join(root, "current"))
		if err := InstallGeoFiles(root, site, ip); err != nil {
			t.Fatal(err)
		}
		current, err := os.Readlink(filepath.Join(root, "current"))
		if err != nil || filepath.Base(current) != current || !strings.HasPrefix(current, "generation.") || current == previous {
			t.Fatal("invalid publication", current, err)
		}
		if previous != "" {
			if _, err := os.Stat(filepath.Join(root, previous, "geoip.dat")); err != nil {
				t.Fatal("previous pair lost", err)
			}
		}
		db, err := OpenGeoDatabase(root)
		if err != nil {
			t.Fatal(err)
		}
		domains, ips, err := db.Expand("mixed:service")
		if err != nil || len(domains) != 1 || domains[0] != "domain:valid.c3.test" || len(ips) != 1 || ips[0] != "192.0.2."+string('0'+i)+"/32" {
			t.Fatal(domains, ips, err)
		}
		cfg := Config{Rules: []Rule{{Action: "block", Sets: []string{"mixed:service"}}}}
		if _, err := CompileWithGeo(cfg, nil, root); err != nil {
			t.Fatal("valid compile rejected", err)
		}
	}
	dirs, err := filepath.Glob(filepath.Join(root, "generation.*"))
	if err != nil || len(dirs) != 2 {
		t.Fatal("current and previous generations expected", dirs, err)
	}
	before, _ := os.Readlink(filepath.Join(root, "current"))
	if err := InstallGeoFiles(root, []byte("invalid"), []byte("invalid")); err == nil {
		t.Fatal("invalid update accepted")
	}
	after, _ := os.Readlink(filepath.Join(root, "current"))
	if before != after {
		t.Fatal("failed update changed current")
	}
}

func TestClosureC3AtomicCurrentKeepsPair(t *testing.T) {
	root := t.TempDir()
	for i, domain := range []string{"alpha.c3.test", "beta.c3.test"} {
		site, ip := closureC3Pair(domain, byte(i+1))
		if err := InstallGeoFiles(root, site, ip); err != nil {
			t.Fatal(err)
		}
	}
	dirs, err := filepath.Glob(filepath.Join(root, "generation.*"))
	if err != nil || len(dirs) != 2 {
		t.Fatal(dirs, err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			next := filepath.Join(root, ".test-next")
			if err := os.Symlink(filepath.Base(dirs[i%2]), next); err != nil {
				t.Error(err)
				return
			}
			if err := os.Rename(next, filepath.Join(root, "current")); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	defer func() { close(stop); wg.Wait() }()
	for i := 0; i < 150; i++ {
		db, err := OpenGeoDatabase(root)
		if err != nil {
			t.Fatal(err)
		}
		domains, ips, err := db.Expand("mixed:service")
		if err != nil || len(domains) != 1 || len(ips) != 1 {
			t.Fatal(domains, ips, err)
		}
		if !(domains[0] == "domain:alpha.c3.test" && ips[0] == "192.0.2.1/32") && !(domains[0] == "domain:beta.c3.test" && ips[0] == "192.0.2.2/32") {
			t.Fatal("mixed generations", domains, ips)
		}
	}
}
