// Package router exposes a narrow rpcd executable API. All files and commands
// are fixed; source files are explicit absolute JSON paths, with no shell interpolation.
package router

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"nadkop/internal/core"
)

const configPath = "/etc/nadkop/config.json"
const nodesPath = "/etc/nadkop/nodes.json"
const runPath = "/var/run/nadkop"
const limit = 4 << 20

var signature = map[string]any{
	"get": map[string]any{}, "status": map[string]any{}, "geodata": map[string]any{}, "updategeo": map[string]any{"geodata": map[string]any{}},
	"save":      map[string]any{"config": map[string]any{}, "nodes": []any{}, "revision": ""},
	"import":    map[string]any{"content": ""},
	"addsource": map[string]any{"content": "", "revision": "", "options": map[string]any{}},
	"check":     map[string]any{}, "validate": map[string]any{"config": map[string]any{}}, "nodes": map[string]any{},
	"probe":      map[string]any{"refs": []any{}, "mode": "", "url": ""},
	"zaprettune": map[string]any{"action": "", "wan": "", "targets": []any{}, "protocols": []any{}, "revision": "", "job": "", "strategy": ""},
	"identity":   map[string]any{},
	"service":    map[string]any{"action": ""},
}

type request struct {
	Geodata   *core.GeoSettings `json:"geodata"`
	Refs      []string          `json:"refs"`
	Mode      string            `json:"mode"`
	URL       string            `json:"url"`
	Config    json.RawMessage   `json:"config"`
	Nodes     json.RawMessage   `json:"nodes"`
	Revision  string            `json:"revision"`
	Content   string            `json:"content"`
	Action    string            `json:"action"`
	WAN       string            `json:"wan"`
	Targets   []string          `json:"targets"`
	Protocols []string          `json:"protocols"`
	Job       string            `json:"job"`
	Strategy  string            `json:"strategy"`
	Options   core.Source       `json:"options"`
	// rpcd may add the authenticated session id; authorization stays in ACLs.
	Session string `json:"ubus_rpc_session"`
}

type backend struct{ config, nodes, runtime string }

// Run implements rpcd's executable plugin protocol: list, or call METHOD with JSON stdin.
func Run(args []string, in io.Reader, out io.Writer) error {
	var value any
	if len(args) == 1 && args[0] == "list" {
		value = signature
	} else if len(args) == 2 && args[0] == "call" {
		var req request
		body, readErr := io.ReadAll(io.LimitReader(in, limit+1))
		d := json.NewDecoder(bytes.NewReader(body))
		d.DisallowUnknownFields()
		err := d.Decode(&req)
		trimmed := bytes.TrimSpace(body)
		if readErr != nil || len(body) > limit || len(trimmed) == 0 || trimmed[0] != '{' {
			err = errors.New("oversized request")
		}
		if err == nil {
			var extra any
			if d.Decode(&extra) != io.EOF {
				err = errors.New("expected one request")
			}
		}
		if err != nil {
			value = map[string]any{"error": "Invalid or oversized request"}
		} else {
			timeout := 24 * time.Second
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			result, err := (backend{configPath, nodesPath, runPath}).call(ctx, args[1], req)
			if err != nil {
				value = rpcError(err)
			} else {
				value = result
			}
		}
	} else {
		return errors.New("usage: nadkop rpc list|call METHOD")
	}
	return json.NewEncoder(out).Encode(value)
}

func rpcError(err error) map[string]any {
	result := map[string]any{"error": err.Error()}
	var issue *core.RuleValidationError
	if errors.As(err, &issue) {
		result["ruleIndex"] = issue.RuleIndex
		result["field"] = issue.Field
		result["message"] = issue.Message
	}
	return result
}

