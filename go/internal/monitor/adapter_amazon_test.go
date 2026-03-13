package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchAmazonUsesSearchJSONPagination(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/en/search.json", func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		if got := r.URL.Query().Get("base_query"); got != "software" {
			t.Fatalf("base_query = %q, want software", got)
		}
		if got := r.URL.Query().Get("loc_query"); got != "United States" {
			t.Fatalf("loc_query = %q, want United States", got)
		}

		jobs := []map[string]any{}
		switch offset {
		case "0":
			jobs = append(jobs,
				map[string]any{
					"id_icims":            "1001",
					"title":               "Software Engineer I",
					"job_path":            "/en/jobs/1001/software-engineer-i",
					"normalized_location": "Seattle, Washington, USA",
					"job_category":        "Software Development",
					"posted_date":         "March 5, 2026",
					"description":         "Build backend services for Amazon retail systems.",
				},
				map[string]any{
					"id_icims":            "1002",
					"title":               "Platform Engineer",
					"job_path":            "/en/jobs/1002/platform-engineer",
					"normalized_location": "Austin, Texas, USA",
					"job_family":          "Infrastructure Engineering",
					"posted_date":         "March 4, 2026",
					"description_short":   "Scale cloud platforms.",
				},
			)
		case "2":
			jobs = append(jobs, map[string]any{
				"id_icims":            "1003",
				"title":               "Senior Software Engineer",
				"job_path":            "/en/jobs/1003/senior-software-engineer",
				"normalized_location": "New York, New York, USA",
				"job_category":        "Software Development",
				"posted_date":         "March 3, 2026",
				"description":         "Own distributed systems and API platforms.",
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jobs": jobs,
		})
	})

	jobs, err := fetchAmazon(Company{
		Name:           "Amazon",
		Source:         "amazon",
		CareersURL:     server.URL + "/en/search?base_query=software&loc_query=United%20States",
		TimeoutSeconds: 10,
		MaxLinks:       10,
		CommandEnv: map[string]string{
			"AMAZON_PAGE_SIZE": "2",
			"AMAZON_MAX_PAGES": "3",
		},
	})
	if err != nil {
		t.Fatalf("fetchAmazon error: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("job count = %d, want 3", len(jobs))
	}
	if jobs[0].URL != server.URL+"/en/jobs/1001/software-engineer-i" {
		t.Fatalf("first URL = %q", jobs[0].URL)
	}
	if jobs[1].Team != "Infrastructure Engineering" {
		t.Fatalf("second team = %q, want Infrastructure Engineering", jobs[1].Team)
	}
	if !strings.Contains(jobs[2].Description, "distributed systems") {
		t.Fatalf("third description = %q", jobs[2].Description)
	}
}
