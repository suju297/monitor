package monitor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func dashboardJobTitles(jobs []DashboardJob) []string {
	titles := make([]string, 0, len(jobs))
	for _, job := range jobs {
		titles = append(titles, job.Title)
	}
	return titles
}

func TestFrontendRouteReady(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/monitor" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if !frontendRouteReady(server.URL, "/monitor") {
		t.Fatalf("expected frontend route to be detected as reachable")
	}
	if frontendRouteReady("http://127.0.0.1:1", "/monitor") {
		t.Fatalf("expected unreachable frontend route to be reported as unavailable")
	}
}

func TestServeFrontendOrEmbeddedHTMLFallsBack(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/monitor", nil)
	rec := httptest.NewRecorder()

	serveFrontendOrEmbeddedHTML(rec, req, "http://127.0.0.1:1", "/monitor", "<html>fallback monitor</html>")

	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "fallback monitor") {
		t.Fatalf("expected embedded fallback HTML body, got %q", body)
	}
}

func TestServeFrontendOrEmbeddedHTMLRedirectsWhenFrontendReady(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/jobs?company=Apple", nil)
	rec := httptest.NewRecorder()

	serveFrontendOrEmbeddedHTML(rec, req, server.URL, "/jobs", "<html>fallback jobs</html>")

	res := rec.Result()
	if res.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusTemporaryRedirect)
	}
	location := res.Header.Get("Location")
	if location != server.URL+"/jobs?company=Apple" {
		t.Fatalf("location = %q, want %q", location, server.URL+"/jobs?company=Apple")
	}
}

func TestSortDashboardJobsNewestDoesNotTreatDiscoveryTimeAsPostedDate(t *testing.T) {
	t.Parallel()

	jobs := []DashboardJob{
		{
			Company:   "Example",
			Title:     "Unknown Posted Date",
			FirstSeen: "2026-03-06T12:00:00Z",
		},
		{
			Company:   "Example",
			Title:     "Posted Yesterday",
			PostedAt:  "2026-03-05T08:00:00Z",
			FirstSeen: "2026-03-06T07:00:00Z",
		},
		{
			Company:   "Example",
			Title:     "Posted Last Week",
			PostedAt:  "2026-02-28T08:00:00Z",
			FirstSeen: "2026-03-06T11:00:00Z",
		},
	}

	sortDashboardJobs(jobs, "newest")

	got := strings.Join(dashboardJobTitles(jobs), " | ")
	want := "Posted Yesterday | Posted Last Week | Unknown Posted Date"
	if got != want {
		t.Fatalf("newest order = %q, want %q", got, want)
	}
}

func TestSortDashboardJobsBestMatchPrefersRealPostedDateOverDiscoveryTime(t *testing.T) {
	t.Parallel()

	jobs := []DashboardJob{
		{
			Company:    "Example",
			Title:      "Unknown Posted Date",
			FirstSeen:  "2026-03-06T12:00:00Z",
			MatchScore: 88,
		},
		{
			Company:    "Example",
			Title:      "Real Posted Date",
			PostedAt:   "2026-03-05T08:00:00Z",
			FirstSeen:  "2026-03-01T08:00:00Z",
			MatchScore: 88,
		},
	}

	sortDashboardJobs(jobs, "best_match")

	if jobs[0].Title != "Real Posted Date" {
		t.Fatalf("best_match first title = %q, want %q", jobs[0].Title, "Real Posted Date")
	}
}

func TestFilterJobsForFeedFiltersBySource(t *testing.T) {
	t.Parallel()

	jobs := []DashboardJob{
		{Company: "Example", Title: "Greenhouse Job", Source: "greenhouse"},
		{Company: "Example", Title: "Ashby Job", Source: "ashby"},
	}

	filtered := filterJobsForFeed(jobs, "", "", "greenhouse", "", "", true)
	if len(filtered) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0].Source != "greenhouse" {
		t.Fatalf("source = %q, want %q", filtered[0].Source, "greenhouse")
	}
}

