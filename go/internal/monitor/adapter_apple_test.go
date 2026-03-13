package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchAppleUsesListingPaginationAndDedupesDetailLinks(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			fmt.Fprintf(w, `
				<html><body>
					<ul>
						<li class="rc-accordion-item">
							<div class="job-title">
								<h3><a href="/en-us/details/2001-0836/software-engineer?team=SFTWR">Software Engineer</a></h3>
								<span class="team-name">Software and Services</span>
								<span class="job-posted-date">Mar 06, 2026</span>
								<div><span>Cupertino</span></div>
							</div>
							<div class="job-summary">
								<p>Build distributed systems and backend services for Apple platforms.</p>
								<a href="/en-us/details/2001-0836/software-engineer?team=SFTWR">See full role description</a>
							</div>
						</li>
					</ul>
				</body></html>
			`)
		case "2":
			fmt.Fprintf(w, `
				<html><body>
					<ul>
						<li class="rc-accordion-item">
							<div class="job-title">
								<h3><a href="/en-us/details/2002-3956/platform-engineer?team=SFTWR">Platform Engineer</a></h3>
								<span class="team-name">Software and Services</span>
								<span class="job-posted-date">Mar 05, 2026</span>
								<div><span>Seattle</span></div>
							</div>
							<div class="job-summary">
								<p>Scale cloud infrastructure and platform tooling for product teams.</p>
							</div>
						</li>
					</ul>
				</body></html>
			`)
		default:
			fmt.Fprint(w, `<html><body><ul></ul></body></html>`)
		}
	})

	jobs, err := fetchApple(Company{
		Name:           "Apple",
		Source:         "apple",
		CareersURL:     server.URL + "/search?search=software&location=united-states-USA",
		TimeoutSeconds: 10,
		MaxLinks:       10,
	})
	if err != nil {
		t.Fatalf("fetchApple error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("job count = %d, want %d", len(jobs), 2)
	}
	if jobs[0].Title != "Software Engineer" {
		t.Fatalf("first title = %q, want Software Engineer", jobs[0].Title)
	}
	if jobs[0].Team != "Software and Services" {
		t.Fatalf("first team = %q, want Software and Services", jobs[0].Team)
	}
	if jobs[0].Location != "Cupertino" {
		t.Fatalf("first location = %q, want Cupertino", jobs[0].Location)
	}
	if jobs[1].Title != "Platform Engineer" {
		t.Fatalf("second title = %q, want Platform Engineer", jobs[1].Title)
	}
}
