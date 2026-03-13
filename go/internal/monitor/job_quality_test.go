package monitor

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsClearlyNonJobLandingPage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		job  Job
		want bool
	}{
		{
			name: "ibm careers search result page",
			job: Job{
				Title: "Software Engineering Role Software Engineer Create impactful software systems for clients around the world. Available jobs",
				URL:   "https://www.ibm.com/careers/search?field_keyword_08%5B0%5D=Software%20Engineering&q=software%20engineer",
			},
			want: true,
		},
		{
			name: "intuit blog article",
			job: Job{
				Title: "How to Write Software Engineer Cover Letters That Stand Out (With Examples)",
				URL:   "https://jobs.intuit.com/blog-innovative-thinking-software-engineer-vs-software-developer",
			},
			want: true,
		},
		{
			name: "zoom benefits page",
			job: Job{
				Title: "Benefits",
				URL:   "https://careers.zoom.us/benefits",
			},
			want: true,
		},
		{
			name: "confluent open positions landing page",
			job: Job{
				Title: "See Open Positions",
				URL:   "https://careers.confluent.io/jobs",
			},
			want: true,
		},
		{
			name: "amazon content landing page",
			job: Job{
				Title: "Learn about working at Amazon",
				URL:   "https://www.amazon.jobs/content/en/our-workplace",
			},
			want: true,
		},
		{
			name: "google results landing page",
			job: Job{
				Title: "work_outline work_outline Jobs Jobs",
				URL:   "https://www.google.com/about/careers/applications/jobs/results/",
			},
			want: true,
		},
		{
			name: "microsoft action center page",
			job: Job{
				Title: "View previous applications and profile here .",
				URL:   "https://jobs.careers.microsoft.com/actioncenter",
			},
			want: true,
		},
		{
			name: "salesforce accommodation form",
			job: Job{
				Title: "Accommodation Request Form",
				URL:   "https://careers.mail.salesforce.com/accommodations-request-form",
			},
			want: true,
		},
		{
			name: "jpmorgan community page",
			job: Job{
				Title: "Community relief",
				URL:   "https://careers.jpmorgan.com/communities/community-relief",
			},
			want: true,
		},
		{
			name: "datadog early career landing page",
			job: Job{
				Title: "Early Career & Internships",
				URL:   "https://careers.datadoghq.com/early-careers/",
			},
			want: true,
		},
		{
			name: "datadog remote landing page",
			job: Job{
				Title: "Remote Job Roles",
				URL:   "https://careers.datadoghq.com/remote",
			},
			want: true,
		},
		{
			name: "stripe search landing page",
			job: Job{
				Title: "See open roles",
				URL:   "https://stripe.com/jobs/search",
			},
			want: true,
		},
		{
			name: "stripe localized search landing page",
			job: Job{
				Title: "Deutsch",
				URL:   "https://stripe.com/de/jobs/search",
			},
			want: true,
		},
		{
			name: "stripe greenhouse detail entry remains valid",
			job: Job{
				Title: "Software Engineer, New Grad",
				URL:   "https://stripe.com/jobs/search?gh_jid=7206515",
			},
			want: false,
		},
		{
			name: "salesforce jobs search landing page",
			job: Job{
				Title: "Apply today",
				URL:   "https://careers.salesforce.com/en/jobs/?search=/",
			},
			want: true,
		},
		{
			name: "apple detail page remains valid",
			job: Job{
				Title: "Software Engineer - Data",
				URL:   "https://jobs.apple.com/en-us/details/200649562-3956/software-engineer-data?team=SFTWR",
			},
			want: false,
		},
		{
			name: "meta job details remain valid",
			job: Job{
				Title: "Data Scientist, Evaluations - Meta Superintelligence Labs",
				URL:   "https://www.metacareers.com/profile/job_details/852803947781269",
			},
			want: false,
		},
		{
			name: "goldman requisition preview remains valid",
			job: Job{
				Title: "Software Engineer – CRG (Analyst / Associate)",
				URL:   "https://hdpc.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/LateralHiring/requisitions/preview/163118",
			},
			want: false,
		},
		{
			name: "privacy role title is not auto-pruned",
			job: Job{
				Title: "Principal Engineer - Privacy",
				URL:   "https://www.databricks.com/company/careers/engineering---pipeline/principal-engineer---privacy-7274488002",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isClearlyNonJobLandingPage(tc.job); got != tc.want {
				t.Fatalf("isClearlyNonJobLandingPage() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLooksLikeNoiseJobRejectsPDFAssets(t *testing.T) {
	t.Parallel()

	job := Job{
		Title:    "Know your rights: workplace discrimination is illegal",
		URL:      "https://careers.google.com/jobs/dist/legal/EEOC_KnowYourRights_10_20.pdf",
		Location: "United States",
	}

	if !looksLikeNoiseJob(job) {
		t.Fatalf("expected PDF asset to be treated as noise")
	}
}

func TestLooksLikeNoiseJobAllowsGoogleDetailPath(t *testing.T) {
	t.Parallel()

	job := Job{
		Title:       "Software Engineer, AI/ML",
		URL:         "https://www.google.com/about/careers/applications/jobs/results/137677516847358662-software-engineer-aiml?q=software&sort_by=date",
		Location:    "Sunnyvale, CA, USA; Kirkland, WA, USA",
		PostedAt:    "2026-03-06T00:29:48Z",
		Description: "Google's software engineers develop next-generation technologies at massive scale.",
	}

	if looksLikeNoiseJob(job) {
		t.Fatalf("expected Google detail job path to survive noise filtering")
	}
}

func TestLooksLikeNoiseJobAllowsOraclePreviewPath(t *testing.T) {
	t.Parallel()

	job := Job{
		Title:       "Principal Software Engineer",
		URL:         "https://eeho.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/jobsearch/requisitions/preview/327086",
		Location:    "United States",
		PostedAt:    "2026-03-05",
		Description: "Design and build software systems for Oracle cloud products.",
	}

	if looksLikeNoiseJob(job) {
		t.Fatalf("expected Oracle requisition preview path to survive noise filtering")
	}
}

func TestDeterministicRoleDecisionClassifiesClearAndAmbiguousCases(t *testing.T) {
	t.Parallel()

	inDecision, inReasons := deterministicRoleDecision(Job{
		Title:       "Platform Software Engineer",
		Location:    "Remote (US)",
		Description: "Build backend services in Go and distributed systems on AWS.",
	})
	if inDecision != roleDecisionIn {
		t.Fatalf("clear engineering role decision = %q, want %q", inDecision, roleDecisionIn)
	}
	if len(inReasons) == 0 {
		t.Fatalf("expected clear engineering role reasons")
	}

	ambiguousDecision, ambiguousReasons := deterministicRoleDecision(Job{
		Title:       "Early Careers Opportunity",
		Location:    "Remote (US)",
		Description: "Join the team building backend services in Go, Python, and cloud infrastructure.",
	})
	if ambiguousDecision != roleDecisionAmbiguous {
		t.Fatalf("technical-but-unclear role decision = %q, want %q", ambiguousDecision, roleDecisionAmbiguous)
	}
	if len(ambiguousReasons) == 0 {
		t.Fatalf("expected ambiguous role reasons")
	}
}

func TestIsUSBasedJobUsesStructuredLocationTokens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		job  Job
		want bool
	}{
		{
			name: "us city and state code",
			job: Job{
				Location: "Seattle, WA",
			},
			want: true,
		},
		{
			name: "microsoft india location does not look us based",
			job: Job{
				Location: "India, Karnataka, Bangalore; Bengaluru, KA, IN; Hyderabad, TS, IN; 3 days / week in-office",
				URL:      "https://apply.careers.microsoft.com/careers/job/1970393556824802",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		if got := isUSBasedJob(tc.job); got != tc.want {
			t.Fatalf("%s: isUSBasedJob() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDeterministicRoleDecisionRejectsMicrosoftIndiaLocations(t *testing.T) {
	t.Parallel()

	decision, reasons := deterministicRoleDecision(Job{
		Title:       "Software Engineer II (Audio)",
		Location:    "India, Karnataka, Bangalore; Bengaluru, KA, IN; Hyderabad, TS, IN; 3 days / week in-office",
		URL:         "https://apply.careers.microsoft.com/careers/job/1970393556824802",
		Description: "Build audio platform software and distributed systems for Microsoft products.",
	})
	if decision != roleDecisionOut {
		t.Fatalf("india microsoft role decision = %q, want %q (reasons=%v)", decision, roleDecisionOut, reasons)
	}
	if !strings.Contains(strings.Join(reasons, " | "), "Non-US location") {
		t.Fatalf("expected non-us location reason, got %v", reasons)
	}
}

func TestDeterministicRoleDecisionRejectsOutOfScopeConsultingRoles(t *testing.T) {
	t.Parallel()

	decision, reasons := deterministicRoleDecision(Job{
		Title:       "Solutions Consultant",
		Location:    "Remote (US)",
		Description: "Partner with customers on pre-sales discovery and solution design.",
	})
	if decision != roleDecisionOut {
		t.Fatalf("consulting role decision = %q, want %q", decision, roleDecisionOut)
	}
	if len(reasons) == 0 {
		t.Fatalf("expected out-of-scope role reasons")
	}
}

func TestDeterministicRoleDecisionPrefersEngineeringTitleOverNonTargetTeam(t *testing.T) {
	t.Parallel()

	decision, reasons := deterministicRoleDecision(Job{
		Title:       "Applied AI Engineer",
		Team:        "Sales",
		Location:    "San Francisco, CA",
		Description: "Build LLM-powered product integrations and production AI systems for customers.",
	})
	if decision != roleDecisionIn {
		t.Fatalf("engineering title with sales team decision = %q, want %q", decision, roleDecisionIn)
	}
	if len(reasons) == 0 {
		t.Fatalf("expected in-scope role reasons")
	}
}

func TestDeterministicRoleDecisionRejectsProductManagerAndArchitectTitles(t *testing.T) {
	t.Parallel()

	cases := []Job{
		{
			Title:       "Entry Level Product Manager | US | Remote",
			Location:    "Remote (US)",
			Description: "Drive product planning for developer tools and platform workflows.",
		},
		{
			Title:       "Associate Observability Architect | USA EST| Remote",
			Location:    "Remote (US)",
			Description: "Help customers design observability rollouts and solution patterns.",
		},
		{
			Title:       "People Business Partner | 12 Month Contract | Sydney or Melbourne | Remote",
			Location:    "Sydney, Australia",
			Description: "Partner with people leaders on org planning and coaching.",
		},
		{
			Title:       "People Technology Analyst | EST or CST USA | Remote",
			Team:        "People",
			Location:    "Remote (US)",
			Description: "Own HR systems support, reporting, and employee technology workflows.",
		},
		{
			Title:       "Technical Marketing Engineer (Agent Development)",
			Team:        "Marketing",
			Location:    "US Remote",
			Description: "Create technical marketing content and demos for agent development campaigns.",
		},
		{
			Title:       "Technical Content Writer",
			Team:        "Engineering | Community | Technical Community",
			Location:    "New York, New York, USA",
			Description: "Write technical product content, guides, and launch materials for developers.",
		},
		{
			Title:       "Strategy & Operations Business Partner",
			Team:        "Marketing Operations",
			Location:    "Remote in the US",
			Description: "Drive planning, business operations, and cross-functional execution for marketing leadership.",
		},
		{
			Title:       "Business Systems Analyst",
			Team:        "Finance Systems",
			Location:    "San Francisco, CA",
			Description: "Support internal business systems, workflows, and stakeholder operations.",
		},
		{
			Title:       "Data Center Mechanical Engineer",
			Team:        "Data Center",
			Location:    "San Francisco, CA",
			Description: "Own HVAC, cooling, and mechanical systems design for data center facilities.",
		},
	}

	for _, job := range cases {
		decision, reasons := deterministicRoleDecision(job)
		if decision != roleDecisionOut {
			t.Fatalf("%q decision = %q, want %q (reasons=%v)", job.Title, decision, roleDecisionOut, reasons)
		}
	}
}

func TestPruneStoredNoiseJobs(t *testing.T) {
	t.Parallel()

	state := MonitorState{
		Seen: map[string]map[string]SeenEntry{
			"IBM": {
				"noise-search": {
					Title: "Software Engineering Role Software Engineer Create impactful software systems for clients around the world. Available jobs",
					URL:   "https://www.ibm.com/careers/search?field_keyword_08%5B0%5D=Software%20Engineering&q=software%20engineer",
				},
			},
			"Zoom": {
				"noise-home": {
					Title: "Home",
					URL:   "https://careers.zoom.us/home",
				},
			},
			"Apple": {
				"real-job": {
					Title:    "Software Engineer - Data",
					URL:      "https://jobs.apple.com/en-us/details/200649562-3956/software-engineer-data?team=SFTWR",
					Source:   "command",
					PostedAt: "2026-03-05T00:00:00Z",
				},
			},
		},
		CompanyState: map[string]CompanyStatus{},
		Blocked:      map[string][]BlockedEvent{},
	}

	removed := pruneStoredNoiseJobs(&state)
	if removed != 2 {
		t.Fatalf("pruneStoredNoiseJobs() removed %d rows, want 2", removed)
	}
	if _, ok := state.Seen["IBM"]; ok {
		t.Fatalf("expected IBM noise rows to be pruned")
	}
	if _, ok := state.Seen["Zoom"]; ok {
		t.Fatalf("expected Zoom noise rows to be pruned")
	}
	if _, ok := state.Seen["Apple"]["real-job"]; !ok {
		t.Fatalf("expected Apple job row to remain")
	}
}

func TestLooksLikeNoiseJobAllowsEarlyCareerDetailPage(t *testing.T) {
	t.Parallel()

	job := Job{
		Title:       "Software Engineering Intern",
		URL:         "https://careers.example.com/early-careers/software-engineering-intern",
		Location:    "United States",
		Description: "Build backend services, ship features, and collaborate with engineers across the platform team.",
	}

	if looksLikeNoiseJob(job) {
		t.Fatalf("expected early-career detail page to survive quality filtering")
	}
}

func TestEarlyCareerLandingPageStillLooksLikeNoise(t *testing.T) {
	t.Parallel()

	job := Job{
		Title:    "Internships and Programs",
		URL:      "https://careers.example.com/early-careers",
		Location: "United States",
	}

	if !looksLikeNoiseJob(job) {
		t.Fatalf("expected early-career landing page to be treated as noise")
	}
}

func TestOrchestrateCompanyTreatsEmptySourceAsSuccess(t *testing.T) {
	t.Parallel()

	company := Company{
		Name:            "EmptyCo",
		Source:          "command",
		FallbackSources: []string{"unsupported"},
		TimeoutSeconds:  5,
		Command:         "printf '[]'",
	}

	outcome := orchestrateCompany(company, nil)
	if outcome.Status != "ok" {
		t.Fatalf("expected empty source result to be ok, got %q (%s)", outcome.Status, outcome.Message)
	}
	if outcome.SelectedSource != "command" {
		t.Fatalf("expected command source to be selected, got %q", outcome.SelectedSource)
	}
	if len(outcome.Jobs) != 0 {
		t.Fatalf("expected zero jobs, got %d", len(outcome.Jobs))
	}
}

func TestIsRecentEnoughPostedDateDefaultWindowAllowsUnknownAndSevenDays(t *testing.T) {
	t.Setenv("POSTED_TODAY_ONLY", "")
	t.Setenv("MAX_POSTED_AGE_DAYS", "")
	t.Setenv("ALLOW_UNKNOWN_POSTED_AT", "")

	if !isRecentEnoughPostedDate("") {
		t.Fatalf("expected unknown posted_at to pass by default")
	}

	sixDaysAgo := time.Now().AddDate(0, 0, -6).Format(time.RFC3339)
	if !isRecentEnoughPostedDate(sixDaysAgo) {
		t.Fatalf("expected six-day-old job to pass default recency window")
	}

	eightDaysAgo := time.Now().AddDate(0, 0, -8).Format(time.RFC3339)
	if isRecentEnoughPostedDate(eightDaysAgo) {
		t.Fatalf("expected eight-day-old job to fail default recency window")
	}
}

func TestIsRecentEnoughPostedDateCanBeMadeStrictAgain(t *testing.T) {
	t.Setenv("POSTED_TODAY_ONLY", "true")
	t.Setenv("MAX_POSTED_AGE_DAYS", "0")
	t.Setenv("ALLOW_UNKNOWN_POSTED_AT", "false")

	if isRecentEnoughPostedDate("") {
		t.Fatalf("expected unknown posted_at to fail when strict recency is requested")
	}

	yesterday := time.Now().AddDate(0, 0, -1).Format(time.RFC3339)
	if isRecentEnoughPostedDate(yesterday) {
		t.Fatalf("expected yesterday job to fail when today-only recency is requested")
	}
}

func TestIsRelevantCareerJobRejectsSeniorRoleEvenWithResumeProfiles(t *testing.T) {
	resetResumeProfilesCacheForTests()
	t.Setenv("RESUME_PROFILES_FILE", filepath.Join(commandWorkingDir(), "resume_profiles.yaml"))

	job := Job{
		Title:       "Principal Software Engineer",
		URL:         "https://example.com/jobs/1234/principal-software-engineer",
		Location:    "Seattle, Washington, United States",
		Description: "Build distributed backend services in Go and Python.",
	}

	if isRelevantCareerJob(job) {
		t.Fatalf("expected senior/principal role to be rejected even with resume profiles loaded")
	}
}

func TestIsRelevantCareerJobRejectsPreSalesSolutionsEngineerRoles(t *testing.T) {
	resetResumeProfilesCacheForTests()
	t.Setenv("RESUME_PROFILES_FILE", filepath.Join(commandWorkingDir(), "resume_profiles.yaml"))

	job := Job{
		Title:       "Associate Solutions Engineer | DX",
		URL:         "https://example.com/jobs/24717/associate-solutions-engineer",
		Location:    "Salt Lake City, Utah, United States",
		Description: "Partner with enterprise sales teams, deliver product demos, and support proofs of concept for customers.",
	}

	if isRelevantCareerJob(job) {
		t.Fatalf("expected customer-facing solutions engineer role to be rejected")
	}
}

func TestWorkAuthStatusAndNotesBlocksClearanceCitizenshipAndNoSponsorshipRoles(t *testing.T) {
	t.Parallel()

	job := Job{
		Title:    "Software Developer (Systems Software)",
		Location: "McLean, Virginia, United States",
		Description: "GRVTY is seeking a Software Developer with a TS/SCI + Poly clearance. " +
			"U.S. citizenship is a basic security clearance eligibility requirement. " +
			"We are unable to sponsor candidates for a U.S. Security Clearance.",
	}

	status, notes := workAuthStatusAndNotes(job)

	if status != "blocked" {
		t.Fatalf("work auth status = %q, want blocked", status)
	}
	joined := strings.Join(notes, " | ")
	if !strings.Contains(joined, "Security clearance requirement") {
		t.Fatalf("expected clearance note, got %v", notes)
	}
	if !strings.Contains(joined, "US citizenship or U.S. person requirement") {
		t.Fatalf("expected citizenship note, got %v", notes)
	}
	if !strings.Contains(joined, "No sponsorship / restricted work authorization") {
		t.Fatalf("expected sponsorship note, got %v", notes)
	}
}
