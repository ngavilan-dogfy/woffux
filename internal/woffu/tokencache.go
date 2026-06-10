package woffu

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// tokenCacheEntry is the on-disk format for a cached bearer token.
type tokenCacheEntry struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// tokenCachePath returns a per-account cache file path.
func tokenCachePath(email, companyURL string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(email + "|" + companyURL))
	return filepath.Join(dir, "woffux", "token-"+hex.EncodeToString(sum[:8])+".json"), nil
}

// jwtExpiry extracts the exp claim from a JWT without verifying it.
func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

// loadCachedToken returns a still-valid cached token, or "".
func loadCachedToken(email, companyURL string) string {
	path, err := tokenCachePath(email, companyURL)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var entry tokenCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return ""
	}
	// Keep a safety margin so a token doesn't expire mid-command.
	if entry.Token == "" || time.Now().Add(10*time.Minute).After(entry.ExpiresAt) {
		return ""
	}
	return entry.Token
}

// saveCachedToken persists a token for reuse across invocations.
func saveCachedToken(email, companyURL, token string) {
	exp, ok := jwtExpiry(token)
	if !ok {
		// Unknown lifetime: assume a conservative one hour.
		exp = time.Now().Add(1 * time.Hour)
	}
	path, err := tokenCachePath(email, companyURL)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(tokenCacheEntry{Token: token, ExpiresAt: exp})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// ClearCachedToken removes the cached token for an account (e.g. on auth errors).
func ClearCachedToken(email, companyURL string) {
	path, err := tokenCachePath(email, companyURL)
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

// AuthenticateCached returns a bearer token, reusing a cached one when it is
// still valid and accepted by the API. Falls back to the full login flow.
func AuthenticateCached(client *Client, companyClient *Client, email, password string) (string, error) {
	companyURL := companyClient.baseURL
	if token := loadCachedToken(email, companyURL); token != "" {
		// Cheap validation round-trip; also catches server-side revocation.
		var probe struct {
			UserID int `json:"UserId"`
		}
		err := companyClient.doJSON("GET", "/api/users", nil, map[string]string{
			"Authorization": "Bearer " + token,
		}, &probe)
		if err == nil && probe.UserID != 0 {
			return token, nil
		}
		ClearCachedToken(email, companyURL)
	}

	token, err := Authenticate(client, companyClient, email, password)
	if err != nil {
		return "", err
	}
	saveCachedToken(email, companyURL, token)
	return token, nil
}
