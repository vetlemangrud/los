package ais

import (
	"context"
	"log"
	"sync"
	"time"

	"los/internal/vessel"
)

type Source interface {
	LatestInArea(ctx context.Context, ring [][2]float64, since time.Time) ([]vessel.Vessel, error)
}

type Result struct {
	Vessels   []vessel.Vessel
	FetchedAt time.Time
	Stale     bool // true when the source failed and this is older data
}

// Cache keeps the last result per key for ttl, and keeps serving it
// (marked Stale) when the source fails.
type Cache struct {
	Now          func() time.Time
	Logf         func(string, ...any)
	FetchTimeout time.Duration // upper bound for one upstream fetch, token included

	src     Source
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]Result
	failed  map[string]time.Time // last failed attempt per key, while serving stale
}

func NewCache(src Source, ttl time.Duration) *Cache {
	return &Cache{
		Now: time.Now, Logf: log.Printf, FetchTimeout: 12 * time.Second,
		src: src, ttl: ttl, entries: map[string]Result{}, failed: map[string]time.Time{},
	}
}

// Get holds the lock across the fetch so concurrent page loads share one
// upstream call. After a failure the stale result is served for ttl before
// retrying, so an outage costs one slow request per ttl, not one per visitor.
// The fetch is detached from the caller's context: it is shared, so one
// visitor leaving must not cancel it for everyone.
func (c *Cache) Get(ctx context.Context, key string, ring [][2]float64, since time.Time) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	prev, ok := c.entries[key]
	if ok && !prev.Stale && now.Sub(prev.FetchedAt) < c.ttl {
		return prev, nil
	}
	if failedAt, f := c.failed[key]; ok && f && now.Sub(failedAt) < c.ttl {
		return prev, nil
	}
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.FetchTimeout)
	defer cancel()
	vs, err := c.src.LatestInArea(fetchCtx, ring, since)
	if err != nil {
		if ok {
			c.failed[key] = now
			c.Logf("ais: %v (serving data from %s)", err, prev.FetchedAt.Format(time.RFC3339))
			prev.Stale = true
			c.entries[key] = prev
			return prev, nil
		}
		return Result{}, err
	}
	res := Result{Vessels: vs, FetchedAt: now}
	c.entries[key] = res
	delete(c.failed, key)
	return res, nil
}
