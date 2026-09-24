package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	"github.com/ngavilan-dogfy/woffux/internal/timing"
)

// ── Look ──
//
// Same palette as the dashboard: violet brand, calm neutrals, green for IN,
// amber for OUT.

var (
	obBrand  = lipgloss.Color("#a78bfa")
	obText   = lipgloss.Color("#e7e5e4")
	obSubtle = lipgloss.Color("#a8a29e")
	obFaint  = lipgloss.Color("#78716c")
	obGhost  = lipgloss.Color("#57534e")
	obIn     = lipgloss.Color("#4ade80")
	obOut    = lipgloss.Color("#fbbf24")
	obBad    = lipgloss.Color("#f87171")

	stBrand  = lipgloss.NewStyle().Foreground(obBrand).Bold(true)
	stText   = lipgloss.NewStyle().Foreground(obText)
	stBold   = lipgloss.NewStyle().Foreground(obText).Bold(true)
	stSubtle = lipgloss.NewStyle().Foreground(obSubtle)
	stFaint  = lipgloss.NewStyle().Foreground(obFaint)
	stGhost  = lipgloss.NewStyle().Foreground(obGhost)
	stIn     = lipgloss.NewStyle().Foreground(obIn)
	stOut    = lipgloss.NewStyle().Foreground(obOut)
	stBad    = lipgloss.NewStyle().Foreground(obBad)
)

// woffuxTheme styles every huh form like the dashboard.
func woffuxTheme() *huh.Theme {
	t := huh.ThemeCharm()
	t.Focused.Base = t.Focused.Base.BorderForeground(obBrand)
	t.Focused.Title = t.Focused.Title.Foreground(obBrand).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(obBrand).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(obSubtle)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(obBrand)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(obBrand)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(obIn)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(obIn)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(obBrand).Foreground(lipgloss.Color("#1c1917")).Bold(true)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(obBrand)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(obBrand)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(obBad)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(obBad)
	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	return t
}

// newForm builds a themed form. Use it instead of huh.NewForm.
func newForm(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).WithTheme(woffuxTheme())
}

// ── Step headers ──

// setupSteps is the onboarding outline; stepHeader shows where you are.
var setupSteps = []string{"Woffu account", "Locations", "Work schedule", "Natural timing", "Who signs", "Notifications"}

func stepHeader(n int, why string) {
	total := len(setupSteps)
	var dots strings.Builder
	for i := 1; i <= total; i++ {
		switch {
		case i < n:
			dots.WriteString(stIn.Render("●"))
		case i == n:
			dots.WriteString(stBrand.Render("●"))
		default:
			dots.WriteString(stGhost.Render("○"))
		}
	}
	fmt.Println()
	fmt.Printf("  %s  %s  %s\n", dots.String(), stFaint.Render(fmt.Sprintf("step %d of %d", n, total)), stBrand.Render(setupSteps[n-1]))
	if why != "" {
		fmt.Println(lipgloss.NewStyle().Foreground(obSubtle).Width(76).PaddingLeft(2).Render(why))
	}
	fmt.Println()
}

func printWelcome(returning bool) {
	fmt.Println()
	fmt.Println("  " + stBrand.Render("◆ woffux"))
	fmt.Println()
	if returning {
		fmt.Println("  " + stBold.Render("Let's review your setup."))
		fmt.Println("  " + stSubtle.Render("Everything is pre-filled — press Enter to keep what you have."))
	} else {
		fmt.Println("  " + stBold.Render("Welcome! Let's make Woffu sign itself."))
		fmt.Println("  " + stSubtle.Render("Six short steps, about three minutes. Here's what happens:"))
		fmt.Println()
		for i, s := range setupSteps {
			fmt.Printf("    %s %s\n", stFaint.Render(fmt.Sprintf("%d.", i+1)), stText.Render(s))
		}
		fmt.Println()
		fmt.Println("  " + stFaint.Render("Your password goes to the system keychain; settings live in ~/.woffux.yaml."))
		fmt.Println("  " + stFaint.Render("Nothing is signed during setup. Ctrl+C quits at any time."))
	}
}

// ── Schedule wizard ──

type scheduleTemplate struct {
	key, name, text string
}

