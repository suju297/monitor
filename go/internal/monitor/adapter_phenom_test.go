package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPhenomUsesEmbeddedJSONPaginationAndApplyURLs(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	writePage := func(w http.ResponseWriter, jobsJSON string) {
		fmt.Fprintf(w, `<html><body><script>phApp.ddo = {"payload":{"jobs":%s}}</script></body></html>`, jobsJSON)
	}

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("from") {
		case "", "0":
			writePage(w, `[
				{
					"jobSeqNo":"ADOBE-1",
					"reqId":"R1",
					"title":"Software Development Engineer",
					"location":"San Jose, California, United States of America",
					"category":"Engineering and Product",
					"postedDate":"2026-03-05T00:00:00.000+0000",
					"descriptionTeaser":"Build distributed systems for creative tooling.",
					"applyUrl":"`+server.URL+`/job/San-Jose/Software-Development-Engineer_R1/apply"
				}
			]`)
		case "10":
			writePage(w, `[
				{
					"jobSeqNo":"ADOBE-2",
					"reqId":"R2",
					"title":"Platform Engineer",
					"location":"Seattle, Washington, United States of America",
					"category":"Engineering and Product",
					"postedDate":"2026-03-04T00:00:00.000+0000",
					"descriptionTeaser":"Scale platform infrastructure for core services.",
					"applyUrl":"`+server.URL+`/job/Seattle/Platform-Engineer_R2/apply"
				}
			]`)
		default:
			writePage(w, `[]`)
		}
	})

	jobs, err := fetchPhenom(Company{
		Name:           "Adobe",
		Source:         "phenom",
		CareersURL:     server.URL + "/search?keywords=software",
		TimeoutSeconds: 10,
		MaxLinks:       20,
		CommandEnv: map[string]string{
			"PHENOM_MAX_PAGES":                "3",
			"PHENOM_PAGE_SIZE":                "10",
			"PHENOM_LOCATION_INCLUDE_PATTERN": `(?i)united states`,
		},
	})
	if err != nil {
		t.Fatalf("fetchPhenom error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("job count = %d, want %d", len(jobs), 2)
	}
	if jobs[0].URL != server.URL+"/job/San-Jose/Software-Development-Engineer_R1" {
		t.Fatalf("first URL = %q", jobs[0].URL)
	}
	if jobs[0].ExternalID != "R1" {
		t.Fatalf("first external id = %q, want R1", jobs[0].ExternalID)
	}
	if jobs[1].Title != "Platform Engineer" {
		t.Fatalf("second title = %q, want Platform Engineer", jobs[1].Title)
	}
}
