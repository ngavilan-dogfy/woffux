package woffu

import (
	"fmt"
	"strings"
	"time"
)

// GetSignInfo returns today's signing information with resolved coordinates.
func GetSignInfo(companyClient *Client, token string, officeLat, officeLon, homeLat, homeLon float64) (*SignInfo, error) {
	now := time.Now()
	todayStr := now.Format("2006-01-02")
	prevYearEnd := fmt.Sprintf("%d-12-31T23:00:00.000Z", now.Year()-1)

	var data []WoffuCalendarEvent
	err := companyClient.doJSON("GET", "/api/users/calendar-events?fromDate="+prevYearEnd, nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &data)
	if err != nil {
		return nil, fmt.Errorf("get calendar events: %w", err)
	}

	var todayEntry *WoffuCalendarEvent
	for i := range data {
		if strings.HasPrefix(data[i].Date, todayStr) {
			todayEntry = &data[i]
			break
		}
	}

	mode := getMode(todayEntry)
	isWorkingDay := getIsWorkingDay(todayEntry)
	lat, lon := getCoordinates(mode, officeLat, officeLon, homeLat, homeLon)
	nextEvents := getNextEvents(data, todayStr)

	return &SignInfo{
		Date:         todayStr,
		Mode:         mode,
		Latitude:     lat,
		Longitude:    lon,
		IsWorkingDay: isWorkingDay,
		NextEvents:   nextEvents,
	}, nil
}

func getMode(event *WoffuCalendarEvent) SignMode {
	if event == nil {
		return SignModeOffice
	}

	allPresence := append(event.Event.PresenceEvents, event.Event.PresencePendingEvents...)
	for _, e := range allPresence {
		if strings.Contains(strings.ToLower(e.AgreementEvent), "teletrabajo") {
			return SignModeRemote
		}
	}

	return SignModeOffice
}

func getIsWorkingDay(event *WoffuCalendarEvent) bool {
	if event == nil {
		return true
	}

	return !event.IsWeekend &&
		!event.Calendar.HasHoliday &&
		!event.Calendar.HasEvent &&
		len(event.Event.AbsenceEvents) == 0
}

func getCoordinates(mode SignMode, officeLat, officeLon, homeLat, homeLon float64) (float64, float64) {
	if mode == SignModeRemote {
		return homeLat, homeLon
	}
	return officeLat, officeLon
}

func getNextEvents(data []WoffuCalendarEvent, todayStr string) []SignEvent {
	var events []SignEvent
	for _, e := range data {
		if e.Date < todayStr {
			continue
		}
		if e.Calendar.HasHoliday || e.Calendar.HasEvent || len(e.Event.AbsenceEvents) > 0 {
			date := e.Date
			if idx := strings.Index(date, "T"); idx != -1 {
				date = date[:idx]
			}
			var names []string
			names = append(names, e.Calendar.HolidayNames...)
			names = append(names, e.Calendar.EventNames...)
			events = append(events, SignEvent{Date: date, Names: names})
		}
	}
	return events
}
