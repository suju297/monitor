package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchGenericFollowsPaginationLinks(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/careers", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch page {
		case "", "1":
			fmt.Fprintf(w, `
				<html><body>
					<ul>
						<li><a href="/jobs/1">Software Engineer I</a><span>Remote, US</span></li>
						<li><a href="/jobs/2">Backend Engineer</a><span>Austin, TX</span></li>
					</ul>
					<nav><a href="/careers?page=2" rel="next">Next</a></nav>
				</body></html>
			`)
		case "2":
			fmt.Fprintf(w, `
				<html><body>
					<ul>
						<li><a href="/jobs/3">Platform Engineer</a><span>New York, NY</span></li>
						<li><a href="/jobs/4">Data Engineer</a><span>Remote, US</span></li>
					</ul>
				</body></html>
			`)
		default:
			http.NotFound(w, r)
		}
	})
	for i, title := range []string{"Software Engineer I", "Backend Engineer", "Platform Engineer", "Data Engineer"} {
		title := title
		mux.HandleFunc(fmt.Sprintf("/jobs/%d", i+1), func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `<html><body><main><h1>%s</h1><p>Responsibilities include building production services and backend systems.</p></main></body></html>`, title)
		})
	}

	jobs, err := fetchGeneric(Company{
		Name:           "TestCo",
		Source:         "generic",
		CareersURL:     server.URL + "/careers?page=1",
		TimeoutSeconds: 10,
		MaxLinks:       10,
	})
	if err != nil {
		t.Fatalf("fetchGeneric error: %v", err)
	}
	if len(jobs) != 4 {
		t.Fatalf("job count = %d, want %d", len(jobs), 4)
	}
}
