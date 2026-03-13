package monitor

import "testing"

func TestInferJobExternalIDFromDescription(t *testing.T) {
	t.Parallel()

	job := Job{
		Company:     "Acme",
		Source:      "generic",
		Title:       "Software Engineer",
		URL:         "https://jobs.example.com/openings/software-engineer",
		Description: "Build backend systems.\nReq ID: ABC-12345\nApply now.",
	}

	if got := inferJobExternalID(job); got != "ABC-12345" {
		t.Fatalf("inferJobExternalID() = %q, want %q", got, "ABC-12345")
	}
}

func TestJobFingerprintUsesStableExternalIDWhenDescriptionProvidesIt(t *testing.T) {
	t.Parallel()

	first := Job{
		Company:     "Acme",
		Source:      "generic",
		Title:       "Software Engineer",
		URL:         "https://jobs.example.com/openings/software-engineer",
		Description: "Req ID: ABC-12345",
	}
	second := Job{
		Company:     "Acme",
		Source:      "generic",
		Title:       "Senior Software Engineer",
		URL:         "https://jobs.example.com/jobs/platform-role",
		Description: "Reference Number ABC-12345",
	}

	if jobFingerprint(first) != jobFingerprint(second) {
		t.Fatalf("expected matching fingerprints when stable external id is the same")
	}
}

func TestApplyOutcomesToStateMigratesLegacyFingerprintWhenExternalIDAppearsLater(t *testing.T) {
	t.Parallel()

	legacyJob := Job{
		Company: "Acme",
		Source:  "generic",
		Title:   "Software Engineer",
		URL:     "https://jobs.example.com/openings/software-engineer",
	}
	legacyFP := legacyJobFingerprint(legacyJob)
	state := &MonitorState{
		Seen: map[string]map[string]SeenEntry{
			"Acme": {
				legacyFP: {
					Title:     legacyJob.Title,
					URL:       legacyJob.URL,
					FirstSeen: "2026-03-01T00:00:00Z",
					LastSeen:  "2026-03-02T00:00:00Z",
					Active:    true,
					Source:    legacyJob.Source,
				},
			},
		},
	}

	outcomes := []CrawlOutcome{{
		Company:        "Acme",
		Status:         "ok",
		SelectedSource: "generic",
		Jobs: []Job{{
			Company:     "Acme",
			Source:      "generic",
			Title:       "Software Engineer",
			URL:         "https://jobs.example.com/openings/software-engineer",
			Description: "Build backend systems. Job ID: ABC-12345",
		}},
	}}

	newJobs, _, _ := ApplyOutcomesToState(outcomes, state)
	if len(newJobs) != 0 {
		t.Fatalf("newJobs len = %d, want 0", len(newJobs))
	}

	newFP := jobFingerprint(Job{
		Company:     "Acme",
		Source:      "generic",
		Title:       "Software Engineer",
		URL:         "https://jobs.example.com/openings/software-engineer",
		Description: "Build backend systems. Job ID: ABC-12345",
	})
	entry, ok := state.Seen["Acme"][newFP]
	if !ok {
		t.Fatalf("expected migrated entry under fingerprint %q", newFP)
	}
	if entry.FirstSeen != "2026-03-01T00:00:00Z" {
		t.Fatalf("first_seen = %q, want preserved original", entry.FirstSeen)
	}
	if entry.ExternalID != "ABC-12345" {
		t.Fatalf("external_id = %q, want %q", entry.ExternalID, "ABC-12345")
	}
	if _, stillLegacy := state.Seen["Acme"][legacyFP]; stillLegacy {
		t.Fatalf("expected legacy fingerprint %q to be removed", legacyFP)
	}
}
