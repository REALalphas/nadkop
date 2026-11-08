package core

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
)

const HWIDPath = "/etc/nadkop/hwid"

var hwidPattern = regexp.MustCompile(`^[a-zA-Z0-9=-]{10,64}$`)

type HWIDSettings struct {
	Enabled bool   `json:"enabled"`
	ID      string `json:"id,omitempty"`
	OS      string `json:"os,omitempty"`
	Version string `json:"version,omitempty"`
	Model   string `json:"model,omitempty"`
}

type DeviceInfo struct {
	OS      string `json:"os"`
	Version string `json:"version"`
	Model   string `json:"model"`
}

func (s Source) validateHWID() error {
	if len(s.Headers) > 64 {
		return errors.New("at most 64 HTTP headers are supported")
	}
	seen := map[string]bool{}
	for key, value := range s.Headers {
		if key == "" || len(key) > 256 || len(value) > 8192 {
			return errors.New("invalid HTTP header name or size")
		}
		for _, r := range key {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
				return errors.New("invalid HTTP header name")
			}
		}
		for _, r := range value {
			if r < 32 && r != '\t' || r == 127 {
				return errors.New("invalid HTTP header value")
			}
		}
		lower := strings.ToLower(key)
		if lower == "host" || lower == "content-length" || lower == "transfer-encoding" {
			return errors.New("reserved HTTP request header")
		}
		if seen[lower] {
			return errors.New("HTTP header names must be unique ignoring case")
		}
		seen[lower] = true
	}
	if s.HWID == nil || !s.HWID.Enabled {
		return nil
	}
	if s.File != "" {
		return errors.New("HWID only applies to HTTPS subscriptions")
	}
	if s.HWID.ID != "" && !hwidPattern.MatchString(s.HWID.ID) {
		return errors.New("HWID must contain 10–64 letters, digits, = or -")
	}
	for _, value := range []string{s.HWID.OS, s.HWID.Version, s.HWID.Model} {
		if len(value) > 64 || strings.IndexFunc(value, func(r rune) bool { return r < 32 || r > 126 }) >= 0 {
			return errors.New("HWID device fields must contain at most 64 printable ASCII characters")
		}
	}
	return nil
}

// DeviceID creates an installation ID once. A separate lock survives the atomic
// rename, so concurrent subscription requests cannot publish different IDs.
func DeviceID(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", errors.New("cannot create HWID directory")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return "", errors.New("cannot open HWID lock")
	}
	defer lock.Close()
	if stat, err := lock.Stat(); err != nil || !stat.Mode().IsRegular() {
		return "", errors.New("HWID lock must be a regular file")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", errors.New("cannot lock HWID file")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	f, err := openPrivateState(path)
	if err == nil {
		data, readErr := io.ReadAll(io.LimitReader(f, 128))
		f.Close()
		if readErr != nil {
			return "", errors.New("cannot read or protect HWID file")
		}
		id := strings.TrimSpace(string(data))
		if id != "" {
			if !hwidPattern.MatchString(id) {
				return "", errors.New("stored HWID is invalid; repair it before enabling HWID")
			}
			return id, nil
		}
		// Empty/whitespace HWID alongside a non-empty sibling catalogue is a
		// partial restore, not a first boot: never mint a fresh identity over
		// an installed system. The packaged empty seed has no catalogue yet.
		if nonEmpty, statErr := siblingCatalogueNonEmpty(path); statErr != nil {
			return "", errors.New("cannot read or protect HWID file")
		} else if nonEmpty {
			return "", errors.New("restored HWID is missing; recover the original HWID instead of reinitializing")
		}
	} else if os.IsNotExist(err) {
		// A catalogue with saved bindings but no HWID file is the same partial
		// restore; minting here would permanently fork the device identity.
		if nonEmpty, statErr := siblingCatalogueNonEmpty(path); statErr != nil {
			return "", errors.New("cannot read HWID file")
		} else if nonEmpty {
			return "", errors.New("restored HWID is missing; recover the original HWID instead of reinitializing")
		}
	} else if errors.Is(err, ErrPrivateStateOwner) {
		return "", err
	} else {
		return "", errors.New("cannot read HWID file")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errors.New("cannot generate HWID")
	}
	id := hex.EncodeToString(random[:])
	temp, err := os.CreateTemp(filepath.Dir(path), ".nadkop-hwid-*")
	if err != nil {
		return "", errors.New("cannot save HWID")
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err := temp.WriteString(id + "\n"); err != nil {
		return "", errors.New("cannot save HWID")
	}
	if temp.Sync() != nil || temp.Close() != nil {
		return "", errors.New("cannot sync HWID")
	}
	if os.Rename(temp.Name(), path) != nil {
		return "", errors.New("cannot publish HWID")
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return "", errors.New("cannot sync HWID directory")
		}
	}
	return id, nil
}

