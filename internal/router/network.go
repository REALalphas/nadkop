package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"nadkop/internal/core"
)

func readRuntime(dir string) (core.RuntimeSettings, error) {
	var plan core.RuntimeSettings
	data, err := os.ReadFile(filepath.Join(dir, "runtime.json"))
	if err != nil || len(data) > 64<<10 {
		return plan, errors.New("Cannot read runtime plan")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, errors.New("Invalid runtime plan")
	}
	var extra any
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return plan, errors.New("Invalid runtime plan")
	}
	if decoder.Decode(&extra) != io.EOF {
		return plan, errors.New("Invalid runtime plan")
	}
	// Reuse the configuration boundary before putting any values into nft.
	encoded, _ := json.Marshal(core.Config{Transparent: plan.Transparent, SOCKSPort: plan.SOCKSPort, WANInterface: plan.WANInterface})
	c, err := core.ReadConfig(bytes.NewReader(encoded))
	if err != nil {
		return plan, err
	}
	plan.Transparent = c.Transparent
	plan.SOCKSPort = c.SOCKSPort
	return plan, nil
}

func number(value any) uint64 {
	s := fmt.Sprint(value)
	if n, err := strconv.ParseUint(s, 0, 64); err == nil {
		return n
	}
	return 0
}

func policyFields(entry map[string]any, allowed string) bool {
	for key := range entry {
		if !strings.Contains(" "+allowed+" ", " "+key+" ") {
			return false
		}
	}
	if flags, ok := entry["flags"]; ok {
		items, ok := flags.([]any)
		if !ok || len(items) != 0 {
			return false
		}
	}
	return true
}

func policyState(ctx context.Context, family string, run commandRunner) (bool, bool, error) {
	data, err := run(ctx, "/sbin/ip", "-j", family, "rule", "show")
	if err != nil {
		return false, false, errors.New("Policy routing requires ip-full and kernel support")
	}
	var entries []map[string]any
	if json.Unmarshal(data, &entries) != nil {
		return false, false, errors.New("Cannot inspect policy routing")
	}
	rule := false
	for _, entry := range entries {
		priority, table, mark := number(entry["priority"]), number(entry["table"]), number(entry["fwmark"])
		if priority != core.RoutingPriority && table != core.RoutingTable && mark&core.TransparentMark == 0 {
			continue
		}
		mask := number(entry["fwmask"])
		if priority != core.RoutingPriority || table != core.RoutingTable || mark != core.TransparentMark || mask != core.TransparentMark || (entry["src"] != nil && entry["src"] != "all") || (entry["dst"] != nil && entry["dst"] != "all") || !policyFields(entry, "priority table fwmark fwmask src dst protocol flags") {
			return false, false, fmt.Errorf("%s: policy rule conflicts with Nadkop mark 0x10000, table 100 or priority 10000", family)
		}
		if rule {
			return false, false, errors.New("Duplicate Nadkop policy rules")
		}
		rule = true
	}
	data, err = run(ctx, "/sbin/ip", "-j", family, "route", "show", "table", "all")
	if err != nil {
		return false, false, errors.New("Cannot inspect routing table 100")
	}
	entries = nil
	if json.Unmarshal(data, &entries) != nil {
		return false, false, errors.New("Cannot inspect routing table 100")
	}
	route := false
	for _, entry := range entries {
		if number(entry["table"]) != core.RoutingTable {
			continue
		}
		v6Defaults := family == "-6" && (entry["metric"] == nil || number(entry["metric"]) == 1024) && (entry["pref"] == nil || entry["pref"] == "medium")
		if route || entry["type"] != "local" || entry["dst"] != "default" || entry["dev"] != "lo" || !policyFields(entry, "table type dst dev protocol scope flags metric pref") || (!v6Defaults && (entry["metric"] != nil || entry["pref"] != nil)) {
			return false, false, errors.New("Routing table 100 belongs to another routing system")
		}
		route = true
	}
	return rule, route, nil
}

