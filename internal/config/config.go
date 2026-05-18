package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ScheduleEntry struct {
	Time string `yaml:"time"` // HH:MM in local time
}

type DaySchedule struct {
	Enabled bool            `yaml:"enabled"`
	Times   []ScheduleEntry `yaml:"times"`
}

type Schedule struct {
	Monday    DaySchedule `yaml:"monday"`
	Tuesday   DaySchedule `yaml:"tuesday"`
	Wednesday DaySchedule `yaml:"wednesday"`
	Thursday  DaySchedule `yaml:"thursday"`
	Friday    DaySchedule `yaml:"friday"`
}

type TelegramConfig struct {
	BotToken string `yaml:"bot_token,omitempty"`
	ChatID   string `yaml:"chat_id,omitempty"`
}

type Config struct {
	WoffuURL        string              `yaml:"woffu_url"`
	WoffuCompanyURL string              `yaml:"woffu_company_url"`
	WoffuEmail      string              `yaml:"woffu_email"`
	Latitude        float64             `yaml:"latitude"`
	Longitude       float64             `yaml:"longitude"`
	HomeLatitude    float64             `yaml:"home_latitude"`
	HomeLongitude   float64             `yaml:"home_longitude"`
	GithubFork      string              `yaml:"github_fork,omitempty"`
	Timezone        string              `yaml:"timezone"`
	Schedule        Schedule            `yaml:"schedule"`
	SavedSchedules  map[string]Schedule `yaml:"saved_schedules,omitempty"`
	ActiveSchedule  string              `yaml:"active_schedule,omitempty"`
	Telegram        TelegramConfig      `yaml:"telegram,omitempty"`
	RandomDelaySecs int                 `yaml:"random_delay_secs,omitempty"` // Max random delay before signing (default: 90)
}

// GetRandomDelaySecs returns the configured random delay or the default (90s).
func (c *Config) GetRandomDelaySecs() int {
	if c.RandomDelaySecs > 0 {
		return c.RandomDelaySecs
	}
	return 90
}

// NormalizePresetName trims user input for stable preset lookup and display.
func NormalizePresetName(name string) string {
	return strings.TrimSpace(name)
}

// SaveSchedulePreset saves a schedule with a name.
func (c *Config) SaveSchedulePreset(name string, s Schedule) error {
	name = NormalizePresetName(name)
	if name == "" {
		return fmt.Errorf("preset name cannot be empty")
	}
	if c.SavedSchedules == nil {
		c.SavedSchedules = make(map[string]Schedule)
	}
	c.SavedSchedules[name] = cloneSchedule(s)
	return nil
}

// LoadSchedulePreset applies a saved schedule preset.
func (c *Config) LoadSchedulePreset(name string) bool {
	name = NormalizePresetName(name)
	if s, ok := c.SavedSchedules[name]; ok {
		c.Schedule = cloneSchedule(s)
		c.ActiveSchedule = name
		return true
	}
	return false
}

// SchedulePresetNames returns saved preset names in deterministic order.
func (c *Config) SchedulePresetNames() []string {
	names := make([]string, 0, len(c.SavedSchedules))
	for name := range c.SavedSchedules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Normalize clears stale active preset metadata after manual schedule edits.
func (c *Config) Normalize() {
	c.ActiveSchedule = NormalizePresetName(c.ActiveSchedule)
	if c.ActiveSchedule == "" {
		return
	}
	preset, ok := c.SavedSchedules[c.ActiveSchedule]
	if !ok || !SchedulesEqual(c.Schedule, preset) {
		c.ActiveSchedule = ""
	}
}

func CloneSchedulePresets(src map[string]Schedule) map[string]Schedule {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]Schedule, len(src))
	for name, schedule := range src {
		dst[name] = cloneSchedule(schedule)
	}
	return dst
}

func SchedulesEqual(a, b Schedule) bool {
	return daySchedulesEqual(a.Monday, b.Monday) &&
		daySchedulesEqual(a.Tuesday, b.Tuesday) &&
		daySchedulesEqual(a.Wednesday, b.Wednesday) &&
		daySchedulesEqual(a.Thursday, b.Thursday) &&
		daySchedulesEqual(a.Friday, b.Friday)
}

