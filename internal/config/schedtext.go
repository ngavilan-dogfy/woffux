package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedules as text: the fastest way to say what your week looks like.
//
//	mon-thu 8:30-13:30 14:15-17:30, fri 8-15
//	L-J 8:30-13:30 14:15-17:30; V 8:00-15:00
//	weekdays 9-18
//
// Days: English or Spanish names, abbreviations, single letters (L M X J V,
// X = miércoles) and ranges. Times: 8, 8:30, 8.30, 0830 or 8h30.
// Groups are separated by commas, semicolons or new lines. Days not
// mentioned are days off.

var weekdayOrder = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}

var dayAliases = map[string]time.Weekday{
	"mon": time.Monday, "monday": time.Monday, "lun": time.Monday, "lunes": time.Monday, "l": time.Monday, "lu": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday, "mar": time.Tuesday, "martes": time.Tuesday, "m": time.Tuesday, "ma": time.Tuesday,
	"wed": time.Wednesday, "wednesday": time.Wednesday, "mie": time.Wednesday, "mié": time.Wednesday, "miercoles": time.Wednesday, "miércoles": time.Wednesday, "x": time.Wednesday, "mi": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday, "jue": time.Thursday, "jueves": time.Thursday, "j": time.Thursday, "ju": time.Thursday,
	"fri": time.Friday, "friday": time.Friday, "vie": time.Friday, "viernes": time.Friday, "v": time.Friday, "vi": time.Friday,
}

var allWeekdayWords = map[string]bool{
	"weekdays": true, "workdays": true, "laborables": true, "lab": true, "all": true, "todos": true,
}

// ParseScheduleText turns a text description into a Schedule.
func ParseScheduleText(text string) (Schedule, error) {
	var s Schedule
	text = strings.TrimSpace(text)
	if text == "" {
		return s, fmt.Errorf("write at least one day, e.g. \"mon-fri 9-18\"")
	}
	seen := map[time.Weekday]bool{}
	for _, group := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		fields := strings.Fields(group)
		days, err := parseDays(fields[0])
		if err != nil {
			return s, err
		}
		if len(fields) < 2 {
			return s, fmt.Errorf("%q: add the hours, e.g. \"%s 9-18\"", group, fields[0])
		}
		var times []ScheduleEntry
		off := false
		for _, blk := range fields[1:] {
			if b := strings.ToLower(blk); b == "off" || b == "libre" || b == "-" {
				off = true
				continue
			}
			from, to, err := parseBlock(blk)
			if err != nil {
				return s, fmt.Errorf("%q: %w", group, err)
			}
			times = append(times, ScheduleEntry{Time: from}, ScheduleEntry{Time: to})
		}
		if err := validateTimes(times); err != nil {
			return s, fmt.Errorf("%q: %w", group, err)
		}
		for _, d := range days {
			if seen[d] {
				return s, fmt.Errorf("%s appears twice", d)
			}
			seen[d] = true
			ds := DaySchedule{Enabled: !off && len(times) > 0, Times: append([]ScheduleEntry(nil), times...)}
			if off {
				ds.Times = nil
			}
			s.setDay(d, ds)
		}
	}
	return s, nil
}

func parseDays(token string) ([]time.Weekday, error) {
	t := strings.ToLower(strings.TrimSuffix(token, ":"))
	if allWeekdayWords[t] {
		return weekdayOrder, nil
	}
	if a, b, ok := strings.Cut(t, "-"); ok {
		from, ok1 := dayAliases[a]
		to, ok2 := dayAliases[b]
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("unknown days %q (try mon-fri or L-V)", token)
		}
		if to < from {
			return nil, fmt.Errorf("%q goes backwards", token)
		}
		var out []time.Weekday
		for d := from; d <= to; d++ {
			out = append(out, d)
		}
		return out, nil
	}
	var out []time.Weekday
	for _, p := range strings.Split(t, "+") {
		d, ok := dayAliases[p]
		if !ok {
			return nil, fmt.Errorf("unknown day %q (try mon, tue… or L, M, X, J, V)", p)
		}
		out = append(out, d)
	}
	return out, nil
}

func parseBlock(blk string) (string, string, error) {
	a, b, ok := strings.Cut(blk, "-")
	if !ok {
		return "", "", fmt.Errorf("%q is not a block like 9-14 or 8:30-13:30", blk)
	}
	from, err := ParseClock(a)
	if err != nil {
		return "", "", err
	}
	to, err := ParseClock(b)
	if err != nil {
		return "", "", err
	}
	return from, to, nil
}

