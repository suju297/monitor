package monitor

import "testing"

func TestJobFetcherCoverageMatchesSupportedSources(t *testing.T) {
	for source := range SupportedSources {
		if jobFetchers[source] == nil {
			t.Fatalf("missing fetcher for supported source %q", source)
		}
	}
}
