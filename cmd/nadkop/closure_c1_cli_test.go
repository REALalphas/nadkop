package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestClosureC1CLIChild(t *testing.T) {
	if os.Getenv("C1_CLI_CHILD") != "1" {
		return
	}
	if err := invoke(t, os.Getenv("C1_CLI_COMMAND"), "-config", os.Getenv("C1_CLI_CONFIG"), "-output", os.Getenv("C1_CLI_OUTPUT")); err != nil {
		t.Fatal(err)
	}
}

func TestClosureC1CLILocksBeforeReadingConfig(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"generate", "nodes"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			config := filepath.Join(root, "config.json")
			lockPath := filepath.Join(root, ".state.lock")
			// If CLI reads before locking, it sees invalid JSON and cannot succeed.
			if err := os.WriteFile(config, []byte(`{"old-incomplete":`), 0600); err != nil {
				t.Fatal(err)
			}
			lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
				t.Fatal(err)
			}
			defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			output := filepath.Join(root, "generation")
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestClosureC1CLIChild$", "-test.count=1")
			cmd.Env = append(os.Environ(), "C1_CLI_CHILD=1", "C1_CLI_CONFIG="+config, "C1_CLI_OUTPUT="+output, "C1_CLI_COMMAND="+command)
			var log bytes.Buffer
			cmd.Stdout = &log
			cmd.Stderr = &log
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			defer func() { cancel(); cmd.Process.Kill() }()

			tick := time.NewTicker(5 * time.Millisecond)
			defer tick.Stop()
			waiting := false
			for !waiting {
				entries, _ := os.ReadDir(filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid), "fd"))
				for _, entry := range entries {
					target, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid), "fd", entry.Name()))
					if target == lockPath {
						waiting = true
						break
					}
				}
				if waiting {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("CLI read invalid C0 before waiting for state lock: %v: %s", err, log.String())
				case <-ctx.Done():
					t.Fatal("CLI did not wait on shared lock")
				case <-tick.C:
				}
			}
			if err = os.WriteFile(config, []byte(`{"sources":[],"rules":[],"default":"block"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("CLI failed to read committed C1: %v: %s", err, log.String())
				}
			case <-ctx.Done():
				t.Fatal("CLI did not release nested locks")
			}
			if command == "generate" {
				if _, err = os.Stat(filepath.Join(output, "xray.json")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