// Common Spanish office schedules. Each one is just text, so choosing one
// and tweaking it is the same gesture.
var scheduleTemplates = []scheduleTemplate{
	{"split-short-friday", "Split day, short Friday", "mon-thu 08:30-13:30 14:15-17:30, fri 08:00-15:00"},
	{"split", "Split day all week", "mon-fri 09:00-14:00 15:00-18:00"},
	{"continuous", "Continuous day (intensiva)", "mon-fri 08:00-15:00"},
	{"early", "Early shift", "mon-fri 07:00-15:00"},
}

type scheduleWizardResult struct {
	Schedule       config.Schedule
	Timezone       string
	ActiveSchedule string
	SavedPreset    string

	// Summer, when set, becomes a seasonal preset between SummerFrom and
	// SummerTo ("MM-DD"); Schedule is then the rest of the year.
	Summer     *config.Schedule
	SummerFrom string
	SummerTo   string
	// NoSeasons means the user said there are no summer hours: any
	// existing seasonal switch is removed.
	NoSeasons bool
}

func applyScheduleWizardResult(cfg *config.Config, result scheduleWizardResult) error {
	cfg.Schedule = result.Schedule
	if result.NoSeasons {
		cfg.Seasons = config.Seasons{}
	}
	if result.Timezone != "" {
		cfg.Timezone = result.Timezone
	}
	if result.Summer != nil {
		regular := config.NormalizePresetName(result.SavedPreset)
		if regular == "" {
			regular = presetNameFor(cfg, result.Schedule, "regular")
		}
		if err := cfg.SaveSchedulePreset(regular, result.Schedule); err != nil {
			return err
		}
		summer := presetNameFor(cfg, *result.Summer, "summer")
		if err := cfg.SaveSchedulePreset(summer, *result.Summer); err != nil {
			return err
		}
		cfg.ActiveSchedule = regular
		cfg.Seasons = config.Seasons{Default: regular, Periods: []config.Season{{Preset: summer, From: result.SummerFrom, To: result.SummerTo}}}
		cfg.ApplySeasons(time.Now())
		return nil
	}
	if result.SavedPreset != "" {
		if err := cfg.SaveSchedulePreset(result.SavedPreset, result.Schedule); err != nil {
			return err
		}
		cfg.ActiveSchedule = config.NormalizePresetName(result.SavedPreset)
		return nil
	}
	cfg.ActiveSchedule = config.NormalizePresetName(result.ActiveSchedule)
	cfg.Normalize()
	return nil
}

// presetNameFor reuses the name of an identical saved preset (so "classic"
// isn't duplicated as "regular"), or falls back to def.
func presetNameFor(cfg *config.Config, s config.Schedule, def string) string {
	if cfg.ActiveSchedule != "" && config.SchedulesEqual(cfg.SavedSchedules[cfg.ActiveSchedule], s) {
		return cfg.ActiveSchedule
	}
	for _, name := range cfg.SchedulePresetNames() {
		if config.SchedulesEqual(cfg.SavedSchedules[name], s) {
			return name
		}
	}
	return def
}

// summerPresetGuess finds an existing summer-ish preset to offer.
func summerPresetGuess(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if p := cfg.Seasons.Periods; len(p) > 0 {
		if s, ok := cfg.SavedSchedules[p[0].Preset]; ok {
			return config.ScheduleText(s)
		}
	}
	for _, name := range cfg.SchedulePresetNames() {
		n := strings.ToLower(name)
		for _, hint := range []string{"summer", "sommer", "verano", "estiu", "intensiv"} {
			if strings.Contains(n, hint) {
				return config.ScheduleText(cfg.SavedSchedules[name])
			}
		}
	}
	return ""
}

