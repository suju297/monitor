package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"careermonitorgo/internal/monitor"
)

func main() {
	config := flag.String("config", "../companies.yaml", "Path to YAML config file.")
	stateFile := flag.String("state-file", "../.state/openings_state.json", "Path to persistent state JSON.")
	reportFile := flag.String("report-file", "../.state/last_run_report.json", "Path to JSON run report.")
	dotenv := flag.String("dotenv", "../.env", "Path to .env file.")
	workers := flag.Int("workers", monitor.DefaultWorkers, "Concurrent crawler workers.")
	baseline := flag.Bool("baseline", false, "Mark current listings as seen, but skip email.")
	dryRun := flag.Bool("dry-run", false, "Do not send email. Print generated email content.")
	alertOnBlocked := flag.Bool("alert-on-blocked", false, "Send email even if only blocked targets were detected.")
	verbose := flag.Bool("verbose", false, "Enable verbose logging.")
	flag.Parse()

	log.SetFlags(log.LstdFlags)
	if *verbose {
		log.SetOutput(os.Stdout)
	}

	opts := monitor.RunOptions{
		ConfigPath:     *config,
		StatePath:      *stateFile,
		ReportPath:     *reportFile,
		DotenvPath:     *dotenv,
		Workers:        *workers,
		Baseline:       *baseline,
		DryRun:         *dryRun,
		AlertOnBlocked: *alertOnBlocked,
		Verbose:        *verbose,
	}
	if _, err := monitor.RunMonitor(opts); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