func (b backend) nftTransaction(ctx context.Context, dir string, run commandRunner) (string, error) {
	// prepareSteps also builds a read-only preflight transaction before init's
	// Network(check) can recover. A pending private journal proves ownership
	// at that point, including a failed first start without current.
	_, err := b.readNetworkTransaction()
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return b.nftTransactionOwned(ctx, dir, run, err == nil)
}

func (b backend) nftTransactionOwned(ctx context.Context, dir string, run commandRunner, transactionOwned bool) (string, error) {
	rules, err := os.ReadFile(filepath.Join(dir, "rules.nft"))
	if err != nil {
		return "", err
	}
	input, err := os.ReadFile(filepath.Join(dir, "input.nft"))
	if err != nil {
		return "", err
	}
	var script strings.Builder
	if data, err := run(ctx, "/usr/sbin/nft", "list", "table", "inet", "nadkop"); err == nil && len(bytes.TrimSpace(data)) > 0 {
		if _, err := os.Stat(filepath.Join(b.runtime, "current", "xray.json")); err != nil && !transactionOwned {
			return "", errors.New("Table inet nadkop exists without an owned generation")
		}
		script.WriteString("delete table inet nadkop\n")
	}
	script.Write(rules)
	chain, fwErr := run(ctx, "/usr/sbin/nft", "-a", "list", "chain", "inet", "fw4", "input")
	if fwErr != nil && len(bytes.TrimSpace(input)) > 0 {
		return "", errors.New("Transparent routing requires an active firewall4 input chain")
	}
	for _, line := range strings.Split(string(chain), "\n") {
		if !strings.Contains(line, `comment "nadkop-tproxy"`) {
			continue
		}
		_, handle, ok := strings.Cut(line, "# handle ")
		id, err := strconv.ParseUint(strings.TrimSpace(handle), 10, 64)
		if !ok || err != nil {
			return "", errors.New("Cannot inspect Nadkop firewall exception")
		}
		fmt.Fprintf(&script, "delete rule inet fw4 input handle %d\n", id)
	}
	if len(bytes.TrimSpace(input)) > 0 {
		script.WriteString("insert rule inet fw4 input ")
		script.Write(input)
	}
	path := filepath.Join(dir, "apply.nft")
	if err := os.WriteFile(path, []byte(script.String()), 0600); err != nil {
		return "", err
	}
	return path, nil
}

func checkNetwork(ctx context.Context, plan core.RuntimeSettings, run commandRunner) error {
	if !plan.Transparent.Enabled {
		return nil
	}
	for _, iface := range plan.Transparent.Interfaces {
		if _, err := run(ctx, "/sbin/ip", "link", "show", "dev", iface); err != nil {
			return fmt.Errorf("LAN device %s does not exist", iface)
		}
	}
	for _, setting := range []string{"firewall.@defaults[0].flow_offloading", "firewall.@defaults[0].flow_offloading_hw"} {
		data, _ := run(ctx, "/sbin/uci", "-q", "get", setting)
		if strings.TrimSpace(string(data)) == "1" {
			return errors.New("Disable firewall flow offloading before enabling transparent routing; existing offloaded flows bypass policy")
		}
	}
	data, _ := run(ctx, "/sbin/uci", "-q", "get", "firewall.@defaults[0].auto_includes")
	if strings.TrimSpace(string(data)) == "0" {
		return errors.New("Enable firewall4 auto_includes to preserve Nadkop routing after firewall reload")
	}
	for _, family := range []string{"-4", "-6"} {
		if family == "-6" && !plan.Transparent.IPv6 {
			continue
		}
		if _, _, err := policyState(ctx, family, run); err != nil {
			return err
		}
	}
	return nil
}

