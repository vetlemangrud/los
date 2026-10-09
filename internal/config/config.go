// Package config reads Los settings from environment variables.
package config

import (
	"fmt"
	"strconv"
	"time"
)

type Config struct {
	ClientID     string
	ClientSecret string
	Lat, Lon     float64
	ArcFrom      float64 // degrees true; the arc sweeps clockwise to ArcTo
	ArcTo        float64
	RadiusNM     float64
	MaxAge       time.Duration
	DBPath       string
	Addr         string
}

func Load(getenv func(string) string) (Config, error) {
	c := Config{
		ClientID:     getenv("BW_CLIENT_ID"),
		ClientSecret: getenv("BW_CLIENT_SECRET"),
		DBPath:       orDefault(getenv("LOS_DB_PATH"), "/data/los.db"),
		Addr:         orDefault(getenv("LOS_ADDR"), ":8080"),
		MaxAge:       15 * time.Minute,
	}
	if c.ClientID == "" {
		return Config{}, fmt.Errorf("BW_CLIENT_ID is required")
	}
	if c.ClientSecret == "" {
		return Config{}, fmt.Errorf("BW_CLIENT_SECRET is required")
	}

	// The observer location has no default: it is personal, so it lives
	// only in the deployment's environment, never in the repo.
	floats := []struct {
		name          string
		required      bool
		def, min, max float64
		dst           *float64
	}{
		{"LOS_LAT", true, 0, -90, 90, &c.Lat},
		{"LOS_LON", true, 0, -180, 180, &c.Lon},
		{"LOS_ARC_FROM", false, 120, 0, 360, &c.ArcFrom},
		{"LOS_ARC_TO", false, 270, 0, 360, &c.ArcTo},
		{"LOS_RADIUS_NM", false, 12, 0.1, 100, &c.RadiusNM},
	}
	for _, f := range floats {
		v := f.def
		s := getenv(f.name)
		if s == "" && f.required {
			return Config{}, fmt.Errorf("%s is required", f.name)
		}
		if s != "" {
			p, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return Config{}, fmt.Errorf("%s: not a number: %q", f.name, s)
			}
			v = p
		}
		if v < f.min || v > f.max {
			return Config{}, fmt.Errorf("%s: %v out of range [%v, %v]", f.name, v, f.min, f.max)
		}
		*f.dst = v
	}

	if s := getenv("LOS_MAX_AGE"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("LOS_MAX_AGE: invalid duration %q", s)
		}
		c.MaxAge = d
	}
	return c, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
