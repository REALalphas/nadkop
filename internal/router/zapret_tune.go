package router

// The tuner never publishes a generation or writes configuration. Only its own
// marked sockets can enter its private queue. A separate guard owns final cleanup.
import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"nadkop/internal/core"
)

const tuneDuration = 6 * time.Minute
const tuneQueue = 201
const tuneMark = 0x28000000 // Includes BypassZapretMark; avoids the production queue.

var tuneIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var tuneWANPattern = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,15}$`)

type tuneStrategy struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Args  []string `json:"args"`
}

func tuneStrategies() []tuneStrategy {
	return []tuneStrategy{
		{"multisplit", "Split at request start", []string{"--filter-tcp=80,443", "--dpi-desync=multisplit", "--dpi-desync-split-pos=2"}},
		{"multidisorder", "Reorder request parts", []string{"--filter-tcp=80,443", "--dpi-desync=multidisorder", "--dpi-desync-split-pos=2"}},
		{"fake-multisplit", "Fake packet and split", []string{"--filter-tcp=80,443", "--dpi-desync=fake,multisplit", "--dpi-desync-split-pos=2", "--dpi-desync-fooling=badseq", "--dpi-desync-repeats=2"}},
		{"fake-multidisorder", "Fake packet and reorder", []string{"--filter-tcp=80,443", "--dpi-desync=fake,multidisorder", "--dpi-desync-split-pos=2", "--dpi-desync-fooling=badseq", "--dpi-desync-repeats=2"}},
	}
}

type tuneProbe struct {
	Host       string `json:"host"`
	Protocol   string `json:"protocol"`
	Stage      string `json:"stage"`
	Strategy   string `json:"strategy,omitempty"`
	Round      int    `json:"round"`
	OK         bool   `json:"ok"`
	Code       string `json:"code"`
	DelayMS    int64  `json:"delayMs"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Queued     bool   `json:"queued"`
}

type tuneJob struct {
	ID         string         `json:"id"`
	Status     string         `json:"status"`
	Message    string         `json:"message"`
	Revision   string         `json:"revision"`
	WAN        string         `json:"wan"`
	Targets    []string       `json:"targets"`
	Protocols  []string       `json:"protocols"`
	Strategies []tuneStrategy `json:"strategies"`
	Results    []tuneProbe    `json:"results"`
	Winner     string         `json:"winner,omitempty"`
	Restored   bool           `json:"restored"`
	Completed  int            `json:"completed"`
	Total      int            `json:"total"`
	StartedAt  int64          `json:"startedAt"`
	ExpiresAt  int64          `json:"expiresAt"`
	FinishedAt int64          `json:"finishedAt,omitempty"`
	// Private process identities are not included in RPC results (see public()).
	WorkerPID  int               `json:"workerPID,omitempty"`
	WorkerTick string            `json:"workerTick,omitempty"`
	EnginePID  int               `json:"enginePID,omitempty"`
	EngineTick string            `json:"engineTick,omitempty"`
	Outcome    string            `json:"outcome,omitempty"`
	Addresses  map[string]string `json:"addresses,omitempty"`
}

func (j tuneJob) active() bool {
	return j.Status == "queued" || j.Status == "running" || j.Status == "restoring"
}

func (j tuneJob) public() tuneJob {
	j.WorkerPID, j.EnginePID = 0, 0
	j.WorkerTick, j.EngineTick, j.Outcome = "", "", ""
	j.Addresses = nil
	return j
}

func (b backend) tuneRoot() string         { return filepath.Join(b.runtime, "zapret-tune") }
func (b backend) tuneDir(id string) string { return filepath.Join(b.tuneRoot(), id) }

func (b backend) tuneLock(name string) (func(), error) {
	if err := os.MkdirAll(b.tuneRoot(), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(b.tuneRoot(), name+".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("Zapret tuning is already running")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func (b backend) readTune(id string) (tuneJob, error) {
	var j tuneJob
	if !tuneIDPattern.MatchString(id) {
		return j, errors.New("Invalid Zapret check ID")
	}
	data, err := os.ReadFile(filepath.Join(b.tuneDir(id), "job.json"))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, &j) != nil || j.ID != id {
		return tuneJob{}, errors.New("Zapret tuning result is unavailable")
	}
	return j, nil
}

func (b backend) writeTune(j tuneJob) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(b.tuneDir(j.ID), "job.json"), data)
}

