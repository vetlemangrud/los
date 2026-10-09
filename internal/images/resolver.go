package images

import (
	"context"
	"fmt"
	"sync"
	"time"

	"los/internal/vessel"
)

type Finder interface {
	Find(ctx context.Context, query, match string) (Image, error)
}

type Cache interface {
	Get(key string) (Image, bool, error)
	Put(key string, img Image) error
}

// Resolver finds images for a page of vessels within a time budget.
// Cache may be nil (the page still works, just without memory).
type Resolver struct {
	Cache       Cache
	Finder      Finder
	Concurrency int
	Budget      time.Duration
	Logf        func(string, ...any)
}

// Resolve returns found images keyed by MMSI. Vessels whose lookup misses,
// fails or overruns the budget are absent; the page shows an icon instead.
func (r *Resolver) Resolve(ctx context.Context, vs []vessel.Vessel) map[int]Image {
	ctx, cancel := context.WithTimeout(ctx, r.Budget)
	defer cancel()

	var (
		mu  sync.Mutex
		out = map[int]Image{}
		wg  sync.WaitGroup
		sem = make(chan struct{}, max(1, r.Concurrency))
	)
	for _, v := range vs {
		wg.Add(1)
		go func(v vessel.Vessel) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if img, ok := r.lookup(ctx, v); ok && img.Found() {
				mu.Lock()
				out[v.MMSI] = img
				mu.Unlock()
			}
		}(v)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}

	mu.Lock()
	defer mu.Unlock()
	res := make(map[int]Image, len(out))
	for k, v := range out {
		res[k] = v
	}
	return res
}

// lookup returns ok=false when the answer is unknown (error or timeout),
// so nothing is cached and the next page load tries again.
func (r *Resolver) lookup(ctx context.Context, v vessel.Vessel) (Image, bool) {
	key := cacheKey(v)
	if r.Cache != nil {
		img, fresh, err := r.Cache.Get(key)
		if err != nil {
			r.logf("image cache get %s: %v", key, err)
		} else if fresh {
			return img, true
		}
	}
	var img Image
	for _, q := range queries(v) {
		found, err := r.Finder.Find(ctx, q.text, q.match)
		if err != nil {
			if ctx.Err() == nil {
				r.logf("image lookup %q: %v", q.text, err)
			}
			return Image{}, false
		}
		if found.Found() {
			img = found
			break
		}
	}
	if r.Cache != nil {
		if err := r.Cache.Put(key, img); err != nil {
			r.logf("image cache put %s: %v", key, err)
		}
	}
	return img, true
}

// cacheKey is versioned: v1 entries came from unchecked search results and
// may hold another ship's photo, so they are left to age out.
func cacheKey(v vessel.Vessel) string {
	if v.IMO > 0 {
		return fmt.Sprintf("v2:imo:%d", v.IMO)
	}
	return fmt.Sprintf("v2:mmsi:%d", v.MMSI)
}

type query struct{ text, match string }

// queries lists Commons searches to try in order, each with the text the
// file title must contain. Names shorter than four characters match too
// much unrelated media to be worth searching.
func queries(v vessel.Vessel) []query {
	var qs []query
	if v.IMO > 0 {
		imo := fmt.Sprint(v.IMO)
		qs = append(qs, query{"IMO " + imo, imo})
	}
	if name := v.CleanName(); len([]rune(name)) >= 4 {
		qs = append(qs, query{`"` + name + `" ship`, name})
	}
	return qs
}

func (r *Resolver) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}
