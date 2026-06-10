package github

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

// CronEntry represents a single cron schedule line with a comment.
type CronEntry struct {
	Cron    string
	Comment string
	Action  string // "in" or "out" (even index = in, odd index = out)
}

const catchUpWindowMinutes = 120

// GenerateCrons converts the schedule config into GitHub Actions cron expressions.
// For DST timezones, it emits one cron per UTC offset. The workflow guard then
// skips the offset that is not active, without relying on comma-separated hours.
func GenerateCrons(schedule config.Schedule, tz string) []CronEntry {
	stdOff, dstOff := utcOffsets(tz)
	offsets := []int{stdOff}
	if dstOff != stdOff {
		offsets = append(offsets, dstOff)
	}

	type scheduledDay struct {
		day  config.DaySchedule
		num  int
		name string
	}
	allDays := []scheduledDay{
		{schedule.Monday, 1, "Mon"},
		{schedule.Tuesday, 2, "Tue"},
		{schedule.Wednesday, 3, "Wed"},
		{schedule.Thursday, 4, "Thu"},
		{schedule.Friday, 5, "Fri"},
	}

	type cronGroup struct {
		minute     int
		hour       int
		offset     int
		action     string
		localTime  string
		localNames map[string]bool
		utcDays    map[int]bool
	}

	groups := make(map[string]*cronGroup)
	for _, d := range allDays {
		if !d.day.Enabled {
			continue
		}
		for i, t := range d.day.Times {
			action := "in"
			if i%2 != 0 {
				action = "out"
			}

			hour, minute, ok := parseTime(t.Time)
			if !ok {
				continue
			}

			for _, offset := range offsets {
				utcHour, dayDelta := localToUTC(hour, offset)
				utcDay := githubWeekday(d.num + dayDelta)
				key := fmt.Sprintf("%02d:%02d:%d:%d:%s:%s", hour, minute, offset, utcHour, action, t.Time)

				g, ok := groups[key]
				if !ok {
					g = &cronGroup{
						minute:     minute,
						hour:       utcHour,
						offset:     offset,
						action:     action,
						localTime:  t.Time,
						localNames: make(map[string]bool),
						utcDays:    make(map[int]bool),
					}
					groups[key] = g
				}
				g.localNames[d.name] = true
				g.utcDays[utcDay] = true
			}
		}
	}

	var keys []string
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var entries []CronEntry
	for _, key := range keys {
		g := groups[key]
		daysStr := intSetJoin(g.utcDays, ",")
		namesStr := stringSetJoin(g.localNames, "-")
		cron := fmt.Sprintf("%d %d * * %s", g.minute, g.hour, daysStr)
		comment := fmt.Sprintf("%s %s (UTC%+d)", namesStr, g.localTime, g.offset)
		entries = append(entries, CronEntry{Cron: cron, Comment: comment, Action: g.action})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Cron != entries[j].Cron {
			return entries[i].Cron < entries[j].Cron
		}
		if entries[i].Action != entries[j].Action {
			return entries[i].Action < entries[j].Action
		}
		return entries[i].Comment < entries[j].Comment
	})

	return entries
}

// localToUTC converts a local hour to UTC and returns the UTC day delta.
func localToUTC(hour, offsetHours int) (utcHour int, dayDelta int) {
	utc := hour - offsetHours
	if utc < 0 {
		utc += 24
		dayDelta = -1
	} else if utc >= 24 {
		utc -= 24
		dayDelta = 1
	}
	return utc, dayDelta
}

// signTimes collects all unique sign times (HH:MM) from the schedule config.
func signTimes(schedule config.Schedule) []string {
	seen := make(map[string]bool)
	for _, day := range []config.DaySchedule{
		schedule.Monday, schedule.Tuesday, schedule.Wednesday,
		schedule.Thursday, schedule.Friday,
	} {
		if !day.Enabled {
			continue
		}
		for _, t := range day.Times {
			seen[t.Time] = true
		}
	}
	var times []string
	for t := range seen {
		times = append(times, t)
	}
	sort.Strings(times)
	return times
}

type scheduleEvent struct {
	day    int
	time   string
	minute int
	action string
}

