package web

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"los/internal/ais"
	"los/internal/vessel"
)

func TestDebugRendersMapData(t *testing.T) {
	b := &fakeBoats{res: ais.Result{Vessels: sampleBoats(), FetchedAt: now}}
	rec := get(t, newServer(b, nil), "/debug")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"leaflet/1.9.4/leaflet.min.js", `"name":"NEAR"`, `"inside":true`, `"inside":false`,
		`"arc":[[`, `"bbox":[[`, "120° → 270°", "12.0 nm", "2 of 4",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if len(b.keys) != 1 || b.keys[0] != "debug" {
		t.Errorf("cache keys = %v", b.keys)
	}
}

func TestDebugWorksWhenAISDown(t *testing.T) {
	rec := get(t, newServer(&fakeBoats{err: errors.New("down")}, nil), "/debug")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"arc":[[`) || !strings.Contains(body, "reach AIS data") {
		t.Fatalf("status %d body %s", rec.Code, body)
	}
}

func TestDebugEscapesAISTextInScript(t *testing.T) {
	vs := []vessel.Vessel{{MMSI: 1, Name: "</script><script>alert(1)</script>", Pos: at(180, 2), MsgTime: now}}
	body := get(t, newServer(&fakeBoats{res: ais.Result{Vessels: vs, FetchedAt: now}}, nil), "/debug").Body.String()
	if strings.Contains(body, "<script>alert") {
		t.Fatal("AIS name broke out of the script block")
	}
}
