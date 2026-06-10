package woffu

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SignRecord represents a single clock in/out event.
type SignRecord struct {
	Date string `json:"date"`
	Time string `json:"time"`
	Type string `json:"type"` // "in" or "out"
}

type woffuDiaryPage struct {
	Diaries []woffuDiary `json:"Diaries"`
}

type woffuDiary struct {
	DiaryID  int64  `json:"DiaryId"`
	Date     string `json:"Date"`
	HasSigns bool   `json:"HasSigns"`
}

type woffuSignRecord struct {
	SignEventId string          `json:"SignEventId"` // UUID since the Woffu API change
	UserId      flexibleInt64   `json:"UserId"`
	TrueDate    string          `json:"TrueDate"` // local wall time
	TrueTime    string          `json:"TrueTime"`
	Date        string          `json:"Date"` // UTC
	SignIn      bool            `json:"SignIn"`
	Deleted     bool            `json:"Deleted"`
	Latitude    flexibleFloat64 `json:"Latitude"`
	Longitude   flexibleFloat64 `json:"Longitude"`
}

// flexibleInt64 handles JSON fields that may be either a number or a string.
type flexibleInt64 int64

func (f *flexibleInt64) UnmarshalJSON(data []byte) error {
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexibleInt64(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			*f = 0
			return nil
		}
		*f = flexibleInt64(n)
		return nil
	}
	*f = 0
	return nil
}

// flexibleFloat64 handles JSON fields that may be either a number or a string.
type flexibleFloat64 float64

func (f *flexibleFloat64) UnmarshalJSON(data []byte) error {
	// Try number first
	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexibleFloat64(n)
		return nil
	}
	// Try string
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			*f = 0
			return nil
		}
		*f = flexibleFloat64(n)
		return nil
	}
	*f = 0
	return nil
}

// GetSignHistory fetches sign records for a date range.
// The flat /api/signs endpoint ignores date filters and only returns the
// current day, so this walks the user's diaries for the range and fetches
// the signs of each day that has any.
func GetSignHistory(companyClient *Client, token string, from, to time.Time) ([]SignRecord, error) {
	userID, err := GetUserID(companyClient, token)
	if err != nil {
		return nil, fmt.Errorf("get sign history: %w", err)
	}

	days := int(to.Sub(from).Hours()/24) + 2
	if days < 2 {
		days = 2
	}

	var page woffuDiaryPage
	err = companyClient.doJSON("GET",
		fmt.Sprintf("/api/users/%d/diaries/presence?fromDate=%s&toDate=%s&pageIndex=0&pageSize=%d",
			userID, from.Format("2006-01-02"), to.Format("2006-01-02"), days),
		nil, map[string]string{
			"Authorization": "Bearer " + token,
		}, &page)
	if err != nil {
		return nil, fmt.Errorf("get sign history: list diaries: %w", err)
	}

	var records []SignRecord
	for _, diary := range page.Diaries {
		if !diary.HasSigns || diary.DiaryID == 0 {
			continue
		}

		var signs []woffuSignRecord
		err := companyClient.doJSON("GET",
			fmt.Sprintf("/api/diaries/%d/signs", diary.DiaryID),
			nil, map[string]string{
				"Authorization": "Bearer " + token,
			}, &signs)
		if err != nil {
			return nil, fmt.Errorf("get sign history: diary %d signs: %w", diary.DiaryID, err)
		}

		for _, s := range signs {
			if s.Deleted {
				continue
			}

			// TrueDate carries local wall time; Date is UTC.
			date, timeStr := splitWoffuTimestamp(s.TrueDate)
			if date == "" {
				date, timeStr = splitWoffuTimestamp(s.Date)
			}

			signType := "in"
			if !s.SignIn {
				signType = "out"
			}

			records = append(records, SignRecord{
				Date: date,
				Time: timeStr,
				Type: signType,
			})
		}
	}

	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Date != records[j].Date {
			return records[i].Date < records[j].Date
		}
		return records[i].Time < records[j].Time
	})

	return records, nil
}

// splitWoffuTimestamp splits a Woffu timestamp ("2026-06-09T08:30:02.797")
// into date ("2026-06-09") and time ("08:30") parts.
func splitWoffuTimestamp(value string) (date, timeStr string) {
	idx := strings.Index(value, "T")
	if idx == -1 {
		return "", ""
	}
	date = value[:idx]
	rest := value[idx+1:]
	if len(rest) >= 5 {
		timeStr = rest[:5]
	}
	return date, timeStr
}