// Network manages only Nadkop's table, narrowly scoped fw4 input exception and
// reserved policy route. Every nft change is a single atomic transaction.
func Network(ctx context.Context, action, dir string) error {
	b := backend{configPath, nodesPath, runPath}
	if err := os.MkdirAll(b.runtime, 0711); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(b.runtime, "network.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if action == "rollback" {
		return b.rollbackNetwork(ctx, dir, command)
	}
	if action == "commit" {
		return b.commitNetwork(dir)
	}
	if action != "stop" && action != "check" && action != "apply" {
		return errors.New("unknown network action")
	}
	if err := b.recoverNetwork(ctx, command); err != nil {
		return err
	}
	if action == "stop" {
		return b.stopNetwork(ctx, command)
	}
	plan, err := readRuntime(dir)
	if err != nil {
		return err
	}
	if err := checkNetwork(ctx, plan, command); err != nil {
		return err
	}
	if action == "check" {
		if plan.Zapret {
			if err := checkZapret(ctx, dir, command); err != nil {
				return err
			}
		}
		if err := checkListeners(ctx, plan, command); err != nil {
			return err
		}
	}
	path, err := b.nftTransaction(ctx, dir, command)
	if err != nil {
		return err
	}
	if _, err := command(ctx, "/usr/sbin/nft", "-c", "-f", path); err != nil {
		return errors.New("nftables preflight failed; inspect modules, marks and firewall4")
	}
	if action == "check" {
		return nil
	}
	return b.applyNetwork(ctx, plan, path, command)
}

func (b backend) applyNetwork(ctx context.Context, plan core.RuntimeSettings, path string, run commandRunner) error {
	if err := b.recoverNetwork(ctx, run); err != nil {
		return err
	}
	tx, err := b.beginNetwork(ctx, plan, filepath.Dir(path), run)
	if err != nil {
		return err
	}
	type added struct{ family, kind string }
	changes := []added{}
	rollback := func(cause error) error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var rollbackErr error
		inspected := map[string]error{}
		for i := len(changes) - 1; i >= 0; i-- {
			item := changes[i]
			if _, ok := inspected[item.family]; !ok {
				_, _, err := policyState(cleanupCtx, item.family, run)
				inspected[item.family] = err
				rollbackErr = errors.Join(rollbackErr, err)
			}
			if inspected[item.family] != nil {
				continue
			}
			rollbackErr = errors.Join(rollbackErr, removePolicy(cleanupCtx, item.family, item.kind, run))
		}
		if rollbackErr == nil {
			rollbackErr = os.Remove(b.networkJournal())
		}
		if rollbackErr != nil {
			return errors.Join(cause, errors.New("Network rollback incomplete; recovery journal retained"), rollbackErr)
		}
		return cause
	}
	if plan.Transparent.Enabled {
		for _, family := range []string{"-4", "-6"} {
			if family == "-6" && !plan.Transparent.IPv6 {
				continue
			}
			rule, route, err := policyState(ctx, family, run)
			if err != nil {
				return rollback(err)
			}
			if !route {
				if _, err := run(ctx, "/sbin/ip", family, "route", "add", "local", "default", "dev", "lo", "table", "100"); err != nil {
					return rollback(errors.New("Cannot add Nadkop local route"))
				}
				changes = append(changes, added{family, "route"})
			}
			if !rule {
				if _, err := run(ctx, "/sbin/ip", family, "rule", "add", "priority", "10000", "fwmark", "0x10000/0x10000", "lookup", "100"); err != nil {
					return rollback(errors.New("Cannot add Nadkop policy rule"))
				}
				changes = append(changes, added{family, "rule"})
			}
		}
	}
	if _, err := run(ctx, "/usr/sbin/nft", "-f", path); err != nil {
		return rollback(errors.New("Cannot apply nftables transaction; previous rules retained"))
	}
	// Remove only previously owned routes when disabling a family or TPROXY.
	previous, _ := readRuntime(filepath.Join(b.runtime, "current"))
	if previous.Transparent.Enabled {
		for _, family := range []string{"-4", "-6"} {
			if family == "-6" && !previous.Transparent.IPv6 {
				continue
			}
			if plan.Transparent.Enabled && (family == "-4" || plan.Transparent.IPv6) {
				continue
			}
			if err := cleanupPolicy(ctx, family, run); err != nil {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				return errors.Join(err, b.restoreNetwork(cleanupCtx, tx, run))
			}
		}
	}
	return nil
}

