package github

import (
	"fmt"
	"strings"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

type WorkflowStatus struct {
	Name  string
	State string // "active" or "disabled_manually"
	ID    int
}

const autoSignWorkflowName = "Auto Sign"

// GetAutoSignStatus checks if the auto-sign workflows are enabled on the fork.
func GetAutoSignStatus(repo string) ([]WorkflowStatus, error) {
	token, err := tokenForRepo(repo)
	if err != nil {
		return nil, err
	}

	out, err := ghOutputWithToken(token, "api", fmt.Sprintf("repos/%s/actions/workflows", repo),
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
	enabled, _ := autoSignEnabledFromWorkflows(workflows)
	return enabled, nil
}

func autoSignEnabledFromWorkflows(workflows []WorkflowStatus) (enabled bool, found bool) {
	for _, w := range workflows {
		if w.Name == autoSignWorkflowName {
			return w.State == "active", true
		}
	}

	return false, false
}

// LastScheduledRun returns when the Auto Sign workflow last ran on a
// schedule trigger and its conclusion ("success", "failure", ...).
// Returns ok=false when there are no scheduled runs.
func LastScheduledRun(repo string) (createdAt string, conclusion string, ok bool, err error) {
	token, err := tokenForRepo(repo)
	if err != nil {
		return "", "", false, err
	}

	out, err := ghOutputWithToken(token, "api",
		fmt.Sprintf("repos/%s/actions/workflows/sign.yml/runs?event=schedule&per_page=1", repo),
		"--jq", `.workflow_runs[0] | "\(.created_at)\t\(.conclusion)"`)
	if err != nil {
		return "", "", false, fmt.Errorf("could not check last run: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" || out == "null\tnull" {
		return "", "", false, nil
	}
	parts := strings.SplitN(out, "\t", 2)
	if len(parts) != 2 {
		return "", "", false, nil
	}
	return parts[0], parts[1], true, nil
}

// EnableAutoSign enables all sign workflows on the fork.
func EnableAutoSign(repo string) error {
	token, err := tokenForRepo(repo)
	if err != nil {
		return err
	}

	workflows, err := GetAutoSignStatus(repo)
	if err != nil {
		return err
	}

	changed := 0
	for _, w := range workflows {
		if isAutoManagedWorkflow(w.Name) {
			err := ghRunWithToken(token, "api", "-X", "PUT",
				fmt.Sprintf("repos/%s/actions/workflows/%d/enable", repo, w.ID))
			if err != nil {
				return fmt.Errorf("enable %s: %w", w.Name, err)
			}
			changed++
		}
	}

	if changed == 0 {
		return fmt.Errorf("no sign workflows found on %s — run 'woffux setup' first", repo)
	}

	return nil
}

// ReloadAutoSign forces GitHub to re-register cron triggers by disabling
// and re-enabling only the Auto Sign workflow. This is needed after updating
// the workflow file, as GitHub may not pick up new cron schedules otherwise.
func ReloadAutoSign(repo string) error {
	token, err := tokenForRepo(repo)
	if err != nil {
		return err
	}

	workflows, err := GetAutoSignStatus(repo)
	if err != nil {
		return err
	}

	for _, w := range workflows {
		if w.Name == autoSignWorkflowName {
			// Disable
			if err := ghRunWithToken(token, "api", "-X", "PUT",
				fmt.Sprintf("repos/%s/actions/workflows/%d/disable", repo, w.ID)); err != nil {
				return fmt.Errorf("disable Auto Sign: %w", err)
			}
			// Re-enable
			if err := ghRunWithToken(token, "api", "-X", "PUT",
				fmt.Sprintf("repos/%s/actions/workflows/%d/enable", repo, w.ID)); err != nil {
				return fmt.Errorf("enable Auto Sign: %w", err)
			}
			return nil
		}
	}

	return fmt.Errorf("Auto Sign workflow not found on %s", repo)
}

// ReloadAutoSignIfEnabled refreshes GitHub's cron registration without
// changing a user's disabled auto-sign setting.
func ReloadAutoSignIfEnabled(repo string) (bool, error) {
	workflows, err := GetAutoSignStatus(repo)
	if err != nil {
		return false, err
	}
	enabled, found := autoSignEnabledFromWorkflows(workflows)
	if !found {
		return false, fmt.Errorf("Auto Sign workflow not found on %s", repo)
	}
	if !enabled {
		return false, nil
	}
	if err := ReloadAutoSign(repo); err != nil {
		return false, err
	}
	return true, nil
}

// SyncWorkflowsAndRefresh updates workflow files and refreshes cron triggers
// only when auto-sign is already enabled.
func SyncWorkflowsAndRefresh(cfg *config.Config) (bool, error) {
	if err := SyncWorkflows(cfg); err != nil {
		return false, err
	}
	return ReloadAutoSignIfEnabled(cfg.GithubFork)
}

// EnableAndRefreshAutoSign syncs workflow definitions, enables scheduled
// signing, and forces GitHub to register the latest cron entries.
func EnableAndRefreshAutoSign(cfg *config.Config) error {
	if cfg.GithubFork == "" {
		return fmt.Errorf("no github fork configured — run 'woffux setup' first")
	}
	if err := SyncWorkflows(cfg); err != nil {
		return fmt.Errorf("sync workflows: %w", err)
	}
	if err := EnableAutoSign(cfg.GithubFork); err != nil {
		return err
	}
	if err := ReloadAutoSign(cfg.GithubFork); err != nil {
		return fmt.Errorf("reload auto-sign: %w", err)
	}
	inSync, err := CheckWorkflowSync(cfg.GithubFork, cfg)
	if err != nil {
		return fmt.Errorf("verify workflow sync: %w", err)
	}
	if !inSync {
		return fmt.Errorf("schedule still out of sync after refreshing workflows")
	}
	return nil
}

// DisableAutoSign disables all sign workflows on the fork.
func DisableAutoSign(repo string) error {
	token, err := tokenForRepo(repo)
	if err != nil {
		return err
	}

	workflows, err := GetAutoSignStatus(repo)
	if err != nil {
		return err
	}

	changed := 0
	for _, w := range workflows {
		if isAutoManagedWorkflow(w.Name) {
			err := ghRunWithToken(token, "api", "-X", "PUT",
				fmt.Sprintf("repos/%s/actions/workflows/%d/disable", repo, w.ID))
			if err != nil {
				return fmt.Errorf("disable %s: %w", w.Name, err)
			}
			changed++
		}
	}

	if changed == 0 {
		return fmt.Errorf("no sign workflows found on %s — run 'woffux setup' first", repo)
	}

	return nil
}

func isAutoManagedWorkflow(name string) bool {
	return strings.Contains(name, "Auto Sign") || strings.Contains(name, "Keepalive")
}
