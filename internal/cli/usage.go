package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
	"github.com/fikrilal/agy-auth/internal/usage"
)

const usageFetchTimeout = 30 * time.Second
const usageWorkers = 4

func (a *App) enrichUsage(rows []accountRow) map[string]int {
	if a.Usage == nil || len(rows) == 0 {
		return nil
	}
	type job struct {
		index int
		key   string
		token *agy.TokenFile
	}
	jobs := make([]job, 0, len(rows))
	for i := range rows {
		if rows[i].token == nil {
			continue
		}
		jobs = append(jobs, job{index: i, key: usageKey(rows[i]), token: rows[i].token})
	}
	if len(jobs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), usageFetchTimeout)
	defer cancel()
	queue := make(chan job)
	var wait sync.WaitGroup
	var mu sync.Mutex
	failures := map[string]int{}
	workers := usageWorkers
	if len(jobs) < workers {
		workers = len(jobs)
	}
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for job := range queue {
				snapshot, err := a.Usage.Fetch(ctx, job.key, job.token)
				if snapshot.Plan != "" {
					rows[job.index].Plan = snapshot.Plan
				}
				if snapshot.FiveHour != nil || snapshot.Weekly != nil {
					value := snapshot
					rows[job.index].Usage = &value
				}
				if err != nil {
					code := usage.ShortError(err)
					rows[job.index].UsageError = code
					mu.Lock()
					failures[code]++
					mu.Unlock()
					continue
				}
				rows[job.index].UsageError = ""
			}
		}()
	}
	for _, job := range jobs {
		queue <- job
	}
	close(queue)
	wait.Wait()
	return failures
}

func usageKey(row accountRow) string {
	if row.ID != "" {
		return row.ID
	}
	if row.token != nil {
		return "fp:" + row.token.Fingerprint()
	}
	return ""
}

func (a *App) warnUsageFailures(failures map[string]int) {
	if len(failures) == 0 {
		return
	}
	codes := make([]string, 0, len(failures))
	total := 0
	for code, count := range failures {
		codes = append(codes, code)
		total += count
	}
	sort.Strings(codes)
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("%s (%d)", code, failures[code]))
	}
	a.warn("quota lookup failed for %d account(s): %s", total, strings.Join(parts, ", "))
}
