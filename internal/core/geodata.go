package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const GeoDataPath = "/etc/nadkop/geodata"
const maxGeoFile = 64 << 20

type GeoSettings struct {
	GeositeURL string `json:"geositeURL,omitempty"`
	GeoIPURL   string `json:"geoipURL,omitempty"`
}

func GeoURLs(c Config) GeoSettings {
	settings := c.Geodata
	if settings.GeositeURL == "" {
		settings.GeositeURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"
	}
	if settings.GeoIPURL == "" {
		settings.GeoIPURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat"
	}
	return settings
}

type geoField struct {
	number  int
	value   []byte
	integer uint64
}

func geoFields(data []byte, visit func(geoField) error) error {
	for len(data) > 0 {
		key, n := binary.Uvarint(data)
		if n <= 0 || key>>3 == 0 {
			return errors.New("invalid protobuf field")
		}
		data = data[n:]
		field := geoField{number: int(key >> 3)}
		switch key & 7 {
		case 0:
			v, n := binary.Uvarint(data)
			if n <= 0 {
				return errors.New("invalid protobuf integer")
			}
			field.integer = v
			data = data[n:]
		case 1:
			if len(data) < 8 {
				return io.ErrUnexpectedEOF
			}
			data = data[8:]
		case 2:
			size, n := binary.Uvarint(data)
			if n <= 0 || size > uint64(len(data)-n) {
				return io.ErrUnexpectedEOF
			}
			data = data[n:]
			field.value = data[:int(size)]
			data = data[int(size):]
		case 5:
			if len(data) < 4 {
				return io.ErrUnexpectedEOF
			}
			data = data[4:]
		default:
			return errors.New("unsupported protobuf wire type")
		}
		if err := visit(field); err != nil {
			return err
		}
	}
	return nil
}

type geoEntry struct {
	code       string
	raw        []byte
	count      int
	attributes []string
	zapret     bool
	inverse    bool
}
type GeoDatabase struct {
	site, ip map[string]geoEntry
	dir      string
}
type GeoItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Count       int    `json:"count"`
	Zapret      bool   `json:"zapret"`
	Unsupported string `json:"unsupported,omitempty"`
}

