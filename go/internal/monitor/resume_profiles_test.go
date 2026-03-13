package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJobMatchSummaryForJobUsesBestResumeVariant(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "resume_profiles.yaml")
	content := `
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: ai_systems
    name: AI Systems
    summary: Applied AI engineer focused on llm systems, evaluations, rag, and model inference.
    focus_keywords: ["llm", "evaluation", "rag", "genai", "machine learning"]
    role_keywords: ["ai engineer", "machine learning engineer", "data scientist"]
    stack_keywords: ["python", "pytorch", "fastapi"]
  - slug: cloud
    name: Cloud
    summary: Cloud engineer focused on aws infrastructure, kubernetes, terraform, and observability.
    focus_keywords: ["aws", "terraform", "kubernetes", "cloudwatch", "platform"]
    role_keywords: ["cloud engineer", "platform engineer", "infrastructure engineer"]
    stack_keywords: ["aws", "terraform", "helm", "docker"]
  - slug: distributed_systems
    name: Distributed Systems
    summary: Distributed systems engineer focused on kafka, grpc, rabbitmq, and event-driven microservices.
    focus_keywords: ["distributed systems", "kafka", "grpc", "rabbitmq", "microservices"]
    role_keywords: ["backend engineer", "distributed systems engineer"]
    stack_keywords: ["go", "kafka", "grpc", "redis"]
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESUME_PROFILES_FILE", configPath)
	resetResumeProfilesCacheForTests()
	t.Cleanup(resetResumeProfilesCacheForTests)

	cases := []struct {
		name       string
		job        Job
		wantResume string
	}{
		{
			name: "ai systems",
			job: Job{
				Title:       "Data Scientist, Evaluations",
				Location:    "Menlo Park, CA",
				PostedAt:    "2026-03-05T14:00:00Z",
				Description: "Design frontier AI benchmarks, evaluate model behavior, and improve llm quality with python experimentation.",
			},
			wantResume: "AI Systems",
		},
		{
			name: "cloud",
			job: Job{
				Title:       "Software Engineer, Infrastructure Services",
				Location:    "Austin, TX",
				PostedAt:    "2026-03-05T14:00:00Z",
				Description: "Build aws platform services with kubernetes, terraform, helm, and cloudwatch for resilient deployments.",
			},
			wantResume: "Cloud",
		},
		{
			name: "distributed systems",
			job: Job{
				Title:       "Backend Engineer, Streaming Platform",
				Location:    "Remote (US)",
				PostedAt:    "2026-03-05T14:00:00Z",
				Description: "Own kafka and grpc microservices, operate rabbitmq workers, and improve throughput for event-driven systems.",
			},
			wantResume: "Distributed Systems",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			summary := jobMatchSummaryForJob(tc.job)
			if summary.RecommendedResume != tc.wantResume {
				t.Fatalf("recommended resume = %q, want %q", summary.RecommendedResume, tc.wantResume)
			}
			if len(summary.MatchReasons) == 0 || !strings.Contains(summary.MatchReasons[0], tc.wantResume) {
				t.Fatalf("expected first reason to mention %q, got %v", tc.wantResume, summary.MatchReasons)
			}
		})
	}
}

func TestJobMatchSummaryUsesResumeFileContentForDistinctiveMatch(t *testing.T) {
	dir := t.TempDir()
	aiResumePath := filepath.Join(dir, "ai.tex")
	cloudResumePath := filepath.Join(dir, "cloud.tex")
	configPath := filepath.Join(dir, "resume_profiles.yaml")

	aiResume := `
\section*{Projects}
\textbf{LLM Platform}
\begin{itemize}
  \item Built LangChain evaluation workflows and fine tuned Phi-4 with QLoRA for agentic reminder generation.
\end{itemize}
`
	cloudResume := `
\section*{Projects}
\textbf{Cloud Platform}
\begin{itemize}
  \item Built Terraform and EKS infrastructure with CloudWatch dashboards.
\end{itemize}
`
	config := `
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: ai
    name: AI Resume
    resume_file: "ai.tex"
    summary: Applied AI engineer.
    role_keywords: ["software engineer", "ai engineer"]
  - slug: cloud
    name: Cloud Resume
    resume_file: "cloud.tex"
    summary: Cloud engineer.
    role_keywords: ["software engineer", "platform engineer"]
