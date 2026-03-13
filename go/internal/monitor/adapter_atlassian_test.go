package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchAtlassianUsesListingsEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/endpoint/careers/listings", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{
				"id": 22932,
				"title": "Principal Software Engineer",
				"type": "Full-Time",
				"locations": [
					"San Francisco - United States - San Francisco, California 94104 United States",
					"Remote - Remote"
				],
				"category": "Engineering",
				"overview": "<p>Build distributed platform services.</p>",
				"responsibilities": "<p>Design and operate backend systems.</p>",
				"qualifications": "<p>Go or Java experience.</p>",
				"portalJobPost": {
					"id": 22932,
					"portalUrl": "`+server.URL+`/jobs/22932/principal-software-engineer/job",
					"updatedDate": "2026-03-05 09:02 AM"
				},
				"applyUrl": "`+server.URL+`/jobs/22932/principal-software-engineer/job?mode=apply"
			},
			{
				"id": 20961,
				"title": "Research Intern, 2026 Summer U.S.",
				"locations": ["Seattle - United States - Seattle, Washington 98101 United States"],
				"category": "Interns",
				"portalJobPost": {
					"id": 20961,
					"portalUrl": "`+server.URL+`/jobs/20961/research-intern/job",
					"updatedDate": "2026-03-04 02:30 PM"
				},
				"overview": "<p>Research AI systems.</p>",
				"responsibilities": "<p>Build evaluation pipelines.</p>",
				"qualifications": "<p>Python experience.</p>"
			}
		]`)
	})

	jobs, err := fetchAtlassian(Company{
		Name:           "Atlassian",
		Source:         "atlassian",
		CareersURL:     server.URL + "/company/careers/all-jobs",
		TimeoutSeconds: 10,
		MaxLinks:       10,
	})
	if err != nil {
		t.Fatalf("fetchAtlassian error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("job count = %d, want 2", len(jobs))
	}
	if jobs[0].URL != server.URL+"/jobs/22932/principal-software-engineer/job" {
		t.Fatalf("first URL = %q", jobs[0].URL)
	}
	if jobs[0].Team != "Engineering" {
		t.Fatalf("first team = %q, want Engineering", jobs[0].Team)
	}
	if jobs[0].PostedAt == "" {
		t.Fatalf("expected first job posted_at to be parsed")
	}
	if !strings.Contains(jobs[0].Description, "backend systems") {
		t.Fatalf("expected first description to contain merged responsibilities, got %q", jobs[0].Description)
	}
	if jobs[1].ExternalID != "20961" {
		t.Fatalf("second external id = %q, want 20961", jobs[1].ExternalID)
	}
}