func geoCode(s string) bool {
	if s == "" || len(s) > 160 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("_-.!", r)) {
			return false
		}
	}
	return true
}
func parseGeo(data []byte, kind string) (map[string]geoEntry, error) {
	result := map[string]geoEntry{}
	err := geoFields(data, func(f geoField) error {
		if f.number != 1 {
			return nil
		}
		entry := geoEntry{raw: f.value, zapret: true}
		attrs := map[string]bool{}
		if err := geoFields(f.value, func(x geoField) error {
			switch x.number {
			case 1:
				entry.code = strings.ToLower(string(x.value))
			case 2:
				entry.count++
				if kind == "geoip" {
					var ip []byte
					var prefix uint64
					if err := geoFields(x.value, func(c geoField) error {
						if c.number == 1 {
							ip = c.value
						}
						if c.number == 2 {
							prefix = c.integer
						}
						return nil
					}); err != nil {
						return err
					}
					address, ok := netip.AddrFromSlice(ip)
					if !ok || prefix > uint64(address.BitLen()) {
						return errors.New("invalid geoip CIDR")
					}
				}
				if kind == "geosite" {
					var value string
					var typ uint64
					if err := geoFields(x.value, func(d geoField) error {
						if d.number == 1 {
							typ = d.integer
						}
						if d.number == 2 {
							value = string(d.value)
						}
						return nil
					}); err != nil {
						return err
					}
					if value == "" || typ > 3 || strings.ContainsAny(value, "\x00\r\n") {
						return errors.New("invalid geosite domain")
					}

					return geoFields(x.value, func(d geoField) error {
						if d.number == 1 && d.integer < 2 {
							entry.zapret = false
						}
						if d.number == 3 {
							return geoFields(d.value, func(a geoField) error {
								if a.number == 1 {
									name := strings.ToLower(string(a.value))
									if geoCode(name) {
										attrs[name] = true
									}
								}
								return nil
							})
						}
						return nil
					})
				}
			case 3:
				if kind == "geoip" {
					entry.inverse = x.integer != 0
					entry.zapret = !entry.inverse
				}
			}
			return nil
		}); err != nil {
			return err
		}
		// Plain is the protobuf default (the type field may be omitted).
		if kind == "geosite" {
			if err := geoFields(f.value, func(d geoField) error {
				if d.number != 2 {
					return nil
				}
				typ := uint64(0)
				if err := geoFields(d.value, func(v geoField) error {
					if v.number == 1 {
						typ = v.integer
					}
					return nil
				}); err != nil {
					return err
				}
				if typ < 2 {
					entry.zapret = false
				}
				return nil
			}); err != nil {
				return err
			}
		}
		if !geoCode(entry.code) || entry.count == 0 {
			return errors.New("empty or invalid geo entry")
		}
		if _, exists := result[entry.code]; exists {
			return errors.New("duplicate geo entry")
		}
		for attr := range attrs {
			entry.attributes = append(entry.attributes, attr)
		}
		sort.Strings(entry.attributes)
		result[entry.code] = entry
		if len(result) > 20000 {
			return errors.New("too many geo entries")
		}
		return nil
	})
	if err == nil && len(result) == 0 {
		err = errors.New("geo database is empty")
	}
	return result, err
}
func OpenGeoDatabase(root string) (*GeoDatabase, error) {
	base, err := os.OpenFile(filepath.Clean(root), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, errors.New("Geodatabases are not loaded: Settings -> Routing databases -> Update")
	}
	defer base.Close()
	// Only the relative, direct generation link published by InstallGeoFiles is
	// supported. Pin the directory before reading either member of the pair;
	// resolving current again could mix generations during an atomic update.
	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil || filepath.Base(target) != target || !strings.HasPrefix(target, "generation.") || target == "generation." {
		return nil, errors.New("Invalid geodatabase generation link; restore the full set")
	}
	fd, err := syscall.Openat(int(base.Fd()), target, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("Unsafe or missing geodatabase generation")
	}
	generation := os.NewFile(uintptr(fd), target)
	defer generation.Close()
	db := &GeoDatabase{dir: filepath.Join(root, target)}
	for _, kind := range []string{"geosite", "geoip"} {
		fd, err := syscall.Openat(int(generation.Fd()), kind+".dat", syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s", kind)
		}
		file := os.NewFile(uintptr(fd), kind+".dat")
		if st, err := file.Stat(); err != nil || !st.Mode().IsRegular() {
			file.Close()
			return nil, errors.New("Geodatabase must be a regular file")
		}
		data, err := io.ReadAll(io.LimitReader(file, maxGeoFile+1))
		file.Close()
		if err != nil || len(data) > maxGeoFile {
			return nil, errors.New("Geodatabase is too large")
		}
		parsed, err := parseGeo(data, kind)
		if err != nil {
			return nil, fmt.Errorf("Corrupted %s database: %w", kind, err)
		}
		if kind == "geosite" {
			db.site = parsed
		} else {
			db.ip = parsed
		}
	}
	return db, nil
}
func (db *GeoDatabase) Catalogue() []GeoItem {
	items := []GeoItem{}
	for name, entry := range db.site {
		items = append(items, GeoItem{"geosite:" + name, name, "geosite", entry.count, entry.zapret, ""})
		for _, attr := range entry.attributes {
			selector := "geosite:" + name + "@" + attr
			domains, _, err := db.Expand(selector)
			supported := err == nil
			reason := ""
			if err != nil {
				reason = "Set is not available in the loaded database."
			}
			for _, domain := range domains {
				supported = supported && (strings.HasPrefix(domain, "domain:") || strings.HasPrefix(domain, "full:"))
			}
			items = append(items, GeoItem{selector, name + "@" + attr, "geosite", len(domains), supported, reason})
		}
	}
	for name, entry := range db.ip {
		reason := ""
		if entry.inverse {
			reason = "Inverse GeoIP is not supported. Select another set."
		}
		items = append(items, GeoItem{"geoip:" + name, name, "geoip", entry.count, entry.zapret, reason})
		if site, ok := db.site[name]; ok {
			items = append(items, GeoItem{"mixed:" + name, name, "mixed", site.count + entry.count, site.zapret && entry.zapret, reason})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
func (db *GeoDatabase) Expand(selector string) (domains, ips []string, err error) {
	parts := strings.SplitN(selector, ":", 2)
	if len(parts) != 2 {
		return nil, nil, errors.New("Invalid set")
	}
	kind, name := parts[0], parts[1]
	if kind == "mixed" {
		d, _, e := db.Expand("geosite:" + name)
		if e != nil {
			return nil, nil, e
		}
		_, ip, e := db.Expand("geoip:" + name)
		return d, ip, e
	}
	attr := ""
	if kind == "geosite" {
		p := strings.SplitN(name, "@", 2)
		name = p[0]
		if len(p) > 1 {
			attr = p[1]
		}
	}
	var entry geoEntry
	var ok bool
	if kind == "geosite" {
		entry, ok = db.site[name]
	} else if kind == "geoip" {
		entry, ok = db.ip[name]
	}
	if !ok {
		return nil, nil, fmt.Errorf("Set %s is missing from the loaded database", selector)
	}
	if kind == "geoip" && entry.inverse {
		return nil, nil, fmt.Errorf("Inverse set %s is not supported", selector)
	}
	err = geoFields(entry.raw, func(f geoField) error {
		if f.number != 2 {
			return nil
		}
		if kind == "geosite" {
			typ := uint64(0)
			value := ""
			matches := attr == ""
			e := geoFields(f.value, func(d geoField) error {
				switch d.number {
				case 1:
					typ = d.integer
				case 2:
					value = string(d.value)
				case 3:
					return geoFields(d.value, func(a geoField) error {
						if a.number == 1 && strings.ToLower(string(a.value)) == attr {
							matches = true
						}
						return nil
					})
				}
				return nil
			})
			if e != nil {
				return e
			}
			if !matches {
				return nil
			}
			prefix, ok := map[uint64]string{0: "", 1: "regexp:", 2: "domain:", 3: "full:"}[typ]
			if !ok || value == "" || strings.ContainsAny(value, "\x00\r\n") {
				return errors.New("invalid geosite domain")
			}
			domains = append(domains, prefix+value)
		} else {
			var ip []byte
			var bits uint64
			if err := geoFields(f.value, func(c geoField) error {
				if c.number == 1 {
					ip = c.value
				}
				if c.number == 2 {
					bits = c.integer
				}
				return nil
			}); err != nil {
				return err
			}
			address, ok := netip.AddrFromSlice(ip)
			if !ok || bits > uint64(address.BitLen()) {
				return errors.New("invalid geoip network")
			}
			ips = append(ips, netip.PrefixFrom(address, int(bits)).Masked().String())
		}
		return nil
	})
	if err == nil && len(domains)+len(ips) == 0 {
		err = fmt.Errorf("Set %s is empty", selector)
	}
	return
}

// InstallGeoFiles validates both inputs before atomically publishing a new pair.
func InstallGeoFiles(root string, site, ip []byte) error {
	for kind, data := range map[string][]byte{"geosite": site, "geoip": ip} {
		if len(data) > maxGeoFile {
			return errors.New("geo file too large")
		}
		if _, err := parseGeo(data, kind); err != nil {
			return fmt.Errorf("%s: %w", kind, err)
		}
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(root, ".update.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	stage, err := os.MkdirTemp(root, "generation.")
	if err != nil {
		return err
	}
	defer func() {
		if stage != "" {
			os.RemoveAll(stage)
		}
	}()
	for kind, data := range map[string][]byte{"geosite": site, "geoip": ip} {
		if err = os.WriteFile(filepath.Join(stage, kind+".dat"), data, 0600); err != nil {
			return err
		}
	}
	previous, _ := os.Readlink(filepath.Join(root, "current"))
	next := filepath.Join(root, ".current-next")
	os.Remove(next)
	if err = os.Symlink(filepath.Base(stage), next); err != nil {
		return err
	}
	if err = os.Rename(next, filepath.Join(root, "current")); err != nil {
		return err
	}
	keep := filepath.Base(stage)
	stage = ""
	dirs, _ := filepath.Glob(filepath.Join(root, "generation.*"))
	for _, dir := range dirs {
		if filepath.Base(dir) != keep && filepath.Base(dir) != previous {
			os.RemoveAll(dir)
		}
	}
	return nil
}
