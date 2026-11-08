package router

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type tuneSearchOps struct {
	begin   func(context.Context, tuneStrategy) error
	end     func() error
	probe   func(context.Context, string, string, string, string, int, bool) tuneProbe
	persist func() error
}

func searchTune(ctx context.Context, j *tuneJob, ops tuneSearchOps) error {
	round := func(stage, strategy string, n int, candidate bool) (map[string]bool, error) {
		good := map[string]bool{}
		for _, host := range j.Targets {
			for _, protocol := range j.Protocols {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				r := ops.probe(ctx, host, protocol, stage, strategy, n, candidate)
				if candidate && !r.Queued {
					r.OK = false
					if r.Code == "" {
						r.Code = "queue_not_used"
					}
				}
				good[host+"/"+protocol] = r.OK
				j.Results = append(j.Results, r)
				j.Completed++
				if err := ops.persist(); err != nil {
					return nil, errors.New("Failed to save check results")
				}
			}
		}
		if candidate {
			count := len(j.Targets) * len(j.Protocols)
			lost := count > 0
			for _, r := range j.Results[len(j.Results)-count:] {
				lost = lost && r.Code == "table_lost"
			}
			if lost {
				return nil, errors.New(tuneTableLostMessage)
			}
		}
		return good, nil
	}
	all := func(good map[string]bool) bool {
		for _, ok := range good {
			if !ok {
				return false
			}
		}
		return len(good) > 0
	}
	j.Message = "Check without strategy"
	baseline, err := round("baseline", "", 1, false)
	if err != nil {
		return err
	}
	if all(baseline) {
		j.Outcome, j.Message = "unneeded", "All selected sites are reachable without a strategy. No improvement confirmed."
		return nil
	}
	for _, r := range j.Results {
		if r.Stage == "baseline" && !r.OK && (r.Code == "tls_certificate" || r.Code == "http_status_or_body") {
			j.Outcome, j.Message = "inconclusive", "Got an HTTP or TLS certificate error without a strategy. It does not confirm DPI blocking; check the site and repeat tuning."
			return nil
		}
	}
	for _, strategy := range j.Strategies {
		if err := ctx.Err(); err != nil {
			return err
		}
		j.Message = "Checking: " + strategy.Title
		if err := ops.begin(ctx, strategy); err != nil {
			return err
		}
		good, err := round("strategy", strategy.ID, 1, true)
		if err != nil {
			_ = ops.end()
			return err
		}
		if !all(good) {
			if err := ops.end(); err != nil {
				return err
			}
			continue
		}
		j.Message = "Control check without strategy between attempts"
		control, err := round("baseline_recheck", "", 1, false)
		if err != nil {
			_ = ops.end()
			return err
		}
		good, err = round("strategy", strategy.ID, 2, true)
		stopErr := ops.end()
		if err != nil {
			return err
		}
		if stopErr != nil {
			return stopErr
		}
		if !all(good) {
			continue
		}
		j.Message = "Repeat comparison without strategy"
		recheck, err := round("baseline_recheck", "", 2, false)
		if err != nil {
			return err
		}
		stable := true
		for key, wasOK := range baseline {
			if control[key] != wasOK || recheck[key] != wasOK {
				stable = false
			}
		}
		for _, r := range j.Results {
			if r.Stage == "baseline_recheck" && (r.Code == "tls_certificate" || r.Code == "http_status_or_body") {
				stable = false
			}
		}
		if !stable {
			j.Outcome, j.Message = "inconclusive", "Access without a strategy changed during the check. Result is unconfirmed; repeat tuning."
			return nil
		}
		j.Winner, j.Outcome, j.Message = strategy.ID, "completed", "Strategy passed two checks of all sites interleaved with failing control requests without a strategy. This is the result of the selected probes, not a guarantee of bypassing any DPI."
		return nil
	}
	j.Outcome, j.Message = "no_strategy", "No strategy passed the check of all sites and protocols. Check DNS, provider access, and the selected sites."
	return nil
}

