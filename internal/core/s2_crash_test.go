package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func s2PB(field int, data []byte) []byte {
	out := binary.AppendUvarint(nil, uint64(field<<3|2))
	out = binary.AppendUvarint(out, uint64(len(data)))
	return append(out, data...)
}

func s2CoreHash(path string) string {
	data, _ := os.ReadFile(path)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestS2CorePermissionAndIncompleteGeo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged fixture owner")
	}
	for _, kind := range []string{"catalogue", "geodata", "device"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "servers.json")
			var before, after string
			var invoke func() error
			if kind == "catalogue" {
				nodes, err := ParseSubscription("s2", []byte(`[{"tag":"kept","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":19080}]}}]`))
				if err != nil {
					t.Fatal(err)
				}
				servers, err := SyncServers(path, nodes, Config{})
				if err != nil {
					t.Fatal(err)
				}
				cfg := Config{Picker: PickerSettings{Pinned: []string{servers[0].ID}}}
				before = s2CoreHash(path)
				invoke = func() error { _, e := SyncServers(path, nil, cfg); return e }
			} else if kind == "geodata" {
				site, ip := s2GeoPair("old", 1)
				if err := InstallGeoFiles(root, site, ip); err != nil {
					t.Fatal(err)
				}
				before = s2CoreHash(filepath.Join(root, "current", "geosite.dat")) + s2CoreHash(filepath.Join(root, "current", "geoip.dat"))
				invoke = func() error { site, ip := s2GeoPair("new", 2); return InstallGeoFiles(root, site, ip) }
			} else {
				path = filepath.Join(root, "hwid")
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
				before = s2CoreHash(path)
				invoke = func() error { _, e := DeviceID(path); return e }
			}
			if err := os.Chmod(root, 0500); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(root, 0700)
			err := invoke()
			if err == nil {
				t.Fatal("permission fault not observed")
			}
			if kind == "geodata" {
				after = s2CoreHash(filepath.Join(root, "current", "geosite.dat")) + s2CoreHash(filepath.Join(root, "current", "geoip.dat"))
			} else {
				after = s2CoreHash(path)
			}
			if before != after {
				t.Fatal("permission failure changed published data")
			}
			if e := os.Chmod(root, 0700); e != nil {
				t.Fatal(e)
			}
			if e := invoke(); e != nil {
				t.Fatal("subsequent retry failed", e)
			}
			t.Logf("kind=%s error=%v before=%s after=%s retryOK=true", kind, err, before, after)
		})
	}
	root := t.TempDir()
	if _, err := OpenGeoDatabase(root); err == nil {
		t.Fatal("missing current accepted")
	}
	stage := filepath.Join(root, "generation.incomplete")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	site, _ := s2GeoPair("old", 1)
	if err := os.WriteFile(filepath.Join(stage, "geosite.dat"), site, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("generation.incomplete", filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenGeoDatabase(root); err == nil {
		t.Fatal("incomplete geo pair accepted")
	} else {
		t.Logf("incomplete pair rejected: %v", err)
	}
}

func s2GeoPair(label string, octet byte) ([]byte, []byte) {
	domain := append([]byte{8, 2}, s2PB(2, []byte(label+".s2.example"))...)
	site := s2PB(1, append(s2PB(1, []byte("S2TEST")), s2PB(2, domain)...))
	cidr := append(s2PB(1, []byte{192, 0, 2, octet}), []byte{16, 32}...)
	ip := s2PB(1, append(s2PB(1, []byte("S2TEST")), s2PB(2, cidr)...))
	return site, ip
}

func TestS2CoreChild(t *testing.T) {
	root, mode := os.Getenv("S2_FIXTURE"), os.Getenv("S2_MODE")
	if root == "" || mode == "" {
		t.Skip("S2 subprocess helper")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var result any
	var err error
	switch mode {
	case "geoinstall":
		site, ip := s2GeoPair("new", 2)
		err = InstallGeoFiles(filepath.Join(root, "geodata"), site, ip)
	case "geoseed":
		site, ip := s2GeoPair("old", 1)
		err = InstallGeoFiles(filepath.Join(root, "geodata"), site, ip)
	case "geoinspect":
		var db *GeoDatabase
		db, err = OpenGeoDatabase(filepath.Join(root, "geodata"))
		if err == nil {
			domains, ips, e := db.Expand("mixed:s2test")
			err = e
			result = map[string]any{"domains": domains, "ips": ips, "dir": db.dir}
		}
	case "device":
		var id string
		id, err = DeviceID(filepath.Join(root, "hwid"))
		result = map[string]any{"idLength": len(id), "valid": hwidPattern.MatchString(id)}
	default:
		t.Fatal("unknown S2 core helper")
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	encoded, _ := json.Marshal(map[string]any{"mode": mode, "error": message, "result": result})
	fmt.Println("S2_RESULT " + string(encoded))
}

func TestS2DeviceIDErrorsAndConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hwid")
	for _, bad := range []string{"invalid", "synthetic-invalid-\x00-device"} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := DeviceID(path)
		after, _ := os.ReadFile(path)
		if err == nil || !bytes.Equal(after, []byte(bad)) {
			t.Fatal("corrupt ID replaced")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 24)
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); id, e := DeviceID(path); ids <- id; errs <- e }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("concurrent identity rotation")
		}
	}
	st, e := os.Stat(path)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("permissions", e)
	}
	t.Log("24 callers, 1 persistent valid ID, mode 0600; invalid bytes unchanged")
}
