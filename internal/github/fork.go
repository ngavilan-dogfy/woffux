package github

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ngavilan-dogfy/woffux/internal/config"
)

const upstreamRepo = "ngavilan-dogfy/woffux"

// ForkAndSetup forks the upstream repo (or uses existing), sets secrets, generates workflows, and pushes.
func ForkAndSetup(cfg *config.Config, password string) (string, error) {
	username, err := getGitHubUsername()
	if err != nil {
		return "", err
	}
	forkName := username + "/woffux"

	// Resolve token for this account
	token, err := tokenForRepo(forkName)
	if err != nil {
		return "", err
	}

	// Check if user already owns the upstream repo (they're the author, not a forker)
	isOwner := forkName == upstreamRepo

	if !isOwner {
		// Check if fork already exists
		existsErr := ghRunWithToken(token, "api", "repos/"+forkName, "--silent")
		if existsErr != nil {
			// Fork doesn't exist, create it
			if err := ghRunWithToken(token, "repo", "fork", upstreamRepo, "--clone=false"); err != nil {
				return "", fmt.Errorf("could not fork repo: %w", err)
			}
		}
	}

	// Set secrets
	if err := setSecrets(forkName, token, cfg, password); err != nil {
		return "", fmt.Errorf("set secrets on %s: %w", forkName, err)
	}

	// Enable GitHub Actions
	enableActions(forkName, token)

	// Generate and push workflows
	if isOwner {
		if err := pushWorkflowsViaAPI(forkName, cfg); err != nil {
			return "", fmt.Errorf("push workflows: %w", err)
		}
	} else {
		if err := pushWorkflows(forkName, cfg); err != nil {
			return "", fmt.Errorf("push workflows: %w", err)
		}
	}

	return forkName, nil
}

// SyncSecrets re-syncs all GitHub secrets from the current config.
func SyncSecrets(cfg *config.Config, password string) error {
	if cfg.GithubFork == "" {
		return fmt.Errorf("no github fork configured — run 'woffux setup' first")
	}
	token, err := tokenForRepo(cfg.GithubFork)
	if err != nil {
		return err
	}
	return setSecrets(cfg.GithubFork, token, cfg, password)
}

// SyncWorkflows regenerates workflows from config and pushes to the fork/repo.
// For the repo owner, uses the GitHub Contents API to avoid conflicts with the
// local working copy. For forks, uses the traditional clone+push approach.
func SyncWorkflows(cfg *config.Config) error {
	if cfg.GithubFork == "" {
		return fmt.Errorf("no github fork configured — run 'woffux setup' first")
	}
	if isRepoOwner(cfg.GithubFork) {
		return pushWorkflowsViaAPI(cfg.GithubFork, cfg)
	}
	return pushWorkflows(cfg.GithubFork, cfg)
}

// isRepoOwner checks if the given repo is the upstream (owner's repo, not a fork).
func isRepoOwner(repo string) bool {
	return repo == upstreamRepo
}

func setSecrets(repo, token string, cfg *config.Config, password string) error {
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
		if err := ghSetSecret(repo, token, name, value); err != nil {
			return fmt.Errorf("set secret %s: %w", name, err)
		}
	}
	return nil
}

// pushWorkflowsViaAPI updates workflow files directly via the GitHub Contents API.
// Used for the repo owner to avoid clone+push conflicts with the local working copy.
func pushWorkflowsViaAPI(repo string, cfg *config.Config) error {
	token, err := tokenForRepo(repo)
	if err != nil {
		return fmt.Errorf("resolve github token for %s: %w", repo, err)
	}

	workflows := map[string]string{
		".github/workflows/sign.yml":        GenerateWorkflowYAML(cfg.Schedule, cfg.Timezone, cfg.GetRandomDelaySecs()),
		".github/workflows/sign-manual.yml": GenerateManualWorkflowYAML(),
		".github/workflows/keepalive.yml":   GenerateKeepaliveWorkflowYAML(),
	}

	for path, content := range workflows {
		if err := putFileViaAPI(repo, token, path, content); err != nil {
			return fmt.Errorf("update %s: %w", path, err)
		}
	}
	return nil
}

// putFileViaAPI creates or updates a file via the GitHub Contents API.
func putFileViaAPI(repo, token, path, content string) error {
	// Get the current file's SHA (required for updates)
	sha := ""
	out, err := ghOutputWithToken(token, "api",
		fmt.Sprintf("repos/%s/contents/%s", repo, path),
		"--jq", ".sha")
	if err == nil {
		sha = strings.TrimSpace(out)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	body := fmt.Sprintf(`{"message":"chore: update %s from woffux","content":"%s"`,
		filepath.Base(path), encoded)
	if sha != "" {
		body += fmt.Sprintf(`,"sha":"%s"`, sha)
	}
	body += "}"

	cmd := exec.Command("gh", "api", "-X", "PUT",
		fmt.Sprintf("repos/%s/contents/%s", repo, path),
		"--input", "-")
	cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	cmd.Stdin = strings.NewReader(body)
	out2, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out2)), err)
	}
	return nil
}

