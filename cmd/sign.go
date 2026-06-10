package cmd

import (
	"bufio"
	"fmt"
	"os"
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

		// Cheap pre-auth skip: in catch-up mode, if no event for today could
		// possibly be overdue yet, exit without touching the network.
		if strings.TrimSpace(signCatchUpSpec) != "" {
			loc := catchUpLocation(signCatchUpTimezone, cfg.Timezone)
			due, err := anyCatchUpEventDue(signCatchUpSpec, time.Now().In(loc), catchUpWindowOrDefault(signCatchUpWindow))
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
			action, matchedTime, ok, err := resolveCatchUpSignAction(signCatchUpSpec, slots, time.Now().In(loc), window)
			if err != nil {
				return err
			}
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
func anyCatchUpEventDue(spec string, now time.Time, window time.Duration) (bool, error) {
	events, err := parseCatchUpSpec(spec)
	if err != nil {
		return false, err
	}
	day := int(now.Weekday())
	if day == 0 {
		day = 7
	}
	nowMinute := now.Hour()*60 + now.Minute()
	for _, event := range events {
		if event.day != day {
			continue
		}
		delta := nowMinute - event.minute
		if delta >= 0 && time.Duration(delta)*time.Minute <= window {
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
	events, err := parseCatchUpSpec(spec)
	if err != nil {
		return "", "", false, err
	}
	if window <= 0 {
		return "", "", false, fmt.Errorf("catch-up window must be positive")
	}

	want := woffu.SignActionIn
	if woffu.IsSignedIn(slots) {
		want = woffu.SignActionOut
	}

	day := int(now.Weekday())
	if day == 0 {
		day = 7
	}
	nowMinute := now.Hour()*60 + now.Minute()
	bestDelta := int(window/time.Minute) + 1
	var best catchUpEvent
	found := false

	for _, event := range events {
		if event.day != day || event.action != want {
			continue
		}
		delta := nowMinute - event.minute
		if delta < 0 || time.Duration(delta)*time.Minute > window {
			continue
		}
		if catchUpEventSatisfied(slots, event) {
			continue
		}
		if delta < bestDelta {
			bestDelta = delta
			best = event
			found = true
		}
	}

	if !found {
		return "", "", false, nil
	}
	return best.action, best.label, true, nil
}

// catchUpSatisfiedSlackMinutes tolerates signs made slightly before their
// scheduled time (e.g. a manual early sign) still counting as that event.
const catchUpSatisfiedSlackMinutes = 5

// catchUpEventSatisfied reports whether a scheduled event already has a
// matching sign today at (or after) its scheduled time. Without this check,
// duplicate DST-offset crons and the 15-minute watchdog re-match events that
// already happened and toggle the user in/out repeatedly.
func catchUpEventSatisfied(slots []woffu.SignSlot, event catchUpEvent) bool {
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
		if minute >= event.minute-catchUpSatisfiedSlackMinutes {
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
