// Package timing decides the exact moment each scheduled sign happens.
//
// Signing at 08:30:00 every single day looks exactly like what it is: a
// robot. Natural timing moves every sign to a slightly different moment
// inside a window around its scheduled time, with two rules that keep HR
// happy:
//
//  1. The windows lean the safe way: clock IN a bit early rather than late,
//     clock OUT a bit late rather than early.
//  2. Each work block is never shorter than planned: a block's OUT is
//     moved at least as much as its IN.
//
// Offsets are derived from a private per-install seed and the date, so they
// look random day to day but every signer (this Mac, GitHub, the TUI)
// computes the very same moment. That is what prevents double signs.
package timing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Settings are the natural-timing windows, in minutes.
type Settings struct {
	InEarly  int    `yaml:"in_early,omitempty"`  // may clock IN up to N min before
	InLate   int    `yaml:"in_late,omitempty"`   // ...or up to N min after
	OutEarly int    `yaml:"out_early,omitempty"` // may clock OUT up to N min before
	OutLate  int    `yaml:"out_late,omitempty"`  // ...or up to N min after
	Seed     string `yaml:"seed,omitempty"`      // private; makes offsets unpredictable
}

// Preset is a named, ready-made set of windows.
type Preset struct {
	Key         string
	Name        string
	Description string
	Settings    Settings
}

// Presets offered in setup and settings, safest first.
var Presets = []Preset{
	{"natural", "Natural (recommended)", "IN 0–6 min early · OUT 0–8 min late", Settings{InEarly: 6, InLate: 1, OutLate: 8}},
	{"relaxed", "Relaxed", "IN 0–12 min early · OUT 0–15 min late", Settings{InEarly: 12, InLate: 2, OutLate: 15}},
	{"exact", "Exact", "sign at the scheduled minute every day", Settings{}},
}

// maxWindow caps any window: beyond this, "natural" stops being natural.
const maxWindow = 30

// Active reports whether any window is open.
func (s Settings) Active() bool {
	return s.InEarly > 0 || s.InLate > 0 || s.OutEarly > 0 || s.OutLate > 0
}

// Normalize clamps windows to [0, maxWindow].
func (s *Settings) Normalize() {
	clamp := func(v *int) {
		if *v < 0 {
			*v = 0
		}
		if *v > maxWindow {
			*v = maxWindow
		}
	}
	clamp(&s.InEarly)
	clamp(&s.InLate)
	clamp(&s.OutEarly)
	clamp(&s.OutLate)
}

// WithPreset returns the preset's windows, keeping the current seed (or
// creating one).
func (s Settings) WithPreset(p Preset) Settings {
	out := p.Settings
	out.Seed = s.Seed
	if out.Seed == "" {
		out.Seed = NewSeed()
	}
	return out
}

// PresetKey names the preset these settings match, or "custom".
func (s Settings) PresetKey() string {
	for _, p := range Presets {
		if s.InEarly == p.Settings.InEarly && s.InLate == p.Settings.InLate &&
			s.OutEarly == p.Settings.OutEarly && s.OutLate == p.Settings.OutLate {
			return p.Key
		}
	}
	return "custom"
}

// Describe renders the windows for humans: "IN −6…+1 min · OUT +0…+8 min".
func (s Settings) Describe() string {
	if !s.Active() {
		return "exact: signs at the scheduled minute"
	}
	win := func(early, late int) string {
		switch {
		case early == 0 && late == 0:
			return "on time"
		case early == 0:
			return fmt.Sprintf("0–%d min late", late)
		case late == 0:
			return fmt.Sprintf("0–%d min early", early)
		}
		return fmt.Sprintf("%d min early to %d late", early, late)
	}
	return "IN " + win(s.InEarly, s.InLate) + " · OUT " + win(s.OutEarly, s.OutLate)
}

// NewSeed returns a random private seed.
func NewSeed() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// ── Env encoding (GitHub Actions secret) ──

// Encode packs settings into one string for the WOFFUX_TIMING secret.
func (s Settings) Encode() string {
	if !s.Active() {
		return ""
	}
	return fmt.Sprintf("%d,%d,%d,%d,%s", s.InEarly, s.InLate, s.OutEarly, s.OutLate, s.Seed)
}

