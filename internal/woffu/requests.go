package woffu

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RequestType is an available request type (vacation, telework, etc).
type RequestType struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	IsVacation  bool    `json:"is_vacation"`
	IsPresence  bool    `json:"is_presence"`
	Allocated   string  `json:"allocated,omitempty"`
	Used        string  `json:"used,omitempty"`
	Available   string  `json:"available,omitempty"`
}

// UserRequest is a submitted request.
type UserRequest struct {
	RequestID   int    `json:"request_id"`
	EventName   string `json:"event_name"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Status      string `json:"status"`
	Days        int    `json:"days,omitempty"`
}

// SignSlot is a clock in/out slot for today.
type SignSlot struct {
	In  string `json:"in,omitempty"`
	Out string `json:"out,omitempty"`
}

// Holiday is a company calendar holiday.
type Holiday struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

// --- API types ---

type woffuRequestContext struct {
	AgreementEvents []woffuAgreementEvent `json:"AgreementEvents"`
}

type woffuAgreementEvent struct {
	AgreementEventID int    `json:"AgreementEventId"`
	Name             string `json:"Name"`
	IsVacation       bool   `json:"IsVacation"`
	IsPresence       bool   `json:"IsPresence"`
	UserStats *struct {
		AllocatedFormatted json.RawMessage `json:"AllocatedFormatted"`
		UsedFormatted      json.RawMessage `json:"UsedFormatted"`
		AvailableFormatted json.RawMessage `json:"AvailableFormatted"`
	} `json:"UserStats"`
}

type woffuUserRequest struct {
	RequestID           int     `json:"RequestId"`
	AgreementEventName  string  `json:"AgreementEventName"`
	StartDate           string  `json:"StartDate"`
	EndDate             string  `json:"EndDate"`
	RequestStatusID     int     `json:"RequestStatusId"`
	NumberDaysRequested float64 `json:"NumberDaysRequested"`
}

type woffuSignSlot struct {
	In  *string `json:"In"`
	Out *string `json:"Out"`
}

type woffuCalendarEventEntry struct {
	Name     string `json:"Name"`
	TrueDate string `json:"TrueDate"`
}

// --- API functions ---

// GetRequestTypes returns available request types with usage stats.
func GetRequestTypes(companyClient *Client, token string) ([]RequestType, error) {
	var ctx woffuRequestContext
	err := companyClient.doJSON("GET", "/api/requests/context", nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &ctx)
	if err != nil {
		return nil, fmt.Errorf("get request context: %w", err)
	}

	var types []RequestType
	for _, e := range ctx.AgreementEvents {
		rt := RequestType{
			ID:         e.AgreementEventID,
			Name:       e.Name,
			IsVacation: e.IsVacation,
			IsPresence: e.IsPresence,
		}
		if e.UserStats != nil {
			rt.Allocated = extractFormatted(e.UserStats.AllocatedFormatted)
			rt.Used = extractFormatted(e.UserStats.UsedFormatted)
			rt.Available = extractFormatted(e.UserStats.AvailableFormatted)
		}
		types = append(types, rt)
	}

	return types, nil
}

// GetUserRequests returns the user's submitted requests.
func GetUserRequests(companyClient *Client, token string, userId int, page, pageSize int) ([]UserRequest, error) {
	var data []woffuUserRequest
	url := fmt.Sprintf("/api/users/%d/requests?page=%d&pageSize=%d", userId, page, pageSize)
	err := companyClient.doJSON("GET", url, nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &data)
	if err != nil {
		return nil, fmt.Errorf("get requests: %w", err)
	}

	var requests []UserRequest
	for _, r := range data {
		startDate := r.StartDate
		if idx := strings.Index(startDate, "T"); idx != -1 {
			startDate = startDate[:idx]
		}
		endDate := r.EndDate
		if idx := strings.Index(endDate, "T"); idx != -1 {
			endDate = endDate[:idx]
		}

		requests = append(requests, UserRequest{
			RequestID: r.RequestID,
			EventName: r.AgreementEventName,
			StartDate: startDate,
			EndDate:   endDate,
			Status:    requestStatusName(r.RequestStatusID),
			Days:      int(r.NumberDaysRequested),
		})
	}

	return requests, nil
}

// GetTodaySlots returns today's clock in/out slots.
func GetTodaySlots(companyClient *Client, token string) ([]SignSlot, error) {
	var data []woffuSignSlot
	err := companyClient.doJSON("GET", "/api/signs/slots", nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &data)
	if err != nil {
		return nil, fmt.Errorf("get slots: %w", err)
	}

	var slots []SignSlot
	for _, s := range data {
		slot := SignSlot{}
		if s.In != nil {
			slot.In = *s.In
		}
		if s.Out != nil {
			slot.Out = *s.Out
		}
		slots = append(slots, slot)
	}

	return slots, nil
}

// GetHolidays returns company calendar holidays.
func GetHolidays(companyClient *Client, token string, calendarId int) ([]Holiday, error) {
	var data []woffuCalendarEventEntry
	url := fmt.Sprintf("/api/calendars/%d/events", calendarId)
	err := companyClient.doJSON("GET", url, nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &data)
	if err != nil {
		return nil, fmt.Errorf("get holidays: %w", err)
	}

	var holidays []Holiday
	for _, h := range data {
		date := h.TrueDate
		if idx := strings.Index(date, "T"); idx != -1 {
			date = date[:idx]
		}
		holidays = append(holidays, Holiday{
			Name: h.Name,
			Date: date,
		})
	}

	return holidays, nil
}

// GetUserId returns the user's ID from their profile.
func GetUserId(companyClient *Client, token string) (int, int, error) {
	var user struct {
		UserId     int `json:"UserId"`
		CalendarId int `json:"CalendarId"`
	}
	err := companyClient.doJSON("GET", "/api/users", nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &user)
	if err != nil {
		return 0, 0, err
	}
	return user.UserId, user.CalendarId, nil
}

// CreateRequest submits a new request (vacation, telework, absence, etc).
func CreateRequest(companyClient *Client, token string, userId, companyId, eventId int, startDate, endDate string, isVacation bool) error {
	body := map[string]interface{}{
		"AgreementEventId":     eventId,
		"IsVacation":           isVacation,
		"NumberHoursRequested": 0,
		"QuickDescription":     "",
		"ResponsibleUserId":    0,
		"UserId":               userId,
		"Files":                []interface{}{},
		"CompanyId":            companyId,
		"Accepted":             false,
		"Documents":            []interface{}{},
		"StartTime":            nil,
		"EndTime":              nil,
		"NumberDaysRequested":  1,
		"StartDate":            startDate,
		"EndDate":              endDate,
	}

	err := companyClient.doJSON("POST", "/api/requests", body, map[string]string{
		"Authorization": "Bearer " + token,
	}, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	return nil
}

// CancelRequest deletes/cancels a request by ID.
func CancelRequest(companyClient *Client, token string, requestId int) error {
	err := companyClient.doJSON("DELETE", fmt.Sprintf("/api/requests/%d", requestId), nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, nil)
	if err != nil {
		return fmt.Errorf("cancel request: %w", err)
	}
	return nil
}

// GetUserIds returns userId and companyId.
func GetUserIds(companyClient *Client, token string) (userId, companyId int, err error) {
	var user struct {
		UserId    int `json:"UserId"`
		CompanyId int `json:"CompanyId"`
	}
	err = companyClient.doJSON("GET", "/api/users", nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &user)
	if err != nil {
		return 0, 0, err
	}
	return user.UserId, user.CompanyId, nil
}

// extractFormatted handles Woffu formatted fields that can be either a plain string
// or an object like {"Resource": "...", "Values": ["..."]}.
func extractFormatted(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Values []string `json:"Values"`
	}
	if json.Unmarshal(raw, &obj) == nil && len(obj.Values) > 0 {
		return obj.Values[0]
	}
	return ""
}

func requestStatusName(id int) string {
	switch id {
	case 0:
		return "pending"
	case 10:
		return "approved"
	case 20:
		return "rejected"
	case 30:
		return "cancelled"
	default:
		return fmt.Sprintf("unknown(%d)", id)
	}
}
