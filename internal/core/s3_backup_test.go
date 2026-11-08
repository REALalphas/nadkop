package core

// S3 is a synthetic filesystem/backup audit. It deliberately leaves failed
// safety assertions visible; it never changes production files or host state.
import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
)

const s3SyntheticHWID = "S3-synthetic-device-00000001"

type s3State struct {
	root   string
	config Config
	ids    map[string]string
}

func s3Write(t *testing.T, name string, data []byte, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		t.Fatal(err)
	}
}

func s3Read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func s3JSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func s3PBString(n int, value []byte) []byte {
	b := binary.AppendUvarint(nil, uint64(n<<3|2))
	b = binary.AppendUvarint(b, uint64(len(value)))
	return append(b, value...)
}

func s3PBInt(n int, value uint64) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(nil, uint64(n<<3)), value)
}

func s3GeoPair(domain string) ([]byte, []byte) {
	d := append(s3PBInt(1, 2), s3PBString(2, []byte(domain))...)
	site := s3PBString(1, append(s3PBString(1, []byte("S3FIXTURE")), s3PBString(2, d)...))
	cidr := append(s3PBString(1, []byte{192, 0, 2, 0}), s3PBInt(2, 24)...)
	ip := s3PBString(1, append(s3PBString(1, []byte("S3FIXTURE")), s3PBString(2, cidr)...))
	return site, ip
}

func s3NodeData(t *testing.T, names ...string) []byte {
	t.Helper()
	list := []map[string]any{}
	for _, name := range names {
		// A suffix changes the tag/password while retaining the unique endpoint.
		base := strings.Split(name, "-")[0]
		list = append(list, map[string]any{
			"tag": name, "protocol": "trojan",
			"settings": map[string]any{"servers": []any{map[string]any{
				"address": base + ".s3.example.invalid", "port": 443, "password": "S3-synthetic-" + name,
			}}},
			"streamSettings": map[string]any{"network": "tcp", "security": "tls", "tlsSettings": map[string]any{"serverName": base + ".s3.example.invalid"}},
			"mux":            map[string]any{"enabled": false, "concurrency": 4},
		})
	}
	return s3JSON(t, list)
}

