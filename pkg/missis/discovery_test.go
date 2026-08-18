package missis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})
}

func writeMarker(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".missis"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveStorePathRelativeMarker(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	writeMarker(t, project, "db/store.db\n")
	chdirForTest(t, project)

	got, err := ResolveStorePath("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, "db", "store.db")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveStorePathAbsoluteMarkerRejected(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	storePath := filepath.Join(tmp, "absolute.db")
	writeMarker(t, project, storePath+"\n")
	chdirForTest(t, project)

	if _, err := ResolveStorePath(""); err == nil {
		t.Fatal("expected absolute marker path to be rejected")
	}
}

func TestResolveStorePathMarkerEscapeRejected(t *testing.T) {
	for _, content := range []string{"../shared.db\n", "../../shared/db\n", "db/../../evil.db\n"} {
		tmp := t.TempDir()
		project := filepath.Join(tmp, "project")
		writeMarker(t, project, content)
		chdirForTest(t, project)

		if _, err := ResolveStorePath(""); err == nil {
			t.Fatalf("expected escaping marker %q to be rejected", content)
		}
	}
}

func TestResolveStorePathDirectoryMarker(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(project, ".missis"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdirForTest(t, project)

	got, err := ResolveStorePath("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, ".missis", "missis.db")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveStorePathInvalidMarker(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "multiple lines", content: "one\ntwo\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			project := filepath.Join(tmp, "project")
			writeMarker(t, project, tt.content)
			chdirForTest(t, project)

			if _, err := ResolveStorePath(""); err == nil {
				t.Fatalf("expected error for %s marker", tt.name)
			}
		})
	}
}

func TestResolveStoreEnvBeatsMarker(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	writeMarker(t, project, "db/store.db\n")
	envPath := filepath.Join(tmp, "env.db")
	t.Setenv("MISSIS_STORE", envPath)
	chdirForTest(t, project)

	got, err := ResolveStorePath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != envPath {
		t.Fatalf("env got %q, want %q (env must outrank the repo marker)", got, envPath)
	}
}

func TestResolveStoreSources(t *testing.T) {
	tmp := t.TempDir()

	flagPath := filepath.Join(tmp, "flag.db")
	rs, err := ResolveStore(flagPath)
	if err != nil {
		t.Fatal(err)
	}
	if rs.Source != DiscoveryFlag || rs.Path != flagPath || rs.Supplied != flagPath {
		t.Fatalf("flag source = %+v", rs)
	}

	envPath := filepath.Join(tmp, "env.db")
	t.Setenv("MISSIS_STORE", envPath)
	rs, err = ResolveStore("")
	if err != nil {
		t.Fatal(err)
	}
	if rs.Source != DiscoveryEnv || rs.Path != envPath {
		t.Fatalf("env source = %+v", rs)
	}

	t.Setenv("MISSIS_STORE", "")
	project := filepath.Join(tmp, "project")
	writeMarker(t, project, "db/store.db\n")
	chdirForTest(t, project)
	rs, err = ResolveStore("")
	if err != nil {
		t.Fatal(err)
	}
	if rs.Source != DiscoveryMarker || rs.MarkerDir != project {
		t.Fatalf("marker source = %+v", rs)
	}
	if rs.Path != filepath.Join(project, "db", "store.db") {
		t.Fatalf("marker path = %q", rs.Path)
	}
}

func TestResolveStoreMarkerThroughSymlinkedCwd(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	writeMarker(t, project, "db/store.db\n")
	link := filepath.Join(tmp, "linked")
	if err := os.Symlink(project, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	chdirForTest(t, link)

	got, err := ResolveStorePath("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, "db", "store.db")
	if got != want {
		t.Fatalf("got %q, want %q (store must stay inside the real project root)", got, want)
	}
}

func TestResolveStoreEnvBeatsMarkerErrorText(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	writeMarker(t, project, "../outside.db\n")
	chdirForTest(t, project)

	_, err := ResolveStorePath("")
	if err == nil {
		t.Fatal("expected escape rejection")
	}
	if !strings.Contains(err.Error(), "MISSIS_STORE") {
		t.Fatalf("error should point to the explicit alternatives: %v", err)
	}
}

func TestDefaultStorePathVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, goos, local, config, home, want string
	}{
		{"windows localappdata", "windows", `C:\Users\u\AppData\Local`, "", "", `C:\Users\u\AppData\Local\missis\missis.db`},
		{"windows userconfigdir fallback", "windows", "", `C:\Users\u\AppData\Roaming`, "", `C:\Users\u\AppData\Roaming\missis\missis.db`},
		{"windows legacy xdg fallback", "windows", "", "", `/home/u`, `/home/u/.local/share/missis/missis.db`},
		{"linux xdg", "linux", "", "", `/home/u`, `/home/u/.local/share/missis/missis.db`},
		{"darwin xdg", "darwin", "", "", `/Users/u`, `/Users/u/.local/share/missis/missis.db`},
		{"missing home", "linux", "", "", "", filepath.Join(".", ".missis", "missis.db")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			normalize := func(p string) string {
				return strings.ReplaceAll(p, "\\", "/")
			}
			got := normalize(filepath.Clean(defaultStorePath(tc.goos, tc.local, tc.config, tc.home)))
			want := normalize(tc.want)
			if got != want {
				t.Fatalf("defaultStorePath(%q) = %q, want %q", tc.goos, got, want)
			}
		})
	}
}
