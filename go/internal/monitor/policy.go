package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type policyPayload struct {
	Company     Company        `json:"company"`
	PriorStatus *CompanyStatus `json:"prior_status,omitempty"`
	AllowedPlan []string       `json:"allowed_plan"`
}

func BaseSourcePlan(company Company) []string {
	plan := []string{strings.ToLower(strings.TrimSpace(company.Source))}
	for _, fallback := range company.FallbackSources {
		plan = append(plan, strings.ToLower(strings.TrimSpace(fallback)))
	}
	return uniqueStrings(plan)
}

func AdaptiveSourcePlan(company Company, priorStatus *CompanyStatus) []string {
	plan := BaseSourcePlan(company)
	if priorStatus == nil {
		return plan
	}
	if strings.ToLower(strings.TrimSpace(priorStatus.Status)) != "blocked" {
		return plan
	}
	if len(priorStatus.AttemptedSources) == 0 {
		return plan
	}
	blockedPrimary := strings.ToLower(strings.TrimSpace(priorStatus.AttemptedSources[0]))
	if blockedPrimary == "" {
		return plan
	}
	contains := false
	for _, source := range plan {
		if source == blockedPrimary {
			contains = true
			break
		}
	}
	if !contains {
		return plan
	}
	reordered := make([]string, 0, len(plan))
	for _, source := range plan {
		if source == blockedPrimary {
			continue
		}
		reordered = append(reordered, source)
	}
	reordered = append(reordered, blockedPrimary)
	return uniqueStrings(reordered)
}

func policyHookPlan(payload policyPayload) []string {
	policyCmd := strings.TrimSpace(os.Getenv("ORCHESTRATOR_POLICY_CMD"))
	if policyCmd == "" {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-lc", policyCmd)
	cmd.Stdin = bytes.NewReader(raw)
	stdout, err := cmd.Output()
	if err != nil {
		return nil
	}
	var response map[string]any
	if err := json.Unmarshal(stdout, &response); err != nil {
		return nil
	}
	list := asSlice(response["plan"])
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, sourceAny := range list {
		source := strings.ToLower(strings.TrimSpace(asString(sourceAny)))
		if source == "" {
			continue
		}
		if _, ok := SupportedSources[source]; !ok {
			continue
		}
		out = append(out, source)
	}
	out = uniqueStrings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func slmPlan(payload policyPayload) []string {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("ORCHESTRATOR_SLM_PROVIDER")))
	if provider == "" {
		return nil
	}
	if provider != "ollama" {
		return nil
	}

	endpoint := strings.TrimSpace(os.Getenv("ORCHESTRATOR_SLM_URL"))
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434/api/chat"
	}
	model := strings.TrimSpace(os.Getenv("ORCHESTRATOR_SLM_MODEL"))
	if model == "" {
		model = "llama3.2:3b"
	}
	timeoutSeconds := 8
	if raw := strings.TrimSpace(os.Getenv("ORCHESTRATOR_SLM_TIMEOUT_SECONDS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			timeoutSeconds = parsed
		}
	}

	payloadJSON, _ := json.Marshal(payload)
	systemPrompt := "You are a source-plan router for job crawling. Return strict JSON only with shape {\"plan\":[\"source1\",\"source2\"]}. Use only sources from allowed_plan. No commentary."
	userPrompt := fmt.Sprintf("Choose source plan for this payload: %s", string(payloadJSON))
	requestBody := map[string]any{
		"model":  model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
	}
	applyOllamaModelTuning(requestBody, model)

	rawReq, err := json.Marshal(requestBody)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(rawReq))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var ollama map[string]any
	if err := json.Unmarshal(body, &ollama); err != nil {
		return nil
	}
	message := asMap(ollama["message"])
	content := strings.TrimSpace(asString(message["content"]))
	if content == "" {
		return nil
	}
	candidateJSON := extractJSONObject(content)
	if candidateJSON == "" {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(candidateJSON), &parsed); err != nil {
		return nil
	}
	list := asSlice(parsed["plan"])
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, sourceAny := range list {
		source := strings.ToLower(strings.TrimSpace(asString(sourceAny)))
		if source == "" {
			continue
		}
		if _, ok := SupportedSources[source]; !ok {
			continue
		}
		out = append(out, source)
	}
	out = uniqueStrings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func orchestratorSLMEnabled(allowedPlan []string) bool {
	if !parseBoolEnv("ORCHESTRATOR_SLM_EXPERIMENTAL", false) {
		return false
	}
	if len(uniqueStrings(allowedPlan)) < 2 {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("ORCHESTRATOR_SLM_PROVIDER")))
	return provider == "ollama"
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") {
		return text
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(text[start : end+1])
	}
	return ""
}

func constrainPlan(plan []string, allowed []string) []string {
	if len(plan) == 0 {
		return nil
	}
	allowedSet := map[string]struct{}{}
	for _, source := range allowed {
		allowedSet[source] = struct{}{}
	}
	out := make([]string, 0, len(plan))
	for _, source := range plan {
		if _, ok := allowedSet[source]; ok {
			out = append(out, source)
		}
	}
	out = uniqueStrings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func ResolveSourcePlan(company Company, priorStatus *CompanyStatus) []string {
	base := BaseSourcePlan(company)
	payload := policyPayload{Company: company, PriorStatus: priorStatus, AllowedPlan: base}
	if hook := constrainPlan(policyHookPlan(payload), base); len(hook) > 0 {
		return hook
	}
	if orchestratorSLMEnabled(base) {
		if slm := constrainPlan(slmPlan(payload), base); len(slm) > 0 {
			return slm
		}
	}
	return AdaptiveSourcePlan(company, priorStatus)
}
