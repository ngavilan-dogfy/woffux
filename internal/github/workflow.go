package github

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

// CronEntry represents a single cron schedule line with a comment.
type CronEntry struct {
	Cron    string
	Comment string
}

// GenerateCrons converts the schedule config into GitHub Actions cron expressions.
// Handles DST automatically: for timezones with DST, generates two cron entries
// per scheduled time (one for standard months, one for DST months) so that the
// local time stays correct year-round.
func GenerateCrons(schedule config.Schedule, tz string) []CronEntry {
	var entries []CronEntry

	stdOff, dstOff, stdMonths, dstMonths := dstOffsets(tz)
	isDST := dstMonths != ""

	// Group days by their schedule to produce compact cron expressions
	type dayTimes struct {
		days  []int
		names []string
		times []config.ScheduleEntry
	}

	allDays := []struct {
		day  config.DaySchedule
		num  int
		name string
	}{
		{schedule.Monday, 1, "Mon"},
		{schedule.Tuesday, 2, "Tue"},
		{schedule.Wednesday, 3, "Wed"},
		{schedule.Thursday, 4, "Thu"},
		{schedule.Friday, 5, "Fri"},
	}

	// Group days with identical schedules
	groups := make(map[string]*dayTimes)
	for _, d := range allDays {
		if !d.day.Enabled {
			continue
		}
		key := timesKey(d.day.Times)
		if g, ok := groups[key]; ok {
			g.days = append(g.days, d.num)
			g.names = append(g.names, d.name)
		} else {
			groups[key] = &dayTimes{
				days:  []int{d.num},
				names: []string{d.name},
				times: d.day.Times,
			}
		}
	}

	for _, g := range groups {
		daysStr := intSliceJoin(g.days, ",")
		namesStr := strings.Join(g.names, "-")

		for _, t := range g.times {
			hour, minute := parseTime(t.Time)

			// Standard time entry
			utcHour := localToUTC(hour, stdOff)
			if isDST {
				cron := fmt.Sprintf("%d %d * %s %s", minute, utcHour, stdMonths, daysStr)
				comment := fmt.Sprintf("%s %s (UTC%+d, standard)", namesStr, t.Time, stdOff)
				entries = append(entries, CronEntry{Cron: cron, Comment: comment})

				// DST entry with different offset
				utcHourDST := localToUTC(hour, dstOff)
				cronDST := fmt.Sprintf("%d %d * %s %s", minute, utcHourDST, dstMonths, daysStr)
				commentDST := fmt.Sprintf("%s %s (UTC%+d, DST)", namesStr, t.Time, dstOff)
				entries = append(entries, CronEntry{Cron: cronDST, Comment: commentDST})
			} else {
				cron := fmt.Sprintf("%d %d * * %s", minute, utcHour, daysStr)
				comment := fmt.Sprintf("%s %s (UTC%+d → %s local)", namesStr, t.Time, stdOff, t.Time)
				entries = append(entries, CronEntry{Cron: cron, Comment: comment})
			}
		}
	}

	return entries
}

// localToUTC converts a local hour to UTC given an offset in hours.
func localToUTC(hour, offsetHours int) int {
	utc := hour - offsetHours
	if utc < 0 {
		utc += 24
	} else if utc >= 24 {
		utc -= 24
	}
	return utc
}

// signHours collects all unique sign hours from the schedule config.
func signHours(schedule config.Schedule) []int {
	seen := make(map[int]bool)
	for _, day := range []config.DaySchedule{
		schedule.Monday, schedule.Tuesday, schedule.Wednesday,
		schedule.Thursday, schedule.Friday,
	} {
		if !day.Enabled {
			continue
		}
		for _, t := range day.Times {
			h, _ := parseTime(t.Time)
			seen[h] = true
		}
	}
	var hours []int
	for h := range seen {
		hours = append(hours, h)
	}
	return hours
}

