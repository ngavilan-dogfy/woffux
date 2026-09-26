package github

import (
	"fmt"
	"sort"
	"strings"
)

// RequiredSecrets are the Actions secrets the sign workflow reads. Telegram's
// are optional.
var RequiredSecrets = []string{
	"WOFFU_URL", "WOFFU_COMPANY_URL", "WOFFU_EMAIL", "WOFFU_PASSWORD",
	"WOFFU_LATITUDE", "WOFFU_LONGITUDE", "WOFFU_HOME_LATITUDE", "WOFFU_HOME_LONGITUDE",
	"WOFFUX_TIMING",
}

// SecretNames lists the Actions secrets set on repo: names only, GitHub
// never gives the values back.
func SecretNames(repo string) ([]string, error) {
	token, err := tokenForRepo(repo)
	if err != nil {
		return nil, err
	}
	out, err := ghOutputWithToken(token, "secret", "list", "-R", repo, "--json", "name", "--jq", ".[].name")
	if err != nil {
		return nil, fmt.Errorf("could not list secrets on %s: %w", repo, err)
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	sort.Strings(names)
	return names, nil
}

// MissingSecrets returns the required secrets absent from names.
func MissingSecrets(names []string) []string {
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	var missing []string
	for _, n := range RequiredSecrets {
		if !have[n] {
			missing = append(missing, n)
		}
	}
	return missing
}

// Login returns the GitHub account woffux acts as for repo.
func Login(repo string) (string, error) {
	token, err := tokenForRepo(repo)
	if err != nil {
		return "", err
	}
	return getGitHubUsername(token)
}

// SecretNamesForDelete are every secret woffux may have set: the required
// ones and Telegram's.
func SecretNamesForDelete() []string {
	return append(append([]string(nil), RequiredSecrets...), "TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID")
}

// DeleteSecrets removes woffux's secrets from repo, ignoring ones that
// aren't there. It returns the names it removed.
func DeleteSecrets(repo string) ([]string, error) {
	token, err := tokenForRepo(repo)
	if err != nil {
		return nil, err
	}
	present, err := SecretNames(repo)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, n := range present {
		have[n] = true
	}
	var removed []string
	for _, name := range SecretNamesForDelete() {
		if !have[name] {
			continue
		}
		if err := ghRunWithToken(token, "secret", "delete", name, "-R", repo); err != nil {
			return removed, fmt.Errorf("delete secret %s: %w", name, err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}
