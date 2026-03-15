package github

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
)

// CronEntry represents a single cron schedule line with a comment.
type CronEntry struct {
	Cron    string
	Comment string
}

// GenerateCrons converts the schedule config into GitHub Actions cron expressions.
// Converts local times to UTC based on the timezone offset.
func GenerateCrons(schedule config.Schedule, tz string) []CronEntry {
	offset := timezoneOffsetHours(tz)

	var entries []CronEntry

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
			utcHour := hour - offset
			if utcHour < 0 {
				utcHour += 24
			} else if utcHour >= 24 {
				utcHour -= 24
			}

			cron := fmt.Sprintf("%d %d * * %s", minute, utcHour, daysStr)
			comment := fmt.Sprintf("%s %s (UTC%+d → %s local)", namesStr, t.Time, offset, t.Time)
			entries = append(entries, CronEntry{Cron: cron, Comment: comment})
		}
	}

	return entries
}

// GenerateWorkflowYAML generates the auto-sign GitHub Actions workflow.
func GenerateWorkflowYAML(schedule config.Schedule, tz string) string {
	crons := GenerateCrons(schedule, tz)

	var cronLines []string
	for _, c := range crons {
		cronLines = append(cronLines, fmt.Sprintf("    - cron: '%s'  # %s", c.Cron, c.Comment))
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
    steps:
      - name: Download woffuk
        run: |
          curl -fsSL "https://github.com/ngavilan-dogfy/woffuk-cli/releases/latest/download/woffuk-linux-amd64" -o woffuk
          chmod +x woffuk

      - name: Random delay (2-5 min)
        run: sleep $(( RANDOM %% 181 + 120 ))

      - name: Sign
        run: ./woffuk sign
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
`, strings.Join(cronLines, "\n"))
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
      - name: Download woffuk
        run: |
          curl -fsSL "https://github.com/ngavilan-dogfy/woffuk-cli/releases/latest/download/woffuk-linux-amd64" -o woffuk
          chmod +x woffuk

      - name: Sign
        run: ./woffuk sign
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

func timezoneOffsetHours(tz string) int {
	switch tz {
	case "CET", "Europe/Madrid", "Europe/Barcelona", "Europe/Paris", "Europe/Berlin":
		return 1
	case "CEST":
		return 2
	case "UTC", "GMT":
		return 0
	case "EST":
		return -5
	case "PST":
		return -8
	default:
		// Try to parse as +N or -N
		if n, err := strconv.Atoi(tz); err == nil {
			return n
		}
		return 1 // Default to CET
	}
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