// scheduleWizard asks for the week (and optionally summer hours). askName
// offers saving it as a named preset.
func scheduleWizard(askName ...bool) (scheduleWizardResult, error) {
	existing, _ := config.Load()
	zone, _ := time.Now().Zone()
	if existing != nil && existing.Timezone != "" {
		zone = existing.Timezone
	}

	schedule, active, err := pickSchedule("Your working week", existing, "")
	if err != nil {
		return scheduleWizardResult{}, err
	}
	result := scheduleWizardResult{Schedule: schedule, Timezone: zone, ActiveSchedule: active}

	// Summer hours: very common in Spain, painful to remember to switch.
	hasSummer := existing != nil && len(existing.Seasons.Periods) > 0
	if err := newForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Different hours in summer?").
			Description("E.g. jornada intensiva in July and August. woffux switches automatically on the dates you choose.").
			Affirmative("Yes, set it up").
			Negative("No").
			Value(&hasSummer),
	)).Run(); err != nil {
		return result, err
	}
	result.NoSeasons = !hasSummer
	if hasSummer {
		initial := ""
		from, to := "1/7", "31/8"
		if existing != nil {
			initial = summerPresetGuess(existing)
			if p := existing.Seasons.Periods; len(p) > 0 {
				from, to = dayMonth(p[0].From), dayMonth(p[0].To)
			}
		}
		summer, _, err := pickSchedule("Summer hours", nil, orDefaultStr(initial, "mon-fri 08:00-15:00"))
		if err != nil {
			return result, err
		}
		validDate := func(s string) error { _, err := config.NormalizeSeasonDate(s); return err }
		if err := newForm(huh.NewGroup(
			huh.NewInput().Title("Summer starts on").Description("day/month, e.g. 1/7").Value(&from).Validate(validDate),
			huh.NewInput().Title("…and ends on").Description("day/month, e.g. 31/8 — inclusive").Value(&to).Validate(validDate),
		)).Run(); err != nil {
			return result, err
		}
		result.Summer = &summer
		result.SummerFrom, _ = config.NormalizeSeasonDate(from)
		result.SummerTo, _ = config.NormalizeSeasonDate(to)
		fmt.Printf("  %s Summer hours from %s to %s, then back to your regular week\n", stIn.Render("✓"), stBold.Render(from), stBold.Render(to))
	}

	if len(askName) > 0 && askName[0] && result.Summer == nil {
		var saveName string
		if err := newForm(huh.NewGroup(
			huh.NewInput().
				Title("Save it as a preset?").
				Description("Give it a name to switch back to it later from the dashboard — or leave empty").
				Placeholder("e.g. winter").
				Value(&saveName),
		)).Run(); err != nil {
			return result, err
		}
		if n := config.NormalizePresetName(saveName); n != "" {
			result.SavedPreset, result.ActiveSchedule = n, n
		}
	}
	return result, nil
}

// pickSchedule lets the user choose a template, a saved preset, type the
// week as text or go day by day, then review and tweak until it's right.
func pickSchedule(title string, existing *config.Config, initialText string) (config.Schedule, string, error) {
	var choice string
	var opts []huh.Option[string]
	if initialText != "" {
		opts = append(opts, huh.NewOption("Keep: "+initialText, "keep"))
	}
	if existing != nil && strings.TrimSpace(config.ScheduleText(existing.Schedule)) != "" && initialText == "" {
		opts = append(opts, huh.NewOption("Keep current: "+config.ScheduleText(existing.Schedule), "current"))
	}
	for _, t := range scheduleTemplates {
		s, _ := config.ParseScheduleText(t.text)
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-28s %s", t.name, stFaint.Render(t.text+" · "+config.FormatMinutes(s.WeekMinutes())+"/wk")), "tpl:"+t.key))
	}
	if existing != nil {
		for _, name := range existing.SchedulePresetNames() {
			opts = append(opts, huh.NewOption("Saved preset: "+name, "saved:"+name))
		}
	}
	opts = append(opts,
		huh.NewOption("Write it myself — fastest", "write"),
		huh.NewOption("Day by day, guided", "guided"),
	)
	if err := newForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Description("Pick the closest one — you can tweak any detail next.").
			Options(opts...).
			Value(&choice),
	)).Run(); err != nil {
		return config.Schedule{}, "", err
	}

	var (
		schedule config.Schedule
		active   string
		err      error
	)
	switch {
	case choice == "keep":
		schedule, err = config.ParseScheduleText(initialText)
	case choice == "current":
		schedule, active = existing.Schedule, existing.ActiveSchedule
	case strings.HasPrefix(choice, "tpl:"):
		for _, t := range scheduleTemplates {
			if "tpl:"+t.key == choice {
				schedule, err = config.ParseScheduleText(t.text)
			}
		}
	case strings.HasPrefix(choice, "saved:"):
		active = strings.TrimPrefix(choice, "saved:")
		schedule = existing.SavedSchedules[active]
	case choice == "write":
		schedule, err = writeSchedule(initialText)
	case choice == "guided":
		schedule, err = customScheduleWizard(stIn, stOut)
	}
	if err != nil {
		return config.Schedule{}, "", err
	}

	// Review loop: see it, then accept or tweak as text.
	for {
		if schedule.WeekMinutes() == 0 {
			fmt.Printf("\n  %s No working hours yet — write your week:\n", stOut.Render("!"))
			if schedule, err = writeSchedule(""); err != nil {
				return config.Schedule{}, "", err
			}
			active = ""
			continue
		}
		printWeek(schedule)
		var next string
		if err := newForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Does this look right?").
				Options(
					huh.NewOption("Yes, that's my week", "ok"),
					huh.NewOption("Tweak it (edit as text)", "tweak"),
				).
				Value(&next),
		)).Run(); err != nil {
			return config.Schedule{}, "", err
		}
		if next == "ok" {
			return schedule, active, nil
		}
		if schedule, err = writeSchedule(config.ScheduleText(schedule)); err != nil {
			return config.Schedule{}, "", err
		}
		active = ""
	}
}

