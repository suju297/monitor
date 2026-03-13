package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchLeverFallsBackToHostedHTMLWhenAPIFails(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/api/palantir", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
	})
	mux.HandleFunc("/boards/palantir", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body>
			<div class="postings-group">
				<div class="posting">
					<a class="posting-title" href="/boards/palantir/role-1">
						<h5 data-qa="posting-name">Software Engineer, Infrastructure</h5>
					</a>
					<div class="posting-categories">
						<span class="posting-category location">New York, NY</span>
						<span class="posting-category commitment">Full-time</span>
					</div>
				</div>
			</div>
		</body></html>`)
	})

	jobs, err := fetchLever(Company{
		Name:           "Palantir",
		Source:         "lever",
		CareersURL:     server.URL + "/boards/palantir",
		LeverSite:      "palantir",
		TimeoutSeconds: 10,
		MaxLinks:       10,
		CommandEnv: map[string]string{
			"LEVER_API_BASE_URL":   server.URL + "/api",
			"LEVER_BOARD_BASE_URL": server.URL + "/boards",
		},
	})
	if err != nil {
		t.Fatalf("fetchLever error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("job count = %d, want 1", len(jobs))
	}
	if jobs[0].Title != "Software Engineer, Infrastructure" {
		t.Fatalf("title = %q", jobs[0].Title)
	}
	if jobs[0].Location != "New York, NY" {
		t.Fatalf("location = %q", jobs[0].Location)
	}
	if jobs[0].Team != "Full-time" {
		t.Fatalf("team = %q", jobs[0].Team)
	}
}
