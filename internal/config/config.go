package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

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
	WoffuURL        string  `yaml:"woffu_url"`
	WoffuCompanyURL string  `yaml:"woffu_company_url"`
	WoffuEmail      string  `yaml:"woffu_email"`
	Latitude        float64 `yaml:"latitude"`
	Longitude       float64 `yaml:"longitude"`
	HomeLatitude    float64 `yaml:"home_latitude"`
	HomeLongitude   float64 `yaml:"home_longitude"`
	GithubFork      string  `yaml:"github_fork,omitempty"`
	Timezone        string  `yaml:"timezone"`
	Schedule        Schedule       `yaml:"schedule"`
	Telegram        TelegramConfig `yaml:"telegram,omitempty"`
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
	return filepath.Join(home, ".woffuk.yaml"), nil
}

func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found — run 'woffuk setup' first")
		}
		return nil, fmt.Errorf("cannot read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

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
		return nil, "", fmt.Errorf("config not found and WOFFU_* env vars not set — run 'woffuk setup' or set environment variables")
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

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	return nil
}