func (b backend) read() ([]byte, []byte, string, error) {
	c, err := core.ReadPrivateState(b.config)
	if err != nil {
		return nil, nil, "", errors.New("Cannot read configuration")
	}
	n, err := core.ReadPrivateState(b.nodes)
	if err != nil {
		return nil, nil, "", errors.New("Cannot read local nodes")
	}
	if err := core.VerifySavedPair(b.config, c, n); err != nil {
		return nil, nil, "", err
	}
	h := sha256.New()
	h.Write(c)
	h.Write([]byte{0})
	h.Write(n)
	return c, n, hex.EncodeToString(h.Sum(nil)), nil
}

func (b backend) lock() (func(), error) {
	if err := os.MkdirAll(b.runtime, 0711); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(b.runtime, "ui.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, errors.New("Another save or service operation is running")
	}
	unlockState, err := core.LockConfiguration(b.config, true)
	if err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		if errors.Is(err, core.ErrInterruptedSave) {
			return nil, err
		}
		return nil, errors.New("Another configuration operation is running")
	}
	return func() { unlockState(); syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func parseNodes(data []byte) ([]core.Node, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("[]")) {
		return []core.Node{}, nil
	}
	nodes, err := core.ParseSubscription("local", data)
	if err != nil {
		return nil, fmt.Errorf("Local node import: %w", err)
	}
	return core.UnprefixNodes("local", nodes)
}

