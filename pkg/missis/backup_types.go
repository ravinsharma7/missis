package missis

import "time"

const (
	BackupManifestVersion   = 2
	BackupManifestVersionV1 = 1
	BackupArtifactEmbedded  = "embedded"
	BackupArtifactExternal  = "external"
)

type BackupFileInfo struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type BackupArtifactEntry struct {
	Ref       string `json:"ref"`
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
	MediaType string `json:"media_type,omitempty"`
	Size      int64  `json:"size"`
	Backend   string `json:"backend"`
}

// BackupManifest is generated beside a SQLite snapshot. It is transport
// metadata, not ledger data and is never used as an event-store authority.
type BackupManifest struct {
	Version      int                   `json:"version"`
	ArtifactMode string                `json:"artifact_mode"`
	Database     BackupFileInfo        `json:"database"`
	Store        ManifestInfo          `json:"store"`
	Artifacts    []BackupArtifactEntry `json:"artifacts"`
}

// BackupCompletion is written last for version-2 bundles. It is deliberately
// sidecar metadata rather than ledger data: a missing marker means publication
// was interrupted and the bundle must not be treated as complete.
type BackupCompletion struct {
	DatabaseSHA256 string    `json:"database_sha256"`
	ManifestSHA256 string    `json:"manifest_sha256"`
	BundleVersion  int       `json:"bundle_version"`
	CompletedAt    time.Time `json:"completed_at"`
}

type RestoreOptions struct {
	ArtifactRoot string
}
