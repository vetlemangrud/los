// Package ais fetches current vessel data from the BarentsWatch Live AIS API.
package ais

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"los/internal/geo"
	"los/internal/vessel"
)

const (
	DefaultTokenURL = "https://id.barentswatch.no/connect/token"
	DefaultAPIURL   = "https://live.ais.barentswatch.no/v1/latest/combined"
)

var ErrUnauthorized = errors.New("ais: unauthorized")

type Client struct {
	HTTP         *http.Client
	TokenURL     string
	APIURL       string
	ClientID     string
	ClientSecret string
	Now          func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
}

func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		HTTP:         &http.Client{Timeout: 5 * time.Second},
		TokenURL:     DefaultTokenURL,
		APIURL:       DefaultAPIURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Now:          time.Now,
	}
}

type polygon struct {
	Type        string         `json:"type"`
	Coordinates [][][2]float64 `json:"coordinates"`
}

type filter struct {
	Geometry    polygon `json:"geometry"`
	Since       string  `json:"since"`
	ModelType   string  `json:"modelType"`
	ModelFormat string  `json:"modelFormat"`
}

// LatestInArea returns the latest known state of every vessel inside the
// [lon, lat] ring that has reported since the given time.
func (c *Client) LatestInArea(ctx context.Context, ring [][2]float64, since time.Time) ([]vessel.Vessel, error) {
	body, err := json.Marshal(filter{
		Geometry:    polygon{Type: "Polygon", Coordinates: [][][2]float64{ring}},
		Since:       since.UTC().Format(time.RFC3339),
		ModelType:   "Full",
		ModelFormat: "Json",
	})
	if err != nil {
		return nil, err
	}
	vs, err := c.fetch(ctx, body)
	if errors.Is(err, ErrUnauthorized) {
		c.dropToken()
		vs, err = c.fetch(ctx, body)
	}
	return vs, err
}

func (c *Client) fetch(ctx context.Context, body []byte) ([]vessel.Vessel, error) {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.APIURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ais: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ais: status %d", resp.StatusCode)
	}
	var raw []comboFull
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("ais: decode: %w", err)
	}
	out := make([]vessel.Vessel, 0, len(raw))
	for _, r := range raw {
		if r.Latitude == nil || r.Longitude == nil {
			continue
		}
		out = append(out, r.toVessel(c.Now()))
	}
	return out, nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.Now().Before(c.expiry) {
		return c.token, nil
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"scope":         {"ais"},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("ais token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ais token: status %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("ais token: decode: %w", err)
	}
	if body.AccessToken == "" {
		return "", errors.New("ais token: empty access_token")
	}
	c.token = body.AccessToken
	c.expiry = c.Now().Add(time.Duration(body.ExpiresIn)*time.Second - 60*time.Second)
	return c.token, nil
}

func (c *Client) dropToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// comboFull mirrors the AisComboFull fields Los uses. Most are nullable.
type comboFull struct {
	MMSI             int      `json:"mmsi"`
	MsgTime          string   `json:"msgtime"`
	Latitude         *float64 `json:"latitude"`
	Longitude        *float64 `json:"longitude"`
	SpeedOverGround  *float64 `json:"speedOverGround"`
	CourseOverGround *float64 `json:"courseOverGround"`
	IMONumber        *int     `json:"imoNumber"`
	CallSign         *string  `json:"callSign"`
	Name             *string  `json:"name"`
	Destination      *string  `json:"destination"`
	ETA              *string  `json:"eta"`
	ShipType         *int     `json:"shipType"`
	ShipLength       *int     `json:"shipLength"`
}

func (r comboFull) toVessel(now time.Time) vessel.Vessel {
	return vessel.Vessel{
		MMSI:         r.MMSI,
		IMO:          deref(r.IMONumber),
		Name:         deref(r.Name),
		CallSign:     deref(r.CallSign),
		ShipType:     deref(r.ShipType),
		Destination:  deref(r.Destination),
		ETA:          parseETA(deref(r.ETA), now),
		Pos:          geo.Point{Lat: *r.Latitude, Lon: *r.Longitude},
		SpeedKn:      deref(r.SpeedOverGround),
		SpeedUnknown: r.SpeedOverGround == nil || *r.SpeedOverGround >= 102.2,
		CourseDeg:    deref(r.CourseOverGround),
		LengthM:      deref(r.ShipLength),
		MsgTime:      parseTime(r.MsgTime),
	}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// parseETA reads the ETA as BarentsWatch passes it from AIS: "MMDDhhmm" in
// UTC with no year. The year is the one that puts the ETA nearest to now
// (an ETA more than half a year back is taken as next year's). AIS "not
// available" values (month/day 0, hour 24, minute 60) and anything else
// unexpected yield the zero time. ISO timestamps are accepted too.
func parseETA(s string, now time.Time) time.Time {
	if len(s) != 8 {
		return parseTime(s)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return time.Time{}
	}
	month, day, hour, minute := n/1000000, n/10000%100, n/100%100, n%100
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 {
		return time.Time{}
	}
	now = now.UTC()
	eta := time.Date(now.Year(), time.Month(month), day, hour, minute, 0, 0, time.UTC)
	if eta.Day() != day { // e.g. 31 June rolled into July
		return time.Time{}
	}
	if now.Sub(eta) > 182*24*time.Hour {
		eta = eta.AddDate(1, 0, 0)
	}
	return eta
}

// parseTime accepts RFC 3339 and zone-less timestamps (treated as UTC, as
// AIS ETAs are). Anything else yields the zero time.
func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