func removePolicy(ctx context.Context, family, kind string, run commandRunner) error {
	var err error
	if kind == "rule" {
		_, err = run(ctx, "/sbin/ip", family, "rule", "del", "priority", "10000", "fwmark", "0x10000/0x10000", "lookup", "100")
	} else {
		_, err = run(ctx, "/sbin/ip", family, "route", "del", "local", "default", "dev", "lo", "table", "100")
	}
	return err
}

// The journal is written before touching the network. It records actual policy
// presence, rather than inferring it from the still-unpublished runtime plan.
// Only one operation can be pending under network.lock.
type networkPolicy struct {
	Family string
	Rule   bool
	Route  bool
}

type networkTransaction struct {
	Generation string
	Rules      []byte
	Input      []byte
	Policies   []networkPolicy
}

func (b backend) networkJournal() string {
	return filepath.Join(b.runtime, "network-transaction.json")
}

func networkGeneration(dir string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Abs(resolved)
	}
	return filepath.Abs(dir)
}

func (b backend) beginNetwork(ctx context.Context, plan core.RuntimeSettings, dir string, run commandRunner) (networkTransaction, error) {
	tx := networkTransaction{}
	var err error
	tx.Generation, err = networkGeneration(dir)
	if err != nil {
		return tx, err
	}
	previous := core.RuntimeSettings{}
	if _, err := os.Lstat(filepath.Join(b.runtime, "current")); err == nil {
		previous, err = readRuntime(filepath.Join(b.runtime, "current"))
		if err != nil {
			return tx, err
		}
		tx.Rules, err = os.ReadFile(filepath.Join(b.runtime, "current", "rules.nft"))
		if err != nil {
			return tx, err
		}
		tx.Input, err = os.ReadFile(filepath.Join(b.runtime, "current", "input.nft"))
		if err != nil {
			return tx, err
		}
	} else if !os.IsNotExist(err) {
		return tx, err
	}
	for _, family := range []string{"-4", "-6"} {
		uses := func(p core.RuntimeSettings) bool {
			return p.Transparent.Enabled && (family == "-4" || p.Transparent.IPv6)
		}
		if !uses(plan) && !uses(previous) {
			continue
		}
		rule, route, err := policyState(ctx, family, run)
		if err != nil {
			return tx, err
		}
		tx.Policies = append(tx.Policies, networkPolicy{family, rule, route})
	}
	if err := os.MkdirAll(b.runtime, 0711); err != nil {
		return tx, err
	}
	data, err := json.Marshal(tx)
	if err == nil {
		err = atomicWrite(b.networkJournal(), data)
	}
	return tx, err
}

func (b backend) readNetworkTransaction() (networkTransaction, error) {
	var tx networkTransaction
	data, err := os.ReadFile(b.networkJournal())
	if err != nil {
		return tx, err
	}
	if json.Unmarshal(data, &tx) != nil || !filepath.IsAbs(tx.Generation) {
		return tx, errors.New("Invalid network recovery journal")
	}
	seen := map[string]bool{}
	for _, p := range tx.Policies {
		if (p.Family != "-4" && p.Family != "-6") || seen[p.Family] {
			return tx, errors.New("Invalid network recovery policies")
		}
		seen[p.Family] = true
	}
	return tx, nil
}

func (b backend) publishedNetwork(tx networkTransaction) bool {
	current, err := filepath.EvalSymlinks(filepath.Join(b.runtime, "current"))
	return err == nil && current == tx.Generation
}

func (b backend) recoverNetwork(ctx context.Context, run commandRunner) error {
	tx, err := b.readNetworkTransaction()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.publishedNetwork(tx) {
		return os.Remove(b.networkJournal())
	}
	return b.restoreNetwork(ctx, tx, run)
}

func (b backend) commitNetwork(dir string) error {
	tx, err := b.readNetworkTransaction()
	if err != nil {
		return err
	}
	generation, err := networkGeneration(dir)
	if err != nil || generation != tx.Generation || !b.publishedNetwork(tx) {
		return errors.New("Network commit does not match current generation")
	}
	return os.Remove(b.networkJournal())
}