// ParseClock accepts 8, 08, 8:30, 8.30, 0830, 8h30, 8h and returns "HH:MM".
func ParseClock(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimSuffix(v, "h")
	v = strings.NewReplacer(".", ":", "h", ":").Replace(v)
	var h, m int
	var err error
	switch {
	case strings.Contains(v, ":"):
		hs, ms, _ := strings.Cut(v, ":")
		if h, err = strconv.Atoi(hs); err == nil {
			m, err = strconv.Atoi(ms)
		}
	case len(v) == 3 || len(v) == 4:
		h, err = strconv.Atoi(v[:len(v)-2])
		if err == nil {
			m, err = strconv.Atoi(v[len(v)-2:])
		}
	default:
		h, err = strconv.Atoi(v)
	}
	if err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return "", fmt.Errorf("%q is not a valid time", v)
	}
	return fmt.Sprintf("%02d:%02d", h, m), nil
}

func validateTimes(times []ScheduleEntry) error {
	prev := -1
	for _, e := range times {
		t, _ := time.Parse("15:04", e.Time)
		m := t.Hour()*60 + t.Minute()
		if m <= prev {
			return fmt.Errorf("times must go forward (%s)", e.Time)
		}
		prev = m
	}
	return nil
}

func (s *Schedule) setDay(d time.Weekday, ds DaySchedule) {
	switch d {
	case time.Monday:
		s.Monday = ds
	case time.Tuesday:
		s.Tuesday = ds
	case time.Wednesday:
		s.Wednesday = ds
	case time.Thursday:
		s.Thursday = ds
	case time.Friday:
		s.Friday = ds
	}
}

// Day returns the schedule for a weekday (weekends are always off).
func (s Schedule) Day(d time.Weekday) DaySchedule {
	switch d {
	case time.Monday:
		return s.Monday
	case time.Tuesday:
		return s.Tuesday
	case time.Wednesday:
		return s.Wednesday
	case time.Thursday:
		return s.Thursday
	case time.Friday:
		return s.Friday
	}
	return DaySchedule{}
}

// DayMinutes is the planned work of a day in minutes.
func (d DaySchedule) DayMinutes() int {
	if !d.Enabled {
		return 0
	}
	total := 0
	for i := 0; i+1 < len(d.Times); i += 2 {
		a, err1 := time.Parse("15:04", d.Times[i].Time)
		b, err2 := time.Parse("15:04", d.Times[i+1].Time)
		if err1 == nil && err2 == nil && b.After(a) {
			total += int(b.Sub(a).Minutes())
		}
	}
	return total
}

// WeekMinutes is the planned work of the whole week in minutes.
func (s Schedule) WeekMinutes() int {
	total := 0
	for _, d := range weekdayOrder {
		total += s.Day(d).DayMinutes()
	}
	return total
}

// ScheduleText renders a schedule compactly, grouping identical
// consecutive days: "mon-thu 08:30-13:30 14:15-17:30, fri 08:00-15:00".
func ScheduleText(s Schedule) string {
	short := map[time.Weekday]string{time.Monday: "mon", time.Tuesday: "tue", time.Wednesday: "wed", time.Thursday: "thu", time.Friday: "fri"}
	blocks := func(d DaySchedule) string {
		if !d.Enabled || len(d.Times) == 0 {
			return ""
		}
		var parts []string
		for i := 0; i+1 < len(d.Times); i += 2 {
			parts = append(parts, d.Times[i].Time+"-"+d.Times[i+1].Time)
		}
		return strings.Join(parts, " ")
	}
	var groups []string
	for i := 0; i < len(weekdayOrder); {
		b := blocks(s.Day(weekdayOrder[i]))
		j := i
		for j+1 < len(weekdayOrder) && blocks(s.Day(weekdayOrder[j+1])) == b {
			j++
		}
		if b != "" {
			days := short[weekdayOrder[i]]
			if j > i {
				days += "-" + short[weekdayOrder[j]]
			}
			groups = append(groups, days+" "+b)
		}
		i = j + 1
	}
	return strings.Join(groups, ", ")
}

// FormatMinutes renders minutes as "37h 30m" / "8h".
func FormatMinutes(m int) string {
	h, mm := m/60, m%60
	if mm == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %02dm", h, mm)
}

// ── Seasonal schedules ──

// Season switches to a preset between two dates every year (e.g. summer
// hours). Dates are "MM-DD"; a range may wrap the new year.
type Season struct {
	Preset string `yaml:"preset"`
	From   string `yaml:"from"`
	To     string `yaml:"to"`
}

// Seasons holds the yearly rules and the preset to use the rest of the year.
type Seasons struct {
	Default string   `yaml:"default,omitempty"`
	Periods []Season `yaml:"periods,omitempty"`
}

