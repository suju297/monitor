package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	monitor "careermonitorgo/internal/monitor"
)

type caseDefinition struct {
	ID          string   `json:"id"`
	CompanyName string   `json:"company_name"`
	AllowedPlan []string `json:"allowed_plan"`
	Notes       string   `json:"notes,omitempty"`
}

type caseProbe struct {
	ID                string                      `json:"id"`
	CompanyName       string                      `json:"company_name"`
	CareersURL        string                      `json:"careers_url"`
	PrimarySource     string                      `json:"primary_source"`
	AllowedPlan       []string                    `json:"allowed_plan"`
	Notes             string                      `json:"notes,omitempty"`
	PayloadCompany    monitor.Company             `json:"payload_company"`
	PriorStatus       *monitor.CompanyStatus      `json:"prior_status,omitempty"`
	Probes            []monitor.SourceProbeResult `json:"probes"`
	SuggestedGoldPlan []string                    `json:"suggested_gold_plan"`
}

type probeReport struct {
	GeneratedAt string      `json:"generated_at"`
	ConfigPath  string      `json:"config_path"`
	CaseDefs    string      `json:"case_defs"`
	Cases       []caseProbe `json:"cases"`
}

func main() {
	var (
		configPath = flag.String("config", "../companies.yaml", "path to companies.yaml")
		dotenvPath = flag.String("dotenv", "../.env", "path to dotenv file")
		defsPath   = flag.String("defs", "../benchmarks/policy_routing_case_defs_20260307.json", "path to benchmark case definitions")
		outPath    = flag.String("out", "../.state/policy_routing_probe_report_20260307.json", "path to write probe report JSON")
	)
	flag.Parse()

	if err := monitor.LoadDotenv(*dotenvPath); err != nil {
		exitErr(fmt.Errorf("load dotenv failed: %w", err))
	}

	companies, err := monitor.LoadCompanies(*configPath)
	if err != nil {
		exitErr(fmt.Errorf("load companies failed: %w", err))
	}
	companyByName := map[string]monitor.Company{}
	for _, company := range companies {
		companyByName[strings.ToLower(strings.TrimSpace(company.Name))] = company
	}

	rawDefs, err := os.ReadFile(*defsPath)
	if err != nil {
		exitErr(fmt.Errorf("read defs failed: %w", err))
	}
	var defs []caseDefinition
	if err := json.Unmarshal(rawDefs, &defs); err != nil {
		exitErr(fmt.Errorf("parse defs failed: %w", err))
	}

	report := probeReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ConfigPath:  *configPath,
		CaseDefs:    *defsPath,
		Cases:       make([]caseProbe, 0, len(defs)),
	}

	for _, def := range defs {
		company, ok := companyByName[strings.ToLower(strings.TrimSpace(def.CompanyName))]
		if !ok {
			exitErr(fmt.Errorf("company %q not found in config", def.CompanyName))
		}
		allowedPlan := uniqueLower(def.AllowedPlan)
		if len(allowedPlan) == 0 {
			exitErr(fmt.Errorf("case %q has empty allowed plan", def.ID))
		}

		payloadCompany := company
		payloadCompany.Source = allowedPlan[0]
		if len(allowedPlan) > 1 {
			payloadCompany.FallbackSources = append([]string(nil), allowedPlan[1:]...)
		} else {
			payloadCompany.FallbackSources = nil
		}

		probes := make([]monitor.SourceProbeResult, 0, len(allowedPlan))
		for _, source := range allowedPlan {
			probes = append(probes, monitor.ProbeSourceCandidate(company, source))
		}

		report.Cases = append(report.Cases, caseProbe{
			ID:                strings.TrimSpace(def.ID),
			CompanyName:       company.Name,
			CareersURL:        company.CareersURL,
			PrimarySource:     company.Source,
			AllowedPlan:       allowedPlan,
			Notes:             strings.TrimSpace(def.Notes),
			PayloadCompany:    payloadCompany,
			Probes:            probes,
			SuggestedGoldPlan: monitor.RankSourceProbeResults(probes, allowedPlan),
		})
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		exitErr(fmt.Errorf("create output dir failed: %w", err))
	}
	rawOut, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		exitErr(fmt.Errorf("marshal report failed: %w", err))
	}
	if err := os.WriteFile(*outPath, rawOut, 0o644); err != nil {
		exitErr(fmt.Errorf("write report failed: %w", err))
	}
	fmt.Fprintln(os.Stdout, *outPath)
}

func uniqueLower(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