// GenerateWorkflowYAML generates the auto-sign GitHub Actions workflow.
// For DST zones, generates dual cron entries so local times stay correct year-round.
// Transition months (e.g. March, October) appear in both standard and DST schedules;
// a timezone guard step verifies that the cron's UTC hour + current offset produces
// a valid configured sign hour — if not, the run is skipped (prevents double signing).
func GenerateWorkflowYAML(schedule config.Schedule, tz string) string {
	crons := GenerateCrons(schedule, tz)
	isDST := hasDST(tz)

	var cronLines []string
	for _, c := range crons {
		cronLines = append(cronLines, fmt.Sprintf("    - cron: '%s'  # %s", c.Cron, c.Comment))
	}

	// Resolve IANA timezone for the guard step
	ianaZone := tz
	if alias, ok := tzAliases[strings.ToUpper(tz)]; ok {
		ianaZone = alias
	}

	// Build the timezone guard step (only for DST zones with overlapping months)
	guardYAML := ""
	guardCondition := ""
	if isDST {
		hours := signHours(schedule)
		var hourStrs []string
		for _, h := range hours {
			hourStrs = append(hourStrs, strconv.Itoa(h))
		}
		hoursStr := strings.Join(hourStrs, " ")

		guardYAML = fmt.Sprintf(`
      - name: Timezone guard
        id: tz
        run: |
          CRON_HOUR=$(echo "%s" | awk '{print $2}')
          # Current UTC offset (in hours) for the configured timezone
          OFFSET=$(TZ=%s date +%%z | sed 's/00$//;s/^+0/+/;s/^+//')
          LOCAL_HOUR=$(( CRON_HOUR + OFFSET ))
          if [ "$LOCAL_HOUR" -lt 0 ]; then LOCAL_HOUR=$(( LOCAL_HOUR + 24 )); fi
          if [ "$LOCAL_HOUR" -ge 24 ]; then LOCAL_HOUR=$(( LOCAL_HOUR - 24 )); fi
          SIGN_HOURS="%s"
          MATCH=false
          for h in $SIGN_HOURS; do
            if [ "$LOCAL_HOUR" -eq "$h" ]; then MATCH=true; break; fi
          done
          if [ "$MATCH" = "false" ]; then
            echo "Skipping: cron hour $CRON_HOUR + offset $OFFSET = local $LOCAL_HOUR, not a configured sign hour"
            echo "skip=true" >> "$GITHUB_OUTPUT"
          else
            echo "skip=false" >> "$GITHUB_OUTPUT"
          fi
`, "${{ github.event.schedule }}", ianaZone, hoursStr)
		guardCondition = "\n        if: steps.tz.outputs.skip != 'true'"
	}

	return fmt.Sprintf(`name: Auto Sign

on:
  schedule:
%s

concurrency:
  group: sign
  cancel-in-progress: true

jobs:
  sign:
    name: Sign in Woffu
    runs-on: ubuntu-latest
    steps:%s

      - name: Download woffux%s
        run: |
          curl -fsSL "https://github.com/ngavilan-dogfy/woffux/releases/latest/download/woffux-linux-amd64" -o woffux
          chmod +x woffux

      - name: Random delay (2-5 min)%s
        run: sleep $(( RANDOM %% 181 + 120 ))

      - name: Sign%s
        run: ./woffux sign
        env:
          WOFFU_URL: ${{ secrets.WOFFU_URL }}
          WOFFU_COMPANY_URL: ${{ secrets.WOFFU_COMPANY_URL }}
          WOFFU_EMAIL: ${{ secrets.WOFFU_EMAIL }}
          WOFFU_PASSWORD: ${{ secrets.WOFFU_PASSWORD }}
          WOFFU_LATITUDE: ${{ secrets.WOFFU_LATITUDE }}
          WOFFU_LONGITUDE: ${{ secrets.WOFFU_LONGITUDE }}
          WOFFU_HOME_LATITUDE: ${{ secrets.WOFFU_HOME_LATITUDE }}
          WOFFU_HOME_LONGITUDE: ${{ secrets.WOFFU_HOME_LONGITUDE }}
          TELEGRAM_BOT_TOKEN: ${{ secrets.TELEGRAM_BOT_TOKEN }}
          TELEGRAM_CHAT_ID: ${{ secrets.TELEGRAM_CHAT_ID }}
`, strings.Join(cronLines, "\n"), guardYAML, guardCondition, guardCondition, guardCondition)
}

