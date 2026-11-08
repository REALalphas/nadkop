package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"nadkop/internal/core"
)

func TestProductRealXrayFirstIPRuleWinsOverLaterDomain(t *testing.T) {
	if _, err := os.Stat("/usr/bin/xray"); err != nil {
		t.Skip("requires the OpenWrt Xray engine")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer target.Close()
	for _, earlier := range []string{"direct", "block"} {
		t.Run(earlier, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			later := "block"
			if earlier == "block" {
				later = "direct"
			}
			raw := fmt.Sprintf(`{"socksPort":%d,"rules":[{"ips":["127.0.0.1/32"],"action":%q},{"domains":["localhost"],"action":%q}]}`, port, earlier, later)
			cfg, err := core.ReadConfig(strings.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			a, err := core.Compile(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "xray.json")
			os.WriteFile(path, a.Xray, 0600)
			cmd := exec.Command("/usr/bin/xray", "run", "-config", path)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()
			ready := false
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 40*time.Millisecond)
				if err == nil {
					conn.Close()
					ready = true
					break
				}
				time.Sleep(30 * time.Millisecond)
			}
			if !ready {
				t.Fatal("Xray did not listen")
			}
			proxy, _ := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(port))
			transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			response, err := client.Get(strings.Replace(target.URL, "127.0.0.1", "localhost", 1))
			if response != nil {
				response.Body.Close()
			}
			if earlier == "direct" && (err != nil || response.StatusCode != 204) {
				t.Fatal("later domain blocked an earlier IP route", err)
			}
			if earlier == "block" && err == nil {
				t.Fatal("later domain bypassed the earlier IP block")
			}
		})
	}
}

func TestProductRealEnginesAcceptTransparentAndZapretArtifacts(t *testing.T) {
	if _, err := os.Stat("/usr/bin/xray"); err != nil {
		t.Skip("requires OpenWrt engines and kernel modules")
	}
	for _, v6 := range []bool{false, true} {
		t.Run(fmt.Sprint(v6), func(t *testing.T) {
			raw := fmt.Sprintf(`{"transparent":{"enabled":true,"interfaces":["br-test"],"ipv6":%v},"wanInterface":"eth0","zapretArgs":["--filter-tcp=80,443","--filter-udp=443","--dpi-desync=fake"],"rules":[{"domains":["dpi.test"],"action":"zapret"}]}`, v6)
			cfg, err := core.ReadConfig(strings.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			a, err := core.Compile(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for name, data := range a.Files() {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, "/usr/bin/xray", "run", "-test", "-config", filepath.Join(dir, "xray.json")).CombinedOutput(); err != nil {
				t.Fatal("Xray rejected transparent DNS configuration", err, string(out))
			}
			if err := checkZapret(ctx, dir, command); err != nil {
				t.Fatal(err)
			}
			// --check never changes the actual firewall. It still validates TPROXY and ports.
			nft := filepath.Join(dir, "rules.nft")
			if out, err := exec.CommandContext(ctx, "/usr/sbin/nft", "-c", "-f", nft).CombinedOutput(); err != nil {
				t.Fatal("kernel rejected generated nftables rules", err, string(out))
			}
			var plan core.RuntimeSettings
			if err := json.Unmarshal(a.Runtime, &plan); err != nil || !plan.Zapret {
				t.Fatal(plan, err)
			}
		})
	}
}
