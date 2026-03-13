package monitor

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

func orchestrateCompany(company Company, prior *CompanyStatus) CrawlOutcome {
	plan := ResolveSourcePlan(company, prior)
	attempted := make([]string, 0, len(plan))
	var lastErr error
	var lastBlocked error

	for idx, source := range plan {
		attempted = append(attempted, source)
		jobs, err := FetchJobsForSource(company, source)
		if err == nil {
			qualityJobs := filterLikelyJobs(jobs)
			maxLinks := company.MaxLinks
			if maxLinks > 0 && len(qualityJobs) > maxLinks {
				qualityJobs = qualityJobs[:maxLinks]
			}
			hasMoreSources := idx < len(plan)-1
			if hasMoreSources && shouldTryFallbackForQuality(len(jobs), len(qualityJobs)) {
				lastErr = newFetchError("%s returned %d raw links but only %d looked like real jobs", source, len(jobs), len(qualityJobs))
				continue
			}
			relevantJobs := filterRelevantJobs(qualityJobs)
			message := fmt.Sprintf("%d job(s) fetched", len(relevantJobs))
			if len(relevantJobs) != len(jobs) {
				message = fmt.Sprintf(
					"%d job(s) fetched (%d raw links, %d quality-filtered, %d downstream-filtered)",
					len(relevantJobs),
					len(jobs),
					len(jobs)-len(qualityJobs),
					len(qualityJobs)-len(relevantJobs),
				)
			}
			return CrawlOutcome{
				Company:          company.Name,
				AttemptedSources: attempted,
				Status:           "ok",
				SelectedSource:   source,
				Jobs:             relevantJobs,
				Message:          message,
			}
		}
		if isBlockedErr(err) {
			lastBlocked = err
			continue
		}
		lastErr = err
	}

	if lastBlocked != nil && lastErr == nil {
		return CrawlOutcome{
			Company:          company.Name,
			AttemptedSources: attempted,
			Status:           "blocked",
			Message:          lastBlocked.Error(),
		}
	}
	if lastBlocked != nil && lastErr != nil {
		return CrawlOutcome{
			Company:          company.Name,
			AttemptedSources: attempted,
			Status:           "blocked",
			Message:          lastBlocked.Error() + " | secondary error: " + lastErr.Error(),
		}
	}
	message := "unknown error"
	if lastErr != nil {
		message = lastErr.Error()
	}
	return CrawlOutcome{
		Company:          company.Name,
		AttemptedSources: attempted,
		Status:           "error",
		Message:          message,
	}
}

func CrawlCompanies(
	companies []Company,
	workers int,
	prior map[string]CompanyStatus,
	onStart func(company string),
	onDone func(outcome CrawlOutcome),
) []CrawlOutcome {
	if len(companies) == 0 {
		return nil
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(companies) {
		workers = len(companies)
	}

	type workItem struct {
		Index   int
		Company Company
	}
	jobs := make(chan workItem)
	results := make(chan CrawlOutcome, len(companies))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if onStart != nil {
					onStart(item.Company.Name)
				}
				var priorStatus *CompanyStatus
				if st, ok := prior[item.Company.Name]; ok {
					copy := st
					priorStatus = &copy
				}
				_ = item.Index
				outcome := orchestrateCompany(item.Company, priorStatus)
				if onDone != nil {
					onDone(outcome)
				}
				results <- outcome
			}
		}()
	}

	for idx, company := range companies {
		jobs <- workItem{Index: idx, Company: company}
	}
	close(jobs)
	wg.Wait()
	close(results)

	outcomes := make([]CrawlOutcome, 0, len(companies))
	for result := range results {
		outcomes = append(outcomes, result)
	}

	sort.Slice(outcomes, func(i, j int) bool {
		return strings.ToLower(outcomes[i].Company) < strings.ToLower(outcomes[j].Company)
	})
	return outcomes
}
