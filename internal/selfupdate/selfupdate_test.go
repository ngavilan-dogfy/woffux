package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v1.2.0", "v1.1.9", true}, {"v0.10.0", "v0.9.0", true}, {"v2.0.0", "1.99.99", true},
		{"v1.2.0", "v1.2.0", false}, {"v1.1.0", "v1.2.0", false}, {"v1.2.0", "dev", false},
	} {
		if Newer(c.a, c.b) != c.want {
			t.Errorf("Newer(%q, %q) != %v", c.a, c.b, c.want)
		}
	}
	for v, want := range map[string]bool{"v1.2.3": true, "1.2.3": true, "dev": false, "d9456bb-dirty": false, "v0.0.0-20260924101010-abcdef123456": false} {
		if IsRelease(v) != want {
			t.Errorf("IsRelease(%q) != %v", v, want)
		}
	}
	if got := (Build{Version: "dev", Commit: "d9456bb", Modified: true}).Short(); got != "dev (d9456bb+changes)" {
		t.Errorf("Short = %q", got)
	}
	if got := (Build{Version: "v1.2.0", Commit: "abc1234"}).Short(); got != "v1.2.0" {
		t.Errorf("Short = %q", got)
	}
	if AssetName("datadog", "windows", "amd64") != "datadog-windows-amd64.exe" || AssetName("jira", "darwin", "arm64") != "jira-darwin-arm64" {
		t.Error("AssetName")
	}
}

func TestParseNotes(t *testing.T) {
	gh := "## What's Changed\n* feat(ui): dashboard viewer by @x in https://github.com/a/b/pull/3\n* fix: retry 429s (#12)\n\n**Full Changelog**: https://…"
	ours := "## Features\n- feat(logs): live tail (a1b2c3d)\n## Fixes\n- fix(setup)!: find the right site (1234567)\n"
	if got := ParseNotes(gh); strings.Join(got, "|") != "Dashboard viewer|Retry 429s" {
		t.Errorf("github notes → %q", got)
	}
	if got := ParseNotes(ours); strings.Join(got, "|") != "Live tail|Find the right site" {
		t.Errorf("commit notes → %q", got)
	}
}

