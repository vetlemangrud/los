package ais

import (
	"context"
	"errors"
	"testing"
	"time"

	"los/internal/vessel"
)

type fakeSource struct {
	calls int
	err   error
	vs    []vessel.Vessel
}

func (f *fakeSource) LatestInArea(context.Context, [][2]float64, time.Time) ([]vessel.Vessel, error) {
	f.calls++
	return f.vs, f.err
}

func newTestCache(src Source) (*Cache, *time.Time) {
	now := t0
	c := NewCache(src, 30*time.Second)
	c.Now = func() time.Time { return now }
	c.Logf = func(string, ...any) {}
	return c, &now
}

func TestCacheServesFreshWithinTTL(t *testing.T) {
	src := &fakeSource{vs: []vessel.Vessel{{MMSI: 1}}}
	c, now := newTestCache(src)
	ctx := context.Background()
	r1, err := c.Get(ctx, "arc", ring, t0)
	if err != nil || len(r1.Vessels) != 1 || r1.Stale || !r1.FetchedAt.Equal(t0) {
		t.Fatalf("first get: %+v %v", r1, err)
	}
	*now = t0.Add(29 * time.Second)
	if _, err := c.Get(ctx, "arc", ring, t0); err != nil {
		t.Fatal(err)
	}
	if src.calls != 1 {
		t.Fatalf("calls = %d, want 1 within TTL", src.calls)
	}
	*now = t0.Add(31 * time.Second)
	if _, err := c.Get(ctx, "arc", ring, t0); err != nil {
		t.Fatal(err)
	}
	if src.calls != 2 {
		t.Fatalf("calls = %d, want 2 after TTL", src.calls)
	}
}

func TestCacheKeysAreIndependent(t *testing.T) {
	src := &fakeSource{}
	c, _ := newTestCache(src)
	ctx := context.Background()
	c.Get(ctx, "arc", ring, t0)
	c.Get(ctx, "debug", ring, t0)
	if src.calls != 2 {
		t.Fatalf("calls = %d, want 2 for two keys", src.calls)
	}
}

func TestCacheServesStaleOnError(t *testing.T) {
	src := &fakeSource{vs: []vessel.Vessel{{MMSI: 1}}}
	c, now := newTestCache(src)
	ctx := context.Background()
	c.Get(ctx, "arc", ring, t0)

	src.err = errors.New("boom")
	*now = t0.Add(5 * time.Minute)
	r, err := c.Get(ctx, "arc", ring, t0)
	if err != nil {
		t.Fatalf("want stale result, got error %v", err)
	}
	if !r.Stale || !r.FetchedAt.Equal(t0) || len(r.Vessels) != 1 {
		t.Fatalf("stale result wrong: %+v", r)
	}

	src.err = nil
	*now = t0.Add(6 * time.Minute)
	r, err = c.Get(ctx, "arc", ring, t0)
	if err != nil || r.Stale || !r.FetchedAt.Equal(t0.Add(6*time.Minute)) {
		t.Fatalf("recovery: %+v %v", r, err)
	}
}

func TestCacheErrorWithoutPrevious(t *testing.T) {
	c, _ := newTestCache(&fakeSource{err: errors.New("boom")})
	if _, err := c.Get(context.Background(), "arc", ring, t0); err == nil {
		t.Fatal("want error when nothing cached")
	}
}

func TestCacheDoesNotRetryImmediatelyAfterFailure(t *testing.T) {
	src := &fakeSource{vs: []vessel.Vessel{{MMSI: 1}}}
	c, now := newTestCache(src)
	ctx := context.Background()
	c.Get(ctx, "arc", ring, t0)
	src.err = errors.New("boom")
	*now = t0.Add(5 * time.Minute)
	c.Get(ctx, "arc", ring, t0) // fails, serves stale
	*now = t0.Add(5*time.Minute + 10*time.Second)
	r, err := c.Get(ctx, "arc", ring, t0)
	if err != nil || !r.Stale {
		t.Fatalf("want stale result, got %+v %v", r, err)
	}
	if src.calls != 2 {
		t.Fatalf("calls = %d, want 2 (no retry within TTL of a failure)", src.calls)
	}
	*now = t0.Add(5*time.Minute + 31*time.Second)
	c.Get(ctx, "arc", ring, t0)
	if src.calls != 3 {
		t.Fatalf("calls = %d, want 3 (retry after TTL)", src.calls)
	}
}

type ctxSource struct{ ctxErr error }

func (s *ctxSource) LatestInArea(ctx context.Context, _ [][2]float64, _ time.Time) ([]vessel.Vessel, error) {
	s.ctxErr = ctx.Err()
	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		return nil, errors.New("fetch has no deadline")
	}
	return nil, nil
}

func TestCacheFetchSurvivesRequestCancellation(t *testing.T) {
	src := &ctxSource{}
	c, _ := newTestCache(src)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the visitor who triggered the fetch has gone away
	if _, err := c.Get(ctx, "arc", ring, t0); err != nil {
		t.Fatal(err)
	}
	if src.ctxErr != nil {
		t.Fatalf("shared fetch inherited request cancellation: %v", src.ctxErr)
	}
}
