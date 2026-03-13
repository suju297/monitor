package monitor

var SupportedSources = map[string]struct{}{
	"greenhouse":    {},
	"lever":         {},
	"icims":         {},
	"icms":          {},
	"ashby":         {},
	"generic":       {},
	"atlassian":     {},
	"apple":         {},
	"amazon":        {},
	"capitalone":    {},
	"datadog":       {},
	"microsoft":     {},
	"google":        {},
	"phenom":        {},
	"template":      {},
	"command":       {},
	"playwright":    {},
	"my_greenhouse": {},
	"veeva":         {},
}

const (
	DefaultTimeoutSeconds = 25
	DefaultWorkers        = 8
)

var JobKeywords = []string{
	"job",
	"jobs",
	"career",
	"careers",
	"opening",
	"openings",
	"position",
	"vacancy",
	"vacancies",
	"join-us",
	"joinus",
	"work-with-us",
}

var BlockedTitleMarkers = []string{
	"attention required",
	"just a moment",
	"vercel security checkpoint",
	"are you a robot",
	"access denied",
	"captcha",
}

var MyGreenhouseDefaultCommand = []string{"uv", "run", "python", "-m", "scripts.fetch_my_greenhouse_jobs"}