// siblingCatalogueNonEmpty reports whether servers.json next to the HWID path
// still carries stored records. A missing/unreadable catalogue is fail-closed
// (treated as non-empty would block fresh installs), so callers surface the
// stat error instead: empty seed catalogue {"version":1,"servers":[]} counts
// as empty and preserves first-boot initialization.
func siblingCatalogueNonEmpty(hwidPath string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(hwidPath), "servers.json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return false, nil
	}
	var stored serverCatalog
	if json.Unmarshal(data, &stored) != nil {
		return true, nil
	}
	return len(stored.Servers) > 0, nil
}

func ReadDeviceInfo() DeviceInfo {
	info := DeviceInfo{OS: "Linux", Model: runtime.GOARCH}
	if release, err := os.ReadFile("/etc/openwrt_release"); err == nil {
		info.OS = "OpenWrt"
		for _, line := range strings.Split(string(release), "\n") {
			if value, ok := strings.CutPrefix(line, "DISTRIB_RELEASE="); ok {
				info.Version = strings.Trim(value, "\"'")
			}
		}
	}
	if model, err := os.ReadFile("/tmp/sysinfo/model"); err == nil {
		info.Model = string(model)
	}
	clean := func(value string) string {
		value = strings.Map(func(r rune) rune {
			if r < 32 || r > 126 {
				return ' '
			}
			return r
		}, value)
		return strings.TrimSpace(value[:min(len(value), 64)])
	}
	info.OS, info.Version, info.Model = clean(info.OS), clean(info.Version), clean(info.Model)
	return info
}

func applySourceHeaders(req *http.Request, s Source, identityPath string, info DeviceInfo) error {
	if err := s.validateHWID(); err != nil {
		return err
	}
	// Subscription panels select Xray JSON using the client name, not Accept
	// alone. Keep the compatibility client visible alongside our own identity.
	req.Header.Set("User-Agent", "Happ/2.4.0 (nadkop/0.2; Linux)")
	req.Header.Set("Accept", "application/json")
	if s.Format == "links" {
		req.Header.Set("User-Agent", "nadkop/0.2")
		req.Header.Set("Accept", "text/plain")
	}
	// Preserve existing manually configured identification, regardless of case.
	for key, value := range s.Headers {
		req.Header.Set(key, value)
	}
	hwid := s.HWID
	if hwid == nil {
		hwid = &HWIDSettings{Enabled: true}
	}
	if !hwid.Enabled {
		return nil
	}
	if req.Header.Get("X-HWID") == "" {
		id := hwid.ID
		if id == "" {
			var err error
			id, err = DeviceID(identityPath)
			if err != nil {
				return err
			}
		}
		req.Header.Set("X-HWID", id)
	}
	for _, item := range []struct{ key, value, fallback string }{
		{"X-Device-OS", hwid.OS, info.OS},
		{"X-Ver-OS", hwid.Version, info.Version},
		{"X-Device-Model", hwid.Model, info.Model},
	} {
		if _, exists := req.Header[http.CanonicalHeaderKey(item.key)]; exists {
			continue
		}
		value := item.value
		if value == "" {
			value = item.fallback
		}
		if value != "" {
			req.Header.Set(item.key, value)
		}
	}
	return nil
}

func trueHeader(value string) bool {
	value = strings.TrimSpace(value)
	return value == "1" || strings.EqualFold(value, "true")
}

func hwidResponseError(headers http.Header, sent bool) error {
	if trueHeader(headers.Get("X-Hwid-Max-Devices-Reached")) || trueHeader(headers.Get("X-Hwid-Limit")) {
		return errors.New("HWID device limit reached; remove an unused device in the provider panel")
	}
	if trueHeader(headers.Get("X-Hwid-Not-Supported")) && sent {
		return errors.New("provider rejected HWID identification; check this source's HWID and HTTP headers")
	}
	if trueHeader(headers.Get("X-Hwid-Not-Supported")) || (!sent && trueHeader(headers.Get("Subscription-Always-Hwid-Enable"))) {
		return errors.New("subscription requires HWID; enable HWID in this source's settings")
	}
	return nil
}

func bodyRequiresHWID(body []byte) bool {
	text := strings.TrimSpace(string(body))
	if !strings.Contains(text, "://") && !strings.HasPrefix(text, "[") {
		if decoded, err := decodeBase64(strings.Join(strings.Fields(text), "")); err == nil {
			text = string(decoded)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), ":")
		if strings.EqualFold(strings.TrimSpace(key), "#subscription-always-hwid-enable") && trueHeader(value) {
			return true
		}
	}
	return false
}
