package monitor

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func resetWorkspaceRootForTests() {
	workspaceRootOnce = sync.Once{}
	workspaceRootDir = ""
}

func makeWorkspace(t *testing.T, root string) {
	t.Helper()
	for _, marker := range []string{"career_monitor", "go", "web"} {
		if err := os.MkdirAll(filepath.Join(root, marker), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", marker, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
}

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func TestPreferredLocalFilePrefersLocalWhenPresent(t *testing.T) {
	root := t.TempDir()
	makeWorkspace(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".local"), 0o755); err != nil {
		t.Fatalf("mkdir .local: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".local", "companies.yaml"), []byte("companies: []\n"), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "companies.yaml"), []byte("companies: []\n"), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	oldWD, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(oldWD)
		resetWorkspaceRootForTests()
	}()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	resetWorkspaceRootForTests()

	got := preferredLocalFile("companies.yaml", "companies.yaml")

	want := canonicalPath(filepath.Join(root, ".local", "companies.yaml"))
	got = canonicalPath(got)
	if got != want {
		t.Fatalf("preferredLocalFile = %q, want %q", got, want)
	}
}

func TestPreferredLegacyOrLocalFallsBackToLegacy(t *testing.T) {
	root := t.TempDir()
	makeWorkspace(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".state"), 0o755); err != nil {
		t.Fatalf("mkdir .state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".state", "sample_state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write legacy storage state: %v", err)
	}

	oldWD, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(oldWD)
		resetWorkspaceRootForTests()
	}()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	resetWorkspaceRootForTests()

	got := preferredLegacyOrLocal("sample_state.json", ".state/sample_state.json")
	want := canonicalPath(filepath.Join(root, ".state", "sample_state.json"))
	got = canonicalPath(got)
	if got != want {
		t.Fatalf("preferredLegacyOrLocal = %q, want %q", got, want)
	}
}