// GenerateManualWorkflowYAML generates the manual sign workflow.
func GenerateManualWorkflowYAML() string {
	return `name: Manual Sign

on:
  workflow_dispatch:

concurrency:
  group: sign
  cancel-in-progress: true

jobs:
  sign:
    name: Sign in Woffu
    runs-on: ubuntu-latest
    steps:
      - name: Download woffux
        run: |
          curl -fsSL "https://github.com/ngavilan-dogfy/woffux/releases/latest/download/woffux-linux-amd64" -o woffux
          chmod +x woffux

      - name: Sign
        run: ./woffux sign
        env:
          WOFFU_URL: ${{ secrets.WOFFU_URL }}
          WOFFU_COMPANY_URL: ${{ secrets.WOFFU_COMPANY_URL }}
          WOFFU_EMAIL: ${{ secrets.WOFFU_EMAIL }}
          WOFFU_PASSWORD: ${{ secrets.WOFFU_PASSWORD }}
          WOFFU_LATITUDE: ${{ secrets.WOFFU_LATITUDE }}
          WOFFU_LONGITUDE: ${{ secrets.WOFFU_LONGITUDE }}
          WOFFU_HOME_LATITUDE: ${{ secrets.WOFFU_HOME_LATITUDE }}
          WOFFU_HOME_LONGITUDE: ${{ secrets.WOFFU_HOME_LONGITUDE }}
          TELEGRAM_BOT_TOKEN: ${{ secrets.TELEGRAM_BOT_TOKEN }}
          TELEGRAM_CHAT_ID: ${{ secrets.TELEGRAM_CHAT_ID }}
`
}

// GenerateKeepaliveWorkflowYAML prevents GitHub from auto-disabling scheduled workflows.
func GenerateKeepaliveWorkflowYAML() string {
	return `name: Keepalive

on:
  schedule:
    - cron: '0 12 1 */2 *'

jobs:
  keepalive:
    name: Keep workflows alive
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
      - name: Keepalive commit
        run: |
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git commit --allow-empty -m "chore: keepalive [skip ci]"
          git push
`
}

// tzAliases maps common abbreviations to IANA zone names.
var tzAliases = map[string]string{
	"CET":  "Europe/Madrid",
	"CEST": "Europe/Madrid",
	"WET":  "Europe/Lisbon",
	"EET":  "Europe/Athens",
	"GMT":  "Europe/London",
	"EST":  "America/New_York",
	"CST":  "America/Chicago",
	"MST":  "America/Denver",
	"PST":  "America/Los_Angeles",
}

// loadTimezone resolves a timezone string to a *time.Location.
// Accepts IANA names (Europe/Madrid) and common aliases (CET).
func loadTimezone(tz string) *time.Location {
	if tz == "UTC" || tz == "" {
		return time.UTC
	}
	// Try alias first
	if iana, ok := tzAliases[strings.ToUpper(tz)]; ok {
		tz = iana
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		// Fallback: try parsing as numeric offset (+1, -5, etc.)
		if n, err := strconv.Atoi(tz); err == nil {
			return time.FixedZone(fmt.Sprintf("UTC%+d", n), n*3600)
		}
		return time.FixedZone("CET", 3600) // safe default
	}
	return loc
}

// timezoneOffsetHours computes the UTC offset in hours for the given timezone
// at a specific reference time. This correctly handles DST transitions.
func timezoneOffsetHours(tz string, at time.Time) int {
	loc := loadTimezone(tz)
	_, offset := at.In(loc).Zone()
	return offset / 3600
}

// hasDST returns true if the timezone observes DST (different offsets in Jan vs Jul).
func hasDST(tz string) bool {
	loc := loadTimezone(tz)
	jan := time.Date(2026, time.January, 15, 12, 0, 0, 0, loc)
	jul := time.Date(2026, time.July, 15, 12, 0, 0, 0, loc)
	_, offJan := jan.Zone()
	_, offJul := jul.Zone()
	return offJan != offJul
}