func (b backend) rollbackNetwork(ctx context.Context, dir string, run commandRunner) error {
	tx, err := b.readNetworkTransaction()
	if err != nil {
		return err
	}
	generation, err := networkGeneration(dir)
	if err != nil || generation != tx.Generation || b.publishedNetwork(tx) {
		return errors.New("Network rollback does not match pending generation")
	}
	return b.restoreNetwork(ctx, tx, run)
}

func (b backend) restoreNetwork(ctx context.Context, tx networkTransaction, run commandRunner) error {
	dir, err := os.MkdirTemp(b.runtime, "network-rollback.")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, data := range map[string][]byte{"rules.nft": tx.Rules, "input.nft": tx.Input} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
	}
	// The private journal proves ownership even on the first start, before
	// current exists. The normal foreign-table guard remains in force elsewhere.
	path, rollbackErr := b.nftTransactionOwned(ctx, dir, run, true)
	if rollbackErr == nil {
		if _, err := run(ctx, "/usr/sbin/nft", "-f", path); err != nil {
			rollbackErr = errors.New("Cannot restore previous Nadkop firewall rules")
		}
	}
	for _, p := range tx.Policies {
		rule, route, err := policyState(ctx, p.Family, run)
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
			continue
		}
		// Restore routes before rules; remove rules before routes. Never flush
		// a table or delete policies whose exact shape has not been inspected.
		if p.Route && !route {
			_, err = run(ctx, "/sbin/ip", p.Family, "route", "add", "local", "default", "dev", "lo", "table", "100")
			rollbackErr = errors.Join(rollbackErr, err)
		}
		if p.Rule && !rule {
			_, err = run(ctx, "/sbin/ip", p.Family, "rule", "add", "priority", "10000", "fwmark", "0x10000/0x10000", "lookup", "100")
			rollbackErr = errors.Join(rollbackErr, err)
		}
		if rule && !p.Rule {
			rollbackErr = errors.Join(rollbackErr, removePolicy(ctx, p.Family, "rule", run))
		}
		if route && !p.Route {
			rollbackErr = errors.Join(rollbackErr, removePolicy(ctx, p.Family, "route", run))
		}
	}
	if rollbackErr != nil {
		return errors.Join(errors.New("Network rollback incomplete; recovery journal retained"), rollbackErr)
	}
	return os.Remove(b.networkJournal())
}

func cleanupPolicy(ctx context.Context, family string, run commandRunner) error {
	rule, route, err := policyState(ctx, family, run)
	if err != nil {
		return err
	}
	if rule {
		err = removePolicy(ctx, family, "rule", run)
	}
	if route {
		err = errors.Join(err, removePolicy(ctx, family, "route", run))
	}
	return err
}

func (b backend) stopNetwork(ctx context.Context, run commandRunner) error {
	// Use a temporary generation to reuse transactional deletion and handle lookup.
	dir, err := os.MkdirTemp(b.runtime, "network-stop.")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, name := range []string{"rules.nft", "input.nft"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			return err
		}
	}
	path, err := b.nftTransaction(ctx, dir, run)
	if err != nil {
		return err
	}
	if _, err = run(ctx, "/usr/sbin/nft", "-f", path); err != nil {
		return errors.New("Cannot remove Nadkop firewall rules")
	}
	var cleanupErr error
	for _, family := range []string{"-4", "-6"} {
		// A kernel built without IPv6 has no IPv6 routing state. Do not
		// mistake an ip-full failure on an IPv6-capable kernel for absence.
		if family == "-6" {
			if _, err := os.Stat("/proc/sys/net/ipv6"); os.IsNotExist(err) {
				continue
			}
		}
		cleanupErr = errors.Join(cleanupErr, cleanupPolicy(ctx, family, run))
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	if info, err := os.Lstat(filepath.Join(b.runtime, "current")); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(filepath.Join(b.runtime, "current"))
	}
	return nil
}
