package router

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"nadkop/internal/core"
)

func (b backend) loadWith(ctx context.Context, load func(context.Context, core.Source) ([]core.Node, error)) (core.Config, []core.Node, error) {
	if err := ctx.Err(); err != nil {
		return core.Config{}, nil, err
	}
	data, _, _, err := b.read()
	if err != nil {
		return core.Config{}, nil, err
	}
	c, err := core.ReadConfig(bytes.NewReader(data))
	if err != nil {
		return c, nil, errors.New("Invalid saved configuration")
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	results := make([][]core.Node, len(c.Sources))
	errs := make([]error, len(c.Sources))
	var wg sync.WaitGroup
	for i, s := range c.Sources {
		wg.Add(1)
		go func(i int, s core.Source) { defer wg.Done(); results[i], errs[i] = load(fetchCtx, s) }(i, s)
	}
	wg.Wait()
	if err := fetchCtx.Err(); err != nil {
		return c, nil, err
	}
	nodes := []core.Node{}
	for i, n := range results {
		if errs[i] != nil {
			return c, nil, fmt.Errorf("Source %s: %w", c.Sources[i].Name, errs[i])
		}
		nodes = append(nodes, n...)
	}
	if _, err := core.SyncServersContext(ctx, filepath.Join(filepath.Dir(b.config), "servers.json"), nodes, c); err != nil {
		return c, nil, err
	}
	return c, nodes, nil
}
