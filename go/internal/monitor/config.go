package monitor

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type companiesConfig struct {
	Companies []Company `yaml:"companies"`
}

func normalizeFallbackSources(raw any, companyName string) ([]string, error) {
	var values []string
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		for _, piece := range strings.Split(v, ",") {
			values = append(values, strings.ToLower(strings.TrimSpace(piece)))
		}
	case []any:
		for _, piece := range v {
			values = append(values, strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", piece))))
		}
	case []string:
		for _, piece := range v {
			values = append(values, strings.ToLower(strings.TrimSpace(piece)))
		}
	default:
		return nil, fmt.Errorf("company %q has invalid fallback_sources; use list or comma-separated string", companyName)
	}
	values = uniqueStrings(values)
	for _, source := range values {
		if _, ok := SupportedSources[source]; !ok {
			return nil, fmt.Errorf("company %q has unsupported fallback source %q", companyName, source)
		}
	}
	return values, nil
}

func sourceString(v any) string {
	return strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", v)))
}

func parseIntDefault(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func LoadCompanies(configPath string) ([]Company, error) {
	if strings.TrimSpace(configPath) == "" {
		return nil, errors.New("config path is empty")
	}
	configPath = preferredLocalFile(configPath, "companies.yaml")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s (expected .local/companies.yaml or companies.yaml)", configPath)
		}
		return nil, err
	}

	var payload companiesConfig
	if err := yaml.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("invalid yaml config: %w", err)
	}
	if len(payload.Companies) == 0 {
		return nil, errors.New("config must contain a non-empty companies list")
	}

	normalized := make([]Company, 0, len(payload.Companies))
	for idx, entry := range payload.Companies {
		if entry.Disabled {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		source := sourceString(entry.Source)
		careersURL := strings.TrimSpace(entry.CareersURL)
		if name == "" {
			return nil, fmt.Errorf("company entry #%d missing name", idx+1)
		}
		if careersURL == "" {
			return nil, fmt.Errorf("company %q missing careers_url", name)
		}
		if _, ok := SupportedSources[source]; !ok {
			return nil, fmt.Errorf("company %q has unsupported source %q", name, source)
		}

		fallback, err := normalizeFallbackSources(entry.FallbackSources, name)
		if err != nil {
			return nil, err
		}
		timeout := parseIntDefault(entry.TimeoutSeconds, DefaultTimeoutSeconds)
		if timeout < 5 {
			timeout = 5
		}
		maxLinks := parseIntDefault(entry.MaxLinks, 200)
		if maxLinks < 1 {
			maxLinks = 1
		}
		commandEnv := map[string]string{}
		for key, value := range entry.CommandEnv {
			commandEnv[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}

		company := Company{
			Name:            name,
			Source:          source,
			CareersURL:      careersURL,
			FallbackSources: fallback,
			TimeoutSeconds:  timeout,
			MaxLinks:        maxLinks,
			GreenhouseBoard: strings.TrimSpace(entry.GreenhouseBoard),
			LeverSite:       strings.TrimSpace(entry.LeverSite),
			Template:        entry.Template,
			Command:         entry.Command,
			CommandEnv:      commandEnv,
			MyGreenhouseCmd: entry.MyGreenhouseCmd,
			Orchestration:   entry.Orchestration,
		}
		normalized = append(normalized, company)
	}

	if len(normalized) == 0 {
		return nil, errors.New("no enabled companies found in config")
	}
	return normalized, nil
}