// fakeGitHub serves releases v1.1.0 (with a working "binary") and v1.0.0.
type fakeGitHub struct {
	srv       *httptest.Server
	apiDown   bool // API answers 403 (rate limited)
	noRelease bool
	tamper    bool
	binary    string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{binary: "#!/bin/sh\necho 'datadog version v1.1.0'\n"}
	asset := AssetName("datadog", runtime.GOOS, runtime.GOARCH)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := f.srv.URL
		switch p := r.URL.Path; {
		case strings.HasPrefix(p, "/api/"):
			if f.apiDown {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if f.noRelease {
				http.NotFound(w, r)
				return
			}
			v110 := map[string]any{"tag_name": "v1.1.0", "html_url": base + "/web/o/datadog-cli/releases/tag/v1.1.0",
				"body": "## Features\n- feat: charts in braille (abc1234)\n",
				"assets": []map[string]string{
					{"name": asset, "browser_download_url": base + "/dl/" + asset},
					{"name": "checksums.txt", "browser_download_url": base + "/dl/checksums.txt"},
				}}
			v100 := map[string]any{"tag_name": "v1.0.0", "body": "- feat: first release\n"}
			if strings.HasSuffix(p, "/latest") {
				json.NewEncoder(w).Encode(v110)
			} else {
				json.NewEncoder(w).Encode([]any{v110, v100})
			}
		case p == "/web/o/datadog-cli/releases/latest":
			if f.noRelease {
				http.Redirect(w, r, base+"/web/o/datadog-cli/releases", http.StatusFound)
				return
			}
			http.Redirect(w, r, base+"/web/o/datadog-cli/releases/tag/v1.1.0", http.StatusFound)
		case p == "/web/o/datadog-cli/releases.atom":
			w.Write([]byte(`<feed><entry><title>v1.1.0</title><content type="html">&lt;ul&gt;&lt;li&gt;feat: charts in braille&lt;/li&gt;&lt;/ul&gt;</content></entry></feed>`))
		case p == "/dl/"+asset, p == "/web/o/datadog-cli/releases/download/v1.1.0/"+asset:
			w.Write([]byte(f.binary))
		case p == "/dl/checksums.txt", p == "/web/o/datadog-cli/releases/download/v1.1.0/checksums.txt":
			sum := sha256.Sum256([]byte(f.binary))
			h := hex.EncodeToString(sum[:])
			if f.tamper {
				h = strings.Repeat("0", 64)
			}
			w.Write([]byte(h + "  " + asset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	oldAPI, oldWeb := APIBase, WebBase
	APIBase, WebBase = f.srv.URL+"/api", f.srv.URL+"/web"
	t.Cleanup(func() { APIBase, WebBase = oldAPI, oldWeb })
	return f
}

var tool = Tool{Repo: "o/datadog-cli", Binary: "datadog", Current: "v1.0.0"}

func TestLatestWithFallback(t *testing.T) {
	f := newFakeGitHub(t)
	rel, err := Latest(tool)
	if err != nil || rel.Tag != "v1.1.0" || rel.Asset("checksums.txt") == "" {
		t.Fatalf("api: %+v %v", rel, err)
	}
	f.apiDown = true // rate limited: the releases page still works
	rel, err = Latest(tool)
	if err != nil || rel.Tag != "v1.1.0" || rel.Asset(PlatformAsset(tool)) == "" {
		t.Fatalf("web fallback: %+v %v", rel, err)
	}
	f.noRelease = true
	if _, err := Latest(tool); err != ErrNoReleases {
		t.Fatalf("no releases → %v", err)
	}
}

func TestNotesSince(t *testing.T) {
	f := newFakeGitHub(t)
	notes := NotesSince(tool, "v1.0.0", "v1.1.0")
	if len(notes) != 1 || notes[0].Tag != "v1.1.0" || notes[0].Bullets[0] != "Charts in braille" {
		t.Fatalf("api notes: %+v", notes)
	}
	f.apiDown = true
	notes = NotesSince(tool, "v1.0.0", "v1.1.0")
	if len(notes) != 1 || notes[0].Bullets[0] != "Charts in braille" {
		t.Fatalf("atom notes: %+v", notes)
	}
}

func TestRunPlain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the fake binary")
	}
	f := newFakeGitHub(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "datadog")
	os.WriteFile(target, []byte("#!/bin/sh\necho 'datadog version v1.0.0'\n"), 0o755)
	var out strings.Builder

	f.tamper = true
	if err := runPlain(tool, Options{Out: &out}, target); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered: %v", err)
	}
	if b, _ := os.ReadFile(target); !strings.Contains(string(b), "v1.0.0") {
		t.Fatal("binary replaced despite a bad checksum")
	}
	f.tamper = false
	f.apiDown = true // and through the web fallback
	if err := runPlain(tool, Options{Out: &out}, target); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != f.binary {
		t.Fatalf("not replaced: %q", b)
	}
	if !strings.Contains(out.String(), "checksum verified") {
		t.Errorf("output: %s", out.String())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".datadog-update-*")); len(left) > 0 {
		t.Errorf("temp files left: %v", left)
	}
	// Already current.
	out.Reset()
	if err := runPlain(Tool{Repo: tool.Repo, Binary: "datadog", Current: "v1.1.0"}, Options{Out: &out}, target); err != nil || !strings.Contains(out.String(), "up to date") {
		t.Fatalf("up to date: %v %s", err, out.String())
	}
}

func TestNotifier(t *testing.T) {
	newFakeGitHub(t)
	t.Setenv("CI", "")
	dir := t.TempDir()
	if StartNotifier(Tool{Repo: tool.Repo, Binary: "datadog", Current: "dev"}, dir) != nil {
		t.Fatal("dev builds must not check")
	}
	n := StartNotifier(tool, dir) // no cache: checks in the background
	deadline := time.Now().Add(5 * time.Second)
	for {
		if tag, _ := n.Newer(); tag == "v1.1.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background check never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if KnownNewer(tool, dir) != "v1.1.0" {
		t.Error("cache not written")
	}
	var w strings.Builder
	n.Print(&w)
	if !strings.Contains(w.String(), "v1.1.0") || !strings.Contains(w.String(), "datadog update") {
		t.Errorf("notice: %q", w.String())
	}
	// A fresh cache means no network at all.
	WebBase = "http://127.0.0.1:1"
	n2 := StartNotifier(tool, dir)
	if n2.done != nil {
		t.Error("checked again within a day")
	}
	if tag, _ := n2.Newer(); tag != "v1.1.0" {
		t.Error("cached result lost")
	}
	t.Setenv("DATADOG_NO_UPDATE_NOTIFIER", "1")
	if StartNotifier(tool, dir) != nil {
		t.Error("opt-out ignored")
	}
}