// writeSchedule is a single text field with live parsing: the description
// shows what woffux understood, so mistakes are obvious before Enter.
func writeSchedule(initial string) (config.Schedule, error) {
	text := initial
	describe := func() string {
		if strings.TrimSpace(text) == "" {
			return "Days, then blocks. Examples:\n" +
				"  mon-thu 8:30-13:30 14:15-17:30, fri 8-15\n" +
				"  L-V 9-14 15-18        (Spanish letters work: L M X J V)"
		}
		s, err := config.ParseScheduleText(text)
		if err != nil {
			return "✗ " + err.Error()
		}
		return "✓ " + weekOneLine(s)
	}
	if err := newForm(huh.NewGroup(
		huh.NewInput().
			Title("Write your week").
			Placeholder("mon-thu 8:30-13:30 14:15-17:30, fri 8-15").
			Value(&text).
			DescriptionFunc(describe, &text).
			Validate(func(s string) error { _, err := config.ParseScheduleText(s); return err }),
	)).Run(); err != nil {
		return config.Schedule{}, err
	}
	return config.ParseScheduleText(text)
}

func weekOneLine(s config.Schedule) string {
	return fmt.Sprintf("%s · %s/week", config.ScheduleText(s), config.FormatMinutes(s.WeekMinutes()))
}

// printWeek draws the week as a small timeline, one row per day.
func printWeek(s config.Schedule) {
	const from, to = 6 * 60, 21 * 60 // 06:00–21:00 axis
	const width = 45
	col := func(m int) int { return (m - from) * width / (to - from) }
	ticks := []rune(strings.Repeat(" ", width+2))
	for h := 6; h <= 21; h += 3 {
		c := col(h * 60)
		lbl := fmt.Sprintf("%02d", h)
		if c+2 > len(ticks) {
			c = len(ticks) - 2
		}
		copy(ticks[c:], []rune(lbl))
	}
	fmt.Println()
	fmt.Printf("  %s%s\n", strings.Repeat(" ", 5), stGhost.Render(string(ticks)))
	for _, d := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
		day := s.Day(d)
		bar := []rune(strings.Repeat("·", width))
		for i := 0; i+1 < len(day.Times) && day.Enabled; i += 2 {
			a, _ := minutesOf(day.Times[i].Time)
			b, _ := minutesOf(day.Times[i+1].Time)
			for c := max(0, col(a)); c < min(width, col(b)); c++ {
				bar[c] = '█'
			}
		}
		var line strings.Builder
		for _, r := range bar {
			if r == '█' {
				line.WriteString(stBrand.Render("█"))
			} else {
				line.WriteString(stGhost.Render("·"))
			}
		}
		hours := stFaint.Render("off")
		if day.Enabled {
			hours = stSubtle.Render(config.FormatMinutes(day.DayMinutes()))
		}
		fmt.Printf("  %s  %s  %s\n", stText.Render(d.String()[:3]), line.String(), hours)
	}
	fmt.Printf("  %s %s\n\n", stFaint.Render(strings.Repeat(" ", 4)+"week"), stBold.Render(config.FormatMinutes(s.WeekMinutes())))
}

