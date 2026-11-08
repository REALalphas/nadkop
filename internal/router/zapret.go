package router

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func checkZapret(ctx context.Context, dir string, run commandRunner) error {
	data, err := os.ReadFile(filepath.Join(dir, "nfqws-args.json"))
	if err != nil {
		return errors.New("Cannot read Zapret arguments")
	}
	var args []string
	if len(data) > 4<<20 || json.Unmarshal(data, &args) != nil {
		return errors.New("Invalid Zapret arguments")
	}
	for i, arg := range args {
		args[i] = strings.ReplaceAll(arg, "/var/run/nadkop/current/", dir+"/")
		// Dry-run checks syntax before publishing a generation. Runtime init
		// transfers only host/ip files to nfqws before its real privilege drop.
		if strings.HasPrefix(arg, "--uid=") {
			args[i] = "--uid=0:0"
		}
	}
	if _, err := run(ctx, "/usr/sbin/nfqws", append([]string{"--dry-run"}, args...)...); err != nil {
		return errors.New("nfqws rejected strategy; check Zapret arguments and required files")
	}
	return nil
}
