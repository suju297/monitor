package monitor

import (
	"strings"
)

var jobFetchers = map[string]func(Company) ([]Job, error){
	"amazon":        fetchAmazon,
	"apple":         fetchApple,
	"ashby":         fetchAshby,
	"atlassian":     fetchAtlassian,
	"capitalone":    fetchCapitalOne,
	"command":       fetchCommand,
	"datadog":       fetchDatadog,
	"generic":       fetchGeneric,
	"google":        fetchGoogle,
	"greenhouse":    fetchGreenhouse,
	"icims":         fetchICIMS,
	"icms":          fetchICIMS,
	"lever":         fetchLever,
	"microsoft":     fetchMicrosoft,
	"my_greenhouse": fetchMyGreenhouse,
	"phenom":        fetchPhenom,
	"playwright":    fetchCommand,
	"template":      fetchTemplate,
	"veeva":         fetchVeeva,
}

func FetchJobsForSource(company Company, source string) ([]Job, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	fetcher := jobFetchers[source]
	if fetcher == nil {
		return nil, newFetchError("unsupported source: %s", source)
	}
	return fetcher(company)
}
