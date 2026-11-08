package router

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ZapretTune runs an internal detached worker/guard. IDs never become paths or
// nft identifiers until they pass validation. Only the worker sends requests.
func ZapretTune(ctx context.Context, role, id string) error {
	if !tuneIDPattern.MatchString(id) {
		return errors.New("Invalid Zapret job ID")
	}
	b := backend{configPath, nodesPath, runPath}
	if role == "zapret-guard" {
		return b.tuneGuard(id, command)
	}
	if role != "zapret-worker" {
		return errors.New("Invalid Zapret worker role")
	}
	return b.tuneWorker(ctx, id)
}

func processTick(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	i := bytes.LastIndexByte(data, ')')
	if i < 0 {
		return ""
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 20 || fields[0] == "Z" {
		return ""
	}
	return fields[19] // /proc stat field 22, after pid and comm.
}

func tuneAlive(pid int, tick string) bool { return pid > 1 && tick != "" && processTick(pid) == tick }

func tuneSignal(pid int, tick string, signal syscall.Signal) {
	if tuneAlive(pid, tick) {
		_ = syscall.Kill(pid, signal)
	}
}

func (b backend) tuneWorker(parent context.Context, id string) error {
	unlock, err := b.tuneLock("session")
	if err != nil {
		return err
	}
	defer unlock()
	j, err := b.readTune(id)
	if err != nil || j.Status != "queued" {
		return errors.New("Zapret job is not queued")
	}
	j.WorkerPID, j.WorkerTick = os.Getpid(), processTick(os.Getpid())
	if err := b.writeTune(j); err != nil {
		return err
	}
	guard := exec.Command("/usr/bin/nadkop", "zapret-guard", id)
	guard.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = guard.Start(); err != nil {
		j.Status, j.Message, j.Restored = "failed", "Failed to start recovery guard", true
		_ = b.writeTune(j)
		return err
	}
	go guard.Wait()
	guardPID, guardTick := guard.Process.Pid, processTick(guard.Process.Pid)
	// No network mutation is permitted until an independent guard is alive.
	ready := time.NewTimer(3 * time.Second)
	defer ready.Stop()
	for {
		if data, err := os.ReadFile(filepath.Join(b.tuneDir(id), "guard-ready")); err == nil && strings.TrimSpace(string(data)) == fmt.Sprintf("%d:%s", guardPID, guardTick) && tuneAlive(guardPID, guardTick) {
			break
		}
		select {
		case <-parent.Done():
			tuneSignal(guardPID, guardTick, syscall.SIGKILL)
			j.Status, j.Restored, j.Message = "cancelled", true, "Check was cancelled before network changes"
			_ = b.writeTune(j)
			return parent.Err()
		case <-ready.C:
			tuneSignal(guardPID, guardTick, syscall.SIGKILL)
			j.Status, j.Restored, j.Message = "failed", true, "Recovery guard did not start; network was not changed"
			_ = b.writeTune(j)
			return errors.New("Zapret guard did not start")
		case <-time.After(30 * time.Millisecond):
		}
	}
	ctx, cancel := context.WithDeadline(parent, time.Unix(j.ExpiresAt, 0))
	defer cancel()
	guardLost := make(chan struct{}, 1)
	defer func() {
		// The worker also cleans independently. A dead guard is replaced so that
		// the terminal result is still published after the worker releases session.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = b.tuneCleanup(cleanupCtx, j, command)
		cleanupCancel()
		if !tuneAlive(guardPID, guardTick) {
			b.resumeTuneGuard(id)
		}
	}()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !tuneAlive(guardPID, guardTick) {
					guardLost <- struct{}{}
					cancel()
					return
				}
				if _, err := os.Stat(filepath.Join(b.tuneDir(id), "cancel")); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	j.Status, j.Message = "running", "Resolving addresses and checking network"
	if err := b.writeTune(j); err != nil {
		return err
	}
	err = b.tuneRun(ctx, &j)
	if ctx.Err() != nil {
		select {
		case <-guardLost:
			tuneFailure(&j, "failed", "Recovery guard was interrupted; check was stopped")
		default:
			if _, err := os.Stat(filepath.Join(b.tuneDir(id), "cancel")); err == nil {
				tuneFailure(&j, "cancelled", "Check was cancelled")
			} else {
				tuneFailure(&j, "failed", "Check timed out")
			}
		}
	} else if err != nil {
		tuneFailure(&j, "failed", err.Error())
	}
	// The guard publishes the terminal result only after this process exits and
	// its independent cleanup has succeeded. A lost final write means failure.
	j.Status = "restoring"
	return b.writeTune(j)
}

func (b backend) tuneGuard(id string, run commandRunner) error {
	unlock, err := b.tuneLock("guard-" + id)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := b.readTune(id)
	if err != nil {
		return err
	}
	if !j.active() && j.Restored {
		return nil
	}
	if err = atomicWrite(filepath.Join(b.tuneDir(id), "guard-ready"), []byte(fmt.Sprintf("%d:%s", os.Getpid(), processTick(os.Getpid())))); err != nil {
		return err
	}
	var stopAt time.Time
	var sessionUnlock func()
	for {
		j, err = b.readTune(id)
		if err != nil {
			return err
		}
		if !tuneAlive(j.WorkerPID, j.WorkerTick) {
			sessionUnlock, err = b.tuneLock("session")
			if err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		_, cancelErr := os.Stat(filepath.Join(b.tuneDir(id), "cancel"))
		if time.Now().Unix() >= j.ExpiresAt || cancelErr == nil {
			if stopAt.IsZero() {
				stopAt = time.Now()
				tuneSignal(j.WorkerPID, j.WorkerTick, syscall.SIGTERM)
			}
			if time.Since(stopAt) > 2*time.Second {
				tuneSignal(j.WorkerPID, j.WorkerTick, syscall.SIGKILL)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer func() {
		if sessionUnlock != nil {
			sessionUnlock()
		}
	}()
	// No writer remains. Reload to pick up the worker's final outcome.
	j, err = b.readTune(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = b.tuneCleanup(ctx, j, run)
	if err != nil {
		j.Status, j.Restored, j.Winner = "recovery_failed", false, ""
		j.Outcome = "failed"
		j.Message = "Failed to remove temporary resources. Repeat the cancellation; check the state via SSH."
	} else {
		retried := j.Status == "recovery_failed"
		j.Restored = true
		j.Status = j.Outcome
		if retried {
			j.Status, j.Winner, j.Message = "failed", "", "Temporary resources were removed after a recovery error. Start tuning again."
		}
		if j.Status == "" {
			j.Status, j.Winner, j.Message = "failed", "", "Check was interrupted; temporary resources were removed"
		}
		if _, err := os.Stat(filepath.Join(b.tuneDir(id), "cancel")); err == nil {
			j.Status, j.Winner, j.Message = "cancelled", "", "Check was cancelled; temporary resources were removed"
		}
	}
	j.FinishedAt = time.Now().Unix()
	sessionUnlock()
	sessionUnlock = nil
	return b.writeTune(j)
}

func tuneNft(j tuneJob) string {
	// Exact socket marks and destination addresses prevent interception of user
	// connections. Kernel routing already chose WAN; production sees bypass bit.
	return fmt.Sprintf("create table inet %s {\n comment \"nadkop-zapret-tune:%s\"\n chain postrouting {\n type filter hook postrouting priority -151; policy accept;\n oifname %q meta mark == 0x28000000 ip daddr { %s } tcp dport { 80, 443 } counter queue num 201 bypass\n }\n}\n", tuneTable(j.ID), j.ID, j.WAN, strings.Join(j.IPs(), ", "))
}

// Addresses are pinned outside the public job, not re-resolved between baseline
// and candidates. A later DNS change cannot redirect a marked connection.
func (j tuneJob) IPs() []string {
	ips := []string{}
	seen := map[string]bool{}
	for _, ip := range j.Addresses {
		if !seen[ip] {
			ips = append(ips, ip)
			seen[ip] = true
		}
	}
	sort.Strings(ips)
	return ips
}

func (b backend) tuneCleanup(ctx context.Context, j tuneJob, run commandRunner) (result error) {
	defer func() { result = errors.Join(result, b.tuneCleanupEngine(ctx, j)) }()
	// The ownership marker is synced before the nft transaction. A name collision
	// rejected during preflight therefore cannot authorize deleting another table.
	if _, err := os.Stat(filepath.Join(b.tuneDir(j.ID), "owns-table")); err == nil {
		owned, err := tuneOwnsTable(ctx, j.ID, run)
		if err != nil {
			return err
		}
		if !owned {
			return nil
		}
		if _, err = run(ctx, "/usr/sbin/nft", "delete", "table", "inet", tuneTable(j.ID)); err != nil {
			data, listErr := run(ctx, "/usr/sbin/nft", "-j", "list", "tables")
			var tables struct {
				NFT []struct {
					Table struct {
						Name   string `json:"name"`
						Family string `json:"family"`
					} `json:"table"`
				} `json:"nftables"`
			}
			if listErr != nil || json.Unmarshal(data, &tables) != nil {
				return errors.New("Cannot confirm temporary table removal")
			}
			for _, entry := range tables.NFT {
				if entry.Table.Name == tuneTable(j.ID) && entry.Table.Family == "inet" {
					return errors.New("Temporary table remains")
				}
			}
		}
	}
	return nil
}

func (b backend) tuneCleanupEngine(ctx context.Context, j tuneJob) error {
	// Covers a worker killed between Start and recording the engine PID. Only the
	// exact fixed executable with this unpredictable job comment is eligible.
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		exe, _ := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		args, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if exe != "/usr/sbin/nfqws" || !bytes.Contains(args, []byte("\x00--comment=nadkop-tune-"+j.ID+"\x00")) {
			continue
		}
		tick := processTick(pid)
		tuneSignal(pid, tick, syscall.SIGKILL)
		for tuneAlive(pid, tick) {
			select {
			case <-ctx.Done():
				return errors.New("Temporary nfqws remains")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	return nil
}

func tuneOwnsTable(ctx context.Context, id string, run commandRunner) (bool, error) {
	data, err := run(ctx, "/usr/sbin/nft", "-j", "list", "tables")
	var list struct {
		NFT []struct {
			Table struct {
				Name    string `json:"name"`
				Family  string `json:"family"`
				Comment string `json:"comment"`
			} `json:"table"`
		} `json:"nftables"`
	}
	if err != nil || json.Unmarshal(data, &list) != nil {
		return false, errors.New("Failed to check the temporary nftables table")
	}
	for _, entry := range list.NFT {
		if entry.Table.Name != tuneTable(id) || entry.Table.Family != "inet" {
			continue
		}
		if entry.Table.Comment != "nadkop-zapret-tune:"+id {
			return false, errors.New("Temporary table name is taken by foreign rules")
		}
		return true, nil
	}
	return false, nil
}

func tunePreflight(ctx context.Context, j tuneJob, run commandRunner) error {
	if _, err := run(ctx, "/sbin/ip", "link", "show", "dev", j.WAN); err != nil {
		return errors.New("WAN device does not exist")
	}
	for _, setting := range []string{"firewall.@defaults[0].flow_offloading", "firewall.@defaults[0].flow_offloading_hw"} {
		value, _ := run(ctx, "/sbin/uci", "-q", "get", setting)
		if strings.TrimSpace(string(value)) == "1" {
			return errors.New("Before tuning, disable flow offloading in firewall settings")
		}
	}
	present, _, _, err := tuneQueueStats("/proc/net/netfilter/nfnetlink_queue")
	if err != nil || present {
		return errors.New("Temporary queue 201 is busy or NFQUEUE is unavailable")
	}
	if owned, err := tuneOwnsTable(ctx, j.ID, run); err != nil || owned {
		return errors.New("Temporary nftables table already exists or is unavailable")
	}
	if _, err := run(ctx, "/usr/sbin/nfqws", "--version"); err != nil {
		return errors.New("nfqws is not installed or does not start")
	}
	return nil
}

func netLookupTune(ctx context.Context, network, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, network, host)
}

func publicTuneIP(ip net.IP) bool {
	if ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4"} {
		_, block, _ := net.ParseCIDR(prefix)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

func resolveTune(ctx context.Context, hosts []string, lookup func(context.Context, string, string) ([]net.IP, error)) (map[string]string, error) {
	result := map[string]string{}
	for _, host := range hosts {
		lookupCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		ips, err := lookup(lookupCtx, "ip4", host)
		cancel()
		if err != nil || len(ips) == 0 {
			return nil, errors.New("DNS did not return an IPv4 address for site" + host)
		}
		for _, ip := range ips {
			if !publicTuneIP(ip) {
				return nil, errors.New("DNS returned a non-public address for site" + host + "; check was stopped")
			}
		}
		result[host] = ips[0].To4().String()
	}
	return result, nil
}

const tuneTableLostMessage = "Temporary nftables rules were removed externally during the check; repeat tuning"

type tuneQueueObservation struct {
	present, ownedBefore, ownedAfter, alive bool
	before, after, dropsBefore, dropsAfter  uint64
	beforeErr, afterErr                     error
}

// Ownership inspection is diagnostic only and never runs for a verified queue.
func tuneCandidateVerdict(q tuneQueueObservation, ownsTable func() (bool, error)) (bool, string) {
	queued := q.beforeErr == nil && q.afterErr == nil && q.present && q.ownedBefore && q.ownedAfter && q.after > q.before && q.dropsBefore == q.dropsAfter && q.alive
	if queued {
		return true, ""
	}
	owned, err := ownsTable()
	if err != nil {
		return false, "queue_not_verified"
	}
	if !owned {
		return false, "table_lost"
	}
	return false, "queue_not_used"
}

func tuneQueueStats(path string) (bool, uint64, uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 8 || f[0] != strconv.Itoa(tuneQueue) {
			continue
		}
		seq, e1 := strconv.ParseUint(f[7], 10, 64)
		d1, e2 := strconv.ParseUint(f[5], 10, 64)
		d2, e3 := strconv.ParseUint(f[6], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			return false, 0, 0, errors.New("Invalid queue counters")
		}
		return true, seq, d1 + d2, nil
	}
	return false, 0, 0, nil
}

func tuneQueueOwned(path string, pid int) bool {
	data, err := os.ReadFile(path)
	if err != nil || pid <= 1 {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 8 && f[0] == strconv.Itoa(tuneQueue) {
			return f[1] == strconv.Itoa(pid)
		}
	}
	return false
}

func tuneHTTP(ctx context.Context, j tuneJob, host, protocol, stage, strategy string, round int, candidate bool) tuneProbe {
	r := tuneProbe{Host: host, Protocol: protocol, Stage: stage, Strategy: strategy, Round: round}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	port := "443"
	if protocol == "http" {
		port = "80"
	}
	mark := tuneMark + 1 // Dedicated baseline identity; still bypasses production Zapret.
	if candidate {
		mark = tuneMark
	}
	dialer := net.Dialer{Timeout: 3 * time.Second, Control: func(_, _ string, conn syscall.RawConn) error {
		var setErr error
		err := conn.Control(func(fd uintptr) {
			if setErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark); setErr != nil {
				return
			}
			setErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, j.WAN)
		})
		if err != nil {
			return err
		}
		return setErr
	}}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSHandshakeTimeout: 3 * time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", net.JoinHostPort(j.Addresses[host], port))
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, protocol+"://"+host+"/", nil)
	req.Header.Set("User-Agent", "Nadkop-Zapret-Check/1.0")
	req.Header.Set("Accept-Encoding", "identity")
	start := time.Now()
	resp, err := client.Do(req)
	r.DelayMS = time.Since(start).Milliseconds()
	if err != nil {
		r.Code = "connection_or_tls"
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			r.Code = "timeout"
		}
		var unknown x509.UnknownAuthorityError
		var invalid x509.CertificateInvalidError
		var hostname x509.HostnameError
		if errors.As(err, &unknown) || errors.As(err, &invalid) || errors.As(err, &hostname) {
			r.Code = "tls_certificate"
		}
		if ctx.Err() != nil {
			r.Code = "timeout"
		}
		return r
	}
	defer resp.Body.Close()
	r.HTTPStatus = resp.StatusCode
	// Read one byte as well as headers. Never store response content or redirect
	// locations. Huge or streaming bodies cannot prolong the probe.
	_, readErr := bufio.NewReader(io.LimitReader(resp.Body, 1)).ReadByte()
	r.DelayMS = time.Since(start).Milliseconds()
	r.OK = resp.StatusCode >= 200 && resp.StatusCode < 400 && (readErr == nil || readErr == io.EOF)
	r.Code = "ok"
	if !r.OK {
		r.Code = "http_status_or_body"
		if ctx.Err() != nil {
			r.Code = "timeout"
		}
	}
	return r
}
