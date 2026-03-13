package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"careermonitorgo/internal/monitor"
)

func resolveLaunchPath(cwd string, raw string, requireExistingFile bool) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || filepath.IsAbs(trimmed) {
		return trimmed
	}

	candidates := []string{
		filepath.Clean(filepath.Join(cwd, trimmed)),
	}
	parent := filepath.Dir(cwd)
	if parent != cwd {
		candidates = append(candidates, filepath.Clean(filepath.Join(parent, trimmed)))
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		if requireExistingFile {
			continue
		}
		if _, err := os.Stat(filepath.Dir(candidate)); err == nil {
			return candidate
		}
	}
	return candidates[0]
}

func main() {
	host := flag.String("host", "127.0.0.1", "Host to bind.")
	port := flag.Int("port", 8765, "Port to bind.")
	config := flag.String("config", "../companies.yaml", "Tracker config file.")
	stateFile := flag.String("state-file", "../.state/openings_state.json", "Tracker state file.")
	reportFile := flag.String("report-file", "../.state/last_run_report.json", "Tracker report file.")
	scheduleFile := flag.String("schedule-file", "../.state/crawl_schedule.json", "Recurring crawl schedule file.")
	dotenv := flag.String("dotenv", "../.env", "Dotenv file.")
	workers := flag.Int("workers", 12, "Workers for triggered monitor runs.")
	alertOnBlocked := flag.Bool("alert-on-blocked", false, "Include blocked summary alerts on dashboard-triggered runs.")
	flag.Parse()

	cwd, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: cannot get cwd: %v\n", err)
		os.Exit(1)
	}

	cfg := monitor.MonitorRunnerConfig{
		CWD:            cwd,
		ConfigPath:     resolveLaunchPath(cwd, monitor.PreferredLocalFile(*config, "companies.yaml"), true),
		StatePath:      resolveLaunchPath(cwd, *stateFile, false),
		ReportPath:     resolveLaunchPath(cwd, *reportFile, false),
		SchedulePath:   resolveLaunchPath(cwd, *scheduleFile, false),
		DotenvPath:     resolveLaunchPath(cwd, *dotenv, true),
		Workers:        *workers,
		AlertOnBlocked: *alertOnBlocked,
	}
	if err := monitor.RunDashboard(*host, *port, cfg); err != nil {
		log.Printf("dashboard stopped with error: %v", err)
		os.Exit(1)
	}
}
