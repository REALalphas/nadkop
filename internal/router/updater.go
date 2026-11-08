package router

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nadkop/internal/core"
)

func Apply(ctx context.Context) error {
	_, err := (backend{configPath, nodesPath, runPath}).call(ctx, "service", request{Action: "restart"})
	return err
}

// procd keeps this command identical across generations. A successful refresh
// cannot terminate the updater halfway through publishing the next generation.
func Updater(ctx context.Context) error {
	last := time.Now()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			data, err := os.ReadFile(configPath)
			if err != nil {
				continue
			}
			c, err := core.ReadConfig(bytes.NewReader(data))
			if err != nil || c.UpdateIntervalHours == 0 {
				continue
			}
			if now.Sub(last) < time.Duration(c.UpdateIntervalHours)*time.Hour {
				continue
			}
			last = now
			refreshCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			err = (backend{configPath, nodesPath, runPath}).updateOnce(refreshCtx, Apply)
			cancel()
			if err != nil {
				fmt.Fprintln(os.Stderr, "nadkop: automatic refresh failed; inspect service status and configuration diagnostics")
			}
		}
	}
}

func (b backend) updateOnce(ctx context.Context, refresh func(context.Context) error) error {
	generation, _ := filepath.EvalSymlinks(filepath.Join(b.runtime, "current"))
	s := core.ActionStatus{Generation: generation, PID: os.Getpid(), ProcessStart: core.ProcessStart(os.Getpid()), State: "running", StartedAt: time.Now().Unix(), Total: 1}
	path := filepath.Join(b.runtime, "update-status.json")
	publish := func() {
		if core.WriteActionStatus(path, s) != nil {
			_ = os.Remove(path)
		}
	}
	publish()
	err := refresh(ctx)
	s.Generation, _ = filepath.EvalSymlinks(filepath.Join(b.runtime, "current"))
	s.Completed, s.FinishedAt, s.OK, s.State = 1, time.Now().Unix(), err == nil, "ok"
	if err != nil {
		s.State, s.Failed = "error", 1
		s.Error = "Automatic refresh failed; inspect service status"
	}
	publish()
	return err
}
