package router

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"nadkop/internal/core"
)

func occupiedSockets(data []byte, tcp bool, ports map[int]bool) []string {
	sockets := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || tcp && fields[3] != "0A" {
			continue
		}
		_, text, ok := strings.Cut(fields[1], ":")
		port, err := strconv.ParseUint(text, 16, 16)
		if ok && err == nil && ports[int(port)] {
			sockets = append(sockets, fields[9])
		}
	}
	return sockets
}

func checkListeners(ctx context.Context, plan core.RuntimeSettings, run commandRunner) error {
	ports := map[int]bool{plan.SOCKSPort: true}
	if plan.Health {
		ports[core.HealthAPIPort] = true
		ports[core.HealthSOCKSPort] = true
	}
	if plan.Transparent.Enabled {
		ports[plan.Transparent.Port] = true
		ports[plan.Transparent.DNSPort] = true
	}
	owned := map[string]bool{}
	data, err := run(ctx, "/bin/ubus", "call", "service", "list", `{"name":"nadkop"}`)
	if err == nil {
		var services map[string]struct {
			Instances map[string]struct {
				PID int `json:"pid"`
			} `json:"instances"`
		}
		if json.Unmarshal(data, &services) == nil {
			instance := services["nadkop"].Instances["xray"]
			if instance.PID > 0 {
				fds, _ := filepath.Glob(fmt.Sprintf("/proc/%d/fd/*", instance.PID))
				for _, fd := range fds {
					target, _ := os.Readlink(fd)
					owned[target] = true
				}
			}
		}
	}
	for _, kind := range []string{"tcp", "tcp6", "udp", "udp6"} {
		data, err := os.ReadFile(filepath.Join("/proc/net", kind))
		if err != nil {
			continue
		}
		for _, inode := range occupiedSockets(data, strings.HasPrefix(kind, "tcp"), ports) {
			if !owned["socket:["+inode+"]"] {
				return fmt.Errorf("A Nadkop listener port is already used by another process; disable the separate Xray/DNS service or change ports")
			}
		}
	}
	return nil
}
