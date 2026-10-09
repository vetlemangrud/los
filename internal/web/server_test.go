package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"los/internal/ais"
	"los/internal/geo"
	"los/internal/images"
	"los/internal/vessel"
)

var (
	observer = geo.Point{Lat: 59.25, Lon: 10.5}
	testArc  = geo.Arc{Center: observer, FromDeg: 120, ToDeg: 270, RadiusNM: 12}
	now      = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) // 14:00 in Oslo
)

func at(bearing, dist float64) geo.Point { return geo.Destination(observer, bearing, dist) }

type fakeBoats struct {
	res  ais.Result
	err  error
	keys []string
}

func (f *fakeBoats) Get(_ context.Context, key string, _ [][2]float64, _ time.Time) (ais.Result, error) {
	f.keys = append(f.keys, key)
	return f.res, f.err
}

type fakeImages map[int]images.Image

func (f fakeImages) Resolve(context.Context, []vessel.Vessel) map[int]images.Image { return f }

func newServer(b *fakeBoats, imgs fakeImages) http.Handler {
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		panic(err)
	}
	s := &Server{
		Arc: testArc, MaxAge: 15 * time.Minute, Boats: b, Images: imgs,
		Now: func() time.Time { return now }, Loc: loc, Logf: func(string, ...any) {},
	}
	return s.Handler()
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func sampleBoats() []vessel.Vessel {
	return []vessel.Vessel{
		{MMSI: 2, Name: "FAR", ShipType: 70, Destination: "ROTTERDAM", Pos: at(200, 8), SpeedKn: 12.1, MsgTime: now.Add(-time.Minute)},
		{MMSI: 1, IMO: 9123456, Name: "NEAR", ShipType: 60, Destination: "NO BGO", ETA: now.Add(18 * time.Hour),
			Pos: at(180, 2), SpeedKn: 9.8, MsgTime: now.Add(-time.Minute)},
		{MMSI: 3, Name: "BEHIND", ShipType: 70, Pos: at(0, 3), MsgTime: now.Add(-time.Minute)},
		{MMSI: 4, Name: "OLD", ShipType: 70, Pos: at(180, 1), MsgTime: now.Add(-30 * time.Minute)},
	}
}

func TestListShowsVisibleBoatsNearestFirst(t *testing.T) {
	b := &fakeBoats{res: ais.Result{Vessels: sampleBoats(), FetchedAt: now}}
	imgs := fakeImages{1: {ThumbURL: "https://upload.wikimedia.org/near.jpg", PageURL: "https://commons.wikimedia.org/wiki/File:Near.jpg"}}
	rec := get(t, newServer(b, imgs), "/")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"NEAR", "FAR", "2.0 nm", "8.0 nm", "Passenger · 9.8 kn", "Cargo · 12.1 kn",
		"Bergen, NO", "ETA 10 Oct 08:00", "Rotterdam",
		"https://upload.wikimedia.org/near.jpg", "/static/icons/ship.svg",
		"https://www.vesselfinder.com/vessels/details/9123456",
		"https://www.vesselfinder.com/vessels/details/2",
		"updated 14:00", "More info ↗", `lang="en"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	for _, unwanted := range []string{"BEHIND", "OLD", "(stale)"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("body should not contain %q", unwanted)
		}
	}
	if strings.Index(body, "NEAR") > strings.Index(body, "FAR") {
		t.Error("NEAR should be listed before FAR")
	}
	if len(b.keys) != 1 || b.keys[0] != "arc" {
		t.Errorf("cache keys = %v", b.keys)
	}
}

func TestListEscapesAISText(t *testing.T) {
	vs := []vessel.Vessel{{MMSI: 1, Name: "<script>alert(1)</script>", Destination: "<b>X</b>",
		Pos: at(180, 2), MsgTime: now}}
	rec := get(t, newServer(&fakeBoats{res: ais.Result{Vessels: vs, FetchedAt: now}}, nil), "/")
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert") || strings.Contains(strings.ToLower(body), "<b>") {
		t.Fatal("AIS text not escaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("escaped name missing")
	}
}

func TestListEmpty(t *testing.T) {
	rec := get(t, newServer(&fakeBoats{res: ais.Result{FetchedAt: now}}, nil), "/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No boats in view right now.") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestListStale(t *testing.T) {
	b := &fakeBoats{res: ais.Result{Vessels: sampleBoats(), FetchedAt: now.Add(-10 * time.Minute), Stale: true}}
	rec := get(t, newServer(b, nil), "/")
	if !strings.Contains(rec.Body.String(), "Data from 13:50 (stale)") {
		t.Fatalf("stale note missing: %s", rec.Body)
	}
}

func TestListUnavailable(t *testing.T) {
	rec := get(t, newServer(&fakeBoats{err: errors.New("down")}, nil), "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "reach AIS data, try again shortly.") {
		t.Fatalf("error copy missing: %s", rec.Body)
	}
}

func TestPartialAISData(t *testing.T) {
	vs := []vessel.Vessel{{MMSI: 257000003, Pos: at(180, 3), MsgTime: now}}
	body := get(t, newServer(&fakeBoats{res: ais.Result{Vessels: vs, FetchedAt: now}}, nil), "/").Body.String()
	for _, want := range []string{"MMSI 257000003", "Destination unknown", "Other"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestHealthz(t *testing.T) {
	rec := get(t, newServer(&fakeBoats{}, nil), "/healthz")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
}

func TestStaticServed(t *testing.T) {
	h := newServer(&fakeBoats{}, nil)
	for _, p := range []string{"/static/style.css", "/static/icons/ship.svg", "/static/icons/sail.svg", "/static/icons/fishing.svg"} {
		if rec := get(t, h, p); rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", p, rec.Code)
		}
	}
}

func TestListStaleBeyondMaxAgeStillShowsBoats(t *testing.T) {
	fetched := now.Add(-20 * time.Minute)
	vs := []vessel.Vessel{{MMSI: 1, Name: "SEEN", Pos: at(180, 2), MsgTime: fetched.Add(-time.Minute)}}
	b := &fakeBoats{res: ais.Result{Vessels: vs, FetchedAt: fetched, Stale: true}}
	body := get(t, newServer(b, nil), "/").Body.String()
	if !strings.Contains(body, "SEEN") || strings.Contains(body, "No boats in view") {
		t.Fatalf("stale data older than max age must still list boats: %s", body)
	}
}

func TestListOmitsUnknownSpeed(t *testing.T) {
	vs := []vessel.Vessel{{MMSI: 1, Name: "DRIFT", ShipType: 70, SpeedUnknown: true, Pos: at(180, 2), MsgTime: now}}
	body := get(t, newServer(&fakeBoats{res: ais.Result{Vessels: vs, FetchedAt: now}}, nil), "/").Body.String()
	if strings.Contains(body, " kn") || !strings.Contains(body, "Cargo") {
		t.Fatalf("unknown speed should be omitted: %s", body)
	}
}