func cloneSchedule(s Schedule) Schedule {
	return Schedule{
		Monday:    cloneDaySchedule(s.Monday),
		Tuesday:   cloneDaySchedule(s.Tuesday),
		Wednesday: cloneDaySchedule(s.Wednesday),
		Thursday:  cloneDaySchedule(s.Thursday),
		Friday:    cloneDaySchedule(s.Friday),
	}
}

func cloneDaySchedule(d DaySchedule) DaySchedule {
	cp := DaySchedule{Enabled: d.Enabled}
	if len(d.Times) > 0 {
		cp.Times = append([]ScheduleEntry(nil), d.Times...)
	}
	return cp
}

func daySchedulesEqual(a, b DaySchedule) bool {
	if a.Enabled != b.Enabled || len(a.Times) != len(b.Times) {
		return false
	}
	for i := range a.Times {
		if a.Times[i] != b.Times[i] {
			return false
		}
	}
	return true
}

// DefaultSchedule returns the default signing schedule.
func DefaultSchedule() Schedule {
	return Schedule{
		Monday:    DaySchedule{Enabled: true, Times: []ScheduleEntry{{Time: "08:30"}, {Time: "13:30"}, {Time: "14:15"}, {Time: "17:30"}}},
		Tuesday:   DaySchedule{Enabled: true, Times: []ScheduleEntry{{Time: "08:30"}, {Time: "13:30"}, {Time: "14:15"}, {Time: "17:30"}}},
		Wednesday: DaySchedule{Enabled: true, Times: []ScheduleEntry{{Time: "08:30"}, {Time: "13:30"}, {Time: "14:15"}, {Time: "17:30"}}},
		Thursday:  DaySchedule{Enabled: true, Times: []ScheduleEntry{{Time: "08:30"}, {Time: "13:30"}, {Time: "14:15"}, {Time: "17:30"}}},
		Friday:    DaySchedule{Enabled: true, Times: []ScheduleEntry{{Time: "08:00"}, {Time: "15:00"}}},
	}
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}
	return filepath.Join(home, ".woffux.yaml"), nil
}

func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found — run 'woffux setup' first")
		}
		return nil, fmt.Errorf("cannot read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	cfg.Normalize()

	return &cfg, nil
}

// LoadOrEnv loads config from file, falling back to environment variables (for CI).
func LoadOrEnv() (*Config, string, error) {
	cfg, err := Load()
	if err == nil {
		password, err := GetPassword(cfg.WoffuEmail)
		if err != nil {
			return nil, "", fmt.Errorf("cannot get password from keychain: %w", err)
		}
		return cfg, password, nil
	}

	// Fall back to environment variables
	url := os.Getenv("WOFFU_URL")
	companyURL := os.Getenv("WOFFU_COMPANY_URL")
	email := os.Getenv("WOFFU_EMAIL")
	password := os.Getenv("WOFFU_PASSWORD")

	if url == "" || companyURL == "" || email == "" || password == "" {
		return nil, "", fmt.Errorf("config not found and WOFFU_* env vars not set — run 'woffux setup' or set environment variables")
	}

	lat, _ := strconv.ParseFloat(os.Getenv("WOFFU_LATITUDE"), 64)
	lon, _ := strconv.ParseFloat(os.Getenv("WOFFU_LONGITUDE"), 64)
	homeLat, _ := strconv.ParseFloat(os.Getenv("WOFFU_HOME_LATITUDE"), 64)
	homeLon, _ := strconv.ParseFloat(os.Getenv("WOFFU_HOME_LONGITUDE"), 64)

	if lat == 0 && lon == 0 {
		return nil, "", fmt.Errorf("WOFFU_LATITUDE/WOFFU_LONGITUDE not set — configure GPS coordinates")
	}
	if homeLat == 0 && homeLon == 0 {
		return nil, "", fmt.Errorf("WOFFU_HOME_LATITUDE/WOFFU_HOME_LONGITUDE not set — configure home GPS coordinates")
	}

	return &Config{
		WoffuURL:        url,
		WoffuCompanyURL: companyURL,
		WoffuEmail:      email,
		Latitude:        lat,
		Longitude:       lon,
		HomeLatitude:    homeLat,
		HomeLongitude:   homeLon,
		Telegram: TelegramConfig{
			BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
			ChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		},
	}, password, nil
}

func Save(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	cfg.Normalize()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	return nil
}