func validateTune(r request) ([]string, []string, error) {
	if !tuneWANPattern.MatchString(r.WAN) || r.WAN == "lo" {
		return nil, nil, errors.New("Select a WAN device for probing")
	}
	if len(r.Targets) < 1 || len(r.Targets) > 4 {
		return nil, nil, errors.New("Specify 1 to 4 sites")
	}
	targets, seen := []string{}, map[string]bool{}
	for _, raw := range r.Targets {
		host := strings.ToLower(strings.TrimSpace(raw))
		if len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil || seen[host] {
			return nil, nil, errors.New("Specify distinct site names without URLs, IPs, ports, or paths")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' || strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
				return nil, nil, errors.New("Invalid site name; use punycode for IDN")
			}
		}
		for _, suffix := range []string{".local", ".lan", ".localhost", ".home.arpa", ".onion"} {
			if strings.HasSuffix(host, suffix) {
				return nil, nil, errors.New("Tuning requires public sites")
			}
		}
		seen[host] = true
		targets = append(targets, host)
	}
	if len(r.Protocols) < 1 || len(r.Protocols) > 2 {
		return nil, nil, errors.New("Select HTTPS or HTTP and HTTPS")
	}
	protocols, seenProtocol := []string{}, map[string]bool{}
	for _, p := range r.Protocols {
		if (p != "https" && p != "http") || seenProtocol[p] {
			return nil, nil, errors.New("Unknown or duplicate protocol")
		}
		seenProtocol[p] = true
		protocols = append(protocols, p)
	}
	return targets, protocols, nil
}

func (b backend) tuneStart(ctx context.Context, r request) (any, error) {
	return b.tuneStartWith(ctx, r, command, (*exec.Cmd).Start)
}

func (b backend) tuneStartWith(ctx context.Context, r request, run commandRunner, start func(*exec.Cmd) error) (any, error) {
	targets, protocols, err := validateTune(r)
	if err != nil {
		return nil, err
	}
	unlock, err := b.tuneLock("manager")
	if err != nil {
		return nil, err
	}
	defer unlock()
	// Refuse to overlap even a worker whose UI connection has disappeared.
	if id, err := os.ReadFile(filepath.Join(b.tuneRoot(), "active")); err == nil {
		j, err := b.readTune(strings.TrimSpace(string(id)))
		if err != nil {
			_ = os.Remove(filepath.Join(b.tuneRoot(), "active"))
		} else if j.active() || !j.Restored {
			if !tuneAlive(j.WorkerPID, j.WorkerTick) && time.Now().Unix() > j.StartedAt+5 {
				b.resumeTuneGuard(j.ID)
			}
			return nil, errors.New("Tuning is already running; open its status or cancel it")
		}
	} else if !os.IsNotExist(err) {
		return nil, errors.New("Failed to check the previous tuning task")
	}
	stateUnlock, err := b.lock()
	if err != nil {
		return nil, err
	}
	defer stateUnlock()
	_, _, rev, err := b.read()
	if err != nil {
		return nil, err
	}
	if rev != r.Revision {
		return nil, errors.New("Settings changed: reload the page before tuning")
	}
	if _, err := run(ctx, "/sbin/ip", "link", "show", "dev", r.WAN); err != nil {
		return nil, errors.New("WAN device does not exist")
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, errors.New("Failed to create check")
	}
	j := tuneJob{ID: hex.EncodeToString(random[:]), Status: "queued", Message: "Preparing check", Revision: rev, WAN: r.WAN,
		Targets: targets, Protocols: protocols, Strategies: tuneStrategies(), Results: []tuneProbe{}, StartedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(tuneDuration).Unix()}
	// Failed candidates use at most three rounds; the winning/final candidate
	// may use four, after which the search always returns. Include baseline.
	j.Total = len(targets) * len(protocols) * (1 + 3*len(j.Strategies) + 1)
	if err = os.Mkdir(b.tuneDir(j.ID), 0700); err != nil {
		return nil, errors.New("Failed to create check")
	}
	started := false
	defer func() {
		if !started {
			_ = os.RemoveAll(b.tuneDir(j.ID))
			active := filepath.Join(b.tuneRoot(), "active")
			if data, err := os.ReadFile(active); err == nil && strings.TrimSpace(string(data)) == j.ID {
				_ = os.Remove(active)
			}
		}
	}()
	if err = b.writeTune(j); err != nil {
		return nil, errors.New("Failed to write check")
	}
	if err = atomicWrite(filepath.Join(b.tuneRoot(), "active"), []byte(j.ID)); err != nil {
		return nil, errors.New("Failed to write check")
	}
	// Keep a bounded, private history. The current and three latest results are
	// sufficient to inspect a rerun; no host names enter syslog or localStorage.
	entries, _ := os.ReadDir(b.tuneRoot())
	old := []os.DirEntry{}
	for _, entry := range entries {
		if entry.IsDir() && tuneIDPattern.MatchString(entry.Name()) && entry.Name() != j.ID {
			old = append(old, entry)
		}
	}
	sort.Slice(old, func(i, k int) bool {
		a, _ := old[i].Info()
		c, _ := old[k].Info()
		return a != nil && c != nil && a.ModTime().After(c.ModTime())
	})
	for i := 3; i < len(old); i++ {
		_ = os.RemoveAll(b.tuneDir(old[i].Name()))
		_ = os.Remove(filepath.Join(b.tuneRoot(), "guard-"+old[i].Name()+".lock"))
	}
	// Setsid and closed RPC stdio make disconnects/rpcd restarts independent of
	// the task. The worker starts a guard before touching the network.
	cmd := exec.Command("/usr/bin/nadkop", "zapret-worker", j.ID)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = start(cmd); err != nil {
		return nil, errors.New("Failed to start check")
	}
	started = true
	go cmd.Wait()
	return map[string]any{"job": j.public()}, nil
}