func TestFilterJobsForFeedPublishesDeterministicDecisionMetadata(t *testing.T) {
	t.Parallel()

	filtered := filterJobsForFeed([]DashboardJob{
		{
			Company:     "Example",
			Title:       "Backend Engineer",
			Source:      "greenhouse",
			Location:    "Remote (US)",
			Description: "Build backend services in Go and AWS.",
		},
	}, "", "", "", "", "", true)
	if len(filtered) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0].DecisionSource != "deterministic" {
		t.Fatalf("decision source = %q, want %q", filtered[0].DecisionSource, "deterministic")
	}
	if filtered[0].RoleDecision != roleDecisionIn {
		t.Fatalf("role decision = %q, want %q", filtered[0].RoleDecision, roleDecisionIn)
	}
	if filtered[0].InternshipDecision != "not_applicable" {
		t.Fatalf("internship decision = %q, want %q", filtered[0].InternshipDecision, "not_applicable")
	}
}

func TestFilterJobsForFeedExcludesDeterministicRoleOutJobs(t *testing.T) {
	t.Parallel()

	filtered := filterJobsForFeed([]DashboardJob{
		{
			Company:     "Example",
			Title:       "Entry Level Product Manager | US | Remote",
			Source:      "greenhouse",
			Location:    "Remote (US)",
			Description: "Drive product planning for developer tools and platform workflows.",
		},
		{
			Company:     "Example",
			Title:       "Technical Content Writer",
			Source:      "greenhouse",
			Location:    "New York, New York, USA",
			Team:        "Engineering | Community | Technical Community",
			Description: "Write technical product content, guides, and launch materials for developers.",
		},
	}, "", "", "", "", "", false)
	if len(filtered) != 0 {
		t.Fatalf("filtered len = %d, want 0", len(filtered))
	}
}

func TestJobsFeedHeuristicArtifactsForDashboardRecomputesRoleOutWhenContextChanges(t *testing.T) {
	t.Parallel()

	artifacts := jobsFeedHeuristicArtifactsForDashboard(DashboardJob{
		Company:                   "Grafana Labs",
		Title:                     "Entry Level Product Manager | US | Remote",
		Source:                    "greenhouse",
		Location:                  "United States (Remote)",
		Team:                      "Product Management",
		URL:                       "https://job-boards.greenhouse.io/grafanalabs/jobs/5795333004",
		Description:               "Grafana Labs is a remote-first, open-source powerhouse. There are more than 20M users of Grafana around the globe.",
		HeuristicCached:           true,
		HeuristicContextHash:      "523acb4d5bbfaa0a2718b835bcc9026d72212878",
		FeedRelevantCached:        true,
		DeterministicRoleDecision: roleDecisionOut,
		MatchScore:                50,
		RecommendedResume:         "Backend Resume",
	})

	if artifacts.ContextHash == "523acb4d5bbfaa0a2718b835bcc9026d72212878" {
		t.Fatalf("expected heuristic context hash to change after classifier update")
	}
	if artifacts.RoleDecision != roleDecisionOut {
		t.Fatalf("role decision = %q, want %q", artifacts.RoleDecision, roleDecisionOut)
	}
	if artifacts.RelevantForFeed {
		t.Fatalf("expected role-out job to be irrelevant for the normal feed")
	}
	if artifacts.RecommendedResume != "" {
		t.Fatalf("recommended resume = %q, want empty for role-out job", artifacts.RecommendedResume)
	}
	if artifacts.MatchScore > 35 {
		t.Fatalf("match score = %d, want <= 35 for role-out job", artifacts.MatchScore)
	}
}

func TestFilterJobsForFeedOverridesStaleCachedRoleDecision(t *testing.T) {
	t.Parallel()

	filtered := filterJobsForFeed([]DashboardJob{
		{
			Company:                   "Grafana Labs",
			Title:                     "Entry Level Product Manager | US | Remote",
			Source:                    "greenhouse",
			Location:                  "United States (Remote)",
			Team:                      "Product Management",
			URL:                       "https://job-boards.greenhouse.io/grafanalabs/jobs/5795333004",
			Description:               "Grafana Labs is a remote-first, open-source powerhouse.",
			HeuristicCached:           true,
			HeuristicContextHash:      jobsFeedHeuristicContextHash(),
			FeedRelevantCached:        true,
			DeterministicRoleDecision: roleDecisionIn,
			MatchScore:                70,
			RecommendedResume:         "Backend Resume",
		},
	}, "", "", "", "", "", false)
	if len(filtered) != 0 {
		t.Fatalf("filtered len = %d, want 0", len(filtered))
	}
}
