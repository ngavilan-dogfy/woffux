package github

import (
	"fmt"
	"strings"
)

type WorkflowStatus struct {
	Name  string
	State string // "active" or "disabled_manually"
	ID    int
}

// GetAutoSignStatus checks if the auto-sign workflows are enabled on the fork.
func GetAutoSignStatus(repo string) ([]WorkflowStatus, error) {
	out, err := ghOutput("api", fmt.Sprintf("repos/%s/actions/workflows", repo),
		"--jq", ".workflows[] | [.id, .name, .state] | @tsv")
	if err != nil {
		return nil, fmt.Errorf("could not check workflows: %w", err)
	}

	var workflows []WorkflowStatus
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		id := 0
		fmt.Sscanf(parts[0], "%d", &id)
		workflows = append(workflows, WorkflowStatus{
			ID:    id,
			Name:  parts[1],
			State: parts[2],
		})
	}

	return workflows, nil
}

// IsAutoSignEnabled returns true if the Auto Sign workflow is active.
func IsAutoSignEnabled(repo string) (bool, error) {
	workflows, err := GetAutoSignStatus(repo)
	if err != nil {
		return false, err
	}

	for _, w := range workflows {
		if strings.Contains(w.Name, "Auto Sign") || strings.Contains(w.Name, "Auto") {
			return w.State == "active", nil
		}
	}

	return false, nil
}

// EnableAutoSign enables all sign workflows on the fork.
func EnableAutoSign(repo string) error {
	workflows, err := GetAutoSignStatus(repo)
	if err != nil {
		return err
	}

	changed := 0
	for _, w := range workflows {
		if strings.Contains(w.Name, "Sign") || strings.Contains(w.Name, "Keepalive") {
			err := ghRun("api", "-X", "PUT",
				fmt.Sprintf("repos/%s/actions/workflows/%d/enable", repo, w.ID))
			if err != nil {
				return fmt.Errorf("enable %s: %w", w.Name, err)
			}
			changed++
		}
	}

	if changed == 0 {
		return fmt.Errorf("no sign workflows found on %s — run 'woffuk setup' first", repo)
	}

	return nil
}

// DisableAutoSign disables all sign workflows on the fork.
func DisableAutoSign(repo string) error {
	workflows, err := GetAutoSignStatus(repo)
	if err != nil {
		return err
	}

	changed := 0
	for _, w := range workflows {
		if strings.Contains(w.Name, "Sign") || strings.Contains(w.Name, "Keepalive") {
			err := ghRun("api", "-X", "PUT",
				fmt.Sprintf("repos/%s/actions/workflows/%d/disable", repo, w.ID))
			if err != nil {
				return fmt.Errorf("disable %s: %w", w.Name, err)
			}
			changed++
		}
	}

	if changed == 0 {
		return fmt.Errorf("no sign workflows found on %s — run 'woffuk setup' first", repo)
	}

	return nil
}