`
	if err := os.WriteFile(aiResumePath, []byte(strings.TrimSpace(aiResume)), 0o644); err != nil {
		t.Fatalf("write ai resume: %v", err)
	}
	if err := os.WriteFile(cloudResumePath, []byte(strings.TrimSpace(cloudResume)), 0o644); err != nil {
		t.Fatalf("write cloud resume: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(config)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESUME_PROFILES_FILE", configPath)
	resetResumeProfilesCacheForTests()
	t.Cleanup(resetResumeProfilesCacheForTests)

	job := Job{
		Title:       "Software Engineer, Agent Platform",
		Location:    "Boston, MA",
		Description: "Build LangChain evaluation tooling, improve Phi-4 QLoRA fine tuning workflows, and support agent orchestration in production.",
	}
	summary := jobMatchSummaryForJob(job)
	if summary.RecommendedResume != "AI Resume" {
		t.Fatalf("recommended resume = %q, want %q", summary.RecommendedResume, "AI Resume")
	}
}

func TestResumeAwarePotentialMatchRejectsTestHeavyRoles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "resume_profiles.yaml")
	content := `
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: distributed_systems
    name: Distributed Systems
    summary: Distributed systems engineer focused on kafka and grpc.
    focus_keywords: ["kafka", "grpc"]
    role_keywords: ["backend engineer"]
    stack_keywords: ["go", "redis"]
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESUME_PROFILES_FILE", configPath)
	resetResumeProfilesCacheForTests()
	t.Cleanup(resetResumeProfilesCacheForTests)

	job := Job{
		Title:       "Software Developer in Test - Server",
		Location:    "Cupertino, CA",
		Description: "Build test automation for backend services.",
	}
	if resumeAwarePotentialMatch(job) {
		t.Fatalf("expected test-heavy role to be rejected")
	}
}

func TestResumeAwarePotentialMatchRejectsAmbiguousInternshipForGraduateWhenSLMDisabled(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "resume_profiles.yaml")
	content := `
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: ai_systems
    name: AI Systems
    summary: Applied AI engineer focused on llm systems.
    focus_keywords: ["ai", "llm"]
    role_keywords: ["software engineer", "ai engineer"]
    stack_keywords: ["python", "fastapi"]
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESUME_PROFILES_FILE", configPath)
	t.Setenv("SLM_SCORING", "false")
	resetResumeProfilesCacheForTests()
	t.Cleanup(resetResumeProfilesCacheForTests)

	job := Job{
		Title:       "Software Engineering Intern",
		Location:    "Remote (US)",
		Description: "Build production Python and FastAPI services for our AI platform and llm-backed tooling during the summer internship program.",
	}
	if resumeAwarePotentialMatch(job) {
		t.Fatalf("expected ambiguous internship to be rejected for graduated candidate")
	}

	summary := jobMatchSummaryForJob(job)
	if summary.MatchScore >= 45 {
		t.Fatalf("expected ambiguous internship to be penalized, got score %d", summary.MatchScore)
	}
}

func TestResumeAwarePotentialMatchAllowsAmbiguousInternshipForGraduateWhenSLMEnabled(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "resume_profiles.yaml")
	content := `
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: ai_systems
    name: AI Systems
    summary: Applied AI engineer focused on llm systems.
    focus_keywords: ["ai", "llm"]
    role_keywords: ["software engineer", "ai engineer"]
    stack_keywords: ["python", "fastapi"]
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESUME_PROFILES_FILE", configPath)
	t.Setenv("SLM_SCORING", "true")
	resetResumeProfilesCacheForTests()
	t.Cleanup(resetResumeProfilesCacheForTests)

	job := Job{
		Title:       "Software Engineering Intern",
		Location:    "Remote (US)",
		Description: "Build production services for our AI platform during the summer internship program.",
	}
	if !resumeAwarePotentialMatch(job) {
		t.Fatalf("expected ambiguous internship to stay eligible for SLM review")
	}

	summary := jobMatchSummaryForJob(job)
	if summary.MatchScore <= 25 {
		t.Fatalf("expected ambiguous internship to retain enough score for SLM review, got %d", summary.MatchScore)
	}
	if summary.RecommendedResume != "AI Systems" {
		t.Fatalf("recommended resume = %q, want %q", summary.RecommendedResume, "AI Systems")
	}
	if len(summary.MatchReasons) == 0 || !strings.Contains(strings.Join(summary.MatchReasons, " | "), "SLM review") {
		t.Fatalf("expected SLM review reason, got %v", summary.MatchReasons)
	}
}

