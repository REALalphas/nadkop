package router

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nadkop/internal/core"
)

func TestInterruptedSaveLockAndRPCSave(t *testing.T) {
	b := fixture(t)
	marker := filepath.Join(filepath.Dir(b.config), core.SaveIntentFile)
	if err := os.WriteFile(marker, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"lock", "save"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			if operation == "lock" {
				var unlock func()
				unlock, err = b.lock()
				if unlock != nil {
					unlock()
				}
			} else {
				_, err = b.call(context.Background(), "save", request{})
			}
			if !errors.Is(err, core.ErrInterruptedSave) {
				t.Fatalf("lost recovery error: %v", err)
			}
			message := rpcError(err)["error"].(string)
			if !strings.Contains(message, "Interrupted save") || !strings.Contains(message, "over SSH") || strings.Contains(message, "Another configuration operation") {
				t.Fatalf("lost SSH recovery instruction: %s", message)
			}
		})
	}
	// Failure must release both locks, leaving the marker for manual recovery.
	if _, err := os.Stat(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	unlock, err := b.lock()
	if err != nil {
		t.Fatal("locks leaked after recovery failure:", err)
	}
	unlock()
}

func TestConfigurationLockContentionRemainsGeneric(t *testing.T) {
	b := fixture(t)
	unlock, err := core.LockConfiguration(b.config, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	gotUnlock, err := b.lock()
	if gotUnlock != nil {
		gotUnlock()
	}
	if err == nil || err.Error() != "Another configuration operation is running" || errors.Is(err, core.ErrInterruptedSave) {
		t.Fatalf("unexpected contention error: %v", err)
	}
}

func TestProbeLoadReleasesConfigurationLocks(t *testing.T) {
	for _, broken := range []bool{false, true} {
		name := "success"
		if broken {
			name = "load-error"
		}
		t.Run(name, func(t *testing.T) {
			b := fixture(t)
			if broken {
				if err := os.WriteFile(b.config, []byte(`{`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// This is the exact load phase called before CompileProbe and Xray start.
			// No command runner or network access is needed for an empty local config.
			_, err := b.loadProbe(context.Background())
			if (err != nil) != broken {
				t.Fatalf("unexpected load result: %v", err)
			}
			unlock, err := b.lock()
			if err != nil {
				t.Fatal("probe load retained ui/state lock:", err)
			}
			unlock()
			// Also check the state lock independently of ui.lock.
			unlock, err = core.LockConfiguration(b.config, true)
			if err != nil {
				t.Fatal("probe load retained state lock:", err)
			}
			unlock()
		})
	}
}
