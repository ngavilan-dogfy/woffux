package cmd

import (
	"bufio"
	"fmt"
	"github.com/ngavilan-dogfy/woffux/internal/timing"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ngavilan-dogfy/woffux/internal/config"
	gh "github.com/ngavilan-dogfy/woffux/internal/github"
	"github.com/ngavilan-dogfy/woffux/internal/notify"
	"github.com/ngavilan-dogfy/woffux/internal/woffu"
)

var signForce bool
var signExpected string
var signScheduled bool
var signCatchUpSpec string
var signCatchUpTimezone string
var signCatchUpWindow time.Duration
var signNoVerify bool
var signLookahead time.Duration
var signSeasons []string

// seasonalSpec picks today's catch-up spec from --season values
// ("MM-DD..MM-DD=<spec>", ranges may wrap the new year), falling back to
// base. This keeps the GitHub fallback on the same dated schedule as the
// local agent.
func seasonalSpec(base string, seasons []string, now time.Time) string {
	md := now.Format("01-02")
	for _, raw := range seasons {
		rng, spec, ok := strings.Cut(raw, "=")
		from, to, ok2 := strings.Cut(rng, "..")
		if !ok || !ok2 {
			continue
		}
		in := md >= from && md <= to
		if from > to {
			in = md >= from || md <= to
		}
		if in {
			return spec
		}
	}
	return base
}

// signerGrace delays the GitHub fallback a few minutes past each natural
// moment so the local agent, when the Mac is awake, always signs first and
// the fallback then finds the event satisfied.
func signerGrace() time.Duration {
	if strings.EqualFold(os.Getenv("WOFFUX_SIGNER"), "github") {
		return 3 * time.Minute
	}
	return 0
}

// waitUntil sleeps until t by wall clock, in short steps so a Mac that
// sleeps and wakes up mid-wait still signs at the right moment.
func waitUntil(t time.Time) {
	for {
		d := time.Until(t)
		if d <= 0 {
			return
		}
		time.Sleep(min(d, 20*time.Second))
	}
}

type catchUpEvent struct {
	day    int
	minute int
	action woffu.SignAction
	label  string
}

