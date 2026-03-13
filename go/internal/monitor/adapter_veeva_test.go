package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchVeevaUsesEmbeddedJobsArrayAndFilters(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/job-search-results/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><script>
			let allJobs = [
				{"id":"1","lever_id":"abc-123","job_title":"Software Engineer","team":"Engineering","region":"United States","city":"Boston","country":"MA","remote":"0","keywords":"software backend platform","slug":"software-engineer-boston"},
				{"id":"2","lever_id":"def-456","job_title":"Account Executive","team":"Sales","region":"United States","city":"New York","country":"NY","remote":"0","keywords":"sales enterprise","slug":"account-executive-new-york"},
				{"id":"3","lever_id":"ghi-789","job_title":"Software Engineer","team":"Engineering","region":"Europe","city":"Paris","country":"France","remote":"1","keywords":"software backend platform","slug":"software-engineer-paris"}
			];
		</script></body></html>`)
	})

	jobs, err := fetchVeeva(Company{
		Name:           "Veeva Systems",
		Source:         "veeva",
		CareersURL:     server.URL + "/job-search-results/?search=software&remote=false&ts=Engineering&regions=United+States",
		TimeoutSeconds: 10,
		MaxLinks:       10,
	})
	if err != nil {
		t.Fatalf("fetchVeeva error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("job count = %d, want 1", len(jobs))
	}
	if jobs[0].URL != server.URL+"/job/abc-123/software-engineer-boston/?lever-source=veeva-career-site" {
		t.Fatalf("job URL = %q", jobs[0].URL)
	}
	if jobs[0].Location != "Boston, MA, United States" {
		t.Fatalf("location = %q", jobs[0].Location)
	}
	if jobs[0].Team != "Engineering" {
		t.Fatalf("team = %q", jobs[0].Team)
	}
}
