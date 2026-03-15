package geocode

import (
	"fmt"
	"regexp"
	"strconv"
)

// ParseGoogleMapsURL extracts latitude and longitude from a Google Maps URL.
// Supports various URL formats:
//   - https://www.google.com/maps/place/.../@41.3531857,2.1448016,17z/...
//   - https://www.google.com/maps/@41.3531857,2.1448016,17z
//   - https://maps.google.com/?ll=41.3531857,2.1448016
//   - https://goo.gl/maps/... (not supported — user should use full URL)
//   - URLs with !3d41.3531857!4d2.1448016 in data params
func ParseGoogleMapsURL(url string) (float64, float64, error) {
	if url == "" {
		return 0, 0, fmt.Errorf("URL is empty")
	}

	// Pattern 1: /@lat,lon,zoom
	re1 := regexp.MustCompile(`@(-?\d+\.?\d*),(-?\d+\.?\d*),`)
	if m := re1.FindStringSubmatch(url); len(m) == 3 {
		return parseCoords(m[1], m[2])
	}

	// Pattern 2: !3dlat!4dlon (in data params)
	re2 := regexp.MustCompile(`!3d(-?\d+\.?\d*)!4d(-?\d+\.?\d*)`)
	if m := re2.FindStringSubmatch(url); len(m) == 3 {
		return parseCoords(m[1], m[2])
	}

	// Pattern 3: ?ll=lat,lon or &ll=lat,lon
	re3 := regexp.MustCompile(`[?&]ll=(-?\d+\.?\d*),(-?\d+\.?\d*)`)
	if m := re3.FindStringSubmatch(url); len(m) == 3 {
		return parseCoords(m[1], m[2])
	}

	// Pattern 4: ?q=lat,lon or &q=lat,lon
	re4 := regexp.MustCompile(`[?&]q=(-?\d+\.?\d*),(-?\d+\.?\d*)`)
	if m := re4.FindStringSubmatch(url); len(m) == 3 {
		return parseCoords(m[1], m[2])
	}

	return 0, 0, fmt.Errorf("could not find coordinates in URL — make sure it's a Google Maps link")
}

func parseCoords(latStr, lonStr string) (float64, float64, error) {
	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid latitude: %s", latStr)
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid longitude: %s", lonStr)
	}

	if lat < -90 || lat > 90 {
		return 0, 0, fmt.Errorf("latitude out of range: %f", lat)
	}
	if lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("longitude out of range: %f", lon)
	}

	return lat, lon, nil
}
