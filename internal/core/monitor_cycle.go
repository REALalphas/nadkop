package core

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"time"
)

type monitorJob struct {
	member HealthMember
	target string
	groups []int
}
type monitorResult struct {
	key    string
	result ProbeResult
}

func monitorLease(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
		if err == nil {
			return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			f.Close()
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Four probe workers consume a deterministic round-robin queue. A single API
// worker applies complete groups in readiness order while other probes run.
// The function joins both workers before returning, including on cancellation.
func monitorCycle(ctx context.Context, manifest HealthManifest, probe func(context.Context, HealthManifest, HealthMember, string) ProbeResult, apply func(context.Context, string, string, string) error) error {
	jobs := map[string]*monitorJob{}
	order := []string{}
	pending := make([]int, len(manifest.Groups))
	for position := 0; ; position++ {
		more := false
		for g, group := range manifest.Groups {
			if position >= len(group.Members) {
				continue
			}
			more = true
			member := group.Members[position]
			if member.Tag == "" {
				continue
			}
			key := group.URL + "\x00" + member.ID
			job := jobs[key]
			if job == nil {
				job = &monitorJob{member: member, target: group.URL}
				jobs[key] = job
				order = append(order, key)
			}
			already := false
			for _, idx := range job.groups {
				if idx == g {
					already = true
					break
				}
			}
			if !already {
				job.groups = append(job.groups, g)
				pending[g]++
			}
		}
		if !more {
			break
		}
	}
	work := make(chan string)
	results := make(chan monitorResult, 4)
	var probes sync.WaitGroup
	for i := 0; i < 4; i++ {
		probes.Add(1)
		go func() {
			defer probes.Done()
			for key := range work {
				if ctx.Err() != nil {
					continue
				}
				job := jobs[key]
				result := probe(ctx, manifest, job.member, job.target)
				results <- monitorResult{key, result}
			}
		}()
	}
	go func() {
		defer close(work)
		for _, key := range order {
			select {
			case work <- key:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { probes.Wait(); close(results) }()
	values := make([]map[string]ProbeResult, len(pending))
	for i := range values {
		values[i] = map[string]ProbeResult{}
	}
	type choice struct{ group, tag string }
	ready := make(chan choice, len(pending))
	applied := make(chan error, 1)
	go func() {
		failed := false
		for next := range ready {
			if ctx.Err() != nil {
				continue
			}
			if err := apply(ctx, manifest.API, next.group, next.tag); err != nil {
				failed = true
			}
		}
		if ctx.Err() != nil {
			applied <- ctx.Err()
		} else if failed {
			applied <- errors.New("cannot update server selection")
		} else {
			applied <- nil
		}
	}()
	queue := func(g int) { group := manifest.Groups[g]; ready <- choice{group.Tag, SelectMember(group, values[g])} }
	queued := make([]bool, len(pending))
	tryQueue := func(g int) {
		if queued[g] {
			return
		}
		complete := pending[g] == 0
		if !complete && manifest.Groups[g].Mode == "priority" {
			for _, member := range manifest.Groups[g].Members {
				if member.Tag == "" {
					continue
				}
				result, known := values[g][member.ID]
				if !known {
					break
				}
				if result.Available {
					complete = true
					break
				}
			}
		}
		if complete {
			queued[g] = true
			queue(g)
		}
	}
	for g, n := range pending {
		if n == 0 {
			tryQueue(g)
		}
	}
	for result := range results {
		for _, g := range jobs[result.key].groups {
			values[g][jobs[result.key].member.ID] = result.result
			pending[g]--
			tryQueue(g)
		}
	}
	close(ready)
	return <-applied
}
