package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"los/internal/ais"
	"los/internal/vessel"
)

func TestNegotiate(t *testing.T) {
	cases := map[string]lang{
		"":                        en,
		"nb":                      nb,
		"nb-NO,nb;q=0.9,en;q=0.8": nb,
		"no":                      nb,
		"nn-NO":                   nb,
		"en-US,nb;q=0.5":          en,
		"da, nb;q=0.8":            nb, // unsupported first choice is skipped
		"fr":                      en,
		"en;q=0.2, nb;q=0.9":      nb, // quality wins over order
		"nb;q=0, en":              en,
		"garbage;;q=x":            en,
	}
	for header, want := range cases {
		if got := negotiate(header); got != want {
			t.Errorf("negotiate(%q) = %q, want %q", header, got, want)
		}
	}
}

func getLang(t *testing.T, h http.Handler, path, acceptLang string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept-Language", acceptLang)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListNorwegian(t *testing.T) {
	b := &fakeBoats{res: ais.Result{Vessels: sampleBoats(), FetchedAt: now}}
	body := getLang(t, newServer(b, nil), "/", "nb-NO,nb;q=0.9,en;q=0.8").Body.String()
	for _, want := range []string{
		`lang="nb"`, "oppdatert 14:00", "Passasjerskip · 9,8 kn", "Lasteskip · 12,1 kn",
		"2,0 nm", "Bergen, NO", "ETA 10. okt 08:00", "Mer info ↗",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	for _, unwanted := range []string{"More info", "Passenger", "updated"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("Norwegian page contains English %q", unwanted)
		}
	}
}

func TestListNorwegianStates(t *testing.T) {
	cases := []struct {
		name string
		b    *fakeBoats
		want []string
	}{
		{"empty", &fakeBoats{res: ais.Result{FetchedAt: now}}, []string{"Ingen båter i sikte akkurat nå."}},
		{"stale", &fakeBoats{res: ais.Result{Vessels: sampleBoats(), FetchedAt: now.Add(-10 * time.Minute), Stale: true}},
			[]string{"Data fra 13:50 (utdatert)"}},
		{"unavailable", &fakeBoats{err: errors.New("down")}, []string{"Fikk ikke kontakt med AIS-data, prøv igjen om litt."}},
		{"partial", &fakeBoats{res: ais.Result{Vessels: []vessel.Vessel{{MMSI: 257000003, SpeedUnknown: true, Pos: at(180, 3), MsgTime: now}}, FetchedAt: now}},
			[]string{"MMSI 257000003", "Ukjent destinasjon", "Annet"}},
	}
	for _, tc := range cases {
		body := getLang(t, newServer(tc.b, nil), "/", "nb").Body.String()
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s: body missing %q", tc.name, want)
			}
		}
	}
}