func (b backend) tuneRun(ctx context.Context, j *tuneJob) error {
	if err := tunePreflight(ctx, *j, command); err != nil {
		return err
	}
	addresses, err := resolveTune(ctx, j.Targets, netLookupTune)
	if err != nil {
		return err
	}
	j.Addresses = addresses
	if err := b.writeTune(*j); err != nil {
		return errors.New("Failed to save probe addresses")
	}
	var engine *exec.Cmd
	var done chan struct{}
	stop := func() error {
		if engine == nil {
			return nil
		}
		_ = engine.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			return errors.New("Failed to stop temporary nfqws")
		}
		engine = nil
		j.EnginePID, j.EngineTick = 0, ""
		return nil
	}
	defer stop()
	installed := false
	checkTable := func() error {
		owned, err := tuneOwnsTable(ctx, j.ID, command)
		if err != nil {
			return err
		}
		if !owned {
			return errors.New(tuneTableLostMessage)
		}
		return nil
	}
	end := func() error {
		tableErr := checkTable()
		return errors.Join(tableErr, stop())
	}
	begin := func(ctx context.Context, s tuneStrategy) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := stop(); err != nil {
			return err
		}
		args := append([]string{"--qnum=201", "--uid=2147483647:2147483647", "--dpi-desync-fwmark=0x40000000", "--comment=nadkop-tune-" + j.ID}, s.Args...)
		if _, err := command(ctx, "/usr/sbin/nfqws", append([]string{"--dry-run"}, args...)...); err != nil {
			return errors.New("nfqws does not support the strategy from the check set")
		}
		// Never steal an existing queue consumer, including a process outside Nadkop.
		present, _, _, err := tuneQueueStats("/proc/net/netfilter/nfnetlink_queue")
		if err != nil || present {
			return errors.New("Temporary queue 201 is busy or unavailable")
		}
		engine = exec.Command("/usr/sbin/nfqws", args...)
		engine.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
		if err := engine.Start(); err != nil {
			engine = nil
			return errors.New("Failed to start temporary nfqws")
		}
		j.EnginePID, j.EngineTick = engine.Process.Pid, processTick(engine.Process.Pid)
		cmd := engine
		done = make(chan struct{})
		waitDone := done
		go func() { cmd.Wait(); close(waitDone) }()
		if err := b.writeTune(*j); err != nil {
			return errors.New("Failed to write nfqws state")
		}
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		for {
			present, _, _, err := tuneQueueStats("/proc/net/netfilter/nfnetlink_queue")
			if err == nil && present && tuneQueueOwned("/proc/net/netfilter/nfnetlink_queue", j.EnginePID) && tuneAlive(j.EnginePID, j.EngineTick) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-done:
				return errors.New("Temporary nfqws exited before the check started")
			case <-deadline.C:
				return errors.New("nfqws did not attach to the temporary queue")
			case <-time.After(30 * time.Millisecond):
			}
		}
		if !installed {
			path := filepath.Join(b.tuneDir(j.ID), "rules.nft")
			if err := os.WriteFile(path, []byte(tuneNft(*j)), 0600); err != nil {
				return errors.New("Failed to prepare temporary rules")
			}
			if _, err := command(ctx, "/usr/sbin/nft", "-c", "-f", path); err != nil {
				return errors.New("nftables rejected the temporary rules")
			}
			if err := atomicWrite(filepath.Join(b.tuneDir(j.ID), "owns-table"), []byte(j.ID)); err != nil {
				return errors.New("Failed to write the temporary rules journal")
			}
			if _, err := command(ctx, "/usr/sbin/nft", "-f", path); err != nil {
				return errors.New("Failed to install temporary rules")
			}
			installed = true
		}
		return checkTable()
	}
	probe := func(ctx context.Context, host, protocol, stage, strategy string, round int, candidate bool) tuneProbe {
		_, before, dropsBefore, beforeErr := tuneQueueStats("/proc/net/netfilter/nfnetlink_queue")
		ownedBefore := tuneQueueOwned("/proc/net/netfilter/nfnetlink_queue", j.EnginePID)
		r := tuneHTTP(ctx, *j, host, protocol, stage, strategy, round, candidate)
		if candidate {
			present, after, dropsAfter, err := tuneQueueStats("/proc/net/netfilter/nfnetlink_queue")
			queued, code := tuneCandidateVerdict(tuneQueueObservation{
				present: present, ownedBefore: ownedBefore,
				ownedAfter: tuneQueueOwned("/proc/net/netfilter/nfnetlink_queue", j.EnginePID),
				alive:      tuneAlive(j.EnginePID, j.EngineTick),
				before:     before, after: after, dropsBefore: dropsBefore, dropsAfter: dropsAfter,
				beforeErr: beforeErr, afterErr: err,
			}, func() (bool, error) { return tuneOwnsTable(ctx, j.ID, command) })
			r.Queued = queued
			if !r.Queued {
				r.OK, r.Code = false, code
			}
		}
		return r
	}
	return searchTune(ctx, j, tuneSearchOps{begin, end, probe, func() error { return b.writeTune(*j) }})
}