// Decode parses Encode's output. An empty string means exact timing.
func Decode(v string) (Settings, error) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "off") {
		return Settings{}, nil
	}
	parts := strings.Split(v, ",")
	if len(parts) != 5 {
		return Settings{}, fmt.Errorf("invalid timing %q", v)
	}
	var n [4]int
	for i := 0; i < 4; i++ {
		x, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return Settings{}, fmt.Errorf("invalid timing %q", v)
		}
		n[i] = x
	}
	s := Settings{InEarly: n[0], InLate: n[1], OutEarly: n[2], OutLate: n[3], Seed: strings.TrimSpace(parts[4])}
	s.Normalize()
	return s, nil
}

// ── Offsets ──

// Event is one scheduled sign of a day, in order.
type Event struct {
	Minute int // minutes from midnight
	In     bool
}

// Targets returns the actual moment for each of the day's events, in the
// location of date. Events must be in chronological order.
func (s Settings) Targets(date time.Time, events []Event) []time.Time {
	loc := date.Location()
	day := date.Format("2006-01-02")

	offsets := make([]int, len(events)) // seconds
	for i, e := range events {
		early, late := s.InEarly, s.InLate
		if !e.In {
			early, late = s.OutEarly, s.OutLate
		}
		offsets[i] = s.offsetSeconds(day, i, early, late)
	}
	// Rule 2: a block (IN i, OUT i+1) never shrinks.
	for i := 0; i+1 < len(events); i++ {
		if events[i].In && !events[i+1].In && offsets[i+1] < offsets[i] {
			hi := s.OutLate * 60
			offsets[i+1] = min(offsets[i], hi)
			if offsets[i+1] < offsets[i] { // OUT window too small: keep IN on time
				offsets[i] = offsets[i+1]
			}
		}
	}

	// Built from the wall-clock time (not midnight + duration) so DST
	// change days stay right, and never past the end of the day: a moment
	// after midnight would belong to the next day and never be signed.
	endOfDay := time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 0, 0, loc)
	out := make([]time.Time, len(events))
	for i, e := range events {
		wall := time.Date(date.Year(), date.Month(), date.Day(), e.Minute/60, e.Minute%60, 0, 0, loc)
		out[i] = wall.Add(time.Duration(offsets[i]) * time.Second)
		// Keep events strictly ordered, at least a minute apart.
		if i > 0 && !out[i].After(out[i-1].Add(time.Minute)) {
			out[i] = out[i-1].Add(time.Minute)
		}
		if out[i].After(endOfDay) {
			out[i] = endOfDay
		}
	}
	return out
}

// offsetSeconds maps (seed, day, index) to [-early, +late] minutes, in
// seconds, with a gentle bell shape (average of two uniforms) so most signs
// land near the middle of the window, like people do.
func (s Settings) offsetSeconds(day string, index, early, late int) int {
	if early == 0 && late == 0 {
		return 0
	}
	h := sha256.Sum256([]byte(s.Seed + "|" + day + "|" + strconv.Itoa(index)))
	a := float64(binary.BigEndian.Uint32(h[0:4])) / float64(^uint32(0))
	b := float64(binary.BigEndian.Uint32(h[4:8])) / float64(^uint32(0))
	u := (a + b) / 2
	lo, hi := -early*60, late*60
	return lo + int(u*float64(hi-lo))
}

// MaxEarly is how far before its scheduled time any sign may happen; used
// to recognise an early sign as satisfying its event.
func (s Settings) MaxEarly() int { return max(s.InEarly, s.OutEarly) }

// MaxLate is how far after its scheduled time any sign may be planned.
func (s Settings) MaxLate() int { return max(s.InLate, s.OutLate) }

// Short is a compact form for tight spaces: "in −6…+1′ · out 0…+8′".
func (s Settings) Short() string {
	if !s.Active() {
		return "exact"
	}
	win := func(early, late int) string {
		lo := "0"
		if early > 0 {
			lo = fmt.Sprintf("−%d", early)
		}
		return fmt.Sprintf("%s…+%d′", lo, late)
	}
	return "in " + win(s.InEarly, s.InLate) + " · out " + win(s.OutEarly, s.OutLate)
}
