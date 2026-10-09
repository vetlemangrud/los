package geo

import (
	"math"
	"testing"
)

var obs = Point{Lat: 59.25, Lon: 10.5}

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v ± %v", what, got, want, tol)
	}
}

func TestDistance(t *testing.T) {
	near(t, "1° lat", Distance(Point{58, 6}, Point{59, 6}), 60.04, 0.05)
	near(t, "same point", Distance(obs, obs), 0, 1e-9)
}

func TestBearing(t *testing.T) {
	near(t, "north", Bearing(Point{58, 6}, Point{59, 6}), 0, 0.01)
	near(t, "south", Bearing(Point{59, 6}, Point{58, 6}), 180, 0.01)
	if b := Bearing(Point{58, 6}, Point{58, 7}); b < 89 || b > 90 {
		t.Errorf("east bearing = %v, want in [89,90]", b)
	}
	if b := Bearing(Point{58, 7}, Point{58, 6}); b < 270 || b > 271 {
		t.Errorf("west bearing = %v, want in [270,271]", b)
	}
}

func TestDestinationRoundTrip(t *testing.T) {
	p := Destination(obs, 200, 7)
	near(t, "distance", Distance(obs, p), 7, 0.001)
	near(t, "bearing", Bearing(obs, p), 200, 0.01)
}

func TestArcContains(t *testing.T) {
	a := Arc{Center: obs, FromDeg: 120, ToDeg: 270, RadiusNM: 12}
	cases := []struct {
		bearing, dist float64
		want          bool
	}{
		{180, 5, true},
		{125, 5, true},
		{265, 11.9, true},
		{90, 5, false},
		{0, 5, false},
		{300, 5, false},
		{180, 12.5, false},
	}
	for _, tc := range cases {
		if got := a.Contains(Destination(obs, tc.bearing, tc.dist)); got != tc.want {
			t.Errorf("bearing %v dist %v: got %v want %v", tc.bearing, tc.dist, got, tc.want)
		}
	}
	if !a.Contains(obs) {
		t.Error("center should be inside")
	}
}

func TestArcContainsWrapsNorth(t *testing.T) {
	a := Arc{Center: obs, FromDeg: 300, ToDeg: 60, RadiusNM: 12}
	near(t, "sweep", a.Sweep(), 120, 1e-9)
	for _, b := range []float64{0, 330, 45, 305, 55} {
		if !a.Contains(Destination(obs, b, 5)) {
			t.Errorf("bearing %v should be inside", b)
		}
	}
	for _, b := range []float64{90, 180, 270, 65, 295} {
		if a.Contains(Destination(obs, b, 5)) {
			t.Errorf("bearing %v should be outside", b)
		}
	}
}

func TestArcFullCircle(t *testing.T) {
	a := Arc{Center: obs, FromDeg: 0, ToDeg: 0, RadiusNM: 12}
	near(t, "sweep", a.Sweep(), 360, 1e-9)
	for _, b := range []float64{0, 90, 180, 270} {
		if !a.Contains(Destination(obs, b, 5)) {
			t.Errorf("bearing %v should be inside full circle", b)
		}
	}
}

// signedArea > 0 means counter-clockwise in lon/lat space.
func signedArea(ring [][2]float64) float64 {
	s := 0.0
	for i := 0; i < len(ring)-1; i++ {
		s += ring[i][0]*ring[i+1][1] - ring[i+1][0]*ring[i][1]
	}
	return s / 2
}

func TestArcRing(t *testing.T) {
	a := Arc{Center: obs, FromDeg: 120, ToDeg: 270, RadiusNM: 12}
	ring := a.Ring(5)
	center := [2]float64{obs.Lon, obs.Lat}
	if ring[0] != center || ring[len(ring)-1] != center {
		t.Fatalf("ring must start and end at center, got %v … %v", ring[0], ring[len(ring)-1])
	}
	if len(ring) != 33 { // 30 steps (0..145) + end edge point + 2 centers
		t.Errorf("len = %d, want 33", len(ring))
	}
	for _, p := range ring[1 : len(ring)-1] {
		near(t, "edge distance", Distance(obs, Point{Lat: p[1], Lon: p[0]}), 12, 0.01)
	}
	if signedArea(ring) <= 0 {
		t.Error("ring must be counter-clockwise")
	}
}

func TestArcRingWrapsNorth(t *testing.T) {
	a := Arc{Center: obs, FromDeg: 300, ToDeg: 60, RadiusNM: 12}
	ring := a.Ring(5)
	for _, p := range ring[1 : len(ring)-1] {
		b := Bearing(obs, Point{Lat: p[1], Lon: p[0]})
		if b > 60.01 && b < 299.99 {
			t.Errorf("edge point at bearing %v is outside the 300→60 sweep", b)
		}
	}
	if signedArea(ring) <= 0 {
		t.Error("ring must be counter-clockwise")
	}
}

func TestArcBBox(t *testing.T) {
	a := Arc{Center: obs, FromDeg: 120, ToDeg: 270, RadiusNM: 12}
	box := a.BBox(0.5)
	if len(box) != 5 || box[0] != box[4] {
		t.Fatalf("bbox must be a closed 4-corner ring, got %v", box)
	}
	if signedArea(box) <= 0 {
		t.Error("bbox must be counter-clockwise")
	}
	minLon, minLat, maxLon, maxLat := box[0][0], box[0][1], box[2][0], box[2][1]
	for _, p := range a.Ring(5) {
		if p[0] <= minLon || p[0] >= maxLon || p[1] <= minLat || p[1] >= maxLat {
			t.Errorf("ring point %v not strictly inside bbox", p)
		}
	}
}
