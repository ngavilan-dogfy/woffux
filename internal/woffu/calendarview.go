package woffu

import (
	"fmt"
	"strings"
	"time"
)

// CalendarDay represents a single day in the calendar view.
type CalendarDay struct {
	Date       string   `json:"date"`
	DayName    string   `json:"day"`
	Status     string   `json:"status"` // "working", "weekend", "holiday", "absence"
	Mode               string   `json:"mode"`   // "office", "remote", ""
	IsHoliday          bool     `json:"is_holiday"`
	IsWeekend          bool     `json:"is_weekend"`
	HasAbsence         bool     `json:"has_absence"`
	HasPendingPresence bool     `json:"has_pending_presence,omitempty"`
	EventNames         []string `json:"events,omitempty"`

	// Rich context data (populated by EnrichCalendarDays)
	Requests []DayRequest `json:"requests,omitempty"`
	Signs    []SignSlot   `json:"signs,omitempty"`
}

// DayRequest represents a request associated with a specific day.
type DayRequest struct {
	RequestID int    `json:"request_id"`
	EventName string `json:"event_name"`
	Status    string `json:"status"` // "pending", "approved", "rejected", "cancelled"
}

// Badge returns a short badge character for display in the calendar grid.
func (d *CalendarDay) Badge() string {
	if d.IsHoliday {
		return "H"
	}
	if d.IsWeekend {
		return ""
	}

	// Check requests (priority: approved > pending)
	for _, r := range d.Requests {
		if r.Status == "cancelled" || r.Status == "rejected" {
			continue
		}
		name := strings.ToLower(r.EventName)
		switch {
		case strings.Contains(name, "teletrabajo"):
			if r.Status == "pending" {
				return "t"
			}
			return "T"
		case strings.Contains(name, "vacaciones"):
			return "V"
		case strings.Contains(name, "médic") || strings.Contains(name, "medic"):
			return "M"
		default:
			return "A" // generic absence/event
		}
	}

	// Check sign slots
	if len(d.Signs) > 0 {
		return "●"
	}

	// Check mode from calendar data (presence events without explicit request)
	if d.Mode == "remote" {
		return "T"
	}

	return ""
}

// GetCalendarMonthYM fetches calendar data for a specific year and month.
func GetCalendarMonthYM(companyClient *Client, token string, year int, month time.Month) ([]CalendarDay, error) {
	targetYear := year
	targetMonth := month

	prevYearEnd := fmt.Sprintf("%d-12-31T23:00:00.000Z", targetYear-1)

	var data []WoffuCalendarEvent
	err := companyClient.doJSON("GET", "/api/users/calendar-events?fromDate="+prevYearEnd, nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &data)
	if err != nil {
		return nil, fmt.Errorf("get calendar: %w", err)
	}

	var days []CalendarDay
	for _, e := range data {
		if !strings.HasPrefix(e.Date, fmt.Sprintf("%d-%02d", targetYear, targetMonth)) {
			continue
		}

		date := e.Date
		if idx := strings.Index(date, "T"); idx != -1 {
			date = date[:idx]
		}

		t, _ := time.Parse("2006-01-02", date)
		dayName := t.Weekday().String()[:3]

		status := "working"
		if e.IsWeekend {
			status = "weekend"
		} else if e.Calendar.HasHoliday {
			status = "holiday"
		} else if len(e.Event.AbsenceEvents) > 0 {
			status = "absence"
		}

		mode := ""
		hasPendingPresence := false
		if !e.IsWeekend && !e.Calendar.HasHoliday {
			// Check approved presence events first
			for _, p := range e.Event.PresenceEvents {
				if strings.Contains(strings.ToLower(p.AgreementEvent), "teletrabajo") {
					mode = "remote"
					break
				}
			}
			// If no approved telework, check pending
			if mode != "remote" {
				for _, p := range e.Event.PresencePendingEvents {
					if strings.Contains(strings.ToLower(p.AgreementEvent), "teletrabajo") {
						mode = "remote"
						hasPendingPresence = true
						break
					}
				}
			}
			if mode == "" {
				mode = "office"
			}
		}

		var eventNames []string
		eventNames = append(eventNames, e.Calendar.HolidayNames...)
		eventNames = append(eventNames, e.Calendar.EventNames...)

		days = append(days, CalendarDay{
			Date:               date,
			DayName:            dayName,
			Status:             status,
			Mode:               mode,
			IsHoliday:          e.Calendar.HasHoliday,
			IsWeekend:          e.IsWeekend,
			HasAbsence:         len(e.Event.AbsenceEvents) > 0,
			HasPendingPresence: hasPendingPresence,
			EventNames:         eventNames,
		})
	}

	return days, nil
}

