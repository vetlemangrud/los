package images

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

func openTemp(t *testing.T) (*Store, *time.Time, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "los.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := t0
	s.Now = func() time.Time { return now }
	return s, &now, path
}

func TestStorePutGet(t *testing.T) {
	s, _, _ := openTemp(t)
	img := Image{ThumbURL: "https://upload.wikimedia.org/a.jpg", PageURL: "https://commons.wikimedia.org/wiki/File:A.jpg"}
	if err := s.Put("imo:1", img); err != nil {
		t.Fatal(err)
	}
	got, fresh, err := s.Get("imo:1")
	if err != nil || !fresh || got != img {
		t.Fatalf("got %+v fresh=%v err=%v", got, fresh, err)
	}
	if _, fresh, _ := s.Get("imo:2"); fresh {
		t.Fatal("unknown key reported fresh")
	}
}

func TestStoreCachesMiss(t *testing.T) {
	s, _, _ := openTemp(t)
	s.Put("imo:1", Image{})
	got, fresh, err := s.Get("imo:1")
	if err != nil || !fresh || got.Found() {
		t.Fatalf("miss: got %+v fresh=%v err=%v", got, fresh, err)
	}
}

func TestStoreTTL(t *testing.T) {
	s, now, _ := openTemp(t)
	s.Put("hit", Image{ThumbURL: "x", PageURL: "y"})
	s.Put("miss", Image{})

	*now = t0.Add(6 * day)
	if _, fresh, _ := s.Get("miss"); !fresh {
		t.Error("miss should be fresh at 6 days")
	}
	*now = t0.Add(8 * day)
	if _, fresh, _ := s.Get("miss"); fresh {
		t.Error("miss should expire after 7 days")
	}
	if _, fresh, _ := s.Get("hit"); !fresh {
		t.Error("hit should be fresh at 8 days")
	}
	*now = t0.Add(31 * day)
	if _, fresh, _ := s.Get("hit"); fresh {
		t.Error("hit should expire after 30 days")
	}
}

func TestStorePruneExpired(t *testing.T) {
	s, now, _ := openTemp(t)
	s.Put("hit", Image{ThumbURL: "x"})
	s.Put("miss", Image{})
	*now = t0.Add(8 * day)
	if err := s.Prune(); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(); n != 1 {
		t.Fatalf("after 8 days count = %d, want 1 (miss pruned)", n)
	}
	*now = t0.Add(31 * day)
	s.Prune()
	if n, _ := s.Count(); n != 0 {
		t.Fatalf("after 31 days count = %d, want 0", n)
	}
}

func TestStorePruneCapsRowsLRU(t *testing.T) {
	s, now, _ := openTemp(t)
	s.MaxRows = 3
	for i := 0; i < 5; i++ {
		*now = t0.Add(time.Duration(i) * time.Minute)
		s.Put(fmt.Sprintf("k%d", i), Image{ThumbURL: "x"})
	}
	*now = t0.Add(10 * time.Minute)
	s.Get("k0") // bump k0 to most recently used
	if err := s.Prune(); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(); n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
	for key, want := range map[string]bool{"k0": true, "k1": false, "k2": false, "k3": true, "k4": true} {
		if _, fresh, _ := s.Get(key); fresh != want {
			t.Errorf("%s present = %v, want %v", key, fresh, want)
		}
	}
}

func TestStorePersistsAcrossReopen(t *testing.T) {
	s, _, path := openTemp(t)
	s.Put("imo:1", Image{ThumbURL: "x", PageURL: "y"})
	s.Close()
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	s2.Now = func() time.Time { return t0 }
	if got, fresh, _ := s2.Get("imo:1"); !fresh || got.ThumbURL != "x" {
		t.Fatalf("after reopen: %+v fresh=%v", got, fresh)
	}
}
