package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func invoke(t *testing.T, args ...string) error {
	t.Helper()
	saved := os.Args
	os.Args = append([]string{"nadkop"}, args...)
	defer func() { os.Args = saved }()
	return run()
}
func TestProductCLICommandBoundaries(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"validate", "-unknown"}, {"validate", "extra"}, {"monitor", "extra"}, {"apply", "extra"}, {"updater", "extra"}, {"network"}, {"network", "check", "extra"}, {"rpc", "unknown"}, {"zapret-worker"}, {"zapret-worker", "../bad"}, {"zapret-guard", "not-a-job"}} {
		t.Run("invalid", func(t *testing.T) {
			if err := invoke(t, args...); err == nil {
				t.Fatal("invalid invocation accepted", args)
			}
		})
	}
	if err := invoke(t, "validate", "-config", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing config passed")
	}
}
func TestProductCLIGenerationIsCompleteAndPrivate(t *testing.T) {
	root := t.TempDir()
	nodes := filepath.Join(root, "nodes.json")
	config := filepath.Join(root, "config.json")
	if err := os.WriteFile(nodes, []byte(`[{"tag":"exit","protocol":"socks","settings":{"servers":[{"address":"127.0.0.1","port":2080}]}}]`), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"sources": []map[string]string{{"name": "lab", "file": nodes}}, "rules": []map[string]any{{"domains": []string{"proxy.test"}, "action": "proxy", "node": "lab/exit"}}})
	os.WriteFile(config, data, 0600)
	if err := invoke(t, "validate", "-config", config); err != nil {
		t.Fatal(err)
	}
	if err := invoke(t, "nodes", "-config", config); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "generation")
	if err := invoke(t, "generate", "-config", config, "-output", output); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(output)
	if err != nil || len(files) != 10 {
		t.Fatal(len(files), err)
	}
	for _, file := range files {
		info, _ := file.Info()
		if info.Mode().Perm() != 0600 {
			t.Fatal(file.Name(), info.Mode())
		}
	}
	if info, _ := os.Stat(output); info.Mode().Perm() != 0700 {
		t.Fatal("generation directory permissions", info.Mode())
	}
	before, _ := os.ReadFile(filepath.Join(output, "xray.json"))
	if err := invoke(t, "generate", "-config", config, "-output", output); err == nil {
		t.Fatal("existing generation replaced")
	}
	after, _ := os.ReadFile(filepath.Join(output, "xray.json"))
	if string(after) != string(before) {
		t.Fatal("existing generation modified")
	}
	os.WriteFile(config, []byte(`{"rules":[{"domains":["proxy.test"],"action":"proxy","nodeRef":"srv_00000000000000000000000000000000"}]}`), 0600)
	bad := filepath.Join(root, "rejected")
	if err := invoke(t, "generate", "-config", config, "-output", bad); err == nil {
		t.Fatal("missing selected server accepted")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("partial generation published")
	}
}
