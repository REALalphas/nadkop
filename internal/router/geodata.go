package router

import (
	"bytes"
	"context"
	"nadkop/internal/core"
	"os"
	"path/filepath"
)

func (b backend) geoRoot() string { return filepath.Join(filepath.Dir(b.config), "geodata") }
func (b backend) geoCatalogue() map[string]any {
	c := core.Config{}
	if data, err := os.ReadFile(b.config); err == nil {
		c, _ = core.ReadConfig(bytes.NewReader(data))
	}
	result := map[string]any{"items": []core.GeoItem{}, "sources": core.GeoURLs(c)}
	db, err := core.OpenGeoDatabase(b.geoRoot())
	if err != nil {
		result["message"] = err.Error()
		return result
	}
	result["items"] = db.Catalogue()
	if info, err := os.Stat(filepath.Join(b.geoRoot(), "current", "geosite.dat")); err == nil {
		result["updatedAt"] = info.ModTime().Unix()
	}
	return result
}
func (b backend) updateGeo(ctx context.Context, settings *core.GeoSettings) (any, error) {
	unlock, err := b.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	data, _, _, err := b.read()
	if err != nil {
		return nil, err
	}
	c, err := core.ReadConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if settings != nil {
		c.Geodata = *settings
	}
	if err = core.UpdateGeoData(ctx, c, b.geoRoot()); err != nil {
		return nil, err
	}
	return b.geoCatalogue(), nil
}