func s3NewState(t *testing.T, root string) s3State {
	t.Helper()
	if root == "" {
		root = t.TempDir()
	}
	dir := filepath.Join(root, "etc", "nadkop")
	s3Write(t, filepath.Join(dir, "nodes.json"), s3NodeData(t, "alpha", "beta", "gamma"), 0600)
	nodes, err := ParseSubscription("s3local", s3Read(t, filepath.Join(dir, "nodes.json")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SyncServers(filepath.Join(dir, "servers.json"), nodes, Config{}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, n := range nodes {
		ids[strings.TrimPrefix(n.Tag, "s3local/")] = n.serverID
	}
	config := Config{
		Default: "block", SOCKSPort: 1080, UpdateIntervalHours: 0,
		Sources: []Source{
			{Name: "s3local", File: "/etc/nadkop/nodes.json", HWID: &HWIDSettings{Enabled: false}},
			{Name: "s3remote", URL: "https://subscription.s3.example.invalid/synthetic", Headers: map[string]string{"X-S3-Synthetic": "fixture-only"}, HWID: &HWIDSettings{Enabled: false, ID: "S3-disabled-device-0001", Model: "S3 disabled draft"}},
		},
		Rules: []Rule{
			{Name: "S3 single", Action: "proxy", Domains: []string{"single.s3.example"}, NodeRef: ids["alpha"]},
			{Name: "S3 group", Action: "proxy", Domains: []string{"group.s3.example"}, NodeRefs: []string{ids["alpha"], ids["beta"]}, Selection: &SelectionSettings{Mode: "ping", URL: "inactive-invalid-url"}},
			{Name: "S3 disabled values", Action: "direct", Conditions: &RuleConditions{Domains: true, IPs: false, Sets: false}, Domains: []string{"disabled.s3.example"}, IPs: []string{"disabled-invalid-ip"}, Sets: []string{"disabled-invalid-set"}, NodeRef: ids["gamma"], Selection: &SelectionSettings{Mode: "url", URL: "inactive-invalid-url"}},
			{Name: "S3 active geodata", Action: "block", Sets: []string{"mixed:s3fixture"}},
		},
		Picker:     PickerSettings{Pinned: []string{ids["gamma"], ids["beta"]}, Order: []string{ids["beta"], ids["alpha"], ids["gamma"]}, EmojiStyle: "apple"},
		ZapretArgs: []string{"S3-inactive-strategy-draft"}, WANInterface: "S3 inactive WAN draft",
		Geodata: GeoSettings{GeositeURL: "https://geo.s3.example.invalid/site", GeoIPURL: "https://geo.s3.example.invalid/ip"},
	}
	config, err = ReadConfig(bytes.NewReader(s3JSON(t, config)))
	if err != nil {
		t.Fatal(err)
	}
	s3Write(t, filepath.Join(dir, "config.json"), s3JSON(t, config), 0600)
	s3Write(t, filepath.Join(dir, "hwid"), []byte(s3SyntheticHWID+"\n"), 0600)
	for _, label := range []string{"old1.s3.example", "old2.s3.example", "current.s3.example"} {
		site, ip := s3GeoPair(label)
		if err := InstallGeoFiles(filepath.Join(dir, "geodata"), site, ip); err != nil {
			t.Fatal(err)
		}
	}
	// Ephemeral runtime is intentionally outside the package backup manifest.
	s3Write(t, filepath.Join(root, "var", "run", "nadkop", "generation.s3", "runtime.json"), []byte("{}\n"), 0600)
	if err := os.Symlink("generation.s3", filepath.Join(root, "var", "run", "nadkop", "current")); err != nil {
		t.Fatal(err)
	}
	return s3State{root: root, config: config, ids: ids}
}

func s3Path(root, name string) string { return filepath.Join(root, "etc", "nadkop", name) }

func s3LoadState(t *testing.T, root string) (Config, []Node, error) {
	t.Helper()
	c, err := ReadConfig(bytes.NewReader(s3Read(t, s3Path(root, "config.json"))))
	if err != nil {
		return c, nil, err
	}
	nodes := []Node{}
	for _, source := range c.Sources {
		if source.File == "" {
			continue
		} // No remote fetch in this audit.
		if source.File != "/etc/nadkop/nodes.json" {
			t.Fatal("unexpected fixture path")
		}
		// Translate the synthetic device root in memory, preserving backup bytes.
		source.File = s3Path(root, "nodes.json")
		n, err := LoadContext(context.Background(), source)
		if err != nil {
			return c, nil, err
		}
		nodes = append(nodes, n...)
	}
	return c, nodes, nil
}

func s3Repository(t *testing.T) string {
	t.Helper()
	p, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
			return p
		}
		if filepath.Dir(p) == p {
			t.Fatal("repository root not found")
		}
		p = filepath.Dir(p)
	}
}

func s3Manifest(t *testing.T, root string) []string {
	t.Helper()
	repo := s3Repository(t)
	recipe := string(s3Read(t, filepath.Join(repo, "openwrt", "nadkop", "Makefile")))
	_, part, found := strings.Cut(recipe, "define Package/nadkop/conffiles\n")
	if !found {
		t.Fatal("conffiles block missing")
	}
	part, _, _ = strings.Cut(part, "\nendef")
	want := []string{"/etc/nadkop/config.json", "/etc/nadkop/nodes.json", "/etc/nadkop/hwid", "/etc/nadkop/servers.json"}
	if !reflect.DeepEqual(strings.Fields(part), want) {
		t.Fatal("unexpected conffiles")
	}
	keep := strings.Fields(string(s3Read(t, filepath.Join(repo, "openwrt", "nadkop", "files", "nadkop.keep"))))
	if !reflect.DeepEqual(keep, []string{"/etc/nadkop/geodata/", "/etc/nadkop/.save-state.json"}) {
		t.Fatal("unexpected keep manifest")
	}
	entries := []string{}
	for _, name := range want {
		rel := strings.TrimPrefix(name, "/")
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			entries = append(entries, rel)
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	marker := strings.TrimPrefix(keep[1], "/")
	if _, err := os.Lstat(filepath.Join(root, marker)); err == nil {
		entries = append(entries, marker)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	// Mirrors OpenWrt find ( -type f -o -type l ): no symlink following,
	// no directory headers. This is a composition simulation, not sysupgrade.
	err := filepath.WalkDir(s3Path(root, "geodata"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() || d.Type()&os.ModeSymlink != 0 {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			entries = append(entries, rel)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return entries
}

func s3Archive(t *testing.T, root string) (string, []string) {
	t.Helper()
	entries := s3Manifest(t, root)
	tmp := t.TempDir()
	list := filepath.Join(tmp, "s3-file-list.txt")
	s3Write(t, list, []byte(strings.Join(entries, "\n")+"\n"), 0600)
	archive := filepath.Join(tmp, "s3-backup.tar.gz")
	cmd := exec.Command("tar", "-C", root, "--no-recursion", "-czf", archive, "-T", list)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("archive: %v: %s", err, out)
	}
	return archive, entries
}

func s3Restore(t *testing.T, archive string) string {
	t.Helper()
	root := t.TempDir()
	// Set a conventional restore umask only in this child process. Go tests
	// never change the process-wide umask while other audits may be running.
	cmd := exec.Command("python3", "-c", "import os,sys\nos.umask(0o022)\nos.execvp('tar',['tar','--no-same-owner','-xzpf',sys.argv[1],'-C',sys.argv[2]])", archive, root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restore: %v: %s", err, out)
	}
	return root
}

func s3Compile(t *testing.T, root string, c Config, nodes []Node) Artifacts {
	t.Helper()
	a, err := CompileWithGeo(c, nodes, s3Path(root, "geodata"))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func s3CheckIDs(t *testing.T, got []Node, want map[string]string) {
	t.Helper()
	for _, n := range got {
		base := strings.Split(strings.TrimPrefix(n.Tag, "s3local/"), "-")[0]
		if n.serverID != want[base] {
			t.Errorf("ID changed for synthetic %s", base)
		}
	}
}

func TestS3FullBackupRestore(t *testing.T) {
	s := s3NewState(t, "")
	archive, entries := s3Archive(t, s.root)
	dst := s3Restore(t, archive)
	for _, name := range entries {
		old, err := os.Lstat(filepath.Join(s.root, name))
		if err != nil {
			t.Fatal(err)
		}
		cur, err := os.Lstat(filepath.Join(dst, name))
		if err != nil {
			t.Fatal(err)
		}
		if old.Mode() != cur.Mode() {
			t.Errorf("mode changed: %s", name)
		}
		if old.Mode()&os.ModeSymlink != 0 {
			a, _ := os.Readlink(filepath.Join(s.root, name))
			b, _ := os.Readlink(filepath.Join(dst, name))
			if a != b || filepath.IsAbs(b) || strings.Contains(b, "..") {
				t.Error("geodata current link changed or is not relocatable")
			}
		} else if !bytes.Equal(s3Read(t, filepath.Join(s.root, name)), s3Read(t, filepath.Join(dst, name))) {
			t.Errorf("content changed: %s", name)
		}
	}
	if _, err := os.Lstat(filepath.Join(dst, "var", "run", "nadkop", "current")); !os.IsNotExist(err) {
		t.Fatal("runtime current unexpectedly archived")
	}
	dirs, err := filepath.Glob(s3Path(dst, "geodata/generation.*"))
	if err != nil || len(dirs) != 2 {
		t.Fatal("expected current and previous geodata generations")
	}
	oldDir, _ := os.Stat(s3Path(s.root, "geodata"))
	newDir, _ := os.Stat(s3Path(dst, "geodata"))
	t.Logf("archive entries=%d; geodata generations=%d; geodata directory modes %04o -> %04o (directory headers absent)", len(entries), len(dirs), oldDir.Mode().Perm(), newDir.Mode().Perm())
	id, err := DeviceID(s3Path(dst, "hwid"))
	if err != nil || id != s3SyntheticHWID {
		t.Fatal("HWID not preserved")
	}
	c, nodes, err := s3LoadState(t, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, s.config) {
		t.Fatal("restored config differs")
	}
	before := s3Read(t, s3Path(dst, "servers.json"))
	if _, err := SyncServers(s3Path(dst, "servers.json"), nodes, c); err != nil {
		t.Fatal(err)
	}
	s3CheckIDs(t, nodes, s.ids)
	if !bytes.Equal(before, s3Read(t, s3Path(dst, "servers.json"))) {
		t.Error("unchanged restore rewrote catalogue")
	}
	a := s3Compile(t, dst, c, nodes)
	var health HealthManifest
	if err := json.Unmarshal(a.Health, &health); err != nil {
		t.Fatal(err)
	}
	if len(health.Groups) != 1 || len(health.Groups[0].Members) != 2 {
		t.Fatal("group membership lost")
	}
	for i, member := range health.Groups[0].Members {
		if member.ID != c.Rules[1].NodeRefs[i] || member.Tag == "" {
			t.Fatal("restored group reference did not resolve")
		}
	}
	if !reflect.DeepEqual(c.Rules[2], s.config.Rules[2]) || !reflect.DeepEqual(c.Picker, s.config.Picker) || !reflect.DeepEqual(c.ZapretArgs, s.config.ZapretArgs) || c.Rules[1].Selection.URL != "inactive-invalid-url" {
		t.Fatal("disabled fields/picker lost")
	}
	round, err := ReadConfig(bytes.NewReader(s3JSON(t, c)))
	if err != nil || !reflect.DeepEqual(round.Rules[2], c.Rules[2]) {
		t.Fatal("disabled fields lost on serialization/reload")
	}
	reenabled := c
	reenabled.Rules = append([]Rule(nil), c.Rules...)
	r := reenabled.Rules[2]
	r.Conditions = &RuleConditions{Domains: true, IPs: true, Sets: true}
	reenabled.Rules[2] = r
	if _, err := ReadConfig(bytes.NewReader(s3JSON(t, reenabled))); err == nil {
		t.Fatal("re-enabled invalid fields were not validated")
	}
	if artifact := os.Getenv("S3_ARTIFACT_DIR"); artifact != "" {
		s3Write(t, filepath.Join(artifact, "s3-full-synthetic-backup.tar.gz"), s3Read(t, archive), 0600)
		s3Write(t, filepath.Join(artifact, "s3-full-archive-list.txt"), []byte(strings.Join(entries, "\n")+"\n"), 0600)
	}
	t.Log("PASS: same 3 server IDs, 1 single reference, 2 group members, 2 pins, 3 picker order entries; disabled fields retained; compilation only, no network exchange")
}

func TestS3MissingServerReturnAfterRestore(t *testing.T) {
	s := s3NewState(t, "")
	s3Write(t, s3Path(s.root, "nodes.json"), s3NodeData(t, "alpha"), 0600)
	c, nodes, err := s3LoadState(t, s.root)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := SyncServers(s3Path(s.root, "servers.json"), nodes, c)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, summary := range missing {
		if !summary.Available {
			count++
		}
	}
	if len(missing) != 3 || count != 2 {
		t.Fatal("bound missing servers were not retained")
	}
	archive, _ := s3Archive(t, s.root)
	dst := s3Restore(t, archive)
	c, nodes, err = s3LoadState(t, dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SyncServers(s3Path(dst, "servers.json"), nodes, c); err != nil {
		t.Fatal(err)
	}
	s3Compile(t, dst, c, nodes)
	s3Write(t, s3Path(dst, "nodes.json"), s3NodeData(t, "gamma-returned", "beta-returned", "alpha-renamed"), 0600)
	c, nodes, err = s3LoadState(t, dst)
	if err != nil {
		t.Fatal(err)
	}
	returned, err := SyncServers(s3Path(dst, "servers.json"), nodes, c)
	if err != nil {
		t.Fatal(err)
	}
	s3CheckIDs(t, nodes, s.ids)
	if len(returned) != 3 {
		t.Fatal("return duplicated catalogue")
	}
	for _, summary := range returned {
		if !summary.Available {
			t.Fatal("returned server remains unavailable")
		}
	}
	if !reflect.DeepEqual(c.Picker, s.config.Picker) || !reflect.DeepEqual(c.Rules, s.config.Rules) {
		t.Fatal("restore/return changed saved bindings")
	}
	s3Compile(t, dst, c, nodes)
	t.Log("PASS: 2 retained missing records returned with their original IDs after reorder, rename and credential rotation")
}

func TestS3PartialCatalogueMustNotRegenerateBoundIDs(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		omit       bool
	}{
		{"missing", "", true}, {"empty", "", false}, {"whitespace", " \n\t", false},
		{"version-only", `{"version":1}`, false}, {"null-servers", `{"version":1,"servers":null}`, false}, {"empty-servers", `{"version":1,"servers":[]}`, false},
		{"json-null", `null`, false}, {"nonempty-subset", "S3-subset", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := s3NewState(t, "")
			if tc.omit {
				if err := os.Remove(s3Path(s.root, "servers.json")); err != nil {
					t.Fatal(err)
				}
			} else if tc.name == "nonempty-subset" {
				var catalogue serverCatalog
				if err := json.Unmarshal(s3Read(t, s3Path(s.root, "servers.json")), &catalogue); err != nil {
					t.Fatal(err)
				}
				for _, record := range catalogue.Servers {
					if record.ID == s.ids["gamma"] {
						catalogue.Servers = []serverRecord{record}
						break
					}
				}
				s3Write(t, s3Path(s.root, "servers.json"), s3JSON(t, catalogue), 0600)
			} else {
				s3Write(t, s3Path(s.root, "servers.json"), []byte(tc.data), 0600)
			}
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			c, nodes, err := s3LoadState(t, dst)
			if err != nil {
				t.Fatal(err)
			}
			summaries, syncErr := SyncServers(s3Path(dst, "servers.json"), nodes, c)
			_, compileErr := CompileWithGeo(c, nodes, s3Path(dst, "geodata"))
			if syncErr == nil {
				old := map[string]bool{}
				for _, id := range s.ids {
					old[id] = true
				}
				reused := 0
				for _, summary := range summaries {
					if old[summary.ID] {
						reused++
					}
				}
				t.Errorf("S3-F01: incomplete catalogue accepted: sync error=nil, new records=%d, retained old IDs=%d/3; compile rejection=%v", len(summaries), reused, compileErr)
			}
			if compileErr == nil {
				t.Error("incomplete catalogue silently routed single reference")
			}
			if !bytes.Equal(s3Read(t, s3Path(dst, "config.json")), s3Read(t, s3Path(s.root, "config.json"))) {
				t.Error("saved references changed")
			}
		})
	}
}

func TestS3CorruptCatalogueRejectedWithoutRewrite(t *testing.T) {
	for _, tc := range []struct{ name, data string }{
		{"truncated", `{"version":1,"servers":[`}, {"wrong-version", `{"version":999,"servers":[]}`}, {"wrong-shape", `[]`},
		{"duplicate-id", `{"version":1,"servers":[{"id":"srv_11111111111111111111111111111111"},{"id":"srv_11111111111111111111111111111111"}]}`},
		{"invalid-id", `{"version":1,"servers":[{"id":"corrupt-reference"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := s3NewState(t, "")
			s3Write(t, s3Path(s.root, "servers.json"), []byte(tc.data), 0600)
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			c, nodes, err := s3LoadState(t, dst)
			if err != nil {
				t.Fatal(err)
			}
			before := s3Read(t, s3Path(dst, "servers.json"))
			if _, err := SyncServers(s3Path(dst, "servers.json"), nodes, c); err == nil {
				t.Error("corrupt catalogue accepted")
			} else {
				t.Logf("explicit rejection: %v", err)
			}
			if !bytes.Equal(before, s3Read(t, s3Path(dst, "servers.json"))) {
				t.Error("corrupt catalogue overwritten")
			}
		})
	}
}

func TestS3GroupOnlyPartialCatalogueBlocksUnresolvedMembers(t *testing.T) {
	s := s3NewState(t, "")
	c := s.config
	c.Rules = append([]Rule(nil), c.Rules[1:]...)
	s3Write(t, s3Path(s.root, "config.json"), s3JSON(t, c), 0600)
	if err := os.Remove(s3Path(s.root, "servers.json")); err != nil {
		t.Fatal(err)
	}
	archive, _ := s3Archive(t, s.root)
	dst := s3Restore(t, archive)
	c, nodes, err := s3LoadState(t, dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SyncServers(s3Path(dst, "servers.json"), nodes, c); err == nil {
		t.Fatal("group-only partial restore accepted without original catalogue bindings")
	}
	if _, err := os.Lstat(s3Path(dst, "servers.json")); !os.IsNotExist(err) {
		t.Fatal("rejected partial restore created a replacement catalogue")
	}
	t.Log("group-only partial restore rejected before publishing replacement IDs")
}

func TestS3HWIDCorruptionAndPermissionRepair(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		mode       fs.FileMode
		valid      bool
	}{
		{"valid-0600", s3SyntheticHWID + "\n", 0600, true}, {"valid-0644", s3SyntheticHWID + "\n", 0644, true},
		{"invalid-short", "broken", 0600, false}, {"invalid-symbol", "S3_corrupt_device_123", 0600, false}, {"invalid-long", strings.Repeat("a", 80), 0600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := s3NewState(t, "")
			s3Write(t, s3Path(s.root, "hwid"), []byte(tc.data), tc.mode)
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			p := s3Path(dst, "hwid")
			before := s3Read(t, p)
			id, err := DeviceID(p)
			if tc.valid {
				if err != nil || id != s3SyntheticHWID {
					t.Fatal("restored identity changed")
				}
			} else if err == nil {
				t.Error("corrupt identity was accepted/replaced")
			}
			if !bytes.Equal(before, s3Read(t, p)) {
				t.Error("stored identity bytes changed")
			}
			st, err := os.Stat(p)
			if err != nil || st.Mode().Perm() != 0600 {
				t.Fatal("HWID not protected as 0600")
			}
		})
	}
}

func TestS3PartialHWIDCannotSilentlyReinitialize(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		omit       bool
	}{{"missing", "", true}, {"empty", "", false}, {"whitespace", " \n\t", false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := s3NewState(t, "")
			if tc.omit {
				if err := os.Remove(s3Path(s.root, "hwid")); err != nil {
					t.Fatal(err)
				}
			} else {
				s3Write(t, s3Path(s.root, "hwid"), []byte(tc.data), 0600)
			}
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			id, err := DeviceID(s3Path(dst, "hwid"))
			if err == nil && id != s3SyntheticHWID {
				t.Error("S3-R01: partial restore is indistinguishable from bootstrap: new HWID written without error (conditional restore capability gap)")
			}
		})
	}
}

func TestS3FreshInstallationBootstrap(t *testing.T) {
	root := t.TempDir()
	p := s3Path(root, "hwid")
	s3Write(t, p, nil, 0600)
	id, err := DeviceID(p)
	if err != nil || len(id) != 32 {
		t.Fatal("fresh empty package HWID no longer bootstraps")
	}
	nodes, err := ParseSubscription("s3local", s3NodeData(t, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if summaries, err := SyncServers(s3Path(root, "servers.json"), nodes, Config{}); err != nil || len(summaries) != 1 {
		t.Fatal("fresh unbound catalogue no longer bootstraps")
	}
	t.Log("PASS: new installation initialization is legitimate; missing HWID alone cannot prove a corrupt restore")
}

func TestS3PartialConfigNodesFailClosed(t *testing.T) {
	for _, name := range []string{"config-missing", "config-empty", "config-truncated", "nodes-missing", "nodes-truncated", "nodes-empty-array"} {
		t.Run(name, func(t *testing.T) {
			s := s3NewState(t, "")
			target := "config.json"
			if strings.HasPrefix(name, "nodes-") {
				target = "nodes.json"
			}
			p := s3Path(s.root, target)
			if strings.HasSuffix(name, "-missing") {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			} else {
				data := []byte(`{"truncated":`)
				if name == "config-empty" {
					data = nil
				}
				if name == "nodes-empty-array" {
					data = []byte("[]\n")
				}
				s3Write(t, p, data, 0600)
			}
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			before := s3Read(t, s3Path(dst, "servers.json"))
			f, err := os.Open(s3Path(dst, "config.json"))
			if err != nil {
				t.Log("explicit missing config failure")
				return
			}
			c, err := ReadConfig(f)
			f.Close()
			if err != nil {
				t.Logf("explicit config failure: %v", err)
				return
			}
			source := c.Sources[0]
			source.File = s3Path(dst, "nodes.json")
			nodes, err := LoadContext(context.Background(), source)
			if err != nil {
				t.Logf("explicit nodes failure: %v", err)
				return
			}
			if _, err := SyncServers(s3Path(dst, "servers.json"), nodes, c); err != nil {
				t.Fatal(err)
			}
			if _, err := CompileWithGeo(c, nodes, s3Path(dst, "geodata")); err == nil {
				t.Fatal("empty restored nodes routed a missing single server")
			}
			catalog, err := ServerSummaries(s3Path(dst, "servers.json"))
			if err != nil || len(catalog) != 3 {
				t.Fatal("referenced missing records lost")
			}
			if bytes.Equal(before, s3Read(t, s3Path(dst, "servers.json"))) {
				t.Error("empty nodes unexpectedly left availability true")
			}
			t.Log("empty node array marks 3 bound records missing; old IDs retained; compile rejects missing single reference")
		})
	}
}

func TestS3PartialGeodataRejectsWithoutFallback(t *testing.T) {
	for _, name := range []string{"no-current", "dangling-current", "missing-site", "missing-ip", "broken-site", "broken-ip"} {
		t.Run(name, func(t *testing.T) {
			s := s3NewState(t, "")
			geo := s3Path(s.root, "geodata")
			if name == "no-current" || name == "dangling-current" {
				if err := os.Remove(filepath.Join(geo, "current")); err != nil {
					t.Fatal(err)
				}
				if name == "dangling-current" {
					if err := os.Symlink("generation.s3-absent", filepath.Join(geo, "current")); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				kind := "geosite.dat"
				if strings.HasSuffix(name, "ip") {
					kind = "geoip.dat"
				}
				p := filepath.Join(geo, "current", kind)
				if strings.HasPrefix(name, "missing-") {
					if err := os.Remove(p); err != nil {
						t.Fatal(err)
					}
				} else {
					s3Write(t, p, []byte("S3-corrupt-database"), 0600)
				}
			}
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			if _, err := OpenGeoDatabase(s3Path(dst, "geodata")); err == nil {
				t.Fatal("incomplete geodata accepted")
			} else {
				t.Logf("explicit geodata failure: %v", err)
			}
			c, nodes, err := s3LoadState(t, dst)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := SyncServers(s3Path(dst, "servers.json"), nodes, c); err != nil {
				t.Fatal(err)
			}
			if _, err := CompileWithGeo(c, nodes, s3Path(dst, "geodata")); err == nil {
				t.Error("active set compiled using corrupt/missing pair")
			}
			for _, kind := range []string{"config.json", "nodes.json", "servers.json", "hwid"} {
				if !bytes.Equal(s3Read(t, s3Path(dst, kind)), s3Read(t, s3Path(s.root, kind))) {
					t.Errorf("geodata failure changed %s", kind)
				}
			}
		})
	}
}

func TestS3GeodataArchiveSymlinkBoundary(t *testing.T) {
	for _, kind := range []string{"current-absolute", "current-relative-escape", "generation-link", "site-file-link", "ip-file-link"} {
		t.Run(kind, func(t *testing.T) {
			s := s3NewState(t, "")
			outside := t.TempDir()
			site, ip := s3GeoPair("outside.s3.example")
			s3Write(t, filepath.Join(outside, "geosite.dat"), site, 0600)
			s3Write(t, filepath.Join(outside, "geoip.dat"), ip, 0600)
			geo := s3Path(s.root, "geodata")
			if strings.HasPrefix(kind, "current-") || kind == "generation-link" {
				p := filepath.Join(geo, "current")
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				link := outside
				if kind == "current-relative-escape" {
					var err error
					link, err = filepath.Rel(geo, outside)
					if err != nil {
						t.Fatal(err)
					}
				}
				if kind == "generation-link" {
					if err := os.Symlink(outside, filepath.Join(geo, "generation.s3-link")); err != nil {
						t.Fatal(err)
					}
					link = "generation.s3-link"
				}
				if err := os.Symlink(link, p); err != nil {
					t.Fatal(err)
				}
			} else {
				name := "geosite.dat"
				if kind == "ip-file-link" {
					name = "geoip.dat"
				}
				p := filepath.Join(geo, "current", name)
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, name), p); err != nil {
					t.Fatal(err)
				}
			}
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			db, err := OpenGeoDatabase(s3Path(dst, "geodata"))
			if err == nil {
				domains, ips, expandErr := db.Expand("mixed:s3fixture")
				t.Errorf("S3-F02: restored geodata followed %s outside its root: domains=%v ips=%v expansion error=%v", kind, domains, ips, expandErr)
				c, nodes, loadErr := s3LoadState(t, dst)
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				if _, syncErr := SyncServers(s3Path(dst, "servers.json"), nodes, c); syncErr != nil {
					t.Fatal(syncErr)
				}
				_, compileErr := CompileWithGeo(c, nodes, s3Path(dst, "geodata"))
				t.Logf("compilation with external/mixed pair error=%v", compileErr)
			}
			if !bytes.Equal(s3Read(t, filepath.Join(outside, "geosite.dat")), site) || !bytes.Equal(s3Read(t, filepath.Join(outside, "geoip.dat")), ip) {
				t.Error("external synthetic sentinel changed")
			}
		})
	}
}

func TestS3RestoredWeakSensitiveModes(t *testing.T) {
	for _, name := range []string{"nodes.json", "servers.json", "config.json"} {
		t.Run(name, func(t *testing.T) {
			s := s3NewState(t, "")
			if err := os.Chmod(s3Path(s.root, name), 0644); err != nil {
				t.Fatal(err)
			}
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			p := s3Path(dst, name)
			var readErr error
			switch name {
			case "nodes.json":
				_, readErr = LoadContext(context.Background(), Source{Name: "s3local", File: p, HWID: &HWIDSettings{Enabled: false}})
			case "servers.json":
				c, nodes, err := s3LoadState(t, dst)
				if err != nil {
					t.Fatal(err)
				}
				_, readErr = SyncServers(p, nodes, c)
			case "config.json":
				binary := os.Getenv("S3_NADKOP_BIN")
				if binary == "" {
					t.Skip("set S3_NADKOP_BIN using scripts/test-s3-backup.py for production CLI mode test")
				}
				out, err := exec.Command(binary, "validate", "-config", p).CombinedOutput()
				readErr = err
				if err != nil {
					t.Logf("CLI rejection: %s", out)
				}
			}
			st, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if readErr == nil && st.Mode().Perm() != 0600 {
				t.Errorf("S3-F03: restored %s consumed successfully and remained mode %04o", name, st.Mode().Perm())
			}
		})
	}
}

func TestS3CLIGenerateAfterRestore(t *testing.T) {
	binary := os.Getenv("S3_NADKOP_BIN")
	if binary == "" {
		t.Skip("set S3_NADKOP_BIN using the S3 runner")
	}
	for _, incomplete := range []bool{false, true} {
		name := "complete"
		if incomplete {
			name = "missing-catalogue"
		}
		t.Run(name, func(t *testing.T) {
			s := s3NewState(t, "")
			if incomplete {
				if err := os.Remove(s3Path(s.root, "servers.json")); err != nil {
					t.Fatal(err)
				}
			}
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			c := s.config
			c.Sources = append([]Source(nil), c.Sources[:1]...)
			c.Sources[0].File = s3Path(dst, "nodes.json")
			// Only the synthetic fixture config is rebased for the real CLI.
			s3Write(t, s3Path(dst, "config.json"), s3JSON(t, c), 0600)
			output := filepath.Join(dst, "var", "run", "nadkop", "generation.s3-cli")
			out, err := exec.Command(binary, "generate", "-config", s3Path(dst, "config.json"), "-output", output).CombinedOutput()
			catalogue, catalogErr := ServerSummaries(s3Path(dst, "servers.json"))
			if catalogErr != nil {
				t.Fatal(catalogErr)
			}
			known := map[string]bool{}
			for _, id := range s.ids {
				known[id] = true
			}
			reused := 0
			for _, record := range catalogue {
				if known[record.ID] {
					reused++
				}
			}
			if incomplete {
				if err == nil {
					t.Fatal("CLI generated with missing single reference")
				}
				if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
					t.Error("failed CLI published output")
				}
				t.Logf("CLI exit=%v; output=%s; catalogue records=%d; preserved IDs=%d/3; runtime output absent", err, bytes.TrimSpace(out), len(catalogue), reused)
				if len(catalogue) != 0 {
					t.Error("S3-F01: rejected CLI partial restore published replacement catalogue records")
				}
				if _, err := os.Lstat(s3Path(dst, "servers.json")); !os.IsNotExist(err) {
					t.Error("S3-F01: rejected CLI partial restore created a catalogue file")
				}
				return
			}
			if err != nil {
				t.Fatalf("CLI generation: %v: %s", err, out)
			}
			if reused != 3 {
				t.Fatal("complete restore CLI changed IDs")
			}
			entries, err := os.ReadDir(output)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 10 {
				t.Fatalf("generated files=%d, want 10", len(entries))
			}
			for _, entry := range entries {
				st, err := entry.Info()
				if err != nil || st.Mode().Perm() != 0600 {
					t.Error("runtime file not 0600")
				}
			}
			st, err := os.Stat(output)
			if err != nil || st.Mode().Perm() != 0700 {
				t.Error("runtime generation not 0700")
			}
			t.Log("PASS: real generate CLI, same 3 IDs, 10 private runtime artifacts, generation 0700; no service/preflight/network execution")
		})
	}
}

func TestS3AmbiguousReturnedServersAreNotSubstituted(t *testing.T) {
	s := s3NewState(t, "")
	sharedEndpoint := func(data []byte) []byte {
		var objects []map[string]any
		if err := json.Unmarshal(data, &objects); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			objects[i]["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any)["address"] = "shared.s3.example.invalid"
			objects[i]["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["serverName"] = "shared.s3.example.invalid"
		}
		return s3JSON(t, objects)
	}
	s3Write(t, s3Path(s.root, "nodes.json"), sharedEndpoint(s3NodeData(t, "alpha", "beta", "gamma")), 0600)
	c, nodes, err := s3LoadState(t, s.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SyncServers(s3Path(s.root, "servers.json"), nodes, c); err != nil {
		t.Fatal(err)
	}
	s3CheckIDs(t, nodes, s.ids)
	s3Write(t, s3Path(s.root, "nodes.json"), s3NodeData(t, "gamma"), 0600)
	c, nodes, err = s3LoadState(t, s.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SyncServers(s3Path(s.root, "servers.json"), nodes, c); err != nil {
		t.Fatal(err)
	}
	archive, _ := s3Archive(t, s.root)
	dst := s3Restore(t, archive)
	s3Write(t, s3Path(dst, "nodes.json"), sharedEndpoint(s3NodeData(t, "alpha-new", "beta-new", "gamma")), 0600)
	c, nodes, err = s3LoadState(t, dst)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SyncServers(s3Path(dst, "servers.json"), nodes, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Tag == "s3local/gamma" {
			if n.serverID != s.ids["gamma"] {
				t.Fatal("unambiguous gamma changed")
			}
			continue
		}
		if n.serverID == s.ids["alpha"] || n.serverID == s.ids["beta"] {
			t.Fatal("ambiguous returned server substituted for old ID")
		}
	}
	retained := 0
	for _, server := range result {
		if server.ID == s.ids["alpha"] || server.ID == s.ids["beta"] {
			retained++
			if server.Available {
				t.Error("ambiguous old ID became available")
			}
		}
	}
	if retained != 2 || len(result) != 5 {
		t.Fatal("ambiguous records were discarded")
	}
	if _, err := CompileWithGeo(c, nodes, s3Path(dst, "geodata")); err == nil {
		t.Error("ambiguous old single reference unexpectedly compiled")
	}
	t.Log("PASS: after archive round-trip, 2 ambiguous matches get new IDs; both bound old IDs retained missing; no substitution")
}

func TestS3SensitiveSymlinksFailClosed(t *testing.T) {
	for _, name := range []string{"hwid", "nodes.json", "servers.json"} {
		t.Run(name, func(t *testing.T) {
			s := s3NewState(t, "")
			sentinel := filepath.Join(t.TempDir(), "synthetic-"+name)
			data := s3Read(t, s3Path(s.root, name))
			s3Write(t, sentinel, data, 0600)
			if err := os.Remove(s3Path(s.root, name)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(sentinel, s3Path(s.root, name)); err != nil {
				t.Fatal(err)
			}
			archive, _ := s3Archive(t, s.root)
			dst := s3Restore(t, archive)
			var err error
			switch name {
			case "hwid":
				_, err = DeviceID(s3Path(dst, name))
			case "nodes.json":
				_, err = LoadContext(context.Background(), Source{Name: "s3local", File: s3Path(dst, name), HWID: &HWIDSettings{Enabled: false}})
			case "servers.json":
				_, err = ServerSummaries(s3Path(dst, name))
			}
			if err == nil {
				t.Error("restored sensitive symlink accepted")
			}
			if !bytes.Equal(data, s3Read(t, sentinel)) {
				t.Error("external synthetic sentinel changed")
			}
		})
	}
}

func TestS3RestoredForeignOwners(t *testing.T) {
	if os.Getenv("S3_FOREIGN_OWNER_CHILD") != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("unshare", "--user", "--map-auto", "--map-root-user", "--mount", "--propagation", "private", binary, "-test.run", "^TestS3RestoredForeignOwners$", "-test.v", "-test.timeout", "30s")
		cmd.Env = append(os.Environ(), "S3_FOREIGN_OWNER_CHILD=1")
		out, err := cmd.CombinedOutput()
		t.Logf("isolated user/mount namespace output:\n%s", out)
		if err != nil {
			if bytes.Contains(out, []byte("unshare:")) || bytes.Contains(out, []byte("newuidmap:")) {
				t.Skipf("foreign owner namespace unavailable: %v", err)
			}
			t.Errorf("S3-F04: foreign-owner safety assertions failed: %v", err)
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Fatal("owner audit requires root ONLY inside user namespace")
	}
	root := t.TempDir()
	if err := syscall.Mount("none", root, "tmpfs", syscall.MS_NODEV|syscall.MS_NOSUID, "size=8m,mode=0700"); err != nil {
		t.Skipf("private tmpfs unavailable: %v", err)
	}
	defer func() {
		if err := syscall.Unmount(root, 0); err != nil {
			t.Error(err)
		}
	}()
	for _, name := range []string{"hwid", "servers.json", "nodes.json", "config.json"} {
		for _, mode := range []fs.FileMode{0600, 0644} {
			t.Run(fmt.Sprintf("%s-%04o", name, mode), func(t *testing.T) {
				s := s3NewState(t, filepath.Join(root, strings.ReplaceAll(name, ".", "-")+fmt.Sprintf("-%04o", mode)))
				p := s3Path(s.root, name)
				if err := os.Chmod(p, mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chown(p, 12345, 12345); err != nil {
					t.Fatal(err)
				}
				archive, _ := s3Archive(t, s.root)
				dst := s.root + "-restored"
				if err := os.Mkdir(dst, 0700); err != nil {
					t.Fatal(err)
				}
				out, err := exec.Command("tar", "--same-owner", "--numeric-owner", "-xzpf", archive, "-C", dst).CombinedOutput()
				if err != nil {
					t.Fatalf("foreign-owner archive extraction: %v: %s", err, out)
				}
				s.root = dst
				p = s3Path(dst, name)
				before, err := os.Stat(p)
				if err != nil {
					t.Fatal(err)
				}
				if before.Sys().(*syscall.Stat_t).Uid != 12345 || before.Mode().Perm() != mode {
					t.Fatal("foreign metadata not actually restored")
				}
				var readErr error
				switch name {
				case "hwid":
					_, readErr = DeviceID(p)
				case "servers.json":
					c, nodes, err := s3LoadState(t, s.root)
					if err != nil {
						t.Fatal(err)
					}
					_, readErr = SyncServers(p, nodes, c)
				case "nodes.json":
					_, readErr = LoadContext(context.Background(), Source{Name: "s3local", File: p, HWID: &HWIDSettings{Enabled: false}})
				case "config.json":
					binary := os.Getenv("S3_NADKOP_BIN")
					if binary == "" {
						t.Skip("set S3_NADKOP_BIN for CLI owner test")
					}
					out, err := exec.Command(binary, "validate", "-config", p).CombinedOutput()
					readErr = err
					if err != nil {
						t.Logf("CLI rejection: %s", out)
					}
				}
				st, err := os.Stat(p)
				if err != nil {
					t.Fatal(err)
				}
				owner := st.Sys().(*syscall.Stat_t).Uid
				if readErr == nil && (owner != 0 || st.Mode().Perm() != 0600) {
					t.Errorf("S3-F04: root reader accepted foreign-owned %s: owner=%d mode=%04o", name, owner, st.Mode().Perm())
				}
			})
		}
	}
}
