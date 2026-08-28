package peerconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/ravinsharma7/missis/internal/storeidentity"
)

const (
	VersionV1     = "missis-local-peer-set-v1"
	AdapterLiveV1 = "sqlite-live-readonly-v1"
	MaxFileSize   = 1 << 20
	MaxPeers      = 32
)

var handlePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type SetV1 struct {
	Version string      `json:"version"`
	Peers   []BindingV1 `json:"peers"`
}

type BindingV1 struct {
	Handle          string `json:"handle"`
	Adapter         string `json:"adapter"`
	ExpectedStoreID string `json:"expected_store_id"`
	SQLitePath      string `json:"sqlite_path"`
}

func Load(path string) (SetV1, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return SetV1{}, fmt.Errorf("peer-config-invalid: stat config: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return SetV1{}, fmt.Errorf("peer-config-invalid: config must be a regular non-symlink file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 {
		return SetV1{}, fmt.Errorf("peer-config-invalid: config must not be group/world writable")
	}
	file, err := os.Open(path)
	if err != nil {
		return SetV1{}, fmt.Errorf("peer-config-invalid: open config: %w", err)
	}
	defer file.Close()
	limited := io.LimitReader(file, MaxFileSize+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return SetV1{}, fmt.Errorf("peer-config-invalid: read config: %w", err)
	}
	if len(raw) > MaxFileSize {
		return SetV1{}, fmt.Errorf("peer-config-invalid: config exceeds %d bytes", MaxFileSize)
	}
	set, err := Parse(raw)
	if err != nil {
		return SetV1{}, err
	}
	if err := validateConfigOwner(info); err != nil {
		return SetV1{}, err
	}
	for index, peer := range set.Peers {
		parent, err := os.Stat(filepath.Dir(peer.SQLitePath))
		if err == nil && runtime.GOOS != "windows" && parent.Mode().Perm()&0o022 != 0 {
			return SetV1{}, fmt.Errorf("peer-config-invalid: peers[%d] database parent must not be group/world writable", index)
		}
		peerInfo, err := os.Lstat(peer.SQLitePath)
		if err == nil && (peerInfo.Mode()&os.ModeSymlink != 0 || !peerInfo.Mode().IsRegular()) {
			return SetV1{}, fmt.Errorf("peer-config-invalid: peers[%d] sqlite_path must be a regular non-symlink file", index)
		}
	}
	return set, nil
}

func Parse(raw []byte) (SetV1, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var set SetV1
	if err := decoder.Decode(&set); err != nil {
		return SetV1{}, fmt.Errorf("peer-config-invalid: decode: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return SetV1{}, fmt.Errorf("peer-config-invalid: %w", err)
	}
	if set.Version != VersionV1 {
		return SetV1{}, fmt.Errorf("peer-config-invalid: unsupported version %q", set.Version)
	}
	if len(set.Peers) > MaxPeers {
		return SetV1{}, fmt.Errorf("peer-config-invalid: peer count %d exceeds %d", len(set.Peers), MaxPeers)
	}
	seen := make(map[string]struct{}, len(set.Peers))
	for index := range set.Peers {
		peer := &set.Peers[index]
		if !handlePattern.MatchString(peer.Handle) {
			return SetV1{}, fmt.Errorf("peer-config-invalid: peers[%d].handle is invalid", index)
		}
		if _, exists := seen[peer.Handle]; exists {
			return SetV1{}, fmt.Errorf("peer-config-invalid: duplicate handle %q", peer.Handle)
		}
		seen[peer.Handle] = struct{}{}
		if peer.Adapter != AdapterLiveV1 {
			return SetV1{}, fmt.Errorf("peer-config-invalid: peers[%d].adapter %q is unsupported", index, peer.Adapter)
		}
		if err := storeidentity.ValidateStoreID(peer.ExpectedStoreID); err != nil {
			return SetV1{}, fmt.Errorf("peer-config-invalid: peers[%d].expected_store_id: %w", index, err)
		}
		if strings.TrimSpace(peer.SQLitePath) != peer.SQLitePath || !filepath.IsAbs(peer.SQLitePath) {
			return SetV1{}, fmt.Errorf("peer-config-invalid: peers[%d].sqlite_path must be absolute", index)
		}
		peer.SQLitePath = filepath.Clean(peer.SQLitePath)
	}
	return set, nil
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("trailing JSON value")
}
