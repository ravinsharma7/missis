package peerconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testStoreID = "store:v1:sha256:120311f35ac84b69682c5b5be1dbe7ab96994ef4a8db9d43473d8d0f1f379867"

func TestParseStrictPeerSet(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"version":"missis-local-peer-set-v1","peers":[{"handle":"spy-local","adapter":"sqlite-live-readonly-v1","expected_store_id":"` + testStoreID + `","sqlite_path":"/tmp/spy.db"}]}`)
	set, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Peers) != 1 || set.Peers[0].Handle != "spy-local" {
		t.Fatalf("set = %#v", set)
	}
}

func TestPeerSetBoundsAreEnforced(t *testing.T) {
	t.Parallel()
	peers := make([]BindingV1, MaxPeers+1)
	for index := range peers {
		peers[index] = BindingV1{
			Handle:          "peer-" + strings.Repeat("x", index%8) + string(rune('A'+index)),
			Adapter:         AdapterLiveV1,
			ExpectedStoreID: testStoreID,
			SQLitePath:      filepath.Join("/tmp", "peer-"+string(rune('A'+index))+".db"),
		}
	}
	raw, err := json.Marshal(SetV1{Version: VersionV1, Peers: peers})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "peer count") {
		t.Fatalf("peer-limit error = %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(configPath, []byte(strings.Repeat("x", MaxFileSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configPath); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("file-limit error = %v", err)
	}
}

func TestLoadRejectsWritableConfigAndSymlinkPeer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux local-peer permission profile")
	}
	dir := t.TempDir()
	peerPath := filepath.Join(dir, "peer.db")
	if err := os.WriteFile(peerPath, []byte("not opened by config validation"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "peer-link.db")
	if err := os.Symlink(peerPath, linkPath); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"version":"missis-local-peer-set-v1","peers":[{"handle":"spy","adapter":"sqlite-live-readonly-v1","expected_store_id":"` + testStoreID + `","sqlite_path":"` + linkPath + `"}]}`)
	configPath := filepath.Join(dir, "peers.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configPath); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("symlink load error = %v", err)
	}
	if err := os.Chmod(configPath, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configPath); err == nil || !strings.Contains(err.Error(), "group/world writable") {
		t.Fatalf("writable config error = %v", err)
	}
}

func TestParseRejectsUnknownRelativeDuplicateAndTrailing(t *testing.T) {
	t.Parallel()
	base := `{"version":"missis-local-peer-set-v1","peers":[{"handle":"spy","adapter":"sqlite-live-readonly-v1","expected_store_id":"` + testStoreID + `","sqlite_path":"/tmp/spy.db"}]}`
	cases := []string{
		strings.Replace(base, `"sqlite_path"`, `"locator"`, 1),
		strings.Replace(base, `/tmp/spy.db`, `spy.db`, 1),
		strings.Replace(base, `]}`, `,{"handle":"spy","adapter":"sqlite-live-readonly-v1","expected_store_id":"`+testStoreID+`","sqlite_path":"/tmp/two.db"}]}`, 1),
		base + `{}`,
	}
	for _, raw := range cases {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("Parse(%s) succeeded", raw)
		}
	}
}
