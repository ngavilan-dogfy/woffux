package tui

import (
	"testing"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

func TestGetActionsSortsPresetNames(t *testing.T) {
	d := &Dashboard{
		cfg: &config.Config{
			SavedSchedules: map[string]config.Schedule{
				"zeta":  {},
				"alpha": {},
			},
		},
	}

	actions := d.getActions()
	var presetKeys []string
	for _, action := range actions {
		if len(action.key) > len("preset:") && action.key[:len("preset:")] == "preset:" {
			presetKeys = append(presetKeys, action.key)
		}
	}

	if len(presetKeys) != 2 || presetKeys[0] != "preset:alpha" || presetKeys[1] != "preset:zeta" {
		t.Fatalf("unexpected preset action order: %#v", presetKeys)
	}
}