func (b backend) call(ctx context.Context, method string, r request) (any, error) {
	switch method {
	case "geodata":
		return b.geoCatalogue(), nil
	case "updategeo":
		return b.updateGeo(ctx, r.Geodata)
	case "identity":
		id, err := core.DeviceID(filepath.Join(filepath.Dir(b.config), "hwid"))
		if err != nil {
			return nil, err
		}
		info := core.ReadDeviceInfo()
		return map[string]any{"id": id, "os": info.OS, "version": info.Version, "model": info.Model}, nil
	case "get":
		c, n, rev, err := b.read()
		if err != nil {
			return nil, err
		}
		// Never return arbitrary file contents when the stored JSON is corrupt.
		if !json.Valid(c) || !json.Valid(n) {
			return nil, errors.New("Invalid JSON on disk; repair configuration over SSH")
		}
		servers, err := core.ServerSummaries(filepath.Join(filepath.Dir(b.config), "servers.json"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"config": json.RawMessage(c), "nodes": json.RawMessage(n), "revision": rev, "servers": servers}, nil
	case "import":
		nodes, err := parseNodes([]byte(r.Content))
		if err != nil {
			return nil, err
		}
		return map[string]any{"nodes": nodes}, nil
	case "probe":
		return b.probe(ctx, r)
	case "zaprettune":
		switch r.Action {
		case "start":
			return b.tuneStart(ctx, r)
		case "status":
			return b.tuneStatus(r)
		case "cancel":
			return b.tuneCancel(r)
		case "select":
			return b.tuneSelect(r)
		default:
			return nil, errors.New("Unknown Zapret tuning action")
		}
	case "addsource":
		return b.addSource(ctx, r)
	case "validate":
		c, err := core.ReadConfig(bytes.NewReader(r.Config))
		if err != nil {
			return nil, err
		}
		if err = core.ValidateRuleSets(c, b.geoRoot()); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "priority": "first-match"}, nil
	case "save":
		unlock, err := b.lock()
		if err != nil {
			return nil, err
		}
		defer unlock()
		oldC, oldN, rev, err := b.read()
		if err != nil {
			return nil, err
		}
		if rev != r.Revision {
			return nil, errors.New("Configuration changed: reload the page before saving")
		}
		c, err := core.ReadConfig(bytes.NewReader(r.Config))
		if err != nil {
			return nil, err
		}
		if len(c.Sources) > 16 {
			return nil, errors.New("At most 16 subscriptions are supported")
		}
		for _, s := range c.Sources {
			if s.File != "" {
				if err := core.ValidateSourceFile(s.File); err != nil {
					return nil, err
				}
			}
		}
		nodes, err := parseNodes(r.Nodes)
		if err != nil {
			return nil, err
		}
		// Catch typos in local references without fetching remote subscriptions on save.
		tags := map[string]bool{}
		for _, s := range c.Sources {
			if s.File == b.nodes {
				for _, n := range nodes {
					tags[s.Name+"/"+n.Tag] = true
				}
			}
		}
		servers, err := core.ServerSummaries(filepath.Join(filepath.Dir(b.config), "servers.json"))
		if err != nil {
			return nil, err
		}
		for i, rule := range c.Rules {
			if rule.Action == "proxy" {
				refs := append([]string{}, rule.NodeRefs...)
				if rule.NodeRef != "" {
					refs = append(refs, rule.NodeRef)
				}
				if len(refs) > 0 {
					for _, ref := range refs {
						known := false
						for _, server := range servers {
							if server.ID == ref && !server.Separator {
								known = true
								break
							}
						}
						if !known {
							return nil, errors.New("Proxy rule references an unknown server ID; refresh the server picker")
						}
					}
					continue
				}
				// Upgrade old tag references when the catalogue identifies them
				// uniquely, including a retained missing-server record.
				ref := ""
				ambiguous := false
				for _, server := range servers {
					match := server.Tag == rule.Node
					for _, alias := range server.Aliases {
						if alias == rule.Node {
							match = true
						}
					}
					if match {
						if server.Separator {
							return nil, errors.New("Subscription separators cannot be used as proxy servers")
						}
						if ref != "" && ref != server.ID {
							ambiguous = true
						}
						ref = server.ID
					}
				}
				if ref != "" && !ambiguous {
					c.Rules[i].NodeRef = ref
					c.Rules[i].Node = ""
					continue
				}
				found := false
				for _, s := range c.Sources {
					if strings.HasPrefix(rule.Node, s.Name+"/") {
						found = s.URL != "" || s.File != b.nodes || tags[rule.Node]
						break
					}
				}
				if !found {
					return nil, errors.New("Proxy rule references an unknown source or local node")
				}
			}
		}
		if err := core.ValidateRuleSets(c, b.geoRoot()); err != nil {
			return nil, err
		}
		cb, err := encodeSavedJSON(c)
		if err != nil {
			return nil, err
		}
		nb, err := encodeSavedJSON(nodes)
		if err != nil {
			return nil, err
		}
		if err := core.CheckServerPrune(filepath.Join(filepath.Dir(b.config), "servers.json"), c); err != nil {
			return nil, err
		}
		intentPath := filepath.Join(filepath.Dir(b.config), core.SaveIntentFile)
		if err = atomicWrite(intentPath, core.SaveIntentJSONBytes(oldC, oldN, cb, nb)); err != nil {
			return nil, errors.New("Cannot prepare save; inspect saved configuration before retrying")
		}
		if err = atomicWrite(b.nodes, nb); err != nil {
			return nil, errors.New("Cannot save local nodes")
		}
		if err = atomicWrite(b.config, cb); err != nil {
			// A directory-fsync error can follow a successful rename. Keep a
			// coherent new pair if both new files are already visible.
			visible, _ := os.ReadFile(b.config)
			if len(visible) == len(cb)+1 && visible[len(cb)] == '\n' && bytes.Equal(visible[:len(cb)], cb) {
				return nil, errors.New("Configuration written but durability is unconfirmed; inspect saved files before retrying")
			}
			if atomicWriteData(b.nodes, oldN) != nil {
				return nil, errors.New("Save and local node rollback failed; inspect files over SSH")
			}
			return nil, errors.New("Cannot save configuration")
		}
		servers, err = core.PruneServers(filepath.Join(filepath.Dir(b.config), "servers.json"), c)
		if err != nil {
			return nil, fmt.Errorf("Configuration saved, but server cleanup failed: %w", err)
		}
		if err = os.Remove(intentPath); err != nil {
			return nil, errors.New("Configuration saved; save verification cleanup failed")
		}
		directory, e := os.Open(filepath.Dir(intentPath))
		if e != nil {
			return nil, e
		}
		e = directory.Sync()
		directory.Close()
		if e != nil {
			return nil, e
		}
		_, _, rev, err = b.read()
		return map[string]any{"revision": rev, "servers": servers}, err
	case "status":
		return b.status(ctx), nil
	case "check":
		unlock, err := b.lock()
		if err != nil {
			return nil, err
		}
		defer unlock()
		return b.diagnose(ctx, command), nil
	case "nodes", "service":
		if method == "service" && r.Action != "start" && r.Action != "restart" && r.Action != "stop" && r.Action != "enable" && r.Action != "disable" {
			return nil, errors.New("Unsupported service action")
		}
		unlock, err := b.lock()
		if err != nil {
			return nil, err
		}
		defer unlock()
		if method == "service" && r.Action != "start" && r.Action != "restart" {
			if _, err := command(ctx, "/etc/init.d/nadkop", r.Action); err != nil {
				return nil, errors.New("Service operation failed")
			}
			return map[string]any{"ok": true}, nil
		}
		c, nodes, err := b.load(ctx)
		if err != nil {
			return nil, err
		}
		if method == "nodes" {
			summaries, err := core.ServerSummaries(filepath.Join(filepath.Dir(b.config), "servers.json"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"nodes": summaries}, nil
		}
		dir, err := b.prepare(ctx, c, nodes)
		if err != nil {
			return nil, err
		}
		defer func() {
			if dir != "" {
				os.RemoveAll(dir)
			}
		}()
		if method == "service" {
			// procd updates the service only after generation/preflight succeeds.
			// Calling restart would stop the working generation before validation.
			cmd := exec.CommandContext(ctx, "/etc/init.d/nadkop", "start")
			cmd.Env = append(os.Environ(), "NADKOP_PREPARED="+dir)
			if err := cmd.Run(); err != nil {
				return nil, errors.New("Service operation failed; inspect service status")
			}
			// The service owns this generation after a successful start.
			dir = ""
		}
		return map[string]any{"ok": true, "nodeCount": len(nodes)}, nil
	}
	return nil, errors.New("Unknown method")
}

func (b backend) load(ctx context.Context) (core.Config, []core.Node, error) {
	return b.loadWith(ctx, core.LoadContext)
}

func (b backend) prepare(ctx context.Context, c core.Config, nodes []core.Node) (string, error) {
	return b.prepareSteps(ctx, c, nodes, command, nil)
}

func (b backend) prepareSteps(ctx context.Context, c core.Config, nodes []core.Node, run commandRunner, report *diagnosticReport) (string, error) {
	var a core.Artifacts
	var dir string
	ok := false
	defer func() {
		if !ok && dir != "" {
			os.RemoveAll(dir)
		}
	}()
	err := report.step("routes", fmt.Sprintf("Rules: %d", len(c.Rules)), func() error {
		var err error
		a, err = core.CompileWithGeo(c, nodes, b.geoRoot())
		if err != nil {
			return fmt.Errorf("Compile: %w", err)
		}
		dir, err = os.MkdirTemp(b.runtime, "generation.")
		if err != nil {
			return errors.New("Cannot create generation")
		}
		for name, data := range a.Files() {
			if os.WriteFile(filepath.Join(dir, name), data, 0600) != nil {
				return errors.New("Cannot write generation")
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err = report.step("xray", "Configuration accepted", func() error {
		if _, err := run(ctx, "/usr/bin/xray", "run", "-test", "-config", filepath.Join(dir, "xray.json")); err != nil {
			return errors.New("Xray rejected configuration; check node settings")
		}
		return nil
	}); err != nil {
		return "", err
	}
	if len(a.Hosts)+len(a.IPs) > 0 {
		if err = report.step("nfqws", "nfqws is available", func() error {
			return checkZapret(ctx, dir, run)
		}); err != nil {
			return "", err
		}
	} else {
		report.skip("nfqws", "No Zapret rules")
	}
	if len(a.NFT) > 0 {
		if err = report.step("nftables", "Routing and rules accepted", func() error {
			plan, err := readRuntime(dir)
			if err != nil {
				return err
			}
			if err := checkNetwork(ctx, plan, run); err != nil {
				return err
			}
			if err := checkListeners(ctx, plan, run); err != nil {
				return err
			}
			path, err := b.nftTransaction(ctx, dir, run)
			if err != nil {
				return err
			}
			if _, err := run(ctx, "/usr/sbin/nft", "-c", "-f", path); err != nil {
				return errors.New("nftables rejected rules; check TPROXY/NFQUEUE modules and firewall4")
			}
			return nil
		}); err != nil {
			return "", err
		}
	} else {
		report.skip("nftables", "No transparent routing or Zapret")
		plan, _ := readRuntime(dir)
		if err := checkListeners(ctx, plan, run); err != nil {
			return "", err
		}
	}
	ok = true
	return dir, nil
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (b backend) status(ctx context.Context) any {
	return b.statusWith(ctx, command, "/proc/net/netfilter/nfnetlink_queue")
}

func (b backend) statusWith(ctx context.Context, run commandRunner, queuePath string) any {
	running := map[string]any{}
	data, err := run(ctx, "/bin/ubus", "call", "service", "list", `{"name":"nadkop"}`)
	if err == nil {
		var services map[string]struct {
			Instances map[string]struct {
				Running bool `json:"running"`
				PID     int  `json:"pid"`
			} `json:"instances"`
		}
		if json.Unmarshal(data, &services) == nil {
			for name, instance := range services["nadkop"].Instances {
				running[name] = map[string]any{"running": instance.Running, "pid": instance.PID}
			}
		}
	}
	_, enabledErr := run(ctx, "/etc/init.d/nadkop", "enabled")
	devices := []string{}
	if interfaces, err := net.Interfaces(); err == nil {
		for _, i := range interfaces {
			if i.Name != "lo" {
				devices = append(devices, i.Name)
			}
		}
	}
	var updated int64
	if info, err := os.Stat(filepath.Join(b.runtime, "current", "xray.json")); err == nil {
		updated = info.ModTime().Unix()
	}
	_, queueErr := os.Stat(queuePath)
	zapretInfo, zapretErr := os.Stat(filepath.Join(b.runtime, "current", "zapret-hosts.txt"))
	ipsetInfo, ipsetErr := os.Stat(filepath.Join(b.runtime, "current", "zapret-ips.txt"))
	zapretRequired := zapretErr == nil && zapretInfo.Size() > 0 || ipsetErr == nil && ipsetInfo.Size() > 0
	checks := b.runtimeChecks(ctx, run, running, zapretRequired, updated != 0, queuePath)
	return map[string]any{"instances": running, "enabled": enabledErr == nil, "devices": devices, "generatedAt": updated, "queueAvailable": queueErr == nil, "zapretRequired": zapretRequired, "runtimeChecks": checks}
}

// encodeSavedJSON checks the bytes atomicWrite will publish, including newline,
// before any file belonging to the operation is changed.
func encodeSavedJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, errors.New("Cannot encode saved JSON")
	}
	if len(data)+1 > limit {
		return nil, errors.New("Saved configuration or local nodes would exceed 4 MiB")
	}
	return data, nil
}

func atomicWrite(path string, data []byte) error {
	return atomicWriteContent(path, data, true)
}

func atomicWriteData(path string, data []byte) error {
	return atomicWriteContent(path, data, false)
}

func atomicWriteContent(path string, data []byte, newline bool) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".nadkop-ui-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if newline {
		if _, err = f.Write([]byte{'\n'}); err != nil {
			return err
		}
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
