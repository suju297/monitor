package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveSourcePlanUsesDeterministicAdaptiveByDefault(t *testing.T) {
	t.Setenv("ORCHESTRATOR_SLM_PROVIDER", "ollama")
	t.Setenv("ORCHESTRATOR_SLM_EXPERIMENTAL", "false")

	company := Company{
		Name:            "Example",
		Source:          "greenhouse",
		FallbackSources: []string{"generic"},
	}
	prior := &CompanyStatus{
		Status:           "blocked",
		AttemptedSources: []string{"greenhouse"},
	}

	got := ResolveSourcePlan(company, prior)
	want := []string{"generic", "greenhouse"}
	if len(got) != len(want) {
		t.Fatalf("unexpected plan length: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected plan: got %v want %v", got, want)
		}
	}
}

func TestResolveSourcePlanSkipsSLMForSingleSource(t *testing.T) {
	t.Setenv("ORCHESTRATOR_SLM_PROVIDER", "ollama")
	t.Setenv("ORCHESTRATOR_SLM_EXPERIMENTAL", "true")

	company := Company{
		Name:   "Example",
		Source: "greenhouse",
	}

	got := ResolveSourcePlan(company, nil)
	want := []string{"greenhouse"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected plan: got %v want %v", got, want)
	}
}

func TestResolveSourcePlanUsesSLMOnlyWhenExperimentalAndMultiSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "phi4-mini:latest" {
			t.Fatalf("unexpected model: %v", payload["model"])
		}
		resp := map[string]any{
			"message": map[string]any{
				"content": "{\"plan\":[\"generic\",\"greenhouse\"]}",
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	t.Setenv("ORCHESTRATOR_SLM_PROVIDER", "ollama")
	t.Setenv("ORCHESTRATOR_SLM_EXPERIMENTAL", "true")
	t.Setenv("ORCHESTRATOR_SLM_URL", server.URL)
	t.Setenv("ORCHESTRATOR_SLM_MODEL", "phi4-mini:latest")

	company := Company{
		Name:            "Example",
		Source:          "greenhouse",
		FallbackSources: []string{"generic"},
	}

	got := ResolveSourcePlan(company, nil)
	want := []string{"generic", "greenhouse"}
	if len(got) != len(want) {
		t.Fatalf("unexpected plan length: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected plan: got %v want %v", got, want)
		}
	}
}
