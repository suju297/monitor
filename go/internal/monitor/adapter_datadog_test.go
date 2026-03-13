package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchDatadogUsesTypesensePaginationAndSortsByPostedAt(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/collections/test_roles/documents/search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-typesense-api-key"); got != "test-key" {
			t.Fatalf("typesense api key = %q, want test-key", got)
		}
		if got := r.URL.Query().Get("q"); got != "software" {
			t.Fatalf("q = %q, want software", got)
		}

		page := r.URL.Query().Get("page")
		hits := []map[string]any{}
		switch page {
		case "1":
			hits = append(hits,
				map[string]any{
					"document": map[string]any{
						"id":                           "doc-1",
						"job_id":                       7001,
						"title":                        "Software Engineer",
						"absolute_url":                 "https://careers.datadoghq.com/detail/7001/?gh_jid=7001",
						"department":                   "Engineering",
						"team":                         "Dev Eng",
						"child_department_Engineering": "Frontend",
						"location_string":              "New York, New York, USA",
						"last_mod":                     "2026-03-04T10:00:00Z",
						"description":                  "Build frontend platform systems.",
					},
				},
				map[string]any{
					"document": map[string]any{
						"id":                           "doc-2",
						"job_id":                       7002,
						"title":                        "Platform Engineer",
						"absolute_url":                 "https://careers.datadoghq.com/detail/7002/?gh_jid=7002",
						"department":                   "Engineering",
						"team":                         "Core Platform",
						"child_department_Engineering": "Infrastructure",
						"location_string":              "Boston, Massachusetts, USA",
						"last_mod":                     "2026-03-02T10:00:00Z",
						"description":                  "Scale internal platform services.",
					},
				},
			)
		case "2":
			hits = append(hits, map[string]any{
				"document": map[string]any{
					"id":                           "doc-3",
					"job_id":                       7003,
					"title":                        "Senior Software Engineer",
					"absolute_url":                 "https://careers.datadoghq.com/detail/7003/?gh_jid=7003",
					"department":                   "Engineering",
					"team":                         "Dev Eng",
					"child_department_Engineering": "Backend",
					"location_string":              "Seattle, Washington, USA",
					"last_mod":                     "2026-03-05T10:00:00Z",
					"description":                  "Own distributed backend services.",
				},
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hits": hits,
		})
	})

	jobs, err := fetchDatadog(Company{
		Name:           "Datadog",
		Source:         "datadog",
		CareersURL:     "https://careers.datadoghq.com/all-jobs/",
		TimeoutSeconds: 10,
		MaxLinks:       10,
		CommandEnv: map[string]string{
			"DATADOG_TYPESENSE_BASE_URL":   server.URL,
			"DATADOG_TYPESENSE_COLLECTION": "test_roles",
			"DATADOG_TYPESENSE_KEY":        "test-key",
			"DATADOG_QUERY":                "software",
			"DATADOG_PAGE_SIZE":            "2",
			"DATADOG_MAX_PAGES":            "3",
		},
	})
	if err != nil {
		t.Fatalf("fetchDatadog error: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("job count = %d, want 3", len(jobs))
	}
	if jobs[0].Title != "Senior Software Engineer" {
		t.Fatalf("first title = %q, want Senior Software Engineer", jobs[0].Title)
	}
	if jobs[0].PostedAt != "2026-03-05T10:00:00Z" {
		t.Fatalf("first posted_at = %q", jobs[0].PostedAt)
	}
	if jobs[1].Team != "Engineering | Dev Eng | Frontend" {
		t.Fatalf("second team = %q", jobs[1].Team)
	}
}
