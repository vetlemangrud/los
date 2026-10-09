package ais

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

var ring = [][2]float64{{10.4, 59.0}, {10.5, 59.0}, {10.5, 59.1}, {10.4, 59.0}}

const fixture = `[
 {"mmsi":257000001,"msgtime":"2026-10-09T12:00:00+00:00","latitude":59.0,"longitude":10.4,
  "speedOverGround":9.8,"courseOverGround":210.5,"imoNumber":9123456,"callSign":"LXYZ",
  "name":"KRONPRINS HAAKON","destination":"NO BGO","eta":"10100600","shipType":60,
  "shipLength":120,"navigationalStatus":0,"positionFixingDeviceType":1,"reportClass":"A",
  "msgtimeStatic":"2026-10-09T11:58:00+00:00"},
 {"mmsi":257000002,"msgtime":"2026-10-09T12:01:00+00:00","latitude":null,"longitude":null,
  "name":"NO POSITION","navigationalStatus":0,"positionFixingDeviceType":1,"reportClass":"A",
  "msgtimeStatic":"2026-10-09T11:58:00+00:00"},
 {"mmsi":257000003,"msgtime":"2026-10-09T12:02:00Z","latitude":58.9,"longitude":10.3,
  "speedOverGround":null,"courseOverGround":null,"imoNumber":null,"callSign":null,"name":null,
  "destination":null,"eta":null,"shipType":null,"shipLength":null,"navigationalStatus":0,
  "positionFixingDeviceType":1,"reportClass":"B","msgtimeStatic":"2026-10-09T11:58:00Z"}
]`

type fakeBW struct {
	tokenHits atomic.Int32
	apiHits   atomic.Int32
	lastBody  map[string]any
	// apiStatus, if set, decides the status for the n-th API call (1-based).
	apiStatus func(n int32, auth string) int
}

func (f *fakeBW) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		n := f.tokenHits.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		for k, want := range map[string]string{
			"grant_type": "client_credentials", "scope": "ais",
			"client_id": "id", "client_secret": "secret",
		} {
			if got := r.PostForm.Get(k); got != want {
				t.Errorf("token form %s = %q, want %q", k, got, want)
			}
		}
		fmt.Fprintf(w, `{"access_token":"tok-%d","expires_in":3600,"token_type":"Bearer"}`, n)
	})
	mux.HandleFunc("POST /latest", func(w http.ResponseWriter, r *http.Request) {
		n := f.apiHits.Add(1)
		if f.apiStatus != nil {
			if code := f.apiStatus(n, r.Header.Get("Authorization")); code != http.StatusOK {
				w.WriteHeader(code)
				return
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&f.lastBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, fixture)
	})
	return mux
}

func newTestClient(t *testing.T, f *fakeBW) (*Client, *time.Time) {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	now := t0
	c := NewClient("id", "secret")
	c.TokenURL = srv.URL + "/token"
	c.APIURL = srv.URL + "/latest"
	c.Now = func() time.Time { return now }
	return c, &now
}

