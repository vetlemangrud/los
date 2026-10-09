package images

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

const DefaultCommonsURL = "https://commons.wikimedia.org/w/api.php"

// Commons searches Wikimedia Commons files. Wikimedia's API policy asks
// for a descriptive User-Agent.
type Commons struct {
	HTTP      *http.Client
	APIURL    string
	UserAgent string
}

func NewCommons() *Commons {
	return &Commons{
		HTTP:      &http.Client{Timeout: 3 * time.Second},
		APIURL:    DefaultCommonsURL,
		UserAgent: "Los/1.0 (self-hosted boat-watching page; Go net/http)",
	}
}

// Find returns the best-ranked bitmap file for query whose title contains
// match (ignoring case and punctuation), or a zero Image. Commons full-text
// search is loose, so the title check keeps other ships' photos out.
func (c *Commons) Find(ctx context.Context, query, match string) (Image, error) {
	q := url.Values{
		"action":        {"query"},
		"format":        {"json"},
		"formatversion": {"2"},
		"generator":     {"search"},
		"gsrsearch":     {query + " filetype:bitmap"},
		"gsrnamespace":  {"6"},
		"gsrlimit":      {"10"},
		"prop":          {"imageinfo"},
		"iiprop":        {"url"},
		"iiurlwidth":    {"960"}, // full-width card image, sharp on 3x phone screens
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL+"?"+q.Encode(), nil)
	if err != nil {
		return Image{}, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Image{}, fmt.Errorf("commons: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("commons: status %d", resp.StatusCode)
	}
	var body struct {
		Query struct {
			Pages []struct {
				Index     int    `json:"index"`
				Title     string `json:"title"`
				ImageInfo []struct {
					ThumbURL       string `json:"thumburl"`
					DescriptionURL string `json:"descriptionurl"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Image{}, fmt.Errorf("commons: decode: %w", err)
	}
	pages := body.Query.Pages
	sort.Slice(pages, func(i, j int) bool { return pages[i].Index < pages[j].Index })
	want := normalize(match)
	for _, p := range pages {
		if !strings.Contains(normalize(p.Title), want) {
			continue
		}
		for _, ii := range p.ImageInfo {
			if ii.ThumbURL != "" {
				return Image{ThumbURL: ii.ThumbURL, PageURL: ii.DescriptionURL}, nil
			}
		}
	}
	return Image{}, nil
}

// normalize lowercases s and drops everything but letters and digits.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