// GetCalendarMonth is a convenience wrapper that accepts a month number (1-12).
func GetCalendarMonth(companyClient *Client, token string, month int) ([]CalendarDay, error) {
	now := time.Now()
	targetMonth := now.Month()
	targetYear := now.Year()
	if month > 0 && month <= 12 {
		targetMonth = time.Month(month)
		if targetMonth < now.Month() {
			targetYear++
		}
	}
	return GetCalendarMonthYM(companyClient, token, targetYear, targetMonth)
}

// EnrichCalendarDays adds request and sign data to calendar days.
func EnrichCalendarDays(days []CalendarDay, requests []UserRequest, signs []SignRecord) {
	// Index requests by date
	reqByDate := make(map[string][]DayRequest)
	for _, r := range requests {
		// Expand date range into individual days
		start, err1 := time.Parse("2006-01-02", r.StartDate)
		end, err2 := time.Parse("2006-01-02", r.EndDate)
		if err1 != nil || err2 != nil {
			continue
		}
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			date := d.Format("2006-01-02")
			reqByDate[date] = append(reqByDate[date], DayRequest{
				RequestID: r.RequestID,
				EventName: r.EventName,
				Status:    r.Status,
			})
		}
	}

	// Index signs by date
	signByDate := make(map[string][]SignSlot)
	for _, s := range signs {
		date := s.Date
		// Group in/out pairs
		existing := signByDate[date]
		if s.Type == "in" {
			existing = append(existing, SignSlot{In: s.Date + "T" + s.Time})
		} else if len(existing) > 0 && existing[len(existing)-1].Out == "" {
			existing[len(existing)-1].Out = s.Date + "T" + s.Time
		} else {
			existing = append(existing, SignSlot{Out: s.Date + "T" + s.Time})
		}
		signByDate[date] = existing
	}

	// Enrich each day
	for i := range days {
		if reqs, ok := reqByDate[days[i].Date]; ok {
			days[i].Requests = reqs
		}
		if slots, ok := signByDate[days[i].Date]; ok {
			days[i].Signs = slots
		}
	}
}

// GetMonthRequests fetches requests that overlap with a specific month.
func GetMonthRequests(companyClient *Client, token string, userId int, year int, month time.Month) ([]UserRequest, error) {
	// Fetch enough pages to cover the month
	var allRequests []UserRequest
	monthStart := fmt.Sprintf("%d-%02d-01", year, month)
	monthEnd := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Format("2006-01-02")

	for page := 1; page <= 5; page++ {
		reqs, err := GetUserRequests(companyClient, token, userId, page, 50)
		if err != nil {
			return allRequests, err
		}
		if len(reqs) == 0 {
			break
		}

		for _, r := range reqs {
			// Include if request overlaps with the month
			if r.EndDate >= monthStart && r.StartDate <= monthEnd {
				allRequests = append(allRequests, r)
			}
		}

		// If oldest request in page is before our month, we have enough
		if reqs[len(reqs)-1].StartDate < monthStart {
			break
		}
	}

	return allRequests, nil
}

// GetMonthSigns fetches sign records for a specific month.
func GetMonthSigns(companyClient *Client, token string, year int, month time.Month) ([]SignRecord, error) {
	from := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
	return GetSignHistory(companyClient, token, from, to)
}
