package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	workspaceRootOnce sync.Once
	workspaceRootDir  string
)

func detectWorkspaceRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	candidates := []string{cwd}
	for dir := cwd; ; {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		candidates = append(candidates, parent)
		dir = parent
	}
	for _, dir := range candidates {
		if hasWorkspaceMarkers(dir) {
			return dir
		}
	}
	return cwd
}

func hasWorkspaceMarkers(dir string) bool {
	markers := []string{"career_monitor", "go", "web", "README.md"}
	for _, marker := range markers {
		if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
			return false
		}
	}
	return true
}

func workspaceRoot() string {
	workspaceRootOnce.Do(func() {
		workspaceRootDir = detectWorkspaceRoot()
	})
	if strings.TrimSpace(workspaceRootDir) == "" {
		return "."
	}
	return workspaceRootDir
}

func resolveWorkspaceRelative(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return trimmed
	}
	if filepath.IsAbs(trimmed) {
		return trimmed
	}
	if abs, err := filepath.Abs(trimmed); err == nil {
		return abs
	}
	return filepath.Join(workspaceRoot(), trimmed)
}

func preferredLocalFile(raw string, localName string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return filepath.Join(workspaceRoot(), ".local", localName)
	}
	if filepath.IsAbs(trimmed) {
		return trimmed
	}

	localCandidate := filepath.Join(workspaceRoot(), ".local", localName)
	if _, err := os.Stat(localCandidate); err == nil {
		return localCandidate
	}

	currentAbs := resolveWorkspaceRelative(trimmed)
	if _, err := os.Stat(currentAbs); err == nil {
		return currentAbs
	}

	if filepath.Base(trimmed) == localName {
		return localCandidate
	}
	return currentAbs
}

func preferredLegacyOrLocal(localName string, legacyRelative string) string {
	localCandidate := filepath.Join(workspaceRoot(), ".local", localName)
	if _, err := os.Stat(localCandidate); err == nil {
		return localCandidate
	}
	legacyCandidate := filepath.Join(workspaceRoot(), legacyRelative)
	if _, err := os.Stat(legacyCandidate); err == nil {
		return legacyCandidate
	}
	return localCandidate
}

func PreferredLocalFile(raw string, localName string) string {
	return preferredLocalFile(raw, localName)
}

func PreferredLegacyOrLocal(localName string, legacyRelative string) string {
	return preferredLegacyOrLocal(localName, legacyRelative)
}
