package router

import (
	"context"
	"encoding/json"
	"nadkop/internal/core"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Runs without touching the main service, firewall, hosts file or user config.
func TestIPAndGeoRoutingRealXray(t *testing.T) {
	if _, err := os.Stat("/usr/bin/xray"); err != nil {
		t.Skip("run on OpenWrt VM with Xray")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer target.Close()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cfg, err := core.ReadConfig(strings.NewReader(`{"default":"block","socksPort":` + strconv.Itoa(port) + `,"rules":[{"name":"OR","domains":["allowed.test"],"ips":["127.0.0.1/32"],"action":"direct"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := core.Compile(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	path := filepath.Join(work, "xray.json")
	os.WriteFile(path, a.Xray, 0600)
	cmd := exec.Command("/usr/bin/xray", "run", "-config", path)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	ready := false
	for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
		c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 40*time.Millisecond)
		if e == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !ready {
		t.Fatal("Xray not ready")
	}
	out, err := exec.Command("/usr/bin/curl", "--fail", "--silent", "--max-time", "3", "--noproxy", "", "--socks5", "127.0.0.1:"+strconv.Itoa(port), target.URL).CombinedOutput()
	if err != nil {
		t.Fatal("IP/CIDR did not route", err, string(out))
	}

	targetPort := strings.Split(target.Listener.Addr().String(), ":")[1]
	secondListener, err := net.Listen("tcp4", "127.0.0.2:"+targetPort)
	if err != nil {
		t.Fatal(err)
	}
	second := &http.Server{Handler: target.Config.Handler}
	go second.Serve(secondListener)
	defer second.Close()
	response, err := http.Get(strings.Replace(target.URL, "127.0.0.1", "127.0.0.2", 1))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	// An unmatched IP must not inherit the direct route.
	command := exec.Command("/usr/bin/curl", "--fail", "--silent", "--max-time", "2", "--noproxy", "", "--socks5", "127.0.0.1:"+strconv.Itoa(port), strings.Replace(target.URL, "127.0.0.1", "127.0.0.2", 1))
	if command.Run() == nil {
		t.Fatal("IP rule widened unexpectedly")
	}

	permitted := exec.Command("/usr/bin/curl", "--fail", "--silent", "--max-time", "2", "--noproxy", "", "--socks5", "127.0.0.1:"+strconv.Itoa(port), "-H", "Host: allowed.test", strings.Replace(target.URL, "127.0.0.1", "127.0.0.2", 1))
	if out, e := permitted.CombinedOutput(); e != nil {
		t.Fatal("domain OR IP routing failed", e, string(out))
	}
	// Validate a real expanded mixed set and Zapret IP-only profiles if databases supplied.
	dir := os.Getenv("NADKOP_GEO_TEST_FILES")
	if dir == "" {
		return
	}
	site, e := os.ReadFile(filepath.Join(dir, "geosite.dat"))
	if e != nil {
		t.Fatal(e)
	}
	ip, e := os.ReadFile(filepath.Join(dir, "geoip.dat"))
	if e != nil {
		t.Fatal(e)
	}
	if e = core.InstallGeoFiles(filepath.Join(work, "geodata"), site, ip); e != nil {
		t.Fatal(e)
	}
	for _, sets := range [][]string{{"mixed:telegram"}, {"geosite:github", "geosite:youtube", "geosite:discord"}} {
		cfg.Rules = []core.Rule{{Action: "block", Sets: sets}}
		a, e = core.CompileWithGeo(cfg, nil, filepath.Join(work, "geodata"))
		if e != nil {
			t.Fatal(e)
		}
		os.WriteFile(path, a.Xray, 0600)
		if out, e = exec.Command("/usr/bin/xray", "run", "-test", "-config", path).CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		}
	}
	cfg.Rules = []core.Rule{{Action: "zapret", IPs: []string{"203.0.113.0/24", "2001:db8::/32"}}}
	cfg.WANInterface = "eth0"
	cfg.ZapretArgs = []string{"--filter-tcp=80,443", "--dpi-desync=fake"}
	a, e = core.CompileWithGeo(cfg, nil, filepath.Join(work, "geodata"))
	if e != nil {
		t.Fatal(e)
	}
	ipset := filepath.Join(work, "zapret-ips.txt")
	os.WriteFile(ipset, a.IPs, 0600)
	os.Chmod(work, 0711)
	os.Chmod(filepath.Dir(work), 0711)
	os.Chown(ipset, 2147483647, 2147483647)
	var args []string
	json.Unmarshal(a.Zapret, &args)
	args = append(args, "--dry-run")
	for i, arg := range args {
		if strings.HasPrefix(arg, "--ipset=") {
			args[i] = "--ipset=" + ipset
		}
	}
	if out, e = exec.CommandContext(context.Background(), "/usr/sbin/nfqws", args...).CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}

	cfg.Rules[0].Domains = []string{"example.test"}
	a, e = core.CompileWithGeo(cfg, nil, filepath.Join(work, "geodata"))
	if e != nil {
		t.Fatal(e)
	}
	hostlist := filepath.Join(work, "zapret-hosts.txt")
	os.WriteFile(hostlist, a.Hosts, 0600)
	os.Chown(hostlist, 2147483647, 2147483647)
	json.Unmarshal(a.Zapret, &args)
	args = append(args, "--dry-run")
	for i, arg := range args {
		if strings.HasPrefix(arg, "--ipset=") {
			args[i] = "--ipset=" + ipset
		}
		if strings.HasPrefix(arg, "--hostlist=") {
			args[i] = "--hostlist=" + hostlist
		}
	}
	if out, e = exec.Command("/usr/sbin/nfqws", args...).CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
}
