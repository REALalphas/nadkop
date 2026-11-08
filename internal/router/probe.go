package router

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"nadkop/internal/core"
)

func validateProbe(r request) (string, error) {
	if len(r.Refs) < 1 || len(r.Refs) > 4 {
		return "", errors.New("Probe 1 to 4 servers per request")
	}
	seen := map[string]bool{}
	for _, ref := range r.Refs {
		if len(ref) != 36 || !strings.HasPrefix(ref, "srv_") || strings.Trim(ref[4:], "0123456789abcdef") != "" || seen[ref] {
			return "", errors.New("Invalid server reference")
		}
		seen[ref] = true
	}
	if r.Mode != "priority" && r.Mode != "ping" && r.Mode != "url" {
		return "", errors.New("Unknown probe method")
	}
	if r.Mode != "url" {
		return core.DefaultProbeURL, nil
	}
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || len(r.URL) > 2048 || strings.ContainsAny(r.URL, "\r\n\t ") {
		return "", errors.New("Specify an HTTP/HTTPS probe URL")
	}
	return r.URL, nil
}

func (b backend) probe(ctx context.Context, r request) (any, error) {
	target, err := validateProbe(r)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(b.runtime, 0711); err != nil {
		return nil, errors.New("Failed to prepare probe")
	}
	lock, err := os.OpenFile(filepath.Join(b.runtime, "probe.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("Failed to prepare probe")
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return nil, errors.New("A probe is already running; retry in a few seconds")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	nodes, err := b.loadProbe(ctx)
	if err != nil {
		return nil, err
	}
	if err := core.PrepareICMPDatagram(nodes); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("Failed to allocate probe port")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	setup, err := core.CompileProbe(nodes, r.Refs, port)
	if err != nil {
		return nil, errors.New("Failed to prepare Xray for probing")
	}
	work, err := os.MkdirTemp(b.runtime, "probe.")
	if err != nil {
		return nil, errors.New("Failed to prepare probe")
	}
	defer os.RemoveAll(work)
	path := filepath.Join(work, "xray.json")
	if err = os.WriteFile(path, setup.Xray, 0600); err != nil {
		return nil, errors.New("Failed to prepare probe")
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/xray", "run", "-config", path)
	if err = cmd.Start(); err != nil {
		return nil, errors.New("Failed to start Xray for probing")
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	defer func() { cmd.Process.Kill(); <-done }()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ready := false
	for !ready {
		select {
		case <-done:
			return nil, errors.New("Xray rejected the probe configuration")
		case <-ctx.Done():
			return nil, errors.New("Probe timed out")
		case <-deadline.C:
			return nil, errors.New("Xray did not start the probe")
		default:
		}
		conn, dialErr := net.DialTimeout("tcp", setup.Manifest.SOCKS, 80*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			ready = true
		} else {
			select {
			case <-ctx.Done():
				return nil, errors.New("Probe timed out")
			case <-time.After(40 * time.Millisecond):
			}
		}
	}
	results := map[string]core.ProbeResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, member := range setup.Members {
		wg.Add(1)
		go func(member core.HealthMember) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			result := core.Probe(ctx, setup.Manifest, member, target)
			mu.Lock()
			results[member.ID] = result
			mu.Unlock()
		}(member)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, errors.New("Probe timed out")
	}
	return map[string]any{"results": results, "mode": r.Mode}, nil
}

// loadProbe holds the configuration locks only while reading and reconciling
// servers. The caller keeps probe.lock for the remaining probe lifetime.
func (b backend) loadProbe(ctx context.Context) ([]core.Node, error) {
	unlock, err := b.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	loadCtx, cancelLoad := context.WithTimeout(ctx, 8*time.Second)
	defer cancelLoad()
	_, nodes, err := b.load(loadCtx)
	if err != nil {
		return nil, errors.New("Failed to load servers for probing")
	}
	return nodes, nil
}
