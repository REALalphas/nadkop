package core

import (
	"encoding/binary"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pbString(n int, data []byte) []byte {
	out := binary.AppendUvarint(nil, uint64(n<<3|2))
	out = binary.AppendUvarint(out, uint64(len(data)))
	return append(out, data...)
}
func pbInt(n int, v uint64) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(nil, uint64(n<<3)), v)
}
func geoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	domain := append(pbInt(1, 2), pbString(2, []byte("test.example"))...)
	exact := append(pbInt(1, 3), pbString(2, []byte("exact.test"))...)
	attr := pbString(1, []byte("extra"))
	exact = append(exact, pbString(3, attr)...)
	entry := append(pbString(1, []byte("SERVICE")), pbString(2, domain)...)
	entry = append(entry, pbString(2, exact)...)
	keyword := pbString(2, []byte("unsafe-keyword"))
	unsafe := append(pbString(1, []byte("REGEX")), pbString(2, keyword)...)
	site := append(pbString(1, entry), pbString(1, unsafe)...)
	cidr := append(pbString(1, []byte{127, 0, 0, 1}), pbInt(2, 32)...)
	ipentry := append(pbString(1, []byte("SERVICE")), pbString(2, cidr)...)
	ip := pbString(1, ipentry)
	if err := InstallGeoFiles(root, site, ip); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestGeoParsingCatalogueExpansionAndAtomicUpdate(t *testing.T) {
	root := geoFixture(t)
	db, err := OpenGeoDatabase(root)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, item := range db.Catalogue() {
		found[item.ID] = true
		if item.ID == "geosite:regex" && item.Zapret {
			t.Fatal("keyword allowed for Zapret")
		}
	}
	for _, id := range []string{"mixed:service", "geosite:service@extra", "geoip:service"} {
		if !found[id] {
			t.Fatal("catalogue missing", id)
		}
	}
	domains, ips, err := db.Expand("mixed:service")
	if err != nil || len(domains) != 2 || len(ips) != 1 || ips[0] != "127.0.0.1/32" {
		t.Fatal(domains, ips, err)
	}
	domains, _, err = db.Expand("geosite:service@extra")
	if err != nil || len(domains) != 1 || domains[0] != "full:exact.test" {
		t.Fatal(domains, err)
	}
	before, _ := os.Readlink(filepath.Join(root, "current"))
	if InstallGeoFiles(root, []byte("invalid"), []byte("invalid")) == nil {
		t.Fatal("corrupt update accepted")
	}
	after, _ := os.Readlink(filepath.Join(root, "current"))
	if before != after {
		t.Fatal("bad update changed database")
	}
	if _, _, err = db.Expand("geosite:missing"); err == nil {
		t.Fatal("unknown set accepted")
	}
}
func TestMatcherValidationAndORRouting(t *testing.T) {
	cfg, err := ReadConfig(strings.NewReader(`{"rules":[{"action":"block","ips":["1.1.1.1","10.3.4.5/8","2001:db8::/32"],"sets":["mixed:service"],"domains":["domain.test"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rules[0].IPs[1] != "10.0.0.0/8" {
		t.Fatal(cfg.Rules)
	}
	a, err := CompileWithGeo(cfg, nil, geoFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	var x struct {
		Routing struct {
			DomainStrategy string
			Rules          []struct {
				Domain      []string
				IP          []string
				OutboundTag string
			}
		}
	}
	json.Unmarshal(a.Xray, &x)
	if x.Routing.DomainStrategy != "IPOnDemand" || len(x.Routing.Rules) != 2 {
		t.Fatal(string(a.Xray))
	}
	for _, r := range x.Routing.Rules {
		if len(r.Domain) > 0 && len(r.IP) > 0 || r.OutboundTag != "block" {
			t.Fatal("domain/IP must be OR", string(a.Xray))
		}
	}
	for _, ip := range []string{"999.1.1.1", "1.1.1.1/33", "2001:db8::/129", "geoip:ru", "example.org", "fe80::1%eth0"} {
		if _, err := normalizeIP(ip); err == nil {
			t.Fatal("invalid IP accepted", ip)
		}
	}
	for _, id := range []string{"geosite:../../secret", "geoip:ru@bad", "mixed:", "unknown:foo"} {
		if validGeoSelector(id) {
			t.Fatal("invalid selector accepted", id)
		}
	}
}
func TestZapretGeoAndIPProfiles(t *testing.T) {
	cfg, err := ReadConfig(strings.NewReader(`{"wanInterface":"eth0","zapretArgs":["--filter-tcp=80,443","--dpi-desync=fake"],"rules":[{"action":"zapret","sets":["mixed:service"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := CompileWithGeo(cfg, nil, geoFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Hosts) != "^exact.test\ntest.example\n" || string(a.IPs) != "127.0.0.1/32\n" || !strings.Contains(string(a.Zapret), "--new") || !strings.Contains(string(a.Zapret), "--ipset=") {
		t.Fatal(string(a.Hosts), string(a.IPs), string(a.Zapret))
	}
	cfg.Rules[0].Sets = []string{"geosite:regex"}
	if _, err := CompileWithGeo(cfg, nil, geoFixture(t)); err == nil {
		t.Fatal("keyword silently widened for Zapret")
	}
	cfg.Rules[0].Sets = nil
	cfg.Rules[0].IPs = []string{"1.1.1.1"}
	a, err = CompileWithGeo(cfg, nil, "")
	if err != nil || len(a.Hosts) != 0 || len(a.IPs) == 0 || len(a.NFT) == 0 || strings.Contains(string(a.Zapret), "--hostlist=") {
		t.Fatal(a, err)
	}
}
func TestGeoRealDownloadedDatabase(t *testing.T) {
	dir := os.Getenv("NADKOP_GEO_TEST_FILES")
	if dir == "" {
		t.Skip("set NADKOP_GEO_TEST_FILES to validate downloaded datasets")
	}
	site, err := os.ReadFile(filepath.Join(dir, "geosite.dat"))
	if err != nil {
		t.Fatal(err)
	}
	ip, err := os.ReadFile(filepath.Join(dir, "geoip.dat"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = InstallGeoFiles(root, site, ip); err != nil {
		t.Fatal(err)
	}
	db, err := OpenGeoDatabase(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("catalogue size", len(db.Catalogue()))
	for _, id := range []string{"geosite:github", "geosite:youtube", "geosite:discord", "mixed:telegram", "geoip:ru"} {
		d, p, err := db.Expand(id)
		if err != nil {
			t.Fatal(err)
		}
		t.Log(id, len(d), len(p))
		for _, prefix := range p {
			if _, err := netip.ParsePrefix(prefix); err != nil {
				t.Fatal(err)
			}
		}
	}
}
