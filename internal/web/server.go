// Package web serves the Los pages.
package web

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"time"

	"los/internal/ais"
	"los/internal/geo"
	"los/internal/images"
	"los/internal/vessel"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

type Boats interface {
	Get(ctx context.Context, key string, ring [][2]float64, since time.Time) (ais.Result, error)
}

type Images interface {
	Resolve(ctx context.Context, vs []vessel.Vessel) map[int]images.Image
}

type Server struct {
	Arc    geo.Arc
	MaxAge time.Duration
	Boats  Boats
	Images Images
	Now    func() time.Time
	Loc    *time.Location
	Logf   func(string, ...any)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleList)
	mux.HandleFunc("GET /debug", s.handleDebug)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	return mux
}

type boatRow struct {
	Name, Type, Icon, Distance, Speed string
	Destination, ETA, PhotosURL       string
	ThumbURL, ImagePage               string
}

type listPage struct {
	Boats   []boatRow
	Updated string
	Stale   bool
	Error   string
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	now := s.Now()
	cutoff := now.Add(-s.MaxAge)
	res, err := s.Boats.Get(r.Context(), "arc", s.Arc.Ring(5), cutoff)
	if err != nil {
		s.logf("list: %v", err)
		s.render(w, http.StatusServiceUnavailable, "list.html",
			listPage{Error: "Couldn't reach AIS data, try again shortly."})
		return
	}
	boats := visible(res.Vessels, s.Arc, freshCutoff(res, cutoff, s.MaxAge))
	var imgs map[int]images.Image
	if s.Images != nil {
		imgs = s.Images.Resolve(r.Context(), boats)
	}
	page := listPage{Updated: s.clock(res.FetchedAt), Stale: res.Stale}
	for _, b := range boats {
		page.Boats = append(page.Boats, s.row(b, imgs[b.MMSI], now))
	}
	s.render(w, http.StatusOK, "list.html", page)
}

// freshCutoff is the oldest report time still shown. Stale data is judged
// against when it was fetched, not against now: otherwise a long outage
// turns into a false "no boats in view".
func freshCutoff(res ais.Result, cutoff time.Time, maxAge time.Duration) time.Time {
	if res.Stale {
		return res.FetchedAt.Add(-maxAge)
	}
	return cutoff
}

// visible keeps boats inside the arc that reported after cutoff, nearest
// first. The API polygon is only an approximation, so this is the real filter.
func visible(vs []vessel.Vessel, arc geo.Arc, cutoff time.Time) []vessel.Vessel {
	var out []vessel.Vessel
	for _, v := range vs {
		if !arc.Contains(v.Pos) || v.MsgTime.Before(cutoff) {
			continue
		}
		v.DistanceNM = geo.Distance(arc.Center, v.Pos)
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DistanceNM < out[j].DistanceNM })
	return out
}

func (s *Server) row(v vessel.Vessel, img images.Image, now time.Time) boatRow {
	r := boatRow{
		Name:        v.DisplayName(),
		Type:        vessel.TypeLabel(v.ShipType),
		Icon:        "/static/icons/" + vessel.TypeIcon(v.ShipType) + ".svg",
		Distance:    fmt.Sprintf("%.1f nm", v.DistanceNM),
		Destination: vessel.ParseDestination(v.Destination),
		PhotosURL:   v.PhotosURL(),
		ThumbURL:    img.ThumbURL,
		ImagePage:   img.PageURL,
	}
	if !v.SpeedUnknown {
		r.Speed = fmt.Sprintf("%.1f kn", v.SpeedKn)
	}
	if vessel.ShowETA(v.ETA, now) {
		r.ETA = v.ETA.In(s.Loc).Format("2 Jan 15:04")
	}
	return r
}

func (s *Server) clock(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(s.Loc).Format("15:04")
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.logf("render %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

type debugBoat struct {
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Name        string  `json:"name"`
	Inside      bool    `json:"inside"`
	MMSI        int     `json:"mmsi"`
	IMO         int     `json:"imo"`
	ShipType    int     `json:"shipType"`
	Type        string  `json:"type"`
	SpeedKn     float64 `json:"speedKn"`
	CourseDeg   float64 `json:"courseDeg"`
	Destination string  `json:"destination"`
	ETA         string  `json:"eta"`
	MsgTime     string  `json:"msgTime"`
	DistanceNM  float64 `json:"distanceNm"`
	BearingDeg  float64 `json:"bearingDeg"`
}

type debugData struct {
	Observer [2]float64   `json:"observer"` // [lat, lon] for Leaflet
	Arc      [][2]float64 `json:"arc"`      // [lon, lat]
	BBox     [][2]float64 `json:"bbox"`     // [lon, lat]
	Boats    []debugBoat  `json:"boats"`
}

type kv struct{ Key, Value string }

type debugPage struct {
	Data    debugData
	Config  []kv
	Updated string
	Stale   bool
	Error   string
}

// handleDebug draws the arc and every boat in a wider box around it, so the
// arc can be tuned against real traffic. It renders even when AIS is down.
func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
	now := s.Now()
	cutoff := now.Add(-s.MaxAge)
	bbox := s.Arc.BBox(0.5)
	page := debugPage{Data: debugData{
		Observer: [2]float64{s.Arc.Center.Lat, s.Arc.Center.Lon},
		Arc:      s.Arc.Ring(5),
		BBox:     bbox,
		Boats:    []debugBoat{},
	}}
	inside := 0
	res, err := s.Boats.Get(r.Context(), "debug", bbox, cutoff)
	if err != nil {
		s.logf("debug: %v", err)
		page.Error = "Couldn't reach AIS data: " + err.Error()
	} else {
		page.Updated = s.clock(res.FetchedAt)
		page.Stale = res.Stale
		for _, v := range res.Vessels {
			in := s.Arc.Contains(v.Pos) && !v.MsgTime.Before(freshCutoff(res, cutoff, s.MaxAge))
			if in {
				inside++
			}
			eta := ""
			if !v.ETA.IsZero() {
				eta = v.ETA.In(s.Loc).Format(time.RFC3339)
			}
			page.Data.Boats = append(page.Data.Boats, debugBoat{
				Lat: v.Pos.Lat, Lon: v.Pos.Lon, Name: v.DisplayName(), Inside: in,
				MMSI: v.MMSI, IMO: v.IMO, ShipType: v.ShipType, Type: vessel.TypeLabel(v.ShipType),
				SpeedKn: v.SpeedKn, CourseDeg: v.CourseDeg, Destination: v.Destination, ETA: eta,
				MsgTime:    v.MsgTime.In(s.Loc).Format(time.RFC3339),
				DistanceNM: geo.Distance(s.Arc.Center, v.Pos),
				BearingDeg: geo.Bearing(s.Arc.Center, v.Pos),
			})
		}
	}
	page.Config = []kv{
		{"Observer", fmt.Sprintf("%.5f, %.5f", s.Arc.Center.Lat, s.Arc.Center.Lon)},
		{"Arc", fmt.Sprintf("%g° → %g° (clockwise, %g° sweep)", s.Arc.FromDeg, s.Arc.ToDeg, s.Arc.Sweep())},
		{"Radius", fmt.Sprintf("%.1f nm", s.Arc.RadiusNM)},
		{"Max age", s.MaxAge.String()},
		{"Boats in arc", fmt.Sprintf("%d of %d", inside, len(page.Data.Boats))},
	}
	s.render(w, http.StatusOK, "debug.html", page)
}
