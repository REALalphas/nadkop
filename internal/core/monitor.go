package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const DefaultProbeURL = "https://connectivitycheck.gstatic.com/generate_204"
const HealthAPIPort = 61080
const HealthSOCKSPort = 61081

type HealthMember struct {
	ID  string `json:"id"`
	Tag string `json:"tag"`
}
type HealthGroup struct {
	Tag     string         `json:"tag"`
	Mode    string         `json:"mode"`
	URL     string         `json:"url"`
	Members []HealthMember `json:"members"`
}
type HealthManifest struct {
	API      string        `json:"api"`
	SOCKS    string        `json:"socks"`
	Password string        `json:"password"`
	Groups   []HealthGroup `json:"groups"`
}
type ProbeResult struct {
	Reason    string `json:"reason,omitempty"`
	Available bool   `json:"available"`
	DelayMS   int64  `json:"delayMs"`
}

// Priority preserves member order; latency modes use order as the tie breaker.
func SelectMember(group HealthGroup, results map[string]ProbeResult) string {
	selected := "block"
	var best int64
	for _, member := range group.Members {
		result, ok := results[member.ID]
		if member.Tag == "" || !ok || !result.Available {
			continue
		}
		if selected == "block" || (group.Mode != "priority" && result.DelayMS < best) {
			selected, best = member.Tag, result.DelayMS
		}
		if group.Mode == "priority" {
			break
		}
	}
	return selected
}

func Probe(ctx context.Context, manifest HealthManifest, member HealthMember, target string) ProbeResult {
	if member.Tag == "" {
		return ProbeResult{Reason: "Not in subscription"}
	}
	proxy := &url.URL{Scheme: "socks5", Host: manifest.SOCKS, User: url.UserPassword(member.ID, manifest.Password)}
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true, TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: 4 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return ProbeResult{Reason: "Invalid URL"}
	}
	request.Header.Set("User-Agent", "Nadkop/0.2")
	start := time.Now()
	response, err := client.Do(request)
	if err != nil {
		reason := "Connection error"
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			reason = "5s timeout"
		}
		return ProbeResult{Reason: reason, DelayMS: time.Since(start).Milliseconds()}
	}
	defer response.Body.Close()
	_, _ = io.CopyN(io.Discard, response.Body, 1)
	result := ProbeResult{Available: response.StatusCode >= 200 && response.StatusCode < 300, DelayMS: time.Since(start).Milliseconds()}
	if !result.Available {
		result.Reason = fmt.Sprintf("HTTP %d", response.StatusCode)
	}
	return result
}

func MonitorCycle(ctx context.Context, manifest HealthManifest, probe func(context.Context, HealthManifest, HealthMember, string) ProbeResult, apply func(context.Context, string, string, string) error) error {
	return monitorCycle(ctx, manifest, probe, apply)
}

// A separate procd instance exists only for multi-server rules. Selection is
// updated through Xray's local RoutingService; Xray and its connections stay up.
func Monitor(ctx context.Context, path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return errors.New("cannot read monitoring configuration")
	}
	generation := filepath.Dir(resolved)
	runtimeRoot := filepath.Dir(generation)
	_, currentErr := os.Lstat(filepath.Join(runtimeRoot, "current"))
	guardGeneration := currentErr == nil || runtimeRoot == "/var/run/nadkop" || runtimeRoot == "/run/nadkop"
	active := func() bool {
		if !guardGeneration {
			return true
		}
		current, err := filepath.EvalSymlinks(filepath.Join(runtimeRoot, "current"))
		return err == nil && current == generation
	}
	cycleCtx, cancelCycle := context.WithCancel(ctx)
	defer cancelCycle()
	// Keep status beside the resolved manifest, including OpenWrt runtime
	// paths resolved through /var -> /tmp. This does not affect generation guards.
	statusPath := filepath.Join(generation, "monitor-status.json")
	for {
		if ctx.Err() != nil || !active() {
			return nil
		}
		data, err := os.ReadFile(resolved)
		if err != nil || len(data) > maxSubscription {
			return errors.New("cannot read monitoring configuration")
		}
		var manifest HealthManifest
		if json.Unmarshal(data, &manifest) != nil || len(manifest.Groups) == 0 {
			return errors.New("invalid monitoring configuration")
		}
		// Only generated loopback listeners are accepted, never a remote API target.
		if manifest.API != "127.0.0.1:61080" || manifest.SOCKS != "127.0.0.1:61081" || len(manifest.Groups) > 16 {
			return errors.New("invalid monitoring listeners")
		}
		state := ActionStatus{Generation: generation, PID: os.Getpid(), ProcessStart: ProcessStart(os.Getpid()), State: "running", StartedAt: time.Now().Unix(), Total: len(manifest.Groups)}
		publish := func() {
			if WriteActionStatus(statusPath, state) != nil {
				_ = os.Remove(statusPath)
			}
		}
		publish()
		probe := func(ctx context.Context, m HealthManifest, member HealthMember, target string) ProbeResult {
			if !active() {
				cancelCycle()
				return ProbeResult{}
			}
			return Probe(ctx, m, member, target)
		}
		err = MonitorCycle(cycleCtx, manifest, probe, func(ctx context.Context, api, group, tag string) error {
			callCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			// Service publication holds this same lock exclusively. Keep the
			// generation check and API call inside a shared lease so an old tag
			// cannot race replacement of the generation/processes.
			if guardGeneration {
				release, e := monitorLease(callCtx, filepath.Join(runtimeRoot, "ui.lock"))
				if e != nil {
					state.Completed++
					state.Failed++
					return errors.New("cannot acquire monitoring lease")
				}
				defer release()
			}
			if ctx.Err() != nil || !active() {
				cancelCycle()
				return context.Canceled
			}
			err := exec.CommandContext(callCtx, "/usr/bin/xray", "api", "bo", "--server="+api, "-b", group, tag).Run()
			state.Completed++
			if err != nil {
				state.Failed++
			}
			return err
		})
		if cycleCtx.Err() != nil {
			return nil
		}
		state.State, state.OK, state.FinishedAt = "ok", err == nil, time.Now().Unix()
		if err != nil {
			state.State = "error"
		}
		publish()
		// procd gives each monitor a fixed generation. Pause starts only after
		// every probe and API call has finished; no successful choice is cached.
		timer := time.NewTimer(15 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
