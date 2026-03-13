package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchGoogleUsesEmbeddedPayloadAndHrefMap(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	googleRow := func(id string, title string, locations []string, postedAt int64) []any {
		locationRows := make([]any, 0, len(locations))
		for _, location := range locations {
			locationRows = append(locationRows, []any{location})
		}
		return []any{
			id,
			title,
			"",
			[]any{nil, "<p>Build backend services and APIs.</p>"},
			[]any{nil, "<ul><li>Experience with distributed systems.</li></ul>"},
			nil,
			nil,
			"Google",
			"en-US",
			locationRows,
			[]any{nil, "<p>Work on cloud infrastructure and AI products.</p>"},
			nil,
			[]any{postedAt, 0},
			nil,
			nil,
			nil,
			nil,
			nil,
			[]any{nil, "<p>Remote eligible.</p>"},
			2,
		}
	}

	mux.HandleFunc("/jobs/results/", func(w http.ResponseWriter, r *http.Request) {
		payload := []any{
			[]any{
				googleRow("9001", "Senior Software Engineer", []string{"New York, NY, USA", "Seattle, WA, USA"}, 1772763586),
				googleRow("9002", "Staff Software Engineer, AI/ML", []string{"Mountain View, CA, USA"}, 1772764586),
			},
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal google payload: %v", err)
		}
		fmt.Fprintf(w, `<html><body>
			<script>AF_initDataCallback({key: 'ds:1', hash: '2', data:%s});</script>
			<a href="jobs/results/9001-senior-software-engineer?q=software" aria-label="Learn more about Senior Software Engineer"></a>
			<a href="jobs/results/9002-staff-software-engineer-aiml?q=software" aria-label="Learn more about Staff Software Engineer, AI/ML"></a>
		</body></html>`, payloadJSON)
	})

	jobs, err := fetchGoogle(Company{
		Name:           "Google",
		Source:         "google",
		CareersURL:     server.URL + "/jobs/results/?q=software",
		TimeoutSeconds: 10,
		MaxLinks:       10,
		CommandEnv: map[string]string{
			"GOOGLE_MAX_PAGES": "2",
		},
	})
	if err != nil {
		t.Fatalf("fetchGoogle error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("job count = %d, want %d", len(jobs), 2)
	}
	if jobs[0].URL != server.URL+"/jobs/results/9001-senior-software-engineer?q=software" {
		t.Fatalf("first URL = %q", jobs[0].URL)
	}
	if jobs[0].Location != "New York, NY, USA; Seattle, WA, USA" {
		t.Fatalf("first location = %q", jobs[0].Location)
	}
	if !strings.HasPrefix(jobs[0].PostedAt, "2026-") {
		t.Fatalf("first posted_at = %q, want 2026 timestamp", jobs[0].PostedAt)
	}
	if !strings.Contains(jobs[0].Description, "distributed systems") {
		t.Fatalf("first description = %q", jobs[0].Description)
	}
}
