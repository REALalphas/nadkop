package router

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"nadkop/internal/core"
)

func geoBytes(field int, value []byte) []byte {
	out := binary.AppendUvarint(nil, uint64(field<<3|2))
	out = binary.AppendUvarint(out, uint64(len(value)))
	return append(out, value...)
}
func TestProductGeoRPCUsesConfigurationDirectory(t *testing.T) {
	b := fixture(t)
	empty := b.geoCatalogue()
	if empty["message"] == nil {
		t.Fatal("missing datasets not explained")
	}
	domain := append([]byte{8, 2}, geoBytes(2, []byte("example.test"))...)
	site := geoBytes(1, append(geoBytes(1, []byte("service")), geoBytes(2, domain)...))
	cidr := append(geoBytes(1, []byte{127, 0, 0, 1}), []byte{16, 32}...)
	ip := geoBytes(1, append(geoBytes(1, []byte("service")), geoBytes(2, cidr)...))
	if err := core.InstallGeoFiles(b.geoRoot(), site, ip); err != nil {
		t.Fatal(err)
	}
	catalogue := b.geoCatalogue()
	items := catalogue["items"].([]core.GeoItem)
	if len(items) != 3 || catalogue["updatedAt"] == nil {
		t.Fatal(catalogue)
	}
	_, _, rev, _ := b.read()
	cfg := []byte(`{"sources":[],"rules":[{"action":"block","sets":["mixed:service"]}]}`)
	if _, err := b.call(context.Background(), "save", request{Config: cfg, Nodes: []byte(`[]`), Revision: rev}); err != nil {
		t.Fatal("save used a global dataset instead of this config's dataset", err)
	}
	before, _ := os.Readlink(filepath.Join(b.geoRoot(), "current"))
	if _, err := b.updateGeo(context.Background(), &core.GeoSettings{GeositeURL: "http://invalid.test/site", GeoIPURL: "http://invalid.test/ip"}); err == nil {
		t.Fatal("invalid geo URLs accepted")
	}
	after, _ := os.Readlink(filepath.Join(b.geoRoot(), "current"))
	if before != after {
		t.Fatal("failed update lost installed data")
	}
}
func TestProductUpdaterHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Updater(ctx); err != nil {
		t.Fatal(err)
	}
}
