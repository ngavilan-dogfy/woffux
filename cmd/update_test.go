package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentVersionIsLatest(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "same with v prefix", current: "1.2.3", latest: "v1.2.3", want: true},
		{name: "same current v prefix", current: "v1.2.3", latest: "1.2.3", want: true},
		{name: "trim spaces", current: " v1.2.3 ", latest: " V1.2.3 ", want: true},
		{name: "dev is not latest release", current: "dev", latest: "v1.2.3", want: false},
		{name: "different", current: "v1.2.2", latest: "v1.2.3", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := currentVersionIsLatest(tt.current, tt.latest); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReleaseAssetName(t *testing.T) {
	got, err := releaseAssetName("darwin", "arm64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "woffux-darwin-arm64" {
		t.Fatalf("got %q", got)
	}

	if _, err := releaseAssetName("windows", "amd64"); err == nil {
		t.Fatal("expected unsupported OS error")
	}
	if _, err := releaseAssetName("linux", "386"); err == nil {
		t.Fatal("expected unsupported architecture error")
	}
}

func TestGithubReleaseDownloadURL(t *testing.T) {
	release := githubRelease{
		TagName: "v1.2.3",
		Assets: []githubReleaseAsset{
			{Name: "woffux-linux-amd64", BrowserDownloadURL: " https://example.com/linux "},
		},
	}

	got, err := release.DownloadURL("woffux-linux-amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/linux" {
		t.Fatalf("got %q", got)
	}

	if _, err := release.DownloadURL("woffux-darwin-arm64"); err == nil {
		t.Fatal("expected missing asset error")
	}
}

func TestFetchLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("Accept header = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"tag_name": " v1.2.3 ",
			"assets": [
				{"name":"woffux-darwin-arm64","browser_download_url":"https://example.com/bin"}
			]
		}`))
	}))
	defer server.Close()

	release, err := fetchLatestRelease(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Fatalf("tag = %q", release.TagName)
	}
	if len(release.Assets) != 1 {
		t.Fatalf("assets = %#v", release.Assets)
	}
}

func TestDownloadFileRejectsEmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dst := filepath.Join(t.TempDir(), "woffux")
	if err := downloadFile(dst, server.URL); err == nil {
		t.Fatal("expected empty download error")
	}
}

func TestShouldAvoidSelfReplace(t *testing.T) {
	if !shouldAvoidSelfReplace(filepath.Join(os.TempDir(), "woffux")) {
		t.Fatal("expected temp executable to be avoided")
	}
	if !shouldAvoidSelfReplace(filepath.Join("var", "folders", "go-build", "woffux")) {
		t.Fatal("expected go-build executable to be avoided")
	}
	if !shouldAvoidSelfReplace("/usr/local/bin/woffux-dev") {
		t.Fatal("expected non-woffux executable to be avoided")
	}
	if shouldAvoidSelfReplace("/usr/local/bin/woffux") {
		t.Fatal("expected installed woffux executable to be replaceable")
	}
}

func TestFetchLatestReleaseFromWeb(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://github.com/ngavilan-dogfy/woffux/releases/tag/v5.10.0")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	release, err := fetchLatestReleaseFromWeb(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if release.TagName != "v5.10.0" {
		t.Fatalf("tag = %q", release.TagName)
	}
	url, err := release.DownloadURL("woffux-darwin-arm64")
	if err != nil || url != "https://github.com/ngavilan-dogfy/woffux/releases/download/v5.10.0/woffux-darwin-arm64" {
		t.Fatalf("url = %q, err = %v", url, err)
	}
}

func TestParseReleaseNotes(t *testing.T) {
	body := "## What's Changed\n* feat(cli): redo every command and flow in the new style by @ngavilan-dogfy in https://github.com/x/pull/9\n* fix: keep things alive (#10)\n\n**Full Changelog**: https://x"
	got := parseReleaseNotes(body)
	if len(got) != 2 || got[0] != "Redo every command and flow in the new style" || got[1] != "Keep things alive" {
		t.Fatalf("notes = %q", got)
	}
}

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{{"v5.12.0", "v5.9.0", true}, {"v5.9.0", "v5.12.0", false}, {"v5.12.1", "v5.12.1", false}, {"v1.0.0", "dev", true}}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%s,%s) = %v", c.a, c.b, got)
		}
	}
}