func scheduleEvents(schedule config.Schedule) []scheduleEvent {
	type scheduledDay struct {
		day config.DaySchedule
		num int
	}
	allDays := []scheduledDay{
		{schedule.Monday, 1},
		{schedule.Tuesday, 2},
		{schedule.Wednesday, 3},
		{schedule.Thursday, 4},
		{schedule.Friday, 5},
	}

	var events []scheduleEvent
	for _, d := range allDays {
		if !d.day.Enabled {
			continue
		}
		for i, t := range d.day.Times {
			hour, minute, ok := parseTime(t.Time)
			if !ok {
				continue
			}
			action := "in"
			if i%2 != 0 {
				action = "out"
			}
			events = append(events, scheduleEvent{
				day:    d.num,
				time:   t.Time,
				minute: hour*60 + minute,
				action: action,
			})
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].day != events[j].day {
			return events[i].day < events[j].day
		}
		return events[i].minute < events[j].minute
	})
	return events
}

// CatchUpSpec serializes the schedule as day:HH:MM:action entries for
// woffux sign --catch-up.
func CatchUpSpec(schedule config.Schedule) string {
	events := scheduleEvents(schedule)
	parts := make([]string, 0, len(events))
	for _, event := range events {
		parts = append(parts, fmt.Sprintf("%d:%s:%s", event.day, event.time, event.action))
	}
	return strings.Join(parts, ";")
}

// GenerateCatchUpCrons emits periodic watchdog schedules. They let a missed
// exact GitHub cron recover shortly after the configured sign time.
func GenerateCatchUpCrons(schedule config.Schedule, tz string, windowMinutes int) []CronEntry {
	events := scheduleEvents(schedule)
	if len(events) == 0 {
		return nil
	}
	if windowMinutes <= 0 {
		windowMinutes = catchUpWindowMinutes
	}

	minMinute := events[0].minute
	maxMinute := events[0].minute
	days := make(map[int]bool)
	for _, event := range events {
		if event.minute < minMinute {
			minMinute = event.minute
		}
		if event.minute > maxMinute {
			maxMinute = event.minute
		}
		days[event.day] = true
	}
	maxMinute += windowMinutes
	if maxMinute > 23*60+59 {
		maxMinute = 23*60 + 59
	}

	stdOff, dstOff := utcOffsets(tz)
	offsets := []int{stdOff}
	if dstOff != stdOff {
		offsets = append(offsets, dstOff)
	}

	type group struct {
		offset  int
		hours   map[int]bool
		utcDays map[int]bool
	}
	groups := make(map[string]*group)
	startHour := minMinute / 60
	endHour := maxMinute / 60
	for _, offset := range offsets {
		for localHour := startHour; localHour <= endHour; localHour++ {
			utcHour, dayDelta := localToUTC(localHour, offset)
			utcDays := make(map[int]bool)
			for day := range days {
				utcDays[githubWeekday(day+dayDelta)] = true
			}
			daysStr := intSetJoin(utcDays, ",")
			key := fmt.Sprintf("%d:%s", offset, daysStr)
			g, ok := groups[key]
			if !ok {
				g = &group{offset: offset, hours: make(map[int]bool), utcDays: utcDays}
				groups[key] = g
			}
			g.hours[utcHour] = true
		}
	}

	var keys []string
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var entries []CronEntry
	for _, key := range keys {
		g := groups[key]
		hours := intSetRanges(g.hours)
		daysStr := intSetJoin(g.utcDays, ",")
		cron := fmt.Sprintf("7,22,37,52 %s * * %s", hours, daysStr)
		comment := fmt.Sprintf("catch-up every 15m (UTC%+d)", g.offset)
		entries = append(entries, CronEntry{Cron: cron, Comment: comment, Action: "catch-up"})
	}
	return entries
}