func minutesOf(hhmm string) (int, bool) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func dayMonth(mmdd string) string {
	t, err := time.Parse("01-02", mmdd)
	if err != nil {
		return mmdd
	}
	return fmt.Sprintf("%d/%d", t.Day(), int(t.Month()))
}

func orDefaultStr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// ── Natural timing ──

// timingWizard picks the natural-timing preset, showing what next week's
// signs would look like with it.
func timingWizard(current timing.Settings, schedule config.Schedule) (timing.Settings, error) {
	choice := "natural"
	if current.Seed != "" || current.Active() {
		choice = current.PresetKey()
		if !current.Active() {
			choice = "exact"
		}
	}
	var opts []huh.Option[string]
	for _, p := range timing.Presets {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-22s %s", p.Name, stFaint.Render(p.Description)), p.Key))
	}
	opts = append(opts, huh.NewOption("Custom windows", "custom"))

	preview := func() string {
		s := current
		for _, p := range timing.Presets {
			if p.Key == choice {
				s = current.WithPreset(p)
			}
		}
		if choice == "custom" {
			return "You'll set the four windows next."
		}
		return "Next week would look like:\n" + timingPreview(s, schedule)
	}
	if err := newForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("How precise should signs be?").
			Options(opts...).
			Height(len(opts)+9). // room for the 6-line preview
			Value(&choice).
			DescriptionFunc(preview, &choice),
	)).Run(); err != nil {
		return current, err
	}

	if choice != "custom" {
		for _, p := range timing.Presets {
			if p.Key == choice {
				return current.WithPreset(p), nil
			}
		}
	}

	s := current
	if s.Seed == "" {
		s.Seed = timing.NewSeed()
	}
	vals := []string{strconv.Itoa(s.InEarly), strconv.Itoa(s.InLate), strconv.Itoa(s.OutEarly), strconv.Itoa(s.OutLate)}
	minutes := func(v string) error {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 || n > 30 {
			return fmt.Errorf("minutes between 0 and 30")
		}
		return nil
	}
	if err := newForm(huh.NewGroup(
		huh.NewInput().Title("Clock IN up to … minutes early").Value(&vals[0]).Validate(minutes),
		huh.NewInput().Title("Clock IN up to … minutes late").Description("Tip: keep this small; late arrivals are what HR notices").Value(&vals[1]).Validate(minutes),
		huh.NewInput().Title("Clock OUT up to … minutes early").Description("Tip: 0 — leaving early is what shortens your day").Value(&vals[2]).Validate(minutes),
		huh.NewInput().Title("Clock OUT up to … minutes late").Value(&vals[3]).Validate(minutes),
	)).Run(); err != nil {
		return current, err
	}
	s.InEarly, _ = strconv.Atoi(strings.TrimSpace(vals[0]))
	s.InLate, _ = strconv.Atoi(strings.TrimSpace(vals[1]))
	s.OutEarly, _ = strconv.Atoi(strings.TrimSpace(vals[2]))
	s.OutLate, _ = strconv.Atoi(strings.TrimSpace(vals[3]))
	s.Normalize()
	fmt.Println()
	fmt.Println(indentLines(timingPreview(s, schedule), 2))
	return s, nil
}

// timingPreview renders the next five working days' sign moments.
func timingPreview(s timing.Settings, schedule config.Schedule) string {
	now := time.Now()
	var rows []string
	for d := 1; len(rows) < 5 && d < 14; d++ {
		date := now.AddDate(0, 0, d)
		day := schedule.Day(date.Weekday())
		if !day.Enabled || len(day.Times) == 0 {
			continue
		}
		var ev []timing.Event
		for i, e := range day.Times {
			m, _ := minutesOf(e.Time)
			ev = append(ev, timing.Event{Minute: m, In: i%2 == 0})
		}
		var parts []string
		for i, t := range s.Targets(date, ev) {
			st := stIn
			if !ev[i].In {
				st = stOut
			}
			parts = append(parts, st.Render(t.Format("15:04")))
		}
		rows = append(rows, fmt.Sprintf("%s  %s", stFaint.Render(date.Format("Mon 02")), strings.Join(parts, "  ")))
	}
	if len(rows) == 0 {
		return stFaint.Render("(no working days scheduled)")
	}
	return strings.Join(rows, "\n")
}

func indentLines(s string, n int) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}