func (b backend) tuneStatus(r request) (any, error) {
	id := r.Job
	if id == "" {
		data, err := os.ReadFile(filepath.Join(b.tuneRoot(), "active"))
		if os.IsNotExist(err) {
			return map[string]any{"job": nil}, nil
		}
		if err != nil {
			return nil, errors.New("Failed to read tuning state")
		}
		id = strings.TrimSpace(string(data))
	}
	j, err := b.readTune(id)
	if err != nil {
		return nil, err
	}
	if (j.active() || !j.Restored) && !tuneAlive(j.WorkerPID, j.WorkerTick) && time.Now().Unix() > j.StartedAt+5 {
		// Also repairs loss of both detached processes. Merely observing elapsed
		// time never marks the job restored; the guard verifies resources.
		b.resumeTuneGuard(j.ID)
	}
	return map[string]any{"job": j.public()}, nil
}

func (b backend) tuneCancel(r request) (any, error) {
	if r.Job == "" {
		data, err := os.ReadFile(filepath.Join(b.tuneRoot(), "active"))
		if err != nil {
			return nil, errors.New("No active check")
		}
		r.Job = strings.TrimSpace(string(data))
	}
	j, err := b.readTune(r.Job)
	if err != nil {
		return nil, err
	}
	if j.active() {
		if err = os.WriteFile(filepath.Join(b.tuneDir(j.ID), "cancel"), nil, 0600); err != nil {
			return nil, errors.New("Failed to deliver cancellation")
		}
	}
	if !j.Restored && !tuneAlive(j.WorkerPID, j.WorkerTick) {
		b.resumeTuneGuard(j.ID)
	}
	return map[string]any{"ok": true}, nil
}

// Selection returns only a verified catalogue strategy into the editor. It
// never silently saves a draft, changes manual arguments, or restarts a service.
func (b backend) tuneSelect(r request) (any, error) {
	unlock, err := b.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	j, err := b.readTune(r.Job)
	if err != nil {
		return nil, err
	}
	if j.Status != "completed" || !j.Restored || j.Winner == "" || r.Strategy != j.Winner {
		return nil, errors.New("No confirmed strategy with completed recovery")
	}
	saved, _, rev, err := b.read()
	if err != nil {
		return nil, err
	}
	c, err := core.ReadConfig(bytes.NewReader(saved))
	if err != nil {
		return nil, err
	}
	c = c.Active()
	hasZapret := false
	for _, rule := range c.Rules {
		hasZapret = hasZapret || rule.Action == "zapret"
	}
	if !hasZapret {
		return nil, errors.New("No rules with Zapret action; add a rule and run tuning again")
	}
	if r.Revision != rev || j.Revision != rev {
		return nil, errors.New("Settings changed after the check; run tuning again")
	}
	if r.WAN != j.WAN {
		return nil, errors.New("WAN changed after the check; repeat tuning")
	}
	if time.Now().Unix() > j.StartedAt+3600 {
		return nil, errors.New("Result is older than an hour; repeat the check")
	}
	for _, s := range tuneStrategies() {
		if s.ID == j.Winner {
			return map[string]any{"args": s.Args, "wan": j.WAN, "strategy": s.ID}, nil
		}
	}
	return nil, errors.New("Strategy is no longer supported")
}

func tuneTable(id string) string { return "nadkop_tune_" + id }

func (b backend) resumeTuneGuard(id string) {
	cmd := exec.Command("/usr/bin/nadkop", "zapret-guard", id)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

func tuneFailure(j *tuneJob, status, message string) {
	j.Status, j.Outcome, j.Message, j.Winner = "restoring", status, message, ""
}
