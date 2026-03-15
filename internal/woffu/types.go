package woffu

import "encoding/json"

// SignMode represents whether the user is working from office or remote.
type SignMode string

const (
	SignModeOffice SignMode = "office"
	SignModeRemote SignMode = "remote"
)

func (m SignMode) Label() string {
	if m == SignModeRemote {
		return "Teletrabajo"
	}
	return "Oficina"
}

func (m SignMode) Emoji() string {
	if m == SignModeRemote {
		return "\U0001F3E0" // 🏠
	}
	return "\U0001F3E2" // 🏢
}

// Auth types

type woffuNewLogin struct {
	UseNewLogin bool        `json:"useNewLogin"`
	CompanyID   json.Number `json:"companyId"`
}

type woffuLoginConfiguration struct {
	AutoLogin    bool   `json:"autoLogin"`
	Domain       string `json:"domain"`
	OpenIDLogin  bool   `json:"openIdLogin"`
	ProviderName string `json:"providerName"`
	WoffuLogin   bool   `json:"woffuLogin"`
}

type woffuGetToken struct {
	Token string `json:"token"`
}

// User types

type woffuUser struct {
	UserID            int    `json:"UserId"`
	CompanyID         int    `json:"CompanyId"`
	ResponsibleUserID int    `json:"ResponsibleUserId"`
	SecretKey         string `json:"SecretKey"`
	CalendarID        int    `json:"CalendarId"`
	ScheduleID        int    `json:"ScheduleId"`
}

type woffuAgreementEventAvailability struct {
	AgreementEventID int    `json:"AgreementEventId"`
	Name             string `json:"Name"`
	AvailableFormatted struct {
		Resource string   `json:"Resource"`
		Values   []string `json:"Values"`
	} `json:"AvailableFormatted"`
}

// Calendar types

type WoffuCalendarEvent struct {
	Date      string `json:"Date"`
	IsWeekend bool   `json:"IsWeekend"`
	Calendar  struct {
		HasHoliday   bool     `json:"HasHoliday"`
		HasEvent     bool     `json:"HasEvent"`
		HolidayNames []string `json:"HolidayNames"`
		EventNames   []string `json:"EventNames"`
	} `json:"Calendar"`
	Schedule *struct {
		Name string `json:"Name"`
	} `json:"Schedule"`
	Event struct {
		AbsenceEvents         []json.RawMessage `json:"AbsenceEvents"`
		PresenceEvents        []presenceEvent   `json:"PresenceEvents"`
		PresencePendingEvents []presenceEvent   `json:"PresencePendingEvents"`
	} `json:"Event"`
}

type presenceEvent struct {
	AgreementEvent string `json:"AgreementEvent"`
}

// Sign types

type woffuSignBody struct {
	AgreementEventID *string `json:"agreementEventId"`
	DeviceID         string  `json:"deviceId"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	RequestID        *string `json:"requestId"`
	TimezoneOffset   int     `json:"timezoneOffset"`
}

// Domain types

type AvailableUserEvent struct {
	Name      string
	Available float64
	Unit      string
}

type SignInfo struct {
	Date         string
	Mode         SignMode
	Latitude     float64
	Longitude    float64
	IsWorkingDay bool
	NextEvents   []SignEvent
}

type SignEvent struct {
	Date  string
	Names []string
}
