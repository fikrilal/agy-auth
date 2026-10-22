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

type usageRowResult struct {
	snapshot usage.Snapshot
	detail   *usage.Detail
	err      error
}

type usageFetcher func(ctx context.Context, key string, token *agy.TokenFile) usageRowResult

func (a *App) enrichUsage(rows []accountRow) map[string]int {
	if a.Usage == nil {
		return nil
	}
	return a.enrichRows(rows, func(ctx context.Context, key string, token *agy.TokenFile) usageRowResult {
		snapshot, err := a.Usage.Fetch(ctx, key, token)
		return usageRowResult{snapshot: snapshot, err: err}
	})
}

func (a *App) enrichUsageFull(rows []accountRow) map[string]int {
	if a.UsageFull == nil {
		return a.enrichUsage(rows)
	}
	return a.enrichRows(rows, func(ctx context.Context, key string, token *agy.TokenFile) usageRowResult {
		full, err := a.UsageFull.FetchFull(ctx, key, token)
		if err != nil {
			return usageRowResult{err: err}
		}
		result := usageRowResult{snapshot: full.Snapshot, err: full.Err}
		if full.Detail.Buckets != nil {
			detail := full.Detail
			result.detail = &detail
		}
		return result
	})
}

func (a *App) enrichRows(rows []accountRow, fetch usageFetcher) map[string]int {
	if fetch == nil || len(rows) == 0 {
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
				result := fetch(ctx, job.key, job.token)
				if result.snapshot.Plan != "" {
					rows[job.index].Plan = result.snapshot.Plan
				}
				if result.snapshot.FiveHour != nil || result.snapshot.Weekly != nil {
					value := result.snapshot
					rows[job.index].Usage = &value
				}
				if result.detail != nil {
					rows[job.index].detail = result.detail
				}
				if result.err != nil {
					code := usage.ShortError(result.err)
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
