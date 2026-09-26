package selfupdate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The notice tells people a newer version exists, like gh does: it reads a
// small cache instantly and refreshes it in the background at most once a
// day, so it never slows a command down and never needs the API quota.

const checkEvery = 24 * time.Hour

type cache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
	URL       string    `json:"url,omitempty"`
}

// Notifier is started before a command runs and printed after it.
type Notifier struct {
	t     Tool
	path  string
	known cache
	done  chan cache
}

// StartNotifier begins the check. cacheDir is the tool's cache folder.
// It returns nil when notices are off: development builds, CI, or
// <BINARY>_NO_UPDATE_NOTIFIER set.
func StartNotifier(t Tool, cacheDir string) *Notifier {
	if !IsRelease(t.Current) || os.Getenv("CI") != "" ||
		os.Getenv(strings.ToUpper(t.Binary)+"_NO_UPDATE_NOTIFIER") != "" {
		return nil
	}
	n := &Notifier{t: t, path: filepath.Join(cacheDir, "update-check.json")}
	if data, err := os.ReadFile(n.path); err == nil {
		_ = json.Unmarshal(data, &n.known)
	}
	if time.Since(n.known.CheckedAt) > checkEvery {
		n.done = make(chan cache, 1)
		go func() {
			c := cache{CheckedAt: time.Now()}
			if rel, err := latestFromWeb(t); err == nil {
				c.Latest, c.URL = rel.Tag, rel.URL
			} else {
				c.Latest = n.known.Latest // keep what we knew; retry tomorrow
			}
			n.save(c)
			n.done <- c
		}()
	}
	return n
}

func (n *Notifier) save(c cache) {
	if data, err := json.Marshal(c); err == nil {
		_ = os.MkdirAll(filepath.Dir(n.path), 0o700)
		_ = os.WriteFile(n.path, data, 0o600)
	}
}

// Newer returns the newer release known so far ("" when none), taking a
// just-finished background check into account without waiting for it.
func (n *Notifier) Newer() (tag, url string) {
	if n == nil {
		return "", ""
	}
	if n.done != nil {
		select {
		case c := <-n.done:
			n.known, n.done = c, nil
		default:
		}
	}
	if Newer(n.known.Latest, n.t.Current) {
		return n.known.Latest, n.known.URL
	}
	return "", ""
}

// Print writes the notice (to stderr, so pipes and --json stay clean).
func (n *Notifier) Print(w io.Writer) {
	tag, _ := n.Newer()
	if tag == "" {
		return
	}
	fmt.Fprintf(w, "\n%s %s %s %s\n%s\n",
		stAccent.Render("●"),
		"A new version of "+n.t.Binary+" is available:",
		stMuted.Render(n.t.Current+" →"), stAccent.Render(tag),
		stMuted.Render("  Run '"+n.t.Binary+" update' to get it."))
}

// KnownNewer reads only the cache: for UIs that show the hint themselves.
func KnownNewer(t Tool, cacheDir string) string {
	if !IsRelease(t.Current) {
		return ""
	}
	var c cache
	data, err := os.ReadFile(filepath.Join(cacheDir, "update-check.json"))
	if err != nil || json.Unmarshal(data, &c) != nil {
		return ""
	}
	if Newer(c.Latest, t.Current) {
		return c.Latest
	}
	return ""
}
