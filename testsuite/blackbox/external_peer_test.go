package blackbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfiguredLocalPeerInspectionAndMovedStoreNavigation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("local SQLite peer profile is confirmed only on Linux")
	}
	root := t.TempDir()
	targetStore := filepath.Join(root, "target", "missis.db")
	sourceStore := filepath.Join(root, "source", "missis.db")
	target := newTicket(t, targetStore, "foreign target")
	health := mustJSON(t, runMissis(t, targetStore, "show", "--health", "--json"))
	storeID := health["store_id"].(string)
	source := newTicket(t, sourceStore, "source")
	external := map[string]any{
		"version": "external-ref-v1", "store_id": storeID, "namespace": "missis",
		"kind": "ticket", "entity_id": target["id"].(string), "display_hint": "target#1",
	}
	rawExternal, err := json.Marshal(external)
	if err != nil {
		t.Fatal(err)
	}
	set := runMissis(t, sourceStore, "set", source["ref"].(string)+"/external", "--kind", "external-ref", "--data-json", string(rawExternal), "--json")
	if set.code != 0 {
		t.Fatalf("set external ref: code=%d stdout=%s stderr=%s", set.code, set.stdout, set.stderr)
	}

	configPath := filepath.Join(root, "peers.json")
	writePeerConfig(t, configPath, storeID, targetStore)
	inspect := runMissisTools(t, "", nil, "peers", "inspect", "--config", configPath, "--json")
	if inspect.code != 0 {
		t.Fatalf("inspect: code=%d stdout=%s stderr=%s", inspect.code, inspect.stdout, inspect.stderr)
	}
	var inspection struct {
		Status string `json:"status"`
		Peers  []struct {
			Classification string `json:"classification"`
		} `json:"peers"`
	}
	if err := json.Unmarshal([]byte(inspect.stdout), &inspection); err != nil {
		t.Fatal(err)
	}
	if inspection.Status != "ok" || len(inspection.Peers) != 1 || inspection.Peers[0].Classification != "verified" {
		t.Fatalf("inspection = %#v", inspection)
	}

	partRef := source["ref"].(string) + "/external"
	resolved := mustJSON(t, runMissis(t, sourceStore, "show", partRef, "--resolve-external", "--peer-config", configPath, "--json"))
	if resolved["authority_state"] != "verified" || resolved["identity_state"] != "matched" || resolved["lifecycle"] != "active" {
		t.Fatalf("resolution = %#v", resolved)
	}

	movedStore := filepath.Join(root, "moved", "missis.db")
	if err := os.MkdirAll(filepath.Dir(movedStore), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", ".maintenance.lock", "-wal", "-shm"} {
		if _, err := os.Stat(targetStore + suffix); err == nil {
			if err := os.Rename(targetStore+suffix, movedStore+suffix); err != nil {
				t.Fatal(err)
			}
		}
	}
	writePeerConfig(t, configPath, storeID, movedStore)
	moved := mustJSON(t, runMissis(t, sourceStore, "show", partRef, "--resolve-external", "--peer-config", configPath, "--json"))
	if moved["authority_state"] != "verified" || moved["identity_state"] != "matched" {
		t.Fatalf("moved resolution = %#v", moved)
	}
	shown := mustJSON(t, runMissis(t, sourceStore, "show", source["ref"].(string), "--json"))
	stored := shown["parts"].(map[string]any)["external"].(map[string]any)["value"].(map[string]any)
	if stored["store_id"] != storeID || stored["entity_id"] != target["id"] {
		t.Fatalf("stored reference changed = %#v", stored)
	}
}

func writePeerConfig(t *testing.T, path, storeID, storePath string) {
	t.Helper()
	config := map[string]any{
		"version": "missis-local-peer-set-v1",
		"peers": []map[string]any{{
			"handle": "target-local", "adapter": "sqlite-live-readonly-v1",
			"expected_store_id": storeID, "sqlite_path": storePath,
		}},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
