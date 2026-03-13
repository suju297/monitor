package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func asString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func asMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if casted, ok := value.(map[string]any); ok {
		return casted
	}
	if casted, ok := value.(map[any]any); ok {
		out := make(map[string]any, len(casted))
		for key, val := range casted {
			out[asString(key)] = val
		}
		return out
	}
	return map[string]any{}
}

func asStringMap(value any) map[string]string {
	raw := asMap(value)
	out := make(map[string]string, len(raw))
	for key, val := range raw {
		out[strings.TrimSpace(key)] = asString(val)
	}
	return out
}

func asSlice(value any) []any {
	if value == nil {
		return nil
	}
	if casted, ok := value.([]any); ok {
		return casted
	}
	if casted, ok := value.([]string); ok {
		out := make([]any, 0, len(casted))
		for _, entry := range casted {
			out = append(out, entry)
		}
		return out
	}
	return nil
}

func normalizeCommand(command any) ([]string, bool, error) {
	switch v := command.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, false, newFetchError("empty command")
		}
		return []string{v}, true, nil
	case []string:
		if len(v) == 0 {
			return nil, false, newFetchError("empty command array")
		}
		return v, false, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, token := range v {
			part := asString(token)
			if part == "" {
				continue
			}
			out = append(out, part)
		}
		if len(out) == 0 {
			return nil, false, newFetchError("empty command array")
		}
		return out, false, nil
	default:
		return nil, false, newFetchError("invalid command type")
	}
}

var (
	commandBaseDirOnce sync.Once
	commandBaseDir     string
)

func detectCommandBaseDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	candidates := []string{cwd}
	for dir := cwd; ; {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		candidates = append(candidates, parent)
		dir = parent
	}
	for _, dir := range candidates {
		if stat, err := os.Stat(filepath.Join(dir, "scripts")); err == nil && stat.IsDir() {
			return dir
		}
	}
	return cwd
}

func commandWorkingDir() string {
	commandBaseDirOnce.Do(func() {
		commandBaseDir = detectCommandBaseDir()
	})
	if strings.TrimSpace(commandBaseDir) == "" {
		return "."
	}
	return commandBaseDir
}

func looksLikeRelativePathToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	lower := strings.ToLower(token)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return false
	}
	if strings.HasPrefix(token, "-") {
		return false
	}
	if filepath.IsAbs(token) {
		return false
	}
	if strings.HasPrefix(token, "~/") {
		return false
	}
	if strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") {
		return true
	}
	if strings.Contains(token, "/") || strings.Contains(token, `\`) {
		return true
	}
	for _, suffix := range []string{".py", ".js", ".mjs", ".cjs", ".sh"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func absolutizeCommandTokens(args []string) []string {
	if len(args) == 0 {
		return args
	}
	base := commandWorkingDir()
	out := make([]string, len(args))
	copy(out, args)
	for i, token := range out {
		if !looksLikeRelativePathToken(token) {
			continue
		}
		candidate := filepath.Clean(filepath.Join(base, token))
		if _, err := os.Stat(candidate); err == nil {
			out[i] = candidate
		}
	}
	return out
}

func runJobCommand(company Company, command any, envUpdates map[string]string) ([]Job, error) {
	normalized, shellMode, err := normalizeCommand(command)
	if err != nil {
		return nil, newFetchError("company %q has invalid command: %v", company.Name, err)
	}
	if !shellMode {
		normalized = absolutizeCommandTokens(normalized)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(company.TimeoutSeconds)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if shellMode {
		cmd = exec.CommandContext(ctx, "sh", "-lc", normalized[0])
	} else {
		cmd = exec.CommandContext(ctx, normalized[0], normalized[1:]...)
	}
	cmd.Dir = commandWorkingDir()
	env := os.Environ()
	env = append(env,
		"COMPANY_NAME="+company.Name,
		"CAREERS_URL="+company.CareersURL,
	)
	for key, value := range company.CommandEnv {
		env = append(env, key+"="+value)
	}
	for key, value := range envUpdates {
		env = append(env, key+"="+value)
	}
	cmd.Env = env

	stdout, err := cmd.Output()
	stderr := ""
	if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, newFetchError("command adapter timed out for %s after %ds", company.Name, company.TimeoutSeconds)
		}
		combined := strings.TrimSpace(stderr)
		if combined == "" {
			combined = strings.TrimSpace(string(stdout))
		}
		if combined == "" {
			combined = err.Error()
		}
		if len(combined) > 900 {
			combined = combined[:900] + "..."
		}
		return nil, newFetchError("command adapter failed for %s: %s", company.Name, combined)
	}

	raw := strings.TrimSpace(string(stdout))
	if raw == "" {
		raw = "[]"
	}
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, newFetchError("command adapter for %s did not output valid JSON", company.Name)
	}

	jobsPayload := payload
	if object, ok := payload.(map[string]any); ok {
		status := strings.ToLower(strings.TrimSpace(asString(object["status"])))
		message := asString(object["message"])
		if status == "blocked" {
			if message == "" {
				message = "command adapter reported blocked target"
			}
			return nil, &CrawlBlockedError{Message: message}
		}
		if status == "error" {
			if message == "" {
				message = "command adapter reported error"
			}
			return nil, &CrawlFetchError{Message: message}
		}
		if status != "" && status != "ok" {
			return nil, newFetchError("command adapter for %s returned unknown status %q", company.Name, status)
		}
		jobsPayload = object["jobs"]
	}

	list := asSlice(jobsPayload)
	if list == nil {
		return nil, newFetchError("command adapter output for %s must be JSON list or object", company.Name)
	}

	jobs := make([]Job, 0, len(list))
	for _, item := range list {
		row := asMap(item)
		title := asString(row["title"])
		jobURL := asString(row["url"])
		if title == "" || jobURL == "" {
			continue
		}
		description := asString(row["description"])
		if description == "" {
			description = asString(row["description_html"])
		}
		jobs = append(jobs, Job{
			Company:     company.Name,
			Source:      "command",
			Title:       title,
			URL:         jobURL,
			ExternalID:  asString(row["external_id"]),
			Location:    asString(row["location"]),
			Team:        asString(row["team"]),
			PostedAt:    normalizeCreatedAt(row["posted_at"]),
			Description: normalizeTextSnippet(description, 2200),
		})
	}
	return jobs, nil
}

func commandEnvValue(commandEnv map[string]string, key string) string {
	if len(commandEnv) == 0 {
		return ""
	}
	for rawKey, rawValue := range commandEnv {
		if strings.EqualFold(strings.TrimSpace(rawKey), key) {
			return strings.TrimSpace(rawValue)
		}
	}
	return ""
}

func commandEnvBool(commandEnv map[string]string, key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(commandEnvValue(commandEnv, key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func commandEnvInt(commandEnv map[string]string, key string, fallback int, minimum int) int {
	value := strings.TrimSpace(commandEnvValue(commandEnv, key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum {
		return fallback
	}
	return parsed
}

func fetchCommand(company Company) ([]Job, error) {
	if company.Command == nil {
		return nil, newFetchError("company %q source command/playwright requires command", company.Name)
	}
	return runJobCommand(company, company.Command, nil)
}

func fetchMyGreenhouse(company Company) ([]Job, error) {
	command := company.MyGreenhouseCmd
	if command == nil {
		command = MyGreenhouseDefaultCommand
	}
	jobs, err := runJobCommand(company, command, map[string]string{
		"MY_GREENHOUSE_COMPANY_LABEL": company.Name,
	})
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		// The My Greenhouse adapter returns the hiring company in "team".
		// Promote it so dashboard company column shows the actual employer.
		employer := strings.TrimSpace(jobs[i].Team)
		if employer != "" {
			jobs[i].Company = employer
			jobs[i].Team = ""
		}
		jobs[i].Source = "my_greenhouse"
	}
	return jobs, nil
}
