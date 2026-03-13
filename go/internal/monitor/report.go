package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func WriteRunReport(path string, outcomes []CrawlOutcome, newJobs []Job, blockedCount int, dryRun bool, baseline bool, verification URLVerificationSummary) (RunReport, error) {
	report := RunReport{
		GeneratedAt: utcNow(),
		DryRun:      dryRun,
		Baseline:    baseline,
		Summary: ReportSummary{
			CompaniesTotal: len(outcomes),
			NewJobsCount:   len(newJobs),
			BlockedCount:   blockedCount,
			OKCount:        0,
			ErrorCount:     0,
		},
		URLVerification: verification,
		Outcomes:        outcomes,
		NewJobs:         newJobs,
	}
	for _, outcome := range outcomes {
		switch outcome.Status {
		case "ok":
			report.Summary.OKCount++
		case "error":
			report.Summary.ErrorCount++
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return report, err
	}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return report, err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return report, err
	}
	return report, nil
}