// pushWorkflows clones the repo, generates workflows, and pushes.
// Used for fork users (not the repo owner).
func pushWorkflows(repo string, cfg *config.Config) error {
	token, err := tokenForRepo(repo)
	if err != nil {
		return fmt.Errorf("resolve github token for %s: %w", repo, err)
	}

	// Clone the fork to a temp dir
	tmpDir, err := os.MkdirTemp("", "woffux-workflows-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Clone using HTTPS with the resolved token
	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, repo)
	if err := runCmd(tmpDir, "git", "clone", "--depth=1", cloneURL, tmpDir); err != nil {
		return fmt.Errorf("clone fork: %w", err)
	}

	// Create .github/workflows directory
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return err
	}

	// Generate and write auto-sign workflow
	autoYAML := GenerateWorkflowYAML(cfg.Schedule, cfg.Timezone, cfg.GetRandomDelaySecs())
	if err := os.WriteFile(filepath.Join(workflowDir, "sign.yml"), []byte(autoYAML), 0644); err != nil {
		return err
	}

	// Generate and write manual workflow
	manualYAML := GenerateManualWorkflowYAML()
	if err := os.WriteFile(filepath.Join(workflowDir, "sign-manual.yml"), []byte(manualYAML), 0644); err != nil {
		return err
	}

	// Generate keepalive workflow
	keepaliveYAML := GenerateKeepaliveWorkflowYAML()
	if err := os.WriteFile(filepath.Join(workflowDir, "keepalive.yml"), []byte(keepaliveYAML), 0644); err != nil {
		return err
	}

	// Check if there are changes
	statusOut, _ := cmdOutput(tmpDir, "git", "status", "--porcelain")
	if strings.TrimSpace(statusOut) == "" {
		return nil // No changes
	}

	// Commit and push
	_ = runCmd(tmpDir, "git", "add", ".github/workflows/")
	_ = runCmd(tmpDir, "git", "commit", "-m", "chore: update auto-sign workflows from woffux")
	if err := runCmd(tmpDir, "git", "push"); err != nil {
		return fmt.Errorf("push workflows: %w", err)
	}

	return nil
}

// CheckWorkflowSync compares the remote sign.yml with what the local config would generate.
// Returns true if they match, false if out of sync.
func CheckWorkflowSync(repo string, cfg *config.Config) (bool, error) {
	token, err := tokenForRepo(repo)
	if err != nil {
		return false, err
	}

	out, err := ghOutputWithToken(token, "api",
		fmt.Sprintf("repos/%s/contents/.github/workflows/sign.yml", repo),
		"--jq", ".content")
	if err != nil {
		return false, fmt.Errorf("fetch remote workflow: %w", err)
	}

	// GitHub API returns base64 with embedded newlines every 76 chars
	clean := strings.ReplaceAll(strings.TrimSpace(out), "\n", "")
	remoteContent, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return false, fmt.Errorf("decode remote workflow: %w", err)
	}

	expected := GenerateWorkflowYAML(cfg.Schedule, cfg.Timezone, cfg.GetRandomDelaySecs())
	return strings.TrimSpace(string(remoteContent)) == strings.TrimSpace(expected), nil
}

func enableActions(repo, token string) {
	cmd := exec.Command("gh", "api", "-X", "PUT",
		"repos/"+repo+"/actions/permissions",
		"--input", "-")
	cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	cmd.Stdin = strings.NewReader(`{"enabled": true, "allowed_actions": "all"}`)
	cmd.Run()
}

// getGitHubUsername returns the login of the currently active gh account.
// If token is provided, uses that token instead of the active account.
func getGitHubUsername(token ...string) (string, error) {
	var out string
	var err error
	if len(token) > 0 && token[0] != "" {
		out, err = ghOutputWithToken(token[0], "api", "user", "-q", ".login")
	} else {
		out, err = ghOutput("api", "user", "-q", ".login")
	}
	if err != nil {
		return "", fmt.Errorf("get github username (is gh authenticated?): %w", err)
	}
	return strings.TrimSpace(out), nil
}

// tokenForRepo resolves the correct gh auth token for the given repo.
// It extracts the owner from "owner/repo" and tries `gh auth token --user owner`.
// Falls back to the default token if the owner-specific lookup fails.
func tokenForRepo(repo string) (string, error) {
	owner := repo
	if idx := strings.IndexByte(repo, '/'); idx >= 0 {
		owner = repo[:idx]
	}

	// Try owner-specific token first
	out, err := ghOutput("auth", "token", "--user", owner)
	if err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}

	// Fall back to default account
	out, err = ghOutput("auth", "token")
	if err != nil {
		return "", fmt.Errorf("no gh token available: %w", err)
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

// ghRunWithToken runs a gh command with GH_TOKEN set so it uses the correct account.
func ghRunWithToken(token string, args ...string) error {
	cmd := exec.Command("gh", args...)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	return cmd.Run()
}

func ghOutputWithToken(token string, args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	out, err := cmd.Output()
	return string(out), err
}

func ghSetSecret(repo, token, name, value string) error {
	cmd := exec.Command("gh", "secret", "set", name, "-R", repo)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
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

func runCmdSilent(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run()
}

func cmdOutput(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
