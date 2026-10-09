package vessel

import (
	"testing"
	"time"
)

func TestTypeLabel(t *testing.T) {
	cases := map[int]string{
		30: "Fishing", 31: "Towing", 32: "Towing", 36: "Sailing", 37: "Pleasure",
		40: "High-speed", 49: "High-speed", 50: "Pilot", 51: "SAR", 52: "Tug",
		60: "Passenger", 69: "Passenger", 70: "Cargo", 79: "Cargo",
		80: "Tanker", 89: "Tanker", 0: "Other", 99: "Other", 33: "Other",
	}
	for code, want := range cases {
		if got := TypeLabel(code); got != want {
			t.Errorf("TypeLabel(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestTypeIcon(t *testing.T) {
	cases := map[int]string{30: "fishing", 36: "sail", 37: "sail", 70: "ship", 60: "ship", 0: "ship"}
	for code, want := range cases {
		if got := TypeIcon(code); got != want {
			t.Errorf("TypeIcon(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestParseDestination(t *testing.T) {
	cases := map[string]string{
		"NO SVG":          "Stavanger, NO",
		"NOSVG":           "Stavanger, NO",
		"no-svg":          "Stavanger, NO",
		"NLRTM":           "Rotterdam, NL",
		"ROTTERDAM":       "Rotterdam",
		"  bergen@@@ ":    "Bergen",
		"NOSVG>DEHAM":     "Hamburg, DE",
		"NO SVG > NL RTM": "Rotterdam, NL",
		"NO XYZ":          "NO XYZ",
		"SKIEN":           "Skien",
		"ST. PETERSBURG":  "St. Petersburg",
		"FOR ORDERS":      "For Orders",
		"":                "Destination unknown",
		"@@@@@@@":         "Destination unknown",
		"NOSVG>":          "Destination unknown",
	}
	for raw, want := range cases {
		if got := ParseDestination(raw); got != want {
			t.Errorf("ParseDestination(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestShowETA(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		eta  time.Time
		want bool
	}{
		{time.Time{}, false},
		{now.Add(-time.Hour), false},
		{now.Add(time.Hour), true},
		{now.Add(59 * 24 * time.Hour), true},
		{now.Add(61 * 24 * time.Hour), false},
	}
	for _, tc := range cases {
		if got := ShowETA(tc.eta, now); got != tc.want {
			t.Errorf("ShowETA(%v) = %v, want %v", tc.eta, got, tc.want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if got := (Vessel{MMSI: 257000000}).DisplayName(); got != "MMSI 257000000" {
		t.Errorf("empty name: got %q", got)
	}
	if got := (Vessel{Name: " KRONPRINS HAAKON@@"}).DisplayName(); got != "KRONPRINS HAAKON" {
		t.Errorf("padded name: got %q", got)
	}
	if got := (Vessel{MMSI: 1, Name: "@@@"}).DisplayName(); got != "MMSI 1" {
		t.Errorf("all-padding name: got %q", got)
	}
}

func TestPhotosURL(t *testing.T) {
	if got := (Vessel{MMSI: 257000000, IMO: 9123456}).PhotosURL(); got != "https://www.vesselfinder.com/vessels/details/9123456" {
		t.Errorf("with IMO: %q", got)
	}
	if got := (Vessel{MMSI: 257000000}).PhotosURL(); got != "https://www.vesselfinder.com/vessels/details/257000000" {
		t.Errorf("without IMO: %q", got)
	}
}
