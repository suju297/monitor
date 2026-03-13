package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchCapitalOneUsesNativeHTMLPagination(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("p") {
		case "", "1":
			fmt.Fprintf(w, `<html><body>
				<section id="search-results-list">
					<ul>
						<li>
							<a href="/job/richmond/software-engineer/1732/1111" data-job-id="1111">
								<div class="job-search-info"><span>1111</span><span class="job-date-posted">03/06/2026</span></div>
								<h2>Software Engineer</h2>
								<span class="job-location">Richmond, VA</span>
							</a>
						</li>
					</ul>
					<nav class="pagination">
						<div class="pagination-paging">
							<a class="next" href="/search?p=2" rel="nofollow">Next</a>
						</div>
					</nav>
				</section>
			</body></html>`)
		case "2":
			fmt.Fprintf(w, `<html><body>
				<section id="search-results-list">
					<ul>
						<li>
							<a href="/job/mclean/platform-engineer/1732/2222" data-job-id="2222">
								<div class="job-search-info"><span>2222</span><span class="job-date-posted">03/05/2026</span></div>
								<h2>Platform Engineer</h2>
								<span class="job-location">McLean, VA</span>
							</a>
						</li>
					</ul>
				</section>
			</body></html>`)
		default:
			http.NotFound(w, r)
		}
	})

	jobs, err := fetchCapitalOne(Company{
		Name:           "Capital One",
		Source:         "capitalone",
		CareersURL:     server.URL + "/search?p=1",
		TimeoutSeconds: 10,
		MaxLinks:       10,
		CommandEnv: map[string]string{
			"CAPITALONE_MAX_PAGES": "4",
		},
	})
	if err != nil {
		t.Fatalf("fetchCapitalOne error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("job count = %d, want 2", len(jobs))
	}
	if jobs[0].ExternalID != "1111" {
		t.Fatalf("first external id = %q", jobs[0].ExternalID)
	}
	if !strings.HasPrefix(jobs[0].PostedAt, "2026-03-06") {
		t.Fatalf("first posted_at = %q", jobs[0].PostedAt)
	}
	if jobs[1].Location != "McLean, VA" {
		t.Fatalf("second location = %q", jobs[1].Location)
	}
}
