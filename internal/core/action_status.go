package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ActionStatus contains counts and process/generation identity only. It is
// written in /var/run, never to the configuration or the server catalogue.
type ActionStatus struct {
	Generation   string `json:"generation"`
	PID          int    `json:"pid"`
	ProcessStart string `json:"processStart"`
	State        string `json:"state"`
	StartedAt    int64  `json:"startedAt"`
	FinishedAt   int64  `json:"updatedAt"`
	OK           bool   `json:"ok"`
	Total        int    `json:"total"`
	Completed    int    `json:"completed"`
	Failed       int    `json:"failed"`
	Error        string `json:"error,omitempty"`
}

func ProcessStart(pid int) string {
	if pid <= 0 {
		return ""
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return ""
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return ""
	}
	return fields[19]
}

func ReadActionStatus(path string) (ActionStatus, error) {
	var s ActionStatus
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return s, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2048 {
		return s, errors.New("invalid action status")
	}
	data, err := io.ReadAll(io.LimitReader(f, 2049))
	if err != nil || len(data) > 2048 {
		return s, errors.New("invalid action status")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF || s.Generation == "" || s.PID <= 0 || s.ProcessStart == "" || s.StartedAt <= 0 || s.Total < 1 || s.Total > 16 || s.Completed < 0 || s.Completed > s.Total || s.Failed < 0 || s.Failed > s.Completed {
		return s, errors.New("invalid action status")
	}
	switch s.State {
	case "running":
		if s.FinishedAt != 0 || s.OK || s.Error != "" {
			return s, errors.New("invalid running status")
		}
	case "ok", "error":
		if s.FinishedAt < s.StartedAt || s.Completed != s.Total || s.OK != (s.State == "ok") || s.State == "ok" && s.Failed != 0 || s.State == "error" && s.Failed == 0 {
			return s, errors.New("invalid completed status")
		}
		if s.State == "ok" && s.Error != "" || s.Error != "" && s.Error != "Automatic refresh failed; inspect service status" {
			return s, errors.New("invalid action summary")
		}
	default:
		return s, errors.New("invalid action state")
	}
	return s, nil
}

// No fsync: these diagnostics are transient RAM state, not a recovery journal.
func WriteActionStatus(path string, s ActionStatus) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".action-status-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