var signCmd = &cobra.Command{
	Use:   "sign",
	Short: "Clock in/out on Woffu (works locally and in CI)",
	Long: `Clock in/out on Woffu. Checks calendar first and only signs on working days.

Examples:
  woffux sign                    Sign for today (toggle in/out)
  woffux sign --force            Sign even if not a working day
  woffux sign --expected in      Only sign IN (skip if already signed in)
  woffux sign --expected out     Only sign OUT (skip if already signed out)

The --expected flag prevents the auto-sign from toggling you in the wrong
direction when you've already signed manually.

Batch (from stdin):
  echo "sign" | woffux sign

In CI, reads credentials from environment variables.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		expectedAction, err := parseExpectedSignAction(signExpected)
		if err != nil {
			return err
		}

		cfg, password, err := config.LoadOrEnv()
		if err != nil {
			return err
		}

		// --scheduled: catch-up against the local config's schedule. Unlike
		// the baked --catch-up spec in the GitHub workflow, this always
		// follows the currently active schedule/preset.
		if signScheduled {
			if signCatchUpSpec == "" {
				signCatchUpSpec = gh.CatchUpSpec(cfg.Schedule)
			}
			if signCatchUpTimezone == "" {
				signCatchUpTimezone = cfg.Timezone
			}
			if signCatchUpSpec == "" {
				if isTTY() {
					fmt.Println("No scheduled sign times configured — skipping.")
				}
				return nil
			}
		}

		if len(signSeasons) > 0 && strings.TrimSpace(signCatchUpSpec) != "" {
			loc := catchUpLocation(signCatchUpTimezone, cfg.Timezone)
			signCatchUpSpec = seasonalSpec(signCatchUpSpec, signSeasons, time.Now().In(loc))
		}

		// Cheap pre-auth skip: in catch-up mode, if no event for today could
		// possibly be overdue yet, exit without touching the network.
		if strings.TrimSpace(signCatchUpSpec) != "" {
			loc := catchUpLocation(signCatchUpTimezone, cfg.Timezone)
			due, err := anyCatchUpEventDue(signCatchUpSpec, time.Now().In(loc), catchUpWindowOrDefault(signCatchUpWindow), cfg.Timing, signLookahead+signerGrace())
			if err != nil {
				return err
			}
			if !due {
				if isTTY() {
					fmt.Println("No scheduled sign due now — skipping.")
				} else {
					fmt.Printf("SKIP %s no scheduled sign due now\n", time.Now().In(loc).Format("2006-01-02 15:04"))
				}
				return nil
			}
		}

		client := woffu.NewWoffuClient(cfg.WoffuURL)
		companyClient := woffu.NewCompanyClient(cfg.WoffuCompanyURL)

		if isTTY() {
			fmt.Println("Authenticating...")
		}
		token, err := woffu.AuthenticateCached(client, companyClient, cfg.WoffuEmail, password)
		if err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}

		if isTTY() {
			fmt.Println("Checking calendar...")
		}
		info, err := woffu.GetSignInfo(companyClient, token, cfg.Latitude, cfg.Longitude, cfg.HomeLatitude, cfg.HomeLongitude)
		if err != nil {
			return fmt.Errorf("get sign info: %w", err)
		}

		telegramCfg := notify.TelegramConfig{
			BotToken: cfg.Telegram.BotToken,
			ChatID:   cfg.Telegram.ChatID,
		}

		if !info.IsWorkingDay && !signForce {
			if isTTY() {
				fmt.Println("Not a working day — skipping.")
			} else {
				fmt.Printf("SKIP %s not a working day\n", info.Date)
			}
			_ = notify.SendSkippedNotification(telegramCfg, info.Date, "Not a working day")
			return nil
		}

		var slots []woffu.SignSlot
		slotsLoaded := false
		loadSlots := func() error {
			if slotsLoaded {
				return nil
			}
			var slotsErr error
			slots, slotsErr = woffu.GetTodaySlots(companyClient, token)
			if slotsErr != nil {
				return fmt.Errorf("get slots: %w", slotsErr)
			}
			slotsLoaded = true
			return nil
		}

		if strings.TrimSpace(signCatchUpSpec) != "" {
			if err := loadSlots(); err != nil {
				return err
			}
			loc := catchUpLocation(signCatchUpTimezone, cfg.Timezone)
			window := catchUpWindowOrDefault(signCatchUpWindow)
			grace := signerGrace()
			var plan catchUpPlan
			for attempt := 0; ; attempt++ {
				plan, err = planCatchUp(signCatchUpSpec, slots, time.Now().In(loc), window, cfg.Timing, signLookahead, grace)
				if err != nil {
					return err
				}
				if !plan.ok || !plan.at.After(time.Now()) || attempt >= 3 {
					break
				}
				// The natural moment is a few minutes away: wait for it,
				// then re-check (a sign may have happened meanwhile).
				if isTTY() {
					fmt.Printf("Waiting until %s to sign %s (scheduled %s)…\n", plan.at.Format("15:04:05"), plan.action, plan.label)
				} else {
					fmt.Printf("WAIT %s until %s for %s %s\n", info.Date, plan.at.Format("15:04:05"), plan.label, plan.action)
				}
				waitUntil(plan.at)
				slotsLoaded = false
				if err := loadSlots(); err != nil {
					return err
				}
			}
			action, matchedTime, ok := plan.action, plan.label, plan.ok && !plan.at.After(time.Now())
			if !ok {
				reason := "No overdue scheduled sign"
				if isTTY() {
					fmt.Printf("%s — skipping.\n", reason)
				} else {
					fmt.Printf("SKIP %s %s\n", info.Date, reason)
				}
				_ = notify.SendSkippedNotification(telegramCfg, info.Date, reason)
				return nil
			}
			expectedAction = action
			if isTTY() {
				fmt.Printf("Catch-up matched %s %s.\n", matchedTime, action)
			}
		}

		// Guard: check if sign should be skipped based on expected action
		if expectedAction != "" {
			if err := loadSlots(); err != nil {
				return err
			}
			if woffu.ShouldSkipSign(slots, expectedAction) {
				reason := fmt.Sprintf("Already signed %s", expectedAction)
				if isTTY() {
					fmt.Printf("%s — skipping.\n", reason)
				} else {
					fmt.Printf("SKIP %s %s\n", info.Date, reason)
				}
				_ = notify.SendSkippedNotification(telegramCfg, info.Date, reason)
				return nil
			}
		}

		if isTTY() {
			fmt.Printf("%s %s — signing with coordinates (%.4f, %.4f)\n",
				info.Mode.Emoji(), info.Mode.Label(), info.Latitude, info.Longitude)
		}

		// Snapshot state before signing so the result can be verified.
		var wasSignedIn bool
		verify := !signNoVerify
		if verify {
			if err := loadSlots(); err != nil {
				verify = false
			} else {
				wasSignedIn = woffu.IsSignedIn(slots)
			}
		}

		err = woffu.DoSign(companyClient, token, info.Latitude, info.Longitude)
		if err != nil {
			_ = notify.SendFailedNotification(telegramCfg, info.Date, fmt.Sprintf("Sign request failed: %s", err))
			return fmt.Errorf("sign failed: %w", err)
		}

		// Verify the sign actually registered: the in/out state must have
		// flipped. Woffu occasionally accepts the POST without recording it.
		if verify {
			if err := woffu.VerifySignRegistered(companyClient, token, wasSignedIn); err != nil {
				_ = notify.SendFailedNotification(telegramCfg, info.Date, err.Error())
				return fmt.Errorf("sign NOT verified: %w", err)
			}
		}

		if isTTY() {
			if verify {
				fmt.Println("Signed and verified!")
			} else {
				fmt.Println("Signed successfully!")
			}
		} else {
			fmt.Printf("OK %s %s %s\n", info.Date, info.Mode, info.Mode.Label())
		}

		if err := notify.SendSignedNotification(telegramCfg, info); err != nil && isTTY() {
			fmt.Printf("Warning: telegram notification failed: %s\n", err)
		}

		return nil
	},
}

// catchUpLocation resolves the timezone for catch-up scheduling.
func catchUpLocation(tz, fallback string) *time.Location {
	name := strings.TrimSpace(tz)
	if name == "" {
		name = strings.TrimSpace(fallback)
	}
	if loc, err := time.LoadLocation(name); err == nil && name != "" {
		return loc
	}
	return time.Local
}

func catchUpWindowOrDefault(window time.Duration) time.Duration {
	if window <= 0 {
		return 2 * time.Hour
	}
	return window
}

// anyCatchUpEventDue reports whether at least one scheduled event for today
// is inside the catch-up window, regardless of sign state. Used to skip the
// network round-trips entirely when nothing can possibly be due.
func anyCatchUpEventDue(spec string, now time.Time, window time.Duration, tm timing.Settings, lookahead time.Duration) (bool, error) {
	events, err := parseCatchUpSpec(spec)
	if err != nil {
		return false, err
	}
	_, moments := todaysMoments(events, now, tm)
	for _, at := range moments {
		if d := now.Sub(at); (d >= 0 && d <= window) || (d < 0 && -d <= lookahead) {
			return true, nil
		}
	}
	return false, nil
}

func init() {
	signCmd.Flags().BoolVar(&signForce, "force", false, "Sign even if not a working day")
	signCmd.Flags().StringVar(&signExpected, "expected", "", "Expected sign action: 'in' or 'out'. Skips if already in that state.")
	signCmd.Flags().StringVar(&signCatchUpSpec, "catch-up", "", "Scheduled catch-up spec: day:HH:MM:action entries separated by semicolons.")
	signCmd.Flags().StringVar(&signCatchUpTimezone, "catch-up-timezone", "", "Timezone used to resolve catch-up schedules.")
	signCmd.Flags().DurationVar(&signCatchUpWindow, "catch-up-window", 2*time.Hour, "How long after a scheduled sign catch-up may run.")
	signCmd.Flags().BoolVar(&signScheduled, "scheduled", false, "Catch-up mode against the local config's active schedule (used by the local agent).")
	signCmd.Flags().BoolVar(&signNoVerify, "no-verify", false, "Skip post-sign verification.")
	signCmd.Flags().StringArrayVar(&signSeasons, "season", nil, "Seasonal catch-up spec \"MM-DD..MM-DD=<spec>\" used instead of --catch-up between those dates (repeatable).")
	signCmd.Flags().DurationVar(&signLookahead, "lookahead", 20*time.Minute, "In scheduled/catch-up mode, wait for a sign whose natural moment is at most this far ahead.")
}

func parseExpectedSignAction(value string) (woffu.SignAction, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return "", nil
	case string(woffu.SignActionIn):
		return woffu.SignActionIn, nil
	case string(woffu.SignActionOut):
		return woffu.SignActionOut, nil
	default:
		return "", fmt.Errorf("invalid --expected value %q (use \"in\" or \"out\")", value)
	}
}

func resolveCatchUpSignAction(spec string, slots []woffu.SignSlot, now time.Time, window time.Duration) (woffu.SignAction, string, bool, error) {
	plan, err := planCatchUp(spec, slots, now, window, timing.Settings{}, 0, 0)
	if err != nil || !plan.ok || plan.at.After(now) {
		return "", "", false, err
	}
	return plan.action, plan.label, true, nil
}

// catchUpPlan is the sign a scheduled run should make, and when.
type catchUpPlan struct {
	action woffu.SignAction
	label  string    // scheduled "HH:MM"
	at     time.Time // the natural moment to sign (may be in the future)
	ok     bool
}

// planCatchUp picks the sign a scheduled run should make. With natural
// timing each event happens at its own moment (see internal/timing); an
// event whose moment is still ahead but within lookahead is returned so the
// caller can wait for it. grace delays every moment (the GitHub fallback
// waits a little so the local agent, when awake, always goes first).
func planCatchUp(spec string, slots []woffu.SignSlot, now time.Time, window time.Duration, tm timing.Settings, lookahead, grace time.Duration) (catchUpPlan, error) {
	events, err := parseCatchUpSpec(spec)
	if err != nil {
		return catchUpPlan{}, err
	}
	if window <= 0 {
		return catchUpPlan{}, fmt.Errorf("catch-up window must be positive")
	}
	want := woffu.SignActionIn
	if woffu.IsSignedIn(slots) {
		want = woffu.SignActionOut
	}
	today, moments := todaysMoments(events, now, tm)
	slack := catchUpSatisfiedSlackMinutes + tm.MaxEarly()

	var due, ahead catchUpPlan
	for i, event := range today {
		if event.action != want || catchUpEventSatisfied(slots, event, slack) {
			continue
		}
		at := moments[i].Add(grace)
		switch {
		case !at.After(now) && now.Sub(at) <= window:
			if !due.ok || at.After(due.at) { // most recent overdue wins
				due = catchUpPlan{event.action, event.label, at, true}
			}
		case at.After(now) && at.Sub(now) <= lookahead:
			if !ahead.ok || at.Before(ahead.at) {
				ahead = catchUpPlan{event.action, event.label, at, true}
			}
		}
	}
	if due.ok {
		return due, nil
	}
	return ahead, nil
}

// todaysMoments returns today's events in order with their natural moments.
func todaysMoments(events []catchUpEvent, now time.Time, tm timing.Settings) ([]catchUpEvent, []time.Time) {
	day := int(now.Weekday())
	if day == 0 {
		day = 7
	}
	var today []catchUpEvent
	for _, e := range events {
		if e.day == day {
			today = append(today, e)
		}
	}
	sort.SliceStable(today, func(i, j int) bool { return today[i].minute < today[j].minute })
	tev := make([]timing.Event, len(today))
	for i, e := range today {
		tev[i] = timing.Event{Minute: e.minute, In: e.action == woffu.SignActionIn}
	}
	return today, tm.Targets(now, tev)
}

// catchUpSatisfiedSlackMinutes tolerates signs made slightly before their
// scheduled time (e.g. a manual early sign) still counting as that event.
const catchUpSatisfiedSlackMinutes = 5

// catchUpEventSatisfied reports whether a scheduled event already has a
// matching sign today at (or after) its scheduled time. Without this check,
// duplicate DST-offset crons and the 15-minute watchdog re-match events that
// already happened and toggle the user in/out repeatedly.
func catchUpEventSatisfied(slots []woffu.SignSlot, event catchUpEvent, slack int) bool {
	for _, slot := range slots {
		var stamp string
		switch event.action {
		case woffu.SignActionIn:
			stamp = slot.In
		case woffu.SignActionOut:
			stamp = slot.Out
		}
		minute, ok := slotMinuteOfDay(stamp)
		if !ok {
			continue
		}
		if minute >= event.minute-slack {
			return true
		}
	}
	return false
}

// slotMinuteOfDay parses a Woffu local timestamp ("2026-06-10T08:46:52" or
// with milliseconds) and returns the minute of day.
func slotMinuteOfDay(stamp string) (int, bool) {
	if stamp == "" {
		return 0, false
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, stamp); err == nil {
			return t.Hour()*60 + t.Minute(), true
		}
	}
	return 0, false
}

func parseCatchUpSpec(spec string) ([]catchUpEvent, error) {
	var events []catchUpEvent
	for _, raw := range strings.Split(spec, ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, ":")
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid catch-up entry %q", raw)
		}
		day, err := strconv.Atoi(parts[0])
		if err != nil || day < 1 || day > 7 {
			return nil, fmt.Errorf("invalid catch-up day %q", parts[0])
		}
		hour, err := strconv.Atoi(parts[1])
		if err != nil || hour < 0 || hour > 23 {
			return nil, fmt.Errorf("invalid catch-up hour %q", parts[1])
		}
		minute, err := strconv.Atoi(parts[2])
		if err != nil || minute < 0 || minute > 59 {
			return nil, fmt.Errorf("invalid catch-up minute %q", parts[2])
		}
		action, err := parseExpectedSignAction(parts[3])
		if err != nil {
			return nil, err
		}
		events = append(events, catchUpEvent{
			day:    day,
			minute: hour*60 + minute,
			action: action,
			label:  fmt.Sprintf("%02d:%02d", hour, minute),
		})
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("catch-up spec cannot be empty")
	}
	return events, nil
}

// readStdinLines reads non-empty lines from stdin (for batch piping).
func readStdinLines() []string {
	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
