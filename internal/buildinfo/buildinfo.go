package buildinfo

import (
	"runtime/debug"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/ravinsharma7/missis/internal/store"
)

const UnknownCommit = "unknown"

// Set by release builds with -ldflags. Module installs fall back to Go build
// metadata, while local source builds retain their VCS revision and dirty bit.
var (
	releaseVersion string
	releaseCommit  string
)

type Info struct {
	Version               string `json:"version"`
	DisplayVersion        string `json:"display_version"`
	Commit                string `json:"commit"`
	CommitNote            string `json:"commit_note,omitempty"`
	Dirty                 bool   `json:"dirty"`
	StoreFormatRevision   int    `json:"store_format_revision"`
	NormalOpenFormat      int    `json:"normal_open_format"`
	MigratableFromFormats []int  `json:"migratable_from_formats"`
	MigrationSetDigest    string `json:"migration_set_digest"`
}

func Read() Info {
	compatibility := store.FormatCompatibility()
	result := Info{
		Version: "dev", Commit: UnknownCommit,
		StoreFormatRevision:   compatibility.NormalOpenFormat,
		NormalOpenFormat:      compatibility.NormalOpenFormat,
		MigratableFromFormats: compatibility.MigratableFromFormats,
		MigrationSetDigest:    compatibility.MigrationSetDigest,
	}
	releaseBuild := releaseVersion != "" && releaseCommit != ""
	if releaseVersion != "" {
		result.Version = releaseVersion
	}
	if releaseCommit != "" {
		result.Commit = releaseCommit
	}
	info, ok := debug.ReadBuildInfo()
	if ok {
		if result.Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			result.Version = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if result.Commit == UnknownCommit && setting.Value != "" {
					result.Commit = setting.Value
				}
			case "vcs.modified":
				result.Dirty = !releaseBuild && setting.Value == "true"
			}
		}
		if result.Commit == UnknownCommit {
			if info.Main.Version == "" || info.Main.Version == "(devel)" {
				result.CommitNote = "built from a source tree without git metadata (VCS stamping disabled or the source directory is not a git checkout)"
			} else {
				result.CommitNote = "built from a module download (e.g. 'go install module@version'), which embeds no git metadata"
			}
		}
	} else {
		result.CommitNote = "no build metadata embedded in this binary"
	}
	result.DisplayVersion = display(result)
	return result
}

func display(info Info) string {
	value := info.Version
	if info.Commit != "" && info.Commit != UnknownCommit {
		short := strings.TrimSuffix(info.Commit, "-dirty")
		if len(short) > 12 {
			short = short[:12]
		}
		if !strings.Contains(value, "+") {
			value += "+g" + short
		}
	}
	if info.Dirty && !strings.HasSuffix(value, "-dirty") {
		value += "-dirty"
	}
	return value
}

// ReleaseDisplay returns the human-facing identity for a release manifest.
// Version remains the sortable SemVer value; the commit is appended only as
// SemVer build metadata so it does not affect update precedence.
func ReleaseDisplay(version, commit string) string {
	return display(Info{Version: version, Commit: commit})
}

func IsStable(info Info) bool {
	return semver.IsValid(info.Version) && semver.Prerelease(info.Version) == "" && !info.Dirty
}
