package monitor

import (
	"sort"
	"strconv"
	"strings"
)

type SourceProbeResult struct {
	Source                string   `json:"source"`
	Status                string   `json:"status"`
	Message               string   `json:"message,omitempty"`
	RawJobs               int      `json:"raw_jobs"`
	QualityJobs           int      `json:"quality_jobs"`
	RelevantJobs          int      `json:"relevant_jobs"`
	ShouldFallbackQuality bool     `json:"should_fallback_quality"`
	SampleTitles          []string `json:"sample_titles,omitempty"`
}

func ProbeSourceCandidate(company Company, source string) SourceProbeResult {
	result := SourceProbeResult{
		Source: strings.ToLower(strings.TrimSpace(source)),
		Status: "error",
	}
	if result.Source == "" {
		result.Message = "missing source"
		return result
	}

	jobs, err := FetchJobsForSource(company, result.Source)
	if err != nil {
		if isBlockedErr(err) {
			result.Status = "blocked"
		}
		result.Message = strings.TrimSpace(err.Error())
		return result
	}

	result.Status = "ok"
	result.RawJobs = len(jobs)
	qualityJobs := filterLikelyJobs(jobs)
	relevantJobs := filterRelevantJobs(qualityJobs)
	result.QualityJobs = len(qualityJobs)
	result.RelevantJobs = len(relevantJobs)
	result.ShouldFallbackQuality = shouldTryFallbackForQuality(result.RawJobs, result.QualityJobs)
	if len(relevantJobs) > 0 {
		result.SampleTitles = make([]string, 0, minInt(3, len(relevantJobs)))
		for _, job := range relevantJobs {
			title := strings.TrimSpace(job.Title)
			if title == "" {
				continue
			}
			result.SampleTitles = append(result.SampleTitles, title)
			if len(result.SampleTitles) >= 3 {
				break
			}
		}
	}
	result.Message = strings.TrimSpace(buildProbeMessage(result))
	return result
}

func buildProbeMessage(result SourceProbeResult) string {
	switch result.Status {
	case "ok":
		message := []string{itoa(result.RelevantJobs) + " relevant", itoa(result.QualityJobs) + " quality", itoa(result.RawJobs) + " raw"}
		if result.ShouldFallbackQuality {
			message = append(message, "fallback-quality")
		}
		return strings.Join(message, ", ")
	case "blocked", "error":
		return strings.TrimSpace(result.Message)
	default:
		return strings.TrimSpace(result.Message)
	}
}

func RankSourceProbeResults(probes []SourceProbeResult, preferredOrder []string) []string {
	if len(probes) == 0 {
		return nil
	}
	preferredIndex := map[string]int{}
	for idx, source := range preferredOrder {
		normalized := strings.ToLower(strings.TrimSpace(source))
		if normalized == "" {
			continue
		}
		if _, exists := preferredIndex[normalized]; exists {
			continue
		}
		preferredIndex[normalized] = idx
	}
	cloned := append([]SourceProbeResult(nil), probes...)
	sort.SliceStable(cloned, func(i, j int) bool {
		left := cloned[i]
		right := cloned[j]
		leftClass := sourceProbeClass(left)
		rightClass := sourceProbeClass(right)
		if leftClass != rightClass {
			return leftClass > rightClass
		}
		if left.RelevantJobs != right.RelevantJobs {
			return left.RelevantJobs > right.RelevantJobs
		}
		if left.QualityJobs != right.QualityJobs {
			return left.QualityJobs > right.QualityJobs
		}
		if left.RawJobs != right.RawJobs {
			return left.RawJobs > right.RawJobs
		}
		leftIdx, leftOK := preferredIndex[left.Source]
		rightIdx, rightOK := preferredIndex[right.Source]
		if leftOK && rightOK && leftIdx != rightIdx {
			return leftIdx < rightIdx
		}
		if leftOK != rightOK {
			return leftOK
		}
		return left.Source < right.Source
	})
	out := make([]string, 0, len(cloned))
	for _, probe := range cloned {
		source := strings.ToLower(strings.TrimSpace(probe.Source))
		if source == "" {
			continue
		}
		out = append(out, source)
	}
	return uniqueStrings(out)
}

func sourceProbeClass(result SourceProbeResult) int {
	switch result.Status {
	case "ok":
		if result.RelevantJobs > 0 && !result.ShouldFallbackQuality {
			return 5
		}
		if result.RelevantJobs > 0 {
			return 4
		}
		if result.QualityJobs > 0 {
			return 3
		}
		return 2
	case "blocked":
		return 1
	default:
		return 0
	}
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
