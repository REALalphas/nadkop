package router

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nadkop/internal/core"
)

type commandRunner func(context.Context, string, ...string) ([]byte, error)

type diagnosticStep struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	State      string `json:"state"`
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

type diagnosticReport struct {
	OK        bool             `json:"ok"`
	NodeCount int              `json:"nodeCount"`
	Steps     []diagnosticStep `json:"steps"`
	Error     string           `json:"error,omitempty"`
}

func (r *diagnosticReport) step(id, detail string, fn func() error) error {
	start := time.Now()
	err := fn()
	if r != nil {
		for i := range r.Steps {
			s := &r.Steps[i]
			if s.ID != id {
				continue
			}
			s.DurationMS, s.Detail, s.State = time.Since(start).Milliseconds(), detail, "ok"
			if err != nil {
				s.State, s.Detail = "error", err.Error()
			}
		}
	}
	return err
}

func (r *diagnosticReport) skip(id, detail string) {
	if r == nil {
		return
	}
	for i := range r.Steps {
		if r.Steps[i].ID == id {
			r.Steps[i].State, r.Steps[i].Detail = "skipped", detail
		}
	}
}

// Each result is tied to an executed check. Failures keep earlier results and
// mark dependent checks skipped. Command output and credentials are not returned.
func (b backend) diagnose(ctx context.Context, run commandRunner) diagnosticReport {
	report := diagnosticReport{Steps: []diagnosticStep{
		{ID: "config", Title: "Configuration", State: "pending"},
		{ID: "sources", Title: "Subscriptions", State: "pending"},
		{ID: "routes", Title: "Routes", State: "pending"},
		{ID: "xray", Title: "Xray", State: "pending"},
		{ID: "nfqws", Title: "Zapret", State: "pending"},
		{ID: "nftables", Title: "nftables", State: "pending"},
	}}
	var c core.Config
	var nodes []core.Node
	err := report.step("config", "Configuration loaded", func() error {
		data, _, _, err := b.read()
		if err != nil {
			return err
		}
		c, err = core.ReadConfig(bytes.NewReader(data))
		if err != nil {
			return errors.New("Invalid saved configuration")
		}
		if len(c.Sources) > 16 {
			return errors.New("At most 16 subscriptions are supported")
		}
		return nil
	})
	if err == nil {
		err = report.step("sources", fmt.Sprintf("Sources: %d", len(c.Sources)), func() error {
			var err error
			c, nodes, err = b.load(ctx)
			if err != nil {
				return err
			}
			summaries, err := core.ServerSummaries(filepath.Join(filepath.Dir(b.config), "servers.json"))
			if err != nil {
				return err
			}
			for _, s := range summaries {
				if s.Available && !s.Separator {
					report.NodeCount++
				}
			}
			return nil
		})
	}
	if err == nil {
		var dir string
		dir, err = b.prepareSteps(ctx, c, nodes, run, &report)
		if dir != "" {
			os.RemoveAll(dir)
		}
	}
	report.OK = err == nil
	if err != nil {
		report.Error = err.Error()
		for i := range report.Steps {
			if report.Steps[i].State == "pending" {
				report.Steps[i].State, report.Steps[i].Detail = "skipped", "Previous step failed"
			}
		}
	}
	return report
}
