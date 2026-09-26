// Package selfupdate keeps a CLI up to date from its GitHub releases, the
// same way in every ngavilan-dogfy tool (woffux, jira-cli, datadog-cli):
//
//   - releases are tagged vMAJOR.MINOR.PATCH from conventional commits and
//     ship raw binaries named <binary>-<os>-<arch> plus checksums.txt;
//   - the latest release comes from the GitHub API, or from the public
//     releases page when the API rate-limits anonymous calls;
//   - `<binary> update` shows what's new, downloads with progress, verifies
//     the checksum and that the new binary runs, then swaps it in;
//   - a cached daily check tells the user when a newer version exists.
//
// This file is kept identical across the repos: change it in one, copy it
// to the others.
package selfupdate

import (
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// Tool describes the CLI being updated.
type Tool struct {
	Repo    string // "ngavilan-dogfy/datadog-cli"
	Binary  string // "datadog"
	Current string // this build's version, see CurrentBuild
}

// Build describes this binary: the release version set at build time, else
// the module version `go install` recorded, else the commit it was built from.
type Build struct {
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Date     string `json:"date,omitempty"`
	Modified bool   `json:"modified,omitempty"`
	Go       string `json:"go"`
	Platform string `json:"platform"`
	Path     string `json:"path,omitempty"`
}

var rePseudo = regexp.MustCompile(`^v\d+\.\d+\.\d+-(?:\d+\.)?\d{14}-([0-9a-f]{12})`)

// CurrentBuild reads the build info. ldflagsVersion is the -X Version value
// ("dev" when not set).
func CurrentBuild(ldflagsVersion string) Build {
	b := Build{Version: ldflagsVersion, Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
	if b.Version == "" {
		b.Version = "dev"
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Commit = s.Value
			case "vcs.time":
				b.Date = s.Value
			case "vcs.modified":
				b.Modified = s.Value == "true"
			}
		}
		if mv := info.Main.Version; b.Version == "dev" && mv != "" && mv != "(devel)" {
			if m := rePseudo.FindStringSubmatch(mv); m != nil {
				if b.Commit == "" {
					b.Commit = m[1]
				}
				b.Modified = b.Modified || strings.HasSuffix(mv, "+dirty")
			} else {
				b.Version = mv // go install …@v1.2.3
			}
		}
	}
	if len(b.Commit) > 7 {
		b.Commit = b.Commit[:7]
	}
	if exe, err := os.Executable(); err == nil {
		b.Path = exe
	}
	return b
}

// Short is the one-line form: "v1.2.0", or "dev (d9456bb+changes)" for a
// development build.
func (b Build) Short() string {
	if IsRelease(b.Version) {
		return b.Version
	}
	s := b.Version
	if b.Commit != "" && !strings.Contains(s, b.Commit) {
		s += " (" + b.Commit
		if b.Modified {
			s += "+changes"
		}
		s += ")"
	}
	return s
}

// Normalize strips the leading "v".
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return strings.TrimPrefix(v, "V")
}

// parts parses "v1.2.3" / "1.2.3-rc1"; ok is false for anything else.
func parts(v string) (p [3]int, ok bool) {
	v = Normalize(v)
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	f := strings.Split(v, ".")
	if len(f) != 3 {
		return p, false
	}
	for i, s := range f {
		n, err := strconv.Atoi(s)
		if err != nil {
			return p, false
		}
		p[i] = n
	}
	return p, true
}

// Newer reports whether version a is newer than b. Development builds
// ("dev", commits) never compare as newer or older.
func Newer(a, b string) bool {
	pa, okA := parts(a)
	pb, okB := parts(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

// IsRelease reports a version that came from a release tag.
func IsRelease(v string) bool {
	_, ok := parts(v)
	return ok && !strings.HasPrefix(Normalize(v), "0.0.0-")
}

// AssetName is the release binary for a platform: datadog-darwin-arm64,
// datadog-windows-amd64.exe.
func AssetName(binary, goos, goarch string) string {
	name := binary + "-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}
