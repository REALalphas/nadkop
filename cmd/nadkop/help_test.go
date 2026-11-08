package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIHelp(t *testing.T) {
	for _, args := range [][]string{
		{"-h"}, {"--help"}, {"help"},
		{"validate", "-config", "/proc/nadkop-help-missing/config.json", "--help"},
		{"nodes", "--help", "-config", "/proc/nadkop-help-missing/config.json"},
		{"generate", "-config", "/proc/nadkop-help-missing/config.json", "-output", "/proc/nadkop-help-output", "-h"},
		{"monitor", "-manifest", "/proc/nadkop-help-missing/health.json", "--help"},
		{"apply", "--help"}, {"updater", "-h"}, {"network", "--help"},
		{"network", "check", "-h"}, {"network", "apply", "--help"}, {"network", "stop", "--help"},
		{"network", "rollback", "-generation", "/proc/nadkop-help-missing", "--help"},
		{"network", "commit", "--help"}, {"rpc", "--help"}, {"rpc", "list", "-h"},
		{"rpc", "call", "--help"}, {"rpc", "call", "service", "--help"}, {"help", "network", "rollback"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			handled, err := cliHelp(args, &out)
			if !handled || err != nil || !strings.HasPrefix(out.String(), "Usage: nadkop ") {
				t.Fatalf("handled=%v err=%v output=%q", handled, err, out.String())
			}
			// Exercise the actual dispatch: help must finish before reading absent
			// files, acquiring runtime locks, reading stdin, or starting daemons.
			if err := invoke(t, args...); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCLIHelpRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--help", "extra"}, {"help", "unknown"}, {"unknown", "--help"},
		{"validate", "--help", "-unknown"}, {"nodes", "-unknown", "--help"},
		{"generate", "--help", "-config"}, {"monitor", "--help", "extra"},
		{"apply", "--help", "extra"}, {"updater", "--help", "-config", "x"},
		{"network", "unknown", "--help"}, {"network", "check", "--help", "-unknown"},
		{"rpc", "unknown", "--help"}, {"rpc", "list", "extra", "--help"},
		{"rpc", "call", "service", "extra", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if err := invoke(t, args...); err == nil {
				t.Fatal("invalid arguments accepted")
			}
		})
	}
}

func TestCLIHelpFlagValuesAndTerminator(t *testing.T) {
	for _, args := range [][]string{{"validate", "-config", "--help"}, {"monitor", "-manifest", "-h"}} {
		var out bytes.Buffer
		if handled, err := cliHelp(args, &out); handled || err != nil || out.Len() != 0 {
			t.Fatalf("flag value interpreted as help: %v, %v", handled, err)
		}
	}
	if handled, err := cliHelp([]string{"apply", "--", "--help"}, &bytes.Buffer{}); !handled || err == nil {
		t.Fatal("positional help after -- was accepted")
	}
	root := t.TempDir()
	config := filepath.Join(root, "missing.json")
	if err := invoke(t, "nodes", "-config", config, "--help"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("help created files: %v, %v", entries, err)
	}
}
