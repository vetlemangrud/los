// Package geo has the spherical-earth helpers Los needs: distances,
// bearings and the visibility arc.
package geo

import "math"

const earthRadiusNM = 3440.065

type Point struct{ Lat, Lon float64 }

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }

// norm maps any angle to [0, 360).
func norm(d float64) float64 {
	d = math.Mod(d, 360)
	if d < 0 {
		d += 360
	}
	return d
}

// Distance is the great-circle distance in nautical miles.
func Distance(a, b Point) float64 {
	dLat := rad(b.Lat - a.Lat)
	dLon := rad(b.Lon - a.Lon)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusNM * math.Asin(math.Sqrt(h))
}

// Bearing is the initial great-circle bearing from a to b, in [0, 360).
func Bearing(a, b Point) float64 {
	φ1, φ2 := rad(a.Lat), rad(b.Lat)
	dλ := rad(b.Lon - a.Lon)
	y := math.Sin(dλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(dλ)
	return norm(deg(math.Atan2(y, x)))
}

// Destination is the point distNM away from p along bearingDeg.
func Destination(p Point, bearingDeg, distNM float64) Point {
	δ := distNM / earthRadiusNM
	θ := rad(bearingDeg)
	φ1, λ1 := rad(p.Lat), rad(p.Lon)
	φ2 := math.Asin(math.Sin(φ1)*math.Cos(δ) + math.Cos(φ1)*math.Sin(δ)*math.Cos(θ))
	λ2 := λ1 + math.Atan2(math.Sin(θ)*math.Sin(δ)*math.Cos(φ1), math.Cos(δ)-math.Sin(φ1)*math.Sin(φ2))
	return Point{Lat: deg(φ2), Lon: deg(λ2)}
}

// Arc is a circular sector: everything within RadiusNM of Center whose
// bearing lies on the clockwise sweep from FromDeg to ToDeg.
type Arc struct {
	Center         Point
	FromDeg, ToDeg float64
	RadiusNM       float64
}

// Sweep is the clockwise angle from FromDeg to ToDeg, in (0, 360].
func (a Arc) Sweep() float64 {
	s := norm(a.ToDeg - a.FromDeg)
	if s == 0 {
		return 360
	}
	return s
}

func (a Arc) Contains(p Point) bool {
	d := Distance(a.Center, p)
	if d > a.RadiusNM {
		return false
	}
	if d < 1e-6 {
		return true
	}
	return norm(Bearing(a.Center, p)-a.FromDeg) <= a.Sweep()
}

func lonLat(p Point) [2]float64 { return [2]float64{p.Lon, p.Lat} }

// Ring returns the arc as a closed GeoJSON ring of [lon, lat] pairs in
// counter-clockwise order, starting and ending at the center.
func (a Arc) Ring(stepDeg float64) [][2]float64 {
	sweep := a.Sweep()
	cw := [][2]float64{lonLat(a.Center)}
	for off := 0.0; off < sweep; off += stepDeg {
		cw = append(cw, lonLat(Destination(a.Center, a.FromDeg+off, a.RadiusNM)))
	}
	cw = append(cw, lonLat(Destination(a.Center, a.FromDeg+sweep, a.RadiusNM)))
	cw = append(cw, lonLat(a.Center))
	// Bearings increase clockwise on the map; GeoJSON wants exterior rings
	// counter-clockwise, so reverse.
	ring := make([][2]float64, len(cw))
	for i, p := range cw {
		ring[len(cw)-1-i] = p
	}
	return ring
}

// BBox returns a closed counter-clockwise rectangle around the arc, grown
// by pad times its width and height (half on each side).
func (a Arc) BBox(pad float64) [][2]float64 {
	r := a.Ring(5)
	minLon, minLat, maxLon, maxLat := r[0][0], r[0][1], r[0][0], r[0][1]
	for _, p := range r {
		minLon, maxLon = math.Min(minLon, p[0]), math.Max(maxLon, p[0])
		minLat, maxLat = math.Min(minLat, p[1]), math.Max(maxLat, p[1])
	}
	dLon := (maxLon - minLon) * pad / 2
	dLat := (maxLat - minLat) * pad / 2
	minLon, maxLon = minLon-dLon, maxLon+dLon
	minLat, maxLat = minLat-dLat, maxLat+dLat
	return [][2]float64{
		{minLon, minLat}, {maxLon, minLat}, {maxLon, maxLat}, {minLon, maxLat}, {minLon, minLat},
	}
}
