package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func emptyState() MonitorState {
	return MonitorState{
		Seen:         map[string]map[string]SeenEntry{},
		CompanyState: map[string]CompanyStatus{},
		Blocked:      map[string][]BlockedEvent{},
		LastRun:      "",
	}
}

func LoadState(path string) (MonitorState, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return emptyState(), nil
		}
		return emptyState(), err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return emptyState(), err
	}
	state := emptyState()
	if err := json.Unmarshal(raw, &state); err != nil {
		// Keep robust behavior: ignore invalid JSON and continue with fresh state.
		return emptyState(), nil
	}
	if state.Seen == nil {
		state.Seen = map[string]map[string]SeenEntry{}
	}
	if state.CompanyState == nil {
		state.CompanyState = map[string]CompanyStatus{}
	}
	if state.Blocked == nil {
		state.Blocked = map[string][]BlockedEvent{}
	}
	pruneStoredNoiseJobs(&state)
	return state, nil
}

func SaveState(path string, state MonitorState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	pruneStoredNoiseJobs(&state)
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func UpdateLastRun(state *MonitorState) {
	state.LastRun = utcNow()
}
