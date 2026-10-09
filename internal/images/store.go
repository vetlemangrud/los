// Package images finds a photo for a vessel on Wikimedia Commons and
// remembers the answer in a small SQLite cache.
package images

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

type Image struct {
	ThumbURL string
	PageURL  string
}

func (i Image) Found() bool { return i.ThumbURL != "" }

const schema = `CREATE TABLE IF NOT EXISTS image_cache (
  key        TEXT PRIMARY KEY,
  thumb_url  TEXT,
  page_url   TEXT,
  fetched_at INTEGER NOT NULL,
  used_at    INTEGER NOT NULL
)`

// Store caches lookup results (URLs only, never image bytes). Size stays
// bounded by TTLs plus a hard row cap enforced in Prune.
type Store struct {
	Now     func() time.Time
	HitTTL  time.Duration
	MissTTL time.Duration
	MaxRows int

	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{
		Now:     time.Now,
		HitTTL:  30 * 24 * time.Hour,
		MissTTL: 7 * 24 * time.Hour,
		MaxRows: 5000,
		db:      db,
	}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Get returns the cached image for key. fresh is false when nothing usable
// is cached; fresh with a zero Image means "looked before, found nothing".
func (s *Store) Get(key string) (Image, bool, error) {
	var thumb, page sql.NullString
	var fetched int64
	err := s.db.QueryRow(`SELECT thumb_url, page_url, fetched_at FROM image_cache WHERE key = ?`, key).
		Scan(&thumb, &page, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return Image{}, false, nil
	}
	if err != nil {
		return Image{}, false, err
	}
	img := Image{ThumbURL: thumb.String, PageURL: page.String}
	ttl := s.MissTTL
	if img.Found() {
		ttl = s.HitTTL
	}
	now := s.Now()
	if now.Sub(time.Unix(fetched, 0)) > ttl {
		return Image{}, false, nil
	}
	_, err = s.db.Exec(`UPDATE image_cache SET used_at = ? WHERE key = ?`, now.Unix(), key)
	return img, true, err
}

func (s *Store) Put(key string, img Image) error {
	now := s.Now().Unix()
	_, err := s.db.Exec(`INSERT INTO image_cache (key, thumb_url, page_url, fetched_at, used_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET thumb_url = excluded.thumb_url, page_url = excluded.page_url,
			fetched_at = excluded.fetched_at, used_at = excluded.used_at`,
		key, nullable(img.ThumbURL), nullable(img.PageURL), now, now)
	return err
}

// Prune drops expired rows, then the least recently used rows beyond
// MaxRows, then compacts the file.
func (s *Store) Prune() error {
	now := s.Now()
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM image_cache WHERE thumb_url IS NOT NULL AND fetched_at < ?`, []any{now.Add(-s.HitTTL).Unix()}},
		{`DELETE FROM image_cache WHERE thumb_url IS NULL AND fetched_at < ?`, []any{now.Add(-s.MissTTL).Unix()}},
		{`DELETE FROM image_cache WHERE key NOT IN (SELECT key FROM image_cache ORDER BY used_at DESC LIMIT ?)`, []any{s.MaxRows}},
		{`VACUUM`, nil},
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st.q, st.args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM image_cache`).Scan(&n)
	return n, err
}

// RunPruner prunes now and then every interval until ctx is done.
func (s *Store) RunPruner(ctx context.Context, every time.Duration, logf func(string, ...any)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.Prune(); err != nil {
			logf("image cache prune: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
