// Package vessel holds the boat model and the rules for presenting AIS
// fields to people.
package vessel

import (
	"fmt"
	"strings"
	"time"

	"los/internal/geo"
)

type Vessel struct {
	MMSI         int
	IMO          int
	Name         string // raw AIS name; may carry '@' padding or be empty
	CallSign     string
	ShipType     int       // AIS type code, 0 if unknown
	Destination  string    // raw AIS destination text
	ETA          time.Time // zero if unknown
	Pos          geo.Point
	SpeedKn      float64
	SpeedUnknown bool // AIS reported no speed (null or the 102.3 kn sentinel)
	CourseDeg    float64
	LengthM      int
	MsgTime      time.Time
	DistanceNM   float64 // set by the web layer
}

// CleanName strips AIS '@' padding and whitespace.
func (v Vessel) CleanName() string {
	return strings.Trim(v.Name, "@ \t")
}

func (v Vessel) DisplayName() string {
	if n := v.CleanName(); n != "" {
		return n
	}
	return fmt.Sprintf("MMSI %d", v.MMSI)
}

func (v Vessel) PhotosURL() string {
	id := v.MMSI
	if v.IMO > 0 {
		id = v.IMO
	}
	return fmt.Sprintf("https://www.vesselfinder.com/vessels/details/%d", id)
}

// ShowETA reports whether an AIS ETA is worth showing. Ships often leave
// stale or placeholder ETAs, so only near-future ones pass.
func ShowETA(eta, now time.Time) bool {
	return !eta.IsZero() && eta.After(now) && eta.Before(now.Add(60*24*time.Hour))
}
