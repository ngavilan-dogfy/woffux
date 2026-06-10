package woffu

import (
	"fmt"
	"time"
)

// SignAction represents the expected sign direction (in or out).
type SignAction string

const (
	SignActionIn  SignAction = "in"
	SignActionOut SignAction = "out"
)

// IsSignedIn returns true if the user is currently clocked in (last slot has In but no Out).
func IsSignedIn(slots []SignSlot) bool {
	if len(slots) == 0 {
		return false
	}
	last := slots[len(slots)-1]
	return last.In != "" && last.Out == ""
}

// ShouldSkipSign returns true if signing should be skipped because
// the user is already in the expected state.
func ShouldSkipSign(slots []SignSlot, expected SignAction) bool {
	signedIn := IsSignedIn(slots)
	switch expected {
	case SignActionIn:
		return signedIn // already signed in → skip the IN
	case SignActionOut:
		return !signedIn // already signed out → skip the OUT
	}
	return false
}

// DoSign clocks in/out on Woffu with the given coordinates.
func DoSign(companyClient *Client, token string, lat, lon float64) error {
	body := woffuSignBody{
		AgreementEventID: nil,
		DeviceID:         "WebApp",
		Latitude:         lat,
		Longitude:        lon,
		RequestID:        nil,
		TimezoneOffset:   currentTimezoneOffset(),
	}

	err := companyClient.doJSON("POST", "/api/svc/signs/signs", body, map[string]string{
		"Authorization": "Bearer " + token,
	}, nil)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	return nil
}

// VerifySignRegistered polls today's slots until the signed-in state flips
// from its pre-sign value. Woffu occasionally accepts the sign POST without
// recording it; callers should treat an error as "sign not registered".
func VerifySignRegistered(companyClient *Client, token string, wasSignedIn bool) error {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		after, err := GetTodaySlots(companyClient, token)
		if err != nil {
			lastErr = err
			continue
		}
		if IsSignedIn(after) != wasSignedIn {
			return nil
		}
		lastErr = fmt.Errorf("sign sent but Woffu still reports signed_in=%v", wasSignedIn)
	}
	return lastErr
}

func currentTimezoneOffset() int {
	// Try to use Europe/Madrid for consistent offset
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		// Fallback to local timezone
		_, offset := time.Now().Zone()
		return -(offset / 60)
	}
	_, offset := time.Now().In(loc).Zone()
	return -(offset / 60)
}
