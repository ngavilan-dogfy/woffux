package geocode

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Result struct {
	Lat         float64
	Lon         float64
	DisplayName string
}

type nominatimResult struct {
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
}

// Search returns up to `limit` geocoding results for the given query.
func Search(query string, limit int) ([]Result, error) {
	u := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=%d&addressdetails=1",
		url.QueryEscape(query), limit,
	)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "woffuk-cli/1.0 (github.com/ngavilan-dogfy/woffuk-cli)")
	req.Header.Set("Accept-Language", "es,en")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geocode request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read geocode response: %w", err)
	}

	var raw []nominatimResult
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse geocode response: %w", err)
	}

	results := make([]Result, 0, len(raw))
	for _, r := range raw {
		lat, err := strconv.ParseFloat(r.Lat, 64)
		if err != nil {
			continue
		}
		lon, err := strconv.ParseFloat(r.Lon, 64)
		if err != nil {
			continue
		}
		results = append(results, Result{
			Lat:         lat,
			Lon:         lon,
			DisplayName: shortenDisplayName(r.DisplayName),
		})
	}

	return results, nil
}

// shortenDisplayName trims overly long Nominatim display names.
func shortenDisplayName(name string) string {
	parts := strings.Split(name, ", ")
	if len(parts) > 5 {
		// Keep first 3 + last 2 (city, country)
		short := append(parts[:3], parts[len(parts)-2:]...)
		return strings.Join(short, ", ")
	}
	return name
}