func (s Season) contains(date time.Time) bool {
	md := date.Format("01-02")
	if s.From <= s.To {
		return md >= s.From && md <= s.To
	}
	return md >= s.From || md <= s.To // wraps the new year
}

// NormalizeSeasonDate accepts "07-01", "1/7" or "01/07" (day/month) and
// returns "MM-DD".
func NormalizeSeasonDate(v string) (string, error) {
	v = strings.TrimSpace(v)
	if d, m, ok := strings.Cut(v, "/"); ok {
		dd, err1 := strconv.Atoi(strings.TrimSpace(d))
		mm, err2 := strconv.Atoi(strings.TrimSpace(m))
		if err1 != nil || err2 != nil {
			return "", fmt.Errorf("%q is not a date like 1/7 (day/month)", v)
		}
		v = fmt.Sprintf("%02d-%02d", mm, dd)
	}
	if _, err := time.Parse("01-02", v); err != nil {
		return "", fmt.Errorf("%q is not a date like 07-01 or 1/7", v)
	}
	return v, nil
}

// PresetFor returns the preset the seasons ask for on date ("" = no rules).
func (s Seasons) PresetFor(date time.Time) string {
	if len(s.Periods) == 0 {
		return ""
	}
	for _, p := range s.Periods {
		if p.contains(date) {
			return p.Preset
		}
	}
	return s.Default
}

// ApplySeasons switches the active schedule to what the seasons ask for on
// date. It reports whether anything changed (the caller may save).
func (c *Config) ApplySeasons(date time.Time) bool {
	want := c.Seasons.PresetFor(date)
	if want == "" || want == c.ActiveSchedule {
		return false
	}
	if _, ok := c.SavedSchedules[want]; !ok {
		return false
	}
	return c.LoadSchedulePreset(want)
}

// NextSeasonChange finds the next date (within a year) the seasons switch
// preset, for display: "summer starts Wed 1 Jul".
func (c *Config) NextSeasonChange(from time.Time) (time.Time, string, bool) {
	if len(c.Seasons.Periods) == 0 {
		return time.Time{}, "", false
	}
	cur := c.Seasons.PresetFor(from)
	for i := 1; i <= 366; i++ {
		d := from.AddDate(0, 0, i)
		if p := c.Seasons.PresetFor(d); p != cur {
			return d, p, true
		}
	}
	return time.Time{}, "", false
}

// RenamePreset renames a preset everywhere it's referenced (active
// schedule and seasons).
func (c *Config) RenamePreset(from, to string) error {
	from, to = NormalizePresetName(from), NormalizePresetName(to)
	if to == "" {
		return fmt.Errorf("give it a name")
	}
	s, ok := c.SavedSchedules[from]
	if !ok {
		return fmt.Errorf("no preset called %q", from)
	}
	if _, exists := c.SavedSchedules[to]; exists && to != from {
		return fmt.Errorf("%q already exists", to)
	}
	delete(c.SavedSchedules, from)
	c.SavedSchedules[to] = s
	if c.ActiveSchedule == from {
		c.ActiveSchedule = to
	}
	if c.Seasons.Default == from {
		c.Seasons.Default = to
	}
	for i := range c.Seasons.Periods {
		if c.Seasons.Periods[i].Preset == from {
			c.Seasons.Periods[i].Preset = to
		}
	}
	return nil
}

// PresetInSeasons reports whether seasons reference the preset.
func (c *Config) PresetInSeasons(name string) bool {
	if c.Seasons.Default == name && len(c.Seasons.Periods) > 0 {
		return true
	}
	for _, p := range c.Seasons.Periods {
		if p.Preset == name {
			return true
		}
	}
	return false
}

// UsePreset makes a preset the current schedule. With seasons on, it
// replaces the preset of whatever season today falls in, so the choice
// sticks instead of being switched back on the next read. It returns a
// short description of the scope ("rest of the year", "until 31 Aug").
func (c *Config) UsePreset(name string, today time.Time) (string, error) {
	name = NormalizePresetName(name)
	if _, ok := c.SavedSchedules[name]; !ok {
		return "", fmt.Errorf("no preset called %q", name)
	}
	scope := ""
	if len(c.Seasons.Periods) > 0 {
		inPeriod := false
		for i, p := range c.Seasons.Periods {
			if p.contains(today) {
				c.Seasons.Periods[i].Preset = name
				to, _ := time.Parse("01-02", p.To)
				scope = "until " + to.Format("2 Jan")
				inPeriod = true
				break
			}
		}
		if !inPeriod {
			c.Seasons.Default = name
			scope = "outside the seasonal dates"
		}
	}
	c.LoadSchedulePreset(name)
	return scope, nil
}
