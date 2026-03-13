package monitor

import (
	"fmt"
	"log"
	"os"
	"strings"
)

func RunMonitor(opts RunOptions) (TrackerResult, error) {
	result := TrackerResult{}
	if opts.Workers < 1 {
		opts.Workers = DefaultWorkers
	}
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = preferredLocalFile("companies.yaml", "companies.yaml")
	}
	if strings.TrimSpace(opts.StatePath) == "" {
		opts.StatePath = resolveWorkspaceRelative(".state/openings_state.json")
	}
	if strings.TrimSpace(opts.ReportPath) == "" {
		opts.ReportPath = resolveWorkspaceRelative(".state/last_run_report.json")
	}
	if strings.TrimSpace(opts.DotenvPath) == "" {
		opts.DotenvPath = resolveWorkspaceRelative(".env")
	}

	if err := LoadDotenv(opts.DotenvPath); err != nil {
		return result, fmt.Errorf("load dotenv failed: %w", err)
	}

	companies, err := LoadCompanies(opts.ConfigPath)
	if err != nil {
		return result, fmt.Errorf("load companies failed: %w", err)
	}
	state, err := LoadState(opts.StatePath)
	if err != nil {
		return result, fmt.Errorf("load state failed: %w", err)
	}

	outcomes := CrawlCompanies(
		companies,
		opts.Workers,
		state.CompanyState,
		opts.OnCompanyStart,
		opts.OnCompanyDone,
	)

	verificationSummary := URLVerificationSummary{Enabled: false}
	if verifyJobURLsEnabled() {
		verificationSummary.Enabled = true
		verified, verifyErr := VerifyOutcomeJobURLs(outcomes, opts.ReportPath)
		if verifyErr != nil {
			log.Printf("Job URL verification warning: %v", verifyErr)
		}
		verificationSummary = verified
		log.Printf(
			"Job URL verification: total=%d checked=%d skipped=%d ok=%d blocked=%d error=%d",
			verificationSummary.TotalURLs,
			verificationSummary.CheckedURLs,
			verificationSummary.SkippedURLs,
			verificationSummary.OKCount,
			verificationSummary.BlockedCount,
			verificationSummary.ErrorCount,
		)
		if strings.TrimSpace(verificationSummary.ArtifactPath) != "" {
			log.Printf("Job URL verification artifact: %s", verificationSummary.ArtifactPath)
		}
	}

	newJobs, blockedOutcomes, statusLines := ApplyOutcomesToState(outcomes, &state)
	UpdateLastRun(&state)
	if err := SaveState(opts.StatePath, state); err != nil {
		return result, fmt.Errorf("save state failed: %w", err)
	}
	if err := PersistJobsRunToDB(opts.StatePath, state, outcomes, newJobs); err != nil {
		log.Printf("Jobs DB persistence warning: %v", err)
	}

	report, err := WriteRunReport(opts.ReportPath, outcomes, newJobs, len(blockedOutcomes), opts.DryRun, opts.Baseline, verificationSummary)
	if err != nil {
		return result, fmt.Errorf("write run report failed: %w", err)
	}

	for _, line := range statusLines {
		log.Print(line)
	}

	result = TrackerResult{
		Outcomes:        outcomes,
		NewJobs:         newJobs,
		BlockedOutcomes: blockedOutcomes,
		StatusLines:     statusLines,
		URLVerification: verificationSummary,
		Report:          report,
	}

	if opts.Baseline {
		log.Printf("Baseline mode enabled. Marked %d opening(s) as seen and skipped email.", len(newJobs))
		return result, nil
	}

	shouldSend := len(newJobs) > 0 || (opts.AlertOnBlocked && len(blockedOutcomes) > 0)
	if !shouldSend {
		log.Print("No new openings detected.")
		if len(blockedOutcomes) > 0 {
			log.Printf("%d target(s) are blocked. Use --alert-on-blocked to email blocked summary.", len(blockedOutcomes))
		}
		return result, nil
	}

	subject, body := BuildEmailContent(newJobs, blockedOutcomes)
	if opts.DryRun {
		log.Print("Dry run enabled. Email was not sent.")
		_, _ = fmt.Fprintln(os.Stdout, "\n--- EMAIL SUBJECT ---")
		_, _ = fmt.Fprintln(os.Stdout, subject)
		_, _ = fmt.Fprintln(os.Stdout, "\n--- EMAIL BODY ---")
		_, _ = fmt.Fprintln(os.Stdout, body)
		return result, nil
	}

	if err := SendEmail(subject, body); err != nil {
		return result, fmt.Errorf("send email failed: %w", err)
	}
	log.Printf("Sent alert email for %d new opening(s), %d blocked target(s).", len(newJobs), len(blockedOutcomes))
	return result, nil
}