// GenerateWorkflowYAML generates the auto-sign GitHub Actions workflow.
// For DST zones, each sign time produces one cron per UTC offset. The sign
// command resolves the local schedule at runtime, so delayed and duplicate
// offset runs either catch up safely or skip.
func GenerateWorkflowYAML(schedule config.Schedule, tz string, opts ...int) string {
	// Optional random delay in seconds (default 90)
	randomDelay := 90
	if len(opts) > 0 && opts[0] > 0 {
		randomDelay = opts[0]
	}
	crons := GenerateCrons(schedule, tz)
	catchUpCrons := GenerateCatchUpCrons(schedule, tz, catchUpWindowMinutes)

	var cronLines []string
	for _, c := range crons {
		cronLines = append(cronLines, fmt.Sprintf("    - cron: '%s'  # %s", c.Cron, c.Comment))
	}
	for _, c := range catchUpCrons {
		cronLines = append(cronLines, fmt.Sprintf("    - cron: '%s'  # %s", c.Cron, c.Comment))
	}
	triggerYAML := "on:\n  workflow_dispatch:\n"
	if len(cronLines) > 0 {
		triggerYAML = fmt.Sprintf("on:\n  schedule:\n%s\n  workflow_dispatch:\n", strings.Join(cronLines, "\n"))
	}

	// Resolve IANA timezone for the guard step
	ianaZone := tz
	if alias, ok := tzAliases[strings.ToUpper(tz)]; ok {
		ianaZone = alias
	}

	spec := CatchUpSpec(schedule)
	signBlock := `if [ "${{ github.event_name }}" = "schedule" ]; then
            ./woffux sign --catch-up '%s' --catch-up-timezone '%s' --catch-up-window '2h'
          else
            ./woffux sign
          fi`
	if spec == "" {
		signBlock = `./woffux sign`
	} else {
		signBlock = fmt.Sprintf(signBlock, spec, ianaZone)
	}

	return fmt.Sprintf(`name: Auto Sign

%s

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

      - name: Random delay
        if: github.event_name == 'schedule'
        run: sleep $(( RANDOM %% %d + 1 ))

      - name: Sign
        run: |
          %s
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

      - name: Notify failure
        if: failure()
        run: |
          if [ -n "$TELEGRAM_BOT_TOKEN" ] && [ -n "$TELEGRAM_CHAT_ID" ]; then
            MSG="❌ woffux auto-sign failed at $(TZ=%s date '+%%H:%%M %%Z %%Y-%%m-%%d')"
            curl -s -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
              -d chat_id="${TELEGRAM_CHAT_ID}" -d text="${MSG}" > /dev/null
          fi
        env:
          TELEGRAM_BOT_TOKEN: ${{ secrets.TELEGRAM_BOT_TOKEN }}
          TELEGRAM_CHAT_ID: ${{ secrets.TELEGRAM_CHAT_ID }}
`, triggerYAML, randomDelay, signBlock, ianaZone)
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

// hasDST returns true if the timezone observes DST.
func hasDST(tz string) bool {
	loc := loadTimezone(tz)
	jan := time.Date(2026, time.January, 15, 12, 0, 0, 0, loc)
	jul := time.Date(2026, time.July, 15, 12, 0, 0, 0, loc)
	_, offJan := jan.Zone()
	_, offJul := jul.Zone()
	return offJan != offJul
}

// utcOffsets returns the standard and DST UTC offsets in hours for the timezone.
// For non-DST zones, both values are the same.
func utcOffsets(tz string) (stdOffset, dstOffset int) {
	loc := loadTimezone(tz)
	jan := time.Date(2026, time.January, 15, 12, 0, 0, 0, loc)
	jul := time.Date(2026, time.July, 15, 12, 0, 0, 0, loc)
	_, offJan := jan.Zone()
	_, offJul := jul.Zone()
	return offJan / 3600, offJul / 3600
}

func parseTime(t string) (hour, minute int, ok bool) {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func githubWeekday(day int) int {
	for day < 0 {
		day += 7
	}
	return day % 7
}

func intSetJoin(ints map[int]bool, sep string) string {
	values := make([]int, 0, len(ints))
	for i := range ints {
		values = append(values, i)
	}
	sort.Ints(values)

	var parts []string
	for _, i := range values {
		parts = append(parts, strconv.Itoa(i))
	}
	return strings.Join(parts, sep)
}

func intSetRanges(ints map[int]bool) string {
	values := make([]int, 0, len(ints))
	for i := range ints {
		values = append(values, i)
	}
	sort.Ints(values)
	if len(values) == 0 {
		return ""
	}

	var parts []string
	start := values[0]
	prev := values[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
		}
	}
	for _, value := range values[1:] {
		if value == prev+1 {
			prev = value
			continue
		}
		flush()
		start = value
		prev = value
	}
	flush()
	return strings.Join(parts, ",")
}

func stringSetJoin(values map[string]bool, sep string) string {
	weekdayOrder := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	parts := make([]string, 0, len(values))
	for _, v := range weekdayOrder {
		if values[v] {
			parts = append(parts, v)
		}
	}
	for v := range values {
		if !containsString(weekdayOrder, v) {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, sep)
}

func containsString(values []string, needle string) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}
