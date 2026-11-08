package router

import (
	"fmt"
	"path/filepath"
	"time"

	"nadkop/internal/core"
)

func (b backend) actionCheck(path, name, title string, instances map[string]any) diagnosticStep {
	check := diagnosticStep{ID: "runtime-" + name + "-action", Title: title, State: "unknown", Detail: "No confirmed result for the current process and applied configuration."}
	instance, ok := instances[name].(map[string]any)
	if !ok || instance["running"] != true {
		check.Detail = "Process is not running; the last action result is unconfirmed."
		return check
	}
	check.Detail = "Process is running, but the last action status is unavailable or unconfirmed for the current process and applied configuration."
	pid, ok := instance["pid"].(int)
	if !ok || pid <= 0 {
		return check
	}
	generation, err := filepath.EvalSymlinks(filepath.Join(b.runtime, "current"))
	if err != nil {
		return check
	}
	s, err := core.ReadActionStatus(path)
	if err != nil || s.Generation != generation || s.PID != pid || s.ProcessStart != core.ProcessStart(pid) || s.StartedAt > time.Now().Unix()+5 || s.FinishedAt > time.Now().Unix()+5 {
		return check
	}
	if s.State == "running" {
		check.State, check.Detail = "pending", "Operation is running; the current cycle result is not known yet."
		return check
	}
	check.State = "success"
	check.Detail = fmt.Sprintf("Last operation finished: %s. This is the action result, not a check of current internet access.", time.Unix(s.FinishedAt, 0).Format("02.01.2006 15:04:05"))
	if s.Failed > 0 {
		check.State = "error"
		check.Detail = fmt.Sprintf("Last operation finished with an error: %s. Unconfirmed actions: %d of %d. Check the configuration and service state.", time.Unix(s.FinishedAt, 0).Format("02.01.2006 15:04:05"), s.Failed, s.Total)
	}
	return check
}
