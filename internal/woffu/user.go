package woffu

import (
	"fmt"
	"strconv"
)

// UserProfile contains the user's profile info from Woffu.
type UserProfile struct {
	FullName         string
	Email            string
	CompanyName      string
	DepartmentName   string
	JobTitle         string
	OfficeName       string
	OfficeLatitude   *float64
	OfficeLongitude  *float64
}

type woffuUserFull struct {
	UserID             int      `json:"UserId"`
	FullName           string   `json:"FullName"`
	Email              string   `json:"Email"`
	CompanyName        string   `json:"CompanyName"`
	DepartmentFullName string   `json:"DepartmentFullName"`
	JobTitleName       string   `json:"JobTitleName"`
	OfficeName         string   `json:"OfficeName"`
	OfficeLatitude     *float64 `json:"OfficeLatitude"`
	OfficeLongitude    *float64 `json:"OfficeLongitude"`
}

// GetUserProfile fetches the user profile from Woffu.
func GetUserProfile(companyClient *Client, token string) (*UserProfile, error) {
	var data woffuUserFull
	err := companyClient.doJSON("GET", "/api/users", nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &data)
	if err != nil {
		return nil, fmt.Errorf("get user profile: %w", err)
	}

	return &UserProfile{
		FullName:        data.FullName,
		Email:           data.Email,
		CompanyName:     data.CompanyName,
		DepartmentName:  data.DepartmentFullName,
		JobTitle:        data.JobTitleName,
		OfficeName:      data.OfficeName,
		OfficeLatitude:  data.OfficeLatitude,
		OfficeLongitude: data.OfficeLongitude,
	}, nil
}

// GetUserID fetches the numeric Woffu user ID for the authenticated user.
func GetUserID(companyClient *Client, token string) (int, error) {
	var data woffuUserFull
	err := companyClient.doJSON("GET", "/api/users", nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &data)
	if err != nil {
		return 0, fmt.Errorf("get user id: %w", err)
	}
	if data.UserID == 0 {
		return 0, fmt.Errorf("get user id: empty UserId in response")
	}
	return data.UserID, nil
}

// GetAvailableEvents returns the user's available events (vacations, hours, etc).
func GetAvailableEvents(companyClient *Client, token string) ([]AvailableUserEvent, error) {
	var data []woffuAgreementEventAvailability
	err := companyClient.doJSON("GET", "/api/user-agreement-events/availability", nil, map[string]string{
		"Authorization": "Bearer " + token,
	}, &data)
	if err != nil {
		return nil, fmt.Errorf("get available events: %w", err)
	}

	events := make([]AvailableUserEvent, 0, len(data))
	for _, item := range data {
		available := 0.0
		if len(item.AvailableFormatted.Values) > 0 {
			available, _ = strconv.ParseFloat(item.AvailableFormatted.Values[0], 64)
		}

		unit := "days"
		if item.AvailableFormatted.Resource == "_HoursFormatted" {
			unit = "hours"
		}

		events = append(events, AvailableUserEvent{
			Name:      item.Name,
			Available: available,
			Unit:      unit,
		})
	}

	return events, nil
}