func TestLatestInAreaMapsVessels(t *testing.T) {
	c, _ := newTestClient(t, &fakeBW{})
	vs, err := c.LatestInArea(context.Background(), ring, t0.Add(-15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 {
		t.Fatalf("got %d vessels, want 2 (null position skipped)", len(vs))
	}
	v := vs[0]
	if v.MMSI != 257000001 || v.IMO != 9123456 || v.Name != "KRONPRINS HAAKON" || v.CallSign != "LXYZ" ||
		v.ShipType != 60 || v.Destination != "NO BGO" || v.SpeedKn != 9.8 || v.CourseDeg != 210.5 ||
		v.LengthM != 120 || v.Pos.Lat != 59.0 || v.Pos.Lon != 10.4 {
		t.Errorf("vessel 0 mapped wrong: %+v", v)
	}
	if !v.ETA.Equal(time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("ETA = %v", v.ETA)
	}
	if !v.MsgTime.Equal(t0) {
		t.Errorf("MsgTime = %v", v.MsgTime)
	}
	n := vs[1]
	if v.SpeedUnknown {
		t.Errorf("vessel 0 speed should be known")
	}
	if n.MMSI != 257000003 || n.Name != "" || n.IMO != 0 || n.ShipType != 0 || !n.ETA.IsZero() || !n.SpeedUnknown {
		t.Errorf("nullable vessel mapped wrong: %+v", n)
	}
}

func TestLatestInAreaRequestBody(t *testing.T) {
	f := &fakeBW{}
	c, _ := newTestClient(t, f)
	if _, err := c.LatestInArea(context.Background(), ring, t0.Add(-15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	b := f.lastBody
	if b["since"] != "2026-10-09T11:45:00Z" || b["modelType"] != "Full" || b["modelFormat"] != "Json" {
		t.Errorf("body = %v", b)
	}
	geom := b["geometry"].(map[string]any)
	if geom["type"] != "Polygon" {
		t.Errorf("geometry type = %v", geom["type"])
	}
	coords := geom["coordinates"].([]any)
	if len(coords) != 1 || len(coords[0].([]any)) != len(ring) {
		t.Errorf("coordinates = %v", coords)
	}
}

func TestTokenCachedUntilExpiry(t *testing.T) {
	f := &fakeBW{}
	c, now := newTestClient(t, f)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := c.LatestInArea(ctx, ring, t0); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.tokenHits.Load(); got != 1 {
		t.Fatalf("token fetched %d times, want 1", got)
	}
	*now = t0.Add(3600 * time.Second) // past expiry minus 60s margin
	if _, err := c.LatestInArea(ctx, ring, t0); err != nil {
		t.Fatal(err)
	}
	if got := f.tokenHits.Load(); got != 2 {
		t.Fatalf("token fetched %d times after expiry, want 2", got)
	}
}

func TestUnauthorizedRetriesWithFreshToken(t *testing.T) {
	f := &fakeBW{apiStatus: func(n int32, auth string) int {
		if auth != "Bearer tok-2" {
			return http.StatusUnauthorized
		}
		return http.StatusOK
	}}
	c, _ := newTestClient(t, f)
	vs, err := c.LatestInArea(context.Background(), ring, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || f.tokenHits.Load() != 2 || f.apiHits.Load() != 2 {
		t.Fatalf("vessels=%d tokenHits=%d apiHits=%d", len(vs), f.tokenHits.Load(), f.apiHits.Load())
	}
}

func TestServerErrorIsReturned(t *testing.T) {
	f := &fakeBW{apiStatus: func(int32, string) int { return http.StatusInternalServerError }}
	c, _ := newTestClient(t, f)
	if _, err := c.LatestInArea(context.Background(), ring, t0); err == nil {
		t.Fatal("want error on 500")
	}
}

func TestSpeedNotAvailableSentinel(t *testing.T) {
	sog := 102.3
	lat, lon := 59.0, 10.4
	v := comboFull{MMSI: 1, Latitude: &lat, Longitude: &lon, SpeedOverGround: &sog}.toVessel(t0)
	if !v.SpeedUnknown {
		t.Fatal("AIS SOG 102.3 means 'not available' and must be unknown")
	}
}

func TestParseETA(t *testing.T) {
	// BarentsWatch passes the raw AIS ETA: MMDDhhmm in UTC, no year.
	cases := map[string]time.Time{
		"10091100":            time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC),
		"10101400":            time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC),
		"01051200":            time.Date(2027, 1, 5, 12, 0, 0, 0, time.UTC), // next year
		"09300800":            time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC), // recent past stays this year
		"00000000":            {},                                           // AIS "not available"
		"10092460":            {},
		"13011200":            {},
		"2026-10-10T06:00:00": time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC),
		"":                    {},
	}
	for in, want := range cases {
		if got := parseETA(in, t0); !got.Equal(want) {
			t.Errorf("parseETA(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseTime(t *testing.T) {
	cases := map[string]time.Time{
		"2026-10-10T06:00:00":       time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC),
		"2026-10-10T06:00:00Z":      time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC),
		"2026-10-10T08:00:00+02:00": time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC),
		"":                          {},
		"not a date":                {},
	}
	for in, want := range cases {
		if got := parseTime(in); !got.Equal(want) {
			t.Errorf("parseTime(%q) = %v, want %v", in, got, want)
		}
	}
}