func TestResumeAwarePotentialMatchRejectsNonTechnicalInternshipWhenSLMEnabled(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "resume_profiles.yaml")
	content := `
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: ai_systems
    name: AI Systems
    summary: Applied AI engineer focused on llm systems.
    focus_keywords: ["ai", "llm"]
    role_keywords: ["software engineer", "ai engineer"]
    stack_keywords: ["python", "fastapi"]
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESUME_PROFILES_FILE", configPath)
	t.Setenv("SLM_SCORING", "true")
	resetResumeProfilesCacheForTests()
	t.Cleanup(resetResumeProfilesCacheForTests)

	job := Job{
		Title:       "Product Management Intern",
		Location:    "Remote (US)",
		Description: "Support roadmap planning, customer research, and launch coordination for the summer internship program.",
	}
	if resumeAwarePotentialMatch(job) {
		t.Fatalf("expected non-technical internship to be rejected")
	}
}

func TestResumeAwarePotentialMatchAllowsGraduateEligibleInternship(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "resume_profiles.yaml")
	content := `
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: ai_systems
    name: AI Systems
    summary: Applied AI engineer focused on llm systems.
    focus_keywords: ["ai", "llm"]
    role_keywords: ["software engineer", "ai engineer"]
    stack_keywords: ["python", "fastapi"]
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESUME_PROFILES_FILE", configPath)
	resetResumeProfilesCacheForTests()
	t.Cleanup(resetResumeProfilesCacheForTests)

	job := Job{
		Title:       "AI Software Engineering Intern",
		Location:    "Remote (US)",
		PostedAt:    time.Now().UTC().Format(time.RFC3339),
		Description: "This internship is open to recent graduates and candidates who completed their degree in the last 12 months. Build AI services in Python and FastAPI.",
	}
	if !resumeAwarePotentialMatch(job) {
		t.Fatalf("expected graduate-eligible internship to be allowed")
	}

	summary := jobMatchSummaryForJob(job)
	if summary.MatchScore <= 40 {
		t.Fatalf("expected graduate-eligible internship to retain a useful score, got %d", summary.MatchScore)
	}
}

func TestResumeAwarePotentialMatchRejectsEnrollmentRequiredInternshipEvenWithSLM(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "resume_profiles.yaml")
	content := `
candidate:
  graduated: true
  graduation_date: "Dec 2025"
  interested_in_internships: true
  internships_require_postgrad_signal: true
profiles:
  - slug: ai_systems
    name: AI Systems
    summary: Applied AI engineer focused on llm systems.
    focus_keywords: ["ai", "llm"]
    role_keywords: ["software engineer", "ai engineer"]
    stack_keywords: ["python", "fastapi"]
`
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RESUME_PROFILES_FILE", configPath)
	t.Setenv("SLM_SCORING", "true")
	resetResumeProfilesCacheForTests()
	t.Cleanup(resetResumeProfilesCacheForTests)

	job := Job{
		Title:       "AI Software Engineering Intern",
		Location:    "Remote (US)",
		Description: "Candidates must be currently enrolled in a master's program and have at least one semester remaining. Build AI services in Python and FastAPI.",
	}
	if resumeAwarePotentialMatch(job) {
		t.Fatalf("expected enrollment-required internship to be rejected")
	}

	summary := jobMatchSummaryForJob(job)
	if summary.MatchScore >= 30 {
		t.Fatalf("expected enrollment-required internship to be heavily penalized, got %d", summary.MatchScore)
	}
	if summary.RecommendedResume != "" {
		t.Fatalf("expected blocked internship to clear recommended resume, got %q", summary.RecommendedResume)
	}
}