// dstOffsets returns the standard and DST offsets (in hours) and the month ranges
// when each applies. For non-DST zones, both offsets are the same.
//
// DST transitions happen mid-month (e.g., last Sunday of March/October in Europe).
// Since GitHub Actions cron only supports month granularity, transition months are
// included in BOTH standard and DST schedules — the worst case is a duplicate
// trigger that gets deduplicated by the concurrency group.
func dstOffsets(tz string) (stdOffset, dstOffset int, stdMonths, dstMonths string) {
	loc := loadTimezone(tz)
	jan := time.Date(2026, time.January, 15, 12, 0, 0, 0, loc)
	jul := time.Date(2026, time.July, 15, 12, 0, 0, 0, loc)
	_, offJan := jan.Zone()
	_, offJul := jul.Zone()

	stdOffset = offJan / 3600
	dstOffset = offJul / 3600

	if stdOffset == dstOffset {
		return stdOffset, dstOffset, "1-12", ""
	}

	// Find the actual DST transition months by scanning the year
	dstStartMonth, dstEndMonth := findDSTTransitions(loc)

	if dstOffset > stdOffset {
		// Northern hemisphere: DST in summer
		// Standard months: from month after DST ends through month before DST starts,
		// PLUS the transition months themselves (overlap ensures coverage)
		stdMonths = monthRange(dstEndMonth, dstStartMonth)   // e.g., "1-3,10-12" (Oct-Mar, inclusive)
		dstMonths = monthRange(dstStartMonth, dstEndMonth)   // e.g., "3-10" (Mar-Oct, inclusive)
	} else {
		// Southern hemisphere: DST in winter (reversed)
		stdMonths = monthRange(dstStartMonth, dstEndMonth)
		dstMonths = monthRange(dstEndMonth, dstStartMonth)
	}

	return stdOffset, dstOffset, stdMonths, dstMonths
}

// findDSTTransitions returns the months where DST starts and ends.
func findDSTTransitions(loc *time.Location) (startMonth, endMonth time.Month) {
	// Check offset at the 1st of each month to find transitions
	prevOffset := 0
	_, prevOffset = time.Date(2026, time.January, 1, 12, 0, 0, 0, loc).Zone()

	for m := time.February; m <= time.December; m++ {
		_, offset := time.Date(2026, m, 1, 12, 0, 0, 0, loc).Zone()
		if offset != prevOffset {
			if offset > prevOffset {
				// Clocks went forward → DST started
				// The transition happened in the previous month
				startMonth = m - 1
			} else {
				// Clocks went back → DST ended
				endMonth = m - 1
			}
		}
		prevOffset = offset
	}

	// Handle December→January wrap
	if startMonth == 0 || endMonth == 0 {
		_, offDec := time.Date(2026, time.December, 1, 12, 0, 0, 0, loc).Zone()
		_, offJan := time.Date(2027, time.January, 1, 12, 0, 0, 0, loc).Zone()
		if offDec != offJan {
			if offJan > offDec {
				startMonth = time.December
			} else {
				endMonth = time.December
			}
		}
	}

	// Fallback: March/October (European default)
	if startMonth == 0 {
		startMonth = time.March
	}
	if endMonth == 0 {
		endMonth = time.October
	}

	return startMonth, endMonth
}

// monthRange builds a cron month range string that includes both boundary months.
// fromMonth and toMonth are both included.
func monthRange(fromMonth, toMonth time.Month) string {
	from := int(fromMonth)
	to := int(toMonth)

	if from <= to {
		return fmt.Sprintf("%d-%d", from, to)
	}
	// Wraps around year boundary: e.g., Oct-Mar → "1-3,10-12"
	return fmt.Sprintf("1-%d,%d-12", to, from)
}

func parseTime(t string) (hour, minute int) {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return 0, 0
	}
	hour, _ = strconv.Atoi(parts[0])
	minute, _ = strconv.Atoi(parts[1])
	return
}

func timesKey(times []config.ScheduleEntry) string {
	var parts []string
	for _, t := range times {
		parts = append(parts, t.Time)
	}
	return strings.Join(parts, ",")
}

func intSliceJoin(ints []int, sep string) string {
	var parts []string
	for _, i := range ints {
		parts = append(parts, strconv.Itoa(i))
	}
	return strings.Join(parts, sep)
}
