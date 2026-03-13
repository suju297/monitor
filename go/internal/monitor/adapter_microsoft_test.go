package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchMicrosoftUsesSearchAPIAndDetailEnrichment(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/api/pcsx/search", func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("start")
		positions := make([]map[string]any, 0, 10)
		switch start {
		case "0":
			for i := 0; i < 10; i++ {
				positions = append(positions, map[string]any{
					"id":                    1000 + i,
					"displayJobId":          fmt.Sprintf("2000%d", i),
					"name":                  fmt.Sprintf("Software Engineer %d", i),
					"standardizedLocations": []any{"Redmond, WA, US"},
					"department":            "Software Engineering",
					"postedTs":              1772763586 + i,
					"positionUrl":           fmt.Sprintf("/careers/job/%d", 1000+i),
				})
			}
		case "10":
			positions = append(positions, map[string]any{
				"id":                    1010,
				"displayJobId":          "200010",
				"name":                  "Principal Software Engineer",
				"standardizedLocations": []any{"Mountain View, CA, US"},
				"department":            "Software Engineering",
				"postedTs":              1772764586,
				"positionUrl":           "/careers/job/1010",
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"positions": positions},
		})
	})

	mux.HandleFunc("/api/pcsx/position_details", func(w http.ResponseWriter, r *http.Request) {
		positionID := r.URL.Query().Get("position_id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"publicUrl":                    server.URL + "/careers/job/" + positionID,
				"jobDescription":               fmt.Sprintf("Build distributed systems and platform services for role %s.", positionID),
				"department":                   "Software Engineering",
				"standardizedLocations":        []any{"Redmond, WA, US"},
				"efcustomTextTaDisciplineName": []any{"Software Engineering"},
				"postedTs":                     1772763586,
			},
		})
	})

	jobs, err := fetchMicrosoft(Company{
		Name:           "Microsoft",
		Source:         "microsoft",
		CareersURL:     server.URL + "/careers?query=software&sort_by=timestamp",
		TimeoutSeconds: 10,
		MaxLinks:       12,
		CommandEnv: map[string]string{
			"MICROSOFT_MAX_PAGES":          "3",
			"MICROSOFT_DETAIL_FETCH_LIMIT": "12",
			"MICROSOFT_DETAIL_WORKERS":     "2",
		},
	})
	if err != nil {
		t.Fatalf("fetchMicrosoft error: %v", err)
	}
	if len(jobs) != 11 {
		t.Fatalf("job count = %d, want %d", len(jobs), 11)
	}
	if jobs[0].URL != server.URL+"/careers/job/1000" {
		t.Fatalf("first URL = %q, want %q", jobs[0].URL, server.URL+"/careers/job/1000")
	}
	if !strings.HasPrefix(jobs[0].PostedAt, "2026-") {
		t.Fatalf("first posted_at = %q, want 2026 timestamp", jobs[0].PostedAt)
	}
	if !strings.Contains(jobs[0].Description, "Build distributed systems") {
		t.Fatalf("first description = %q", jobs[0].Description)
	}
	if jobs[10].Title != "Principal Software Engineer" {
		t.Fatalf("last title = %q, want Principal Software Engineer", jobs[10].Title)
	}
}
