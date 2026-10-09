package images

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestCommons(t *testing.T, h http.HandlerFunc) *Commons {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewCommons()
	c.APIURL = srv.URL
	return c
}

func TestCommonsFind(t *testing.T) {
	c := newTestCommons(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("gsrsearch") != "IMO 9123456 filetype:bitmap" || q.Get("gsrnamespace") != "6" ||
			q.Get("generator") != "search" || q.Get("iiurlwidth") != "960" || q.Get("formatversion") != "2" {
			t.Errorf("query = %v", q)
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Los/") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		w.Write([]byte(`{"batchcomplete":true,"query":{"pages":[{"pageid":1,"ns":6,"title":"File:Ship (IMO 9123456).jpg",
			"imageinfo":[{"thumburl":"https://upload.wikimedia.org/thumb/ship.jpg","descriptionurl":"https://commons.wikimedia.org/wiki/File:Ship.jpg"}]}]}}`))
	})
	img, err := c.Find(context.Background(), "IMO 9123456", "9123456")
	if err != nil {
		t.Fatal(err)
	}
	want := Image{ThumbURL: "https://upload.wikimedia.org/thumb/ship.jpg", PageURL: "https://commons.wikimedia.org/wiki/File:Ship.jpg"}
	if img != want {
		t.Fatalf("got %+v", img)
	}
}

func TestCommonsFindSkipsUnrelatedTitles(t *testing.T) {
	c := newTestCommons(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("gsrlimit") != "10" {
			t.Errorf("gsrlimit = %q", r.URL.Query().Get("gsrlimit"))
		}
		// Pages arrive unordered; "index" is the search rank.
		w.Write([]byte(`{"query":{"pages":[
			{"index":2,"title":"File:Norheim at quay.jpg","imageinfo":[{"thumburl":"right","descriptionurl":"p2"}]},
			{"index":1,"title":"File:\"Arklow Brook\" passing the Albert Dock.jpg","imageinfo":[{"thumburl":"wrong","descriptionurl":"p1"}]},
			{"index":3,"title":"File:Norheim (ship) 2019.jpg","imageinfo":[{"thumburl":"later","descriptionurl":"p3"}]}]}}`))
	})
	img, err := c.Find(context.Background(), `"NORHEIM" ship`, "NORHEIM")
	if err != nil || img.ThumbURL != "right" {
		t.Fatalf("got %+v %v, want the best-ranked title containing the name", img, err)
	}
	img, err = c.Find(context.Background(), "IMO 9433377", "9433377")
	if err != nil || img.Found() {
		t.Fatalf("no title has the IMO, want nothing, got %+v %v", img, err)
	}
}

func TestCommonsFindNothing(t *testing.T) {
	c := newTestCommons(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"batchcomplete":true}`))
	})
	img, err := c.Find(context.Background(), "IMO 1", "1")
	if err != nil || img.Found() {
		t.Fatalf("got %+v %v", img, err)
	}
}

func TestCommonsFindError(t *testing.T) {
	c := newTestCommons(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if _, err := c.Find(context.Background(), "IMO 1", "1"); err == nil {
		t.Fatal("want error on 503")
	}
}
