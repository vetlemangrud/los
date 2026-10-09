package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func creds() map[string]string {
	return map[string]string{
		"BW_CLIENT_ID": "id", "BW_CLIENT_SECRET": "secret",
		"LOS_LAT": "59.25", "LOS_LON": "10.5",
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(creds()))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		ClientID: "id", ClientSecret: "secret",
		Lat: 59.25, Lon: 10.5,
		ArcFrom: 120, ArcTo: 270, RadiusNM: 12,
		MaxAge: 15 * time.Minute,
		DBPath: "/data/los.db", Addr: ":8080",
	}
	if c != want {
		t.Fatalf("got %+v\nwant %+v", c, want)
	}
}

func TestLoadRequiresCredentialsAndLocation(t *testing.T) {
	for _, missing := range []string{"BW_CLIENT_ID", "BW_CLIENT_SECRET", "LOS_LAT", "LOS_LON"} {
		m := creds()
		delete(m, missing)
		_, err := Load(env(m))
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("missing %s: got err %v", missing, err)
		}
	}
}

func TestLoadOverrides(t *testing.T) {
	m := creds()
	m["LOS_LAT"] = "59.5"
	m["LOS_LON"] = "10.25"
	m["LOS_ARC_FROM"] = "300"
	m["LOS_ARC_TO"] = "60"
	m["LOS_RADIUS_NM"] = "8.5"
	m["LOS_MAX_AGE"] = "5m"
	m["LOS_DB_PATH"] = "/tmp/x.db"
	m["LOS_ADDR"] = ":9000"
	c, err := Load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.Lat != 59.5 || c.Lon != 10.25 || c.ArcFrom != 300 || c.ArcTo != 60 ||
		c.RadiusNM != 8.5 || c.MaxAge != 5*time.Minute || c.DBPath != "/tmp/x.db" || c.Addr != ":9000" {
		t.Fatalf("overrides not applied: %+v", c)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	cases := []struct{ key, val string }{
		{"LOS_RADIUS_NM", "abc"},
		{"LOS_RADIUS_NM", "0"},
		{"LOS_ARC_FROM", "400"},
		{"LOS_ARC_TO", "-1"},
		{"LOS_LAT", "95"},
		{"LOS_LON", "181"},
		{"LOS_MAX_AGE", "soon"},
		{"LOS_MAX_AGE", "0s"},
	}
	for _, tc := range cases {
		m := creds()
		m[tc.key] = tc.val
		_, err := Load(env(m))
		if err == nil || !strings.Contains(err.Error(), tc.key) {
			t.Errorf("%s=%s: got err %v, want error naming %s", tc.key, tc.val, err, tc.key)
		}
	}
}
