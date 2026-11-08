package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"nadkop/internal/core"
	"nadkop/internal/router"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nadkop:", err)
		os.Exit(1)
	}
}

func run() error {
	if handled, err := cliHelp(os.Args[1:], os.Stdout); handled {
		return err
	}
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: nadkop validate|nodes|generate|apply|updater|network|monitor|rpc")
	}
	command := os.Args[1]
	if command == "zapret-worker" || command == "zapret-guard" {
		if len(os.Args) != 3 {
			return fmt.Errorf("expected Zapret job ID")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer cancel()
		return router.ZapretTune(ctx, command, os.Args[2])
	}
	if command == "apply" || command == "updater" {
		if len(os.Args) != 2 {
			return fmt.Errorf("unexpected arguments")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer cancel()
		if command == "updater" {
			return router.Updater(ctx)
		}
		return router.Apply(ctx)
	}
	if command == "network" {
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: nadkop network check|apply|stop|rollback|commit [-generation DIR]")
		}
		fs := flag.NewFlagSet("network", flag.ContinueOnError)
		dir := fs.String("generation", "/var/run/nadkop/current", "generation directory")
		if err := fs.Parse(os.Args[3:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("unexpected arguments")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return router.Network(ctx, os.Args[2], *dir)
	}
	if command == "monitor" {
		fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
		manifest := fs.String("manifest", "/var/run/nadkop/current/health.json", "monitor manifest")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("unexpected arguments")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer cancel()
		return core.Monitor(ctx, *manifest)
	}
	if command == "rpc" {
		return router.Run(os.Args[2:], os.Stdin, os.Stdout)
	}
	if command != "validate" && command != "nodes" && command != "generate" {
		return fmt.Errorf("unknown command")
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	config := fs.String("config", "/etc/nadkop/config.json", "configuration file")
	output := fs.String("output", "/var/run/nadkop/current", "new output directory (must not exist)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if command != "validate" {
		unlock, err := core.LockConfiguration(*config, false)
		if err != nil {
			return err
		}
		defer unlock()
	} else if err := core.CheckSavedPair(*config); err != nil {
		return err
	}
	f, err := core.OpenPrivateState(*config)
	if err != nil {
		return err
	}
	c, err := core.ReadConfig(f)
	f.Close()
	if err != nil {
		return err
	}
	if command == "validate" {
		fmt.Println("configuration valid (subscriptions not fetched)")
		return nil
	}
	nodes := []core.Node{}
	for _, source := range c.Sources {
		n, err := core.Load(source)
		if err != nil {
			return fmt.Errorf("source %s: %w", source.Name, err)
		}
		nodes = append(nodes, n...)
	}
	if _, err := core.SyncServers(filepath.Join(filepath.Dir(*config), "servers.json"), nodes, c); err != nil {
		return err
	}
	if command == "nodes" {
		for _, n := range nodes {
			fmt.Println(n.Tag)
		}
		return nil
	}
	a, err := core.CompileWithGeo(c, nodes, filepath.Join(filepath.Dir(*config), "geodata"))
	if err != nil {
		return err
	}
	// Publish a complete directory in one rename. An existing generation is never overwritten.
	parent := filepath.Dir(*output)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(*output); !os.IsNotExist(err) {
		return fmt.Errorf("output path already exists or cannot be inspected")
	}
	tmp, err := os.MkdirTemp(parent, ".nadkop-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for name, data := range a.Files() {
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0600); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, *output); err != nil {
		return err
	}
	fmt.Println("generated", *output)
	return nil
}
