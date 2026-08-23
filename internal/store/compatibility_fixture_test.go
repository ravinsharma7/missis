package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/compatfixture"
	"github.com/ravinsharma7/missis/internal/model"
	"github.com/ravinsharma7/missis/internal/plugin/builtin"
	"github.com/ravinsharma7/missis/internal/store"
	missispkg "github.com/ravinsharma7/missis/pkg/missis"
)

func fixtureRoot() string {
	return filepath.Join("testdata", "compatibility", compatfixture.RevisionDirectory)
}

func stringsOf[T ~string](values []T) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	sort.Strings(result)
	return result
}

func TestCompatibilityFixtureCompleteness(t *testing.T) {
	// covers PH1-FMT-001
	manifest, err := compatfixture.ReadManifest(filepath.Join(fixtureRoot(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantValues := append(stringsOf(model.AllBuiltInValueKinds()), "plugin/card")
	sort.Strings(wantValues)
	wantPlugins := []string{"fixture/custom-kind"}
	wantPluginContracts := []string{"fixture/custom-kind@1.0.0#" + strings.Repeat("b", 64)}
	wantOperationVersions := make([]string, 0, len(model.AllOperations()))
	for _, operation := range model.AllOperations() {
		descriptor, ok := model.LookupOperation(operation)
		if !ok {
			t.Fatalf("operation %s is not registered", operation)
		}
		wantOperationVersions = append(wantOperationVersions, fmt.Sprintf("%s/v%d", operation, descriptor.Version))
	}
	sort.Strings(wantOperationVersions)
	for _, item := range builtin.Manifests() {
		wantPlugins = append(wantPlugins, item.ID)
		wantPluginContracts = append(wantPluginContracts, fmt.Sprintf("%s@%s#%s", item.ID, item.Version, item.CodeHash))
	}
	sort.Strings(wantPlugins)
	sort.Strings(wantPluginContracts)
	checks := []struct {
		name string
		got  []string
		want []string
	}{
		{"operations", manifest.Operations, stringsOf(model.AllOperations())},
		{"operation versions", manifest.OperationVersions, wantOperationVersions},
		{"value kinds", manifest.ValueKinds, wantValues},
		{"inline kinds", manifest.InlineKinds, stringsOf(model.AllInlineItemKinds())},
		{"reference kinds", manifest.RefKinds, stringsOf(model.AllBuiltInRefKinds())},
		{"relations", manifest.Relations, model.ValidRelations()},
		{"plugins", manifest.Plugins, wantPlugins},
		{"plugin contracts", manifest.PluginContracts, wantPluginContracts},
		{"schema declarations", manifest.SchemaDeclarations, []string{"schema/plugin-card=plugin/card"}},
	}
	for _, check := range checks {
		if !reflect.DeepEqual(check.got, check.want) {
			t.Errorf("fixture %s coverage\n got: %v\nwant: %v\nadd a fixture case or create a new store-format revision", check.name, check.got, check.want)
		}
	}
	if !manifest.OrderedContainment || !manifest.LegacyContainment {
		t.Fatalf("fixture containment coverage ordered=%t legacy=%t", manifest.OrderedContainment, manifest.LegacyContainment)
	}
	wantIdempotency := []string{"fixture/base", "fixture/schema", "fixture/plugin-markdown", "fixture/plugin-artifact"}
	if !reflect.DeepEqual(manifest.IdempotencyKeys, wantIdempotency) {
		t.Fatalf("fixture idempotency keys = %v, want %v", manifest.IdempotencyKeys, wantIdempotency)
	}
}

func TestCompatibilityFixtureFresh(t *testing.T) {
	// covers PH1-FMT-001
	want, err := compatfixture.ReadManifest(filepath.Join(fixtureRoot(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	generatedRoot := filepath.Join(t.TempDir(), compatfixture.RevisionDirectory)
	got, err := compatfixture.Build(context.Background(), generatedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !compatfixture.EqualManifest(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("durable fixture changed without a new format revision\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestCompatibilityFixtureOpenBackupAndArtifacts(t *testing.T) {
	// covers PH1-FMT-001
	root := filepath.Join(t.TempDir(), compatfixture.RevisionDirectory)
	if err := os.CopyFS(root, os.DirFS(fixtureRoot())); err != nil {
		t.Fatal(err)
	}
	want, err := compatfixture.ReadManifest(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "fixture.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := compatfixture.Inspect(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !compatfixture.EqualManifest(got, want) {
		t.Fatalf("opened fixture differs from manifest: %#v != %#v", got, want)
	}
	cas, err := artifact.NewLocalStore(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range want.Artifacts {
		ref, err := artifact.ParseRef(expected.Ref)
		if err != nil {
			t.Fatal(err)
		}
		metadata, err := cas.Stat(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.Digest != expected.Digest || metadata.Size != expected.Size || metadata.MediaType != expected.MediaType {
			t.Fatalf("artifact %s = %#v, want %#v", ref, metadata, expected)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISSIS_ARTIFACT_STORE", filepath.Join(root, "artifacts"))
	service, err := application.OpenPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	client := missispkg.NewClient(service)
	backup := filepath.Join(root, "backup.db")
	if err := client.BackupTo(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	restoredDB := filepath.Join(root, "restored.db")
	restoredArtifacts := filepath.Join(root, "restored-artifacts")
	restoreService, err := application.OpenPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	restoreClient := missispkg.NewClient(restoreService)
	if err := restoreClient.RestoreWithOptions(context.Background(), backup, restoredDB, missispkg.RestoreOptions{ArtifactRoot: restoredArtifacts}); err != nil {
		t.Fatal(err)
	}
	if err := restoreClient.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(restoredDB)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := compatfixture.Inspect(context.Background(), reopened)
	if err != nil {
		t.Fatal(err)
	}
	if !compatfixture.EqualManifest(restored, want) {
		t.Fatalf("restored logical state differs from fixture manifest")
	}
	restoredCAS, err := artifact.NewLocalStore(restoredArtifacts)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range want.Artifacts {
		ref, err := artifact.ParseRef(expected.Ref)
		if err != nil {
			t.Fatal(err)
		}
		metadata, err := restoredCAS.Stat(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.Digest != expected.Digest || metadata.Size != expected.Size {
			t.Fatalf("restored artifact %s = %#v", ref, metadata)
		}
	}
}
