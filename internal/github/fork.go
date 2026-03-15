package github

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ngavilan-dogfy/woffuk-cli/internal/config"
)

const upstreamRepo = "ngavilan-dogfy/woffuk-cli"

// ForkAndSetup forks the upstream repo, sets secrets, generates workflows, and pushes.
func ForkAndSetup(cfg *config.Config, password string) (string, error) {
	username, err := getGitHubUsername()
	if err != nil {
		return "", err
	}
	forkName := username + "/woffuk-cli"

	// Fork the repo (may already exist)
	_ = ghRun("repo", "fork", upstreamRepo, "--clone=false")

	// Set secrets
	if err := setSecrets(forkName, cfg, password); err != nil {
		return "", err
	}

	// Enable GitHub Actions
	enableActions(forkName)

	// Clone, generate workflows, and push
	if err := pushWorkflows(forkName, cfg); err != nil {
		return "", fmt.Errorf("push workflows: %w", err)
	}

	return forkName, nil
}

// SyncSecrets re-syncs all GitHub secrets from the current config.
func SyncSecrets(cfg *config.Config, password string) error {
	if cfg.GithubFork == "" {
		return fmt.Errorf("no github fork configured — run 'woffuk setup' first")
	}
	return setSecrets(cfg.GithubFork, cfg, password)
}

// SyncWorkflows regenerates workflows from config and pushes to the fork.
func SyncWorkflows(cfg *config.Config) error {
	if cfg.GithubFork == "" {
		return fmt.Errorf("no github fork configured — run 'woffuk setup' first")
	}
	return pushWorkflows(cfg.GithubFork, cfg)
}

func setSecrets(repo string, cfg *config.Config, password string) error {
	secrets := map[string]string{
		"WOFFU_URL":            cfg.WoffuURL,
		"WOFFU_COMPANY_URL":    cfg.WoffuCompanyURL,
		"WOFFU_EMAIL":          cfg.WoffuEmail,
		"WOFFU_PASSWORD":       password,
		"WOFFU_LATITUDE":       fmt.Sprintf("%f", cfg.Latitude),
		"WOFFU_LONGITUDE":      fmt.Sprintf("%f", cfg.Longitude),
		"WOFFU_HOME_LATITUDE":  fmt.Sprintf("%f", cfg.HomeLatitude),
		"WOFFU_HOME_LONGITUDE": fmt.Sprintf("%f", cfg.HomeLongitude),
	}

	// Add Telegram secrets if configured
	if cfg.Telegram.BotToken != "" {
		secrets["TELEGRAM_BOT_TOKEN"] = cfg.Telegram.BotToken
		secrets["TELEGRAM_CHAT_ID"] = cfg.Telegram.ChatID
	}

	for name, value := range secrets {
		if err := ghSetSecret(repo, name, value); err != nil {
			return fmt.Errorf("set secret %s: %w", name, err)
		}
	}
	return nil
}

func pushWorkflows(repo string, cfg *config.Config) error {
	// Clone the fork to a temp dir
	tmpDir, err := os.MkdirTemp("", "woffuk-workflows-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	cloneURL := fmt.Sprintf("https://github.com/%s.git", repo)
	if err := runCmd(tmpDir, "gh", "repo", "clone", cloneURL, tmpDir, "--", "--depth=1"); err != nil {
		return fmt.Errorf("clone fork: %w", err)
	}

	// Create .github/workflows directory
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return err
	}

	// Generate and write auto-sign workflow
	autoYAML := GenerateWorkflowYAML(cfg.Schedule, cfg.Timezone)
	if err := os.WriteFile(filepath.Join(workflowDir, "sign.yml"), []byte(autoYAML), 0644); err != nil {
		return err
	}

	// Generate and write manual workflow
	manualYAML := GenerateManualWorkflowYAML()
	if err := os.WriteFile(filepath.Join(workflowDir, "sign-manual.yml"), []byte(manualYAML), 0644); err != nil {
		return err
	}

	// Check if there are changes
	statusOut, _ := cmdOutput(tmpDir, "git", "status", "--porcelain")
	if strings.TrimSpace(statusOut) == "" {
		return nil // No changes
	}

	// Commit and push
	_ = runCmd(tmpDir, "git", "add", ".github/workflows/")
	_ = runCmd(tmpDir, "git", "commit", "-m", "chore: update auto-sign workflows from woffuk-cli")
	if err := runCmd(tmpDir, "git", "push"); err != nil {
		return fmt.Errorf("push workflows: %w", err)
	}

	return nil
}

func enableActions(repo string) {
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf(`echo '{"enabled": true, "allowed_actions": "all"}' | gh api -X PUT repos/%s/actions/permissions --input -`, repo))
	cmd.Run()
}

func getGitHubUsername() (string, error) {
	out, err := ghOutput("api", "user", "-q", ".login")
	if err != nil {
		return "", fmt.Errorf("get github username (is gh authenticated?): %w", err)
	}
	return strings.TrimSpace(out), nil
}

func ghRun(args ...string) error {
	cmd := exec.Command("gh", args...)
	return cmd.Run()
}

func ghOutput(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	out, err := cmd.Output()
	return string(out), err
}

func ghSetSecret(repo, name, value string) error {
	cmd := exec.Command("gh", "secret", "set", name, "-R", repo)
	cmd.Stdin = strings.NewReader(value)
	return cmd.Run()
}

func runCmd(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdOutput(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
