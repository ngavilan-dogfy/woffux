package selfupdate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Release is one GitHub release.
type Release struct {
	Tag    string  `json:"tag_name"`
	URL    string  `json:"html_url"`
	Body   string  `json:"body"`
	Assets []Asset `json:"assets"`
}

// Asset is a file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Asset returns the download URL of a named asset, or "".
func (r Release) Asset(name string) string {
	for _, a := range r.Assets {
		if a.Name == name {
			return strings.TrimSpace(a.URL)
		}
	}
	return ""
}

// ErrNoReleases means the repository hasn't published any release yet.
var ErrNoReleases = errors.New("no releases published yet")

// Endpoints, swappable in tests.
var (
	APIBase = "https://api.github.com"
	WebBase = "https://github.com"
	client  = &http.Client{Timeout: 20 * time.Second}
)

func token() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func apiGet(url string, out any) (int, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if t := token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("can't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("GitHub API answered %d", resp.StatusCode)
	}
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
}

// Latest finds the newest release: the API first, then — when anonymous
// calls are rate-limited (60/hour per IP, often shared behind an office or
// VPN) — the public releases page redirect, building the standard asset URLs.
func Latest(t Tool) (Release, error) {
	var rel Release
	_, apiErr := apiGet(APIBase+"/repos/"+t.Repo+"/releases/latest", &rel)
	if apiErr == nil && rel.Tag != "" {
		return rel, nil
	}
	rel, webErr := latestFromWeb(t)
	if webErr == nil || webErr == ErrNoReleases {
		return rel, webErr
	}
	return Release{}, fmt.Errorf("%v; fallback: %v", apiErr, webErr)
}

func latestFromWeb(t Tool) (Release, error) {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Get(WebBase + "/" + t.Repo + "/releases/latest")
	if err != nil {
		return Release{}, fmt.Errorf("can't reach GitHub: %w", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, ErrNoReleases
	case resp.StatusCode < 300 || resp.StatusCode >= 400:
		return Release{}, fmt.Errorf("releases page answered %d", resp.StatusCode)
	case i < 0:
		return Release{}, ErrNoReleases // redirects to /releases: nothing published
	}
	tag := strings.TrimSpace(loc[i+len("/tag/"):])
	base := WebBase + "/" + t.Repo + "/releases/download/" + tag + "/"
	rel := Release{Tag: tag, URL: WebBase + "/" + t.Repo + "/releases/tag/" + tag}
	for _, p := range [][2]string{{"darwin", "arm64"}, {"darwin", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}} {
		name := AssetName(t.Binary, p[0], p[1])
		rel.Assets = append(rel.Assets, Asset{Name: name, URL: base + name})
	}
	rel.Assets = append(rel.Assets, Asset{Name: "checksums.txt", URL: base + "checksums.txt"})
	return rel, nil
}

// Note is what changed in one version, as short human bullets.
type Note struct {
	Tag     string
	Bullets []string
}

var (
	noteBullet  = regexp.MustCompile(`^\s*[*-]\s+(.*)$`)
	noteTrailer = regexp.MustCompile(`\s+by @\S+ in \S+$`)
	notePrefix  = regexp.MustCompile(`^(feat|fix|perf|docs|chore|refactor|test|build|ci|style)(\([^)]*\))?!?:\s*`)
	notePR      = regexp.MustCompile(`\s*\(#\d+\)$`)
	noteSHA     = regexp.MustCompile(`\s*\(\s*[0-9a-f]{7,40}\s*\)$`)
)

// ParseNotes turns release notes into short bullets:
// "* feat(ui): dashboard viewer by @x in https://…" → "Dashboard viewer".
func ParseNotes(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		m := noteBullet.FindStringSubmatch(line)
		if m == nil || strings.Contains(line, "Full Changelog") || strings.Contains(line, "first contribution") {
			continue
		}
		text := noteTrailer.ReplaceAllString(strings.TrimSpace(m[1]), "")
		text = notePrefix.ReplaceAllString(text, "")
		text = notePR.ReplaceAllString(text, "")
		text = noteSHA.ReplaceAllString(text, "")
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		r := []rune(text)
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		out = append(out, string(r))
	}
	return out
}

// NotesSince collects the notes of every release newer than current, up to
// latest, newest first. Best effort: nothing when GitHub can't be asked.
func NotesSince(t Tool, current, latest string) []Note {
	var rels []Release
	if _, err := apiGet(APIBase+"/repos/"+t.Repo+"/releases?per_page=30", &rels); err != nil {
		return notesFromAtom(t, current, latest)
	}
	var out []Note
	for _, r := range rels {
		if !inRange(r.Tag, current, latest) {
			continue
		}
		if b := ParseNotes(r.Body); len(b) > 0 {
			out = append(out, Note{Tag: r.Tag, Bullets: b})
		}
	}
	sortNotes(out)
	return out
}

// inRange: current < tag <= latest (every tag counts for a dev build).
func inRange(tag, current, latest string) bool {
	if Newer(tag, latest) {
		return false
	}
	if !IsRelease(current) {
		return Normalize(tag) == Normalize(latest)
	}
	return Newer(tag, current)
}

func sortNotes(n []Note) {
	sort.SliceStable(n, func(i, j int) bool { return Newer(n[i].Tag, n[j].Tag) })
}

var (
	atomEntry = regexp.MustCompile(`(?s)<entry>.*?<title>([^<]+)</title>.*?<content type="html">(.*?)</content>`)
	htmlLi    = regexp.MustCompile(`(?s)<li>(.*?)</li>`)
	htmlTag   = regexp.MustCompile(`<[^>]+>`)
)

// notesFromAtom reads the public releases feed: same notes, no API quota.
func notesFromAtom(t Tool, current, latest string) []Note {
	resp, err := client.Get(WebBase + "/" + t.Repo + "/releases.atom")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	var out []Note
	for _, m := range atomEntry.FindAllStringSubmatch(string(body), -1) {
		tag := strings.TrimSpace(m[1])
		if !inRange(tag, current, latest) {
			continue
		}
		content := html.UnescapeString(m[2])
		var md strings.Builder
		for _, li := range htmlLi.FindAllStringSubmatch(content, -1) {
			md.WriteString("* " + strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(li[1], ""))) + "\n")
		}
		if b := ParseNotes(md.String()); len(b) > 0 {
			out = append(out, Note{Tag: tag, Bullets: b})
		}
	}
	sortNotes(out)
	return out
}

// Download fetches url into w, reporting progress (0..1) when the size is known.
func Download(url string, w io.Writer, progress func(float64)) (int64, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	c := &http.Client{Timeout: 5 * time.Minute}
	resp, err := c.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download answered HTTP %d (the release may still be building — try again in a minute)", resp.StatusCode)
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64<<10)
	last := time.Time{}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return done, werr
			}
			done += int64(n)
			if progress != nil && total > 0 && time.Since(last) > 50*time.Millisecond {
				progress(float64(done) / float64(total))
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return done, fmt.Errorf("download interrupted: %w", rerr)
		}
	}
	if done == 0 {
		return 0, errors.New("the downloaded file is empty")
	}
	return done, nil
}

// VerifyChecksum checks a file against its line in a checksums.txt body.
// found is false when the file isn't listed (older releases had no sums).
func VerifyChecksum(path, name string, sums []byte) (found bool, err error) {
	want := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return true, fmt.Errorf("checksum mismatch for %s — the download is corrupt or was tampered with; nothing was changed", name)
	}
	return true, nil
}

// PlatformAsset is this machine's release binary name.
func PlatformAsset(t Tool) string { return AssetName(t.Binary, runtime.GOOS, runtime.GOARCH) }
