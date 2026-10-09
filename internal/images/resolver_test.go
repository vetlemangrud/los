package images

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"los/internal/vessel"
)

type fakeFinder struct {
	mu      sync.Mutex
	queries []string
	matches []string
	results map[string]Image
	err     error
	block   bool // wait for ctx cancellation
}

func (f *fakeFinder) Find(ctx context.Context, q, match string) (Image, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.matches = append(f.matches, match)
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return Image{}, ctx.Err()
	}
	return f.results[q], f.err
}

type memCache struct {
	mu   sync.Mutex
	data map[string]Image
}

func newMemCache() *memCache { return &memCache{data: map[string]Image{}} }

func (m *memCache) Get(key string) (Image, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	img, ok := m.data[key]
	return img, ok, nil
}

func (m *memCache) Put(key string, img Image) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = img
	return nil
}

var shipImg = Image{ThumbURL: "t", PageURL: "p"}

func newResolver(f Finder, c Cache) *Resolver {
	return &Resolver{Cache: c, Finder: f, Concurrency: 4, Budget: 200 * time.Millisecond, Logf: func(string, ...any) {}}
}

func TestResolveByIMOThenName(t *testing.T) {
	f := &fakeFinder{results: map[string]Image{`"NORDIC STAR" ship`: shipImg}}
	c := newMemCache()
	got := newResolver(f, c).Resolve(context.Background(), []vessel.Vessel{{MMSI: 1, IMO: 9123456, Name: "NORDIC STAR@@"}})
	if got[1] != shipImg {
		t.Fatalf("got %+v", got)
	}
	if len(f.queries) != 2 || f.queries[0] != "IMO 9123456" || f.queries[1] != `"NORDIC STAR" ship` {
		t.Fatalf("queries = %v", f.queries)
	}
	if f.matches[0] != "9123456" || f.matches[1] != "NORDIC STAR" {
		t.Fatalf("matches = %v", f.matches)
	}
	if c.data["v2:imo:9123456"] != shipImg {
		t.Fatalf("not cached under imo key: %v", c.data)
	}
}

func TestResolveUsesCache(t *testing.T) {
	f := &fakeFinder{}
	c := newMemCache()
	c.data["v2:mmsi:2"] = shipImg
	got := newResolver(f, c).Resolve(context.Background(), []vessel.Vessel{{MMSI: 2, Name: "X"}})
	if got[2] != shipImg || len(f.queries) != 0 {
		t.Fatalf("got %+v queries %v", got, f.queries)
	}
}

func TestResolveCachesMiss(t *testing.T) {
	f := &fakeFinder{}
	c := newMemCache()
	got := newResolver(f, c).Resolve(context.Background(), []vessel.Vessel{{MMSI: 3, Name: "AB"}})
	if _, ok := got[3]; ok {
		t.Fatal("miss should not be in result map")
	}
	if img, ok := c.data["v2:mmsi:3"]; !ok || img.Found() {
		t.Fatalf("miss not cached: %v", c.data)
	}
	if len(f.queries) != 0 {
		t.Fatalf("short name and no IMO should not search, got %v", f.queries)
	}
}

func TestResolveErrorNotCached(t *testing.T) {
	f := &fakeFinder{err: errors.New("down")}
	c := newMemCache()
	got := newResolver(f, c).Resolve(context.Background(), []vessel.Vessel{{MMSI: 4, IMO: 1}})
	if len(got) != 0 || len(c.data) != 0 {
		t.Fatalf("got %v cache %v", got, c.data)
	}
}

func TestResolveRespectsBudget(t *testing.T) {
	f := &fakeFinder{block: true}
	c := newMemCache()
	start := time.Now()
	got := newResolver(f, c).Resolve(context.Background(), []vessel.Vessel{{MMSI: 5, IMO: 1}, {MMSI: 6, IMO: 2}})
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Resolve took %v, budget is 200ms", d)
	}
	if len(got) != 0 || len(c.data) != 0 {
		t.Fatalf("timed-out lookups must not produce results or cache entries: %v %v", got, c.data)
	}
}

func TestResolveWithoutCache(t *testing.T) {
	f := &fakeFinder{results: map[string]Image{"IMO 7": shipImg}}
	got := newResolver(f, nil).Resolve(context.Background(), []vessel.Vessel{{MMSI: 7, IMO: 7}})
	if got[7] != shipImg {
		t.Fatalf("got %+v", got)
	}
}
