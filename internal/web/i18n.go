package web

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// lang is a supported page language, picked from the browser's
// Accept-Language header. English is the source language: catalog keys are
// the English strings, so a missing translation falls back to English.
type lang string

const (
	en lang = "en"
	nb lang = "nb"
)

var catalog = map[lang]map[string]string{
	nb: {
		"updated":                     "oppdatert",
		"Data from %s (stale)":        "Data fra %s (utdatert)",
		"No boats in view right now.": "Ingen båter i sikte akkurat nå.",
		"Couldn't reach AIS data, try again shortly.": "Fikk ikke kontakt med AIS-data, prøv igjen om litt.",
		"Destination unknown":                         "Ukjent destinasjon",
		"More info ↗":                                 "Mer info ↗",
		// Ship type labels from vessel.TypeLabel.
		"Fishing":    "Fiskefartøy",
		"Towing":     "Slep",
		"Sailing":    "Seilbåt",
		"Pleasure":   "Fritidsbåt",
		"High-speed": "Hurtigbåt",
		"Pilot":      "Losbåt",
		"SAR":        "Redningsfartøy",
		"Tug":        "Slepebåt",
		"Passenger":  "Passasjerskip",
		"Cargo":      "Lasteskip",
		"Tanker":     "Tankskip",
		"Other":      "Annet",
	},
}

var nbMonths = [...]string{"jan", "feb", "mar", "apr", "mai", "jun", "jul", "aug", "sep", "okt", "nov", "des"}

// T translates an English UI string.
func (l lang) T(s string) string {
	if t, ok := catalog[l][s]; ok {
		return t
	}
	return s
}

// num formats a one-decimal number with the language's decimal separator.
func (l lang) num(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	if l == nb {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// date formats a day and time: "10 Oct 08:00" or "10. okt 08:00".
func (l lang) date(t time.Time) string {
	if l == nb {
		return fmt.Sprintf("%d. %s %s", t.Day(), nbMonths[t.Month()-1], t.Format("15:04"))
	}
	return t.Format("2 Jan 15:04")
}

// negotiate picks the best supported language from an Accept-Language
// header, honouring q-values. All Norwegian tags (nb, no, nn) map to nb.
func negotiate(header string) lang {
	type pref struct {
		tag string
		q   float64
	}
	var prefs []pref
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		tag := strings.ToLower(strings.TrimSpace(fields[0]))
		q := 1.0
		for _, f := range fields[1:] {
			if v, ok := strings.CutPrefix(strings.TrimSpace(f), "q="); ok {
				if p, err := strconv.ParseFloat(v, 64); err == nil {
					q = p
				} else {
					q = 0
				}
			}
		}
		if tag != "" && q > 0 {
			prefs = append(prefs, pref{tag, q})
		}
	}
	sort.SliceStable(prefs, func(i, j int) bool { return prefs[i].q > prefs[j].q })
	for _, p := range prefs {
		base, _, _ := strings.Cut(p.tag, "-")
		switch base {
		case "nb", "no", "nn":
			return nb
		case "en":
			return en
		}
	}
	return en
}
