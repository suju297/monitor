package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type crawlScheduleStore struct {
	Enabled           bool   `json:"enabled"`
	IntervalMinutes   int    `json:"interval_minutes"`
	LastTriggerAt     string `json:"last_trigger_at,omitempty"`
	LastTriggerResult string `json:"last_trigger_result,omitempty"`
}

type CrawlScheduler struct {
	mu       sync.Mutex
	runner   *MonitorRunner
	filePath string
	status   CrawlScheduleStatus
	stopCh   chan struct{}
}

func normalizeCrawlScheduleIntervalMinutes(value int) int {
	if value < 5 {
		return 5
	}
	if value > 7*24*60 {
		return 7 * 24 * 60
	}
	return value
}

func crawlScheduleDefaultIntervalMinutes() int {
	interval := 60
	if raw := strings.TrimSpace(os.Getenv("CRAWL_SCHEDULE_INTERVAL_MINUTES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			interval = parsed
		}
	}
	return normalizeCrawlScheduleIntervalMinutes(interval)
}

func resolveCrawlSchedulePath(statePath string, configuredPath string) string {
	if configured := strings.TrimSpace(configuredPath); configured != "" {
		return configured
	}
	statePath = strings.TrimSpace(statePath)
	if statePath == "" {
		return ".state/crawl_schedule.json"
	}
	stateDir := strings.TrimSpace(filepath.Dir(statePath))
	if stateDir == "" || stateDir == "." {
		return ".state/crawl_schedule.json"
	}
	return filepath.Join(stateDir, "crawl_schedule.json")
}

func NewCrawlScheduler(runner *MonitorRunner, statePath string, configuredPath string) *CrawlScheduler {
	scheduler := &CrawlScheduler{
		runner:   runner,
		filePath: resolveCrawlSchedulePath(statePath, configuredPath),
		status: CrawlScheduleStatus{
			Enabled:         parseBoolEnv("CRAWL_SCHEDULE_ENABLED", false),
			IntervalMinutes: crawlScheduleDefaultIntervalMinutes(),
		},
	}

	scheduler.mu.Lock()
	if err := scheduler.loadLocked(); err != nil {
		scheduler.status.LastError = err.Error()
	}
	if scheduler.status.Enabled {
		scheduler.startLoopLocked()
		if err := scheduler.persistLocked(); err != nil {
			scheduler.status.LastError = err.Error()
		}
	}
	scheduler.mu.Unlock()

	return scheduler
}

func (s *CrawlScheduler) Snapshot() CrawlScheduleStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *CrawlScheduler) Update(enabled bool, intervalMinutes int) (CrawlScheduleStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status.Enabled = enabled
	s.status.IntervalMinutes = normalizeCrawlScheduleIntervalMinutes(intervalMinutes)
	s.status.LastError = ""
	if s.status.Enabled {
		s.startLoopLocked()
	} else {
		s.stopLoopLocked()
		s.status.NextRunAt = ""
	}
	err := s.persistLocked()
	if err != nil {
		s.status.LastError = err.Error()
	}
	return s.status, err
}

func (s *CrawlScheduler) startLoopLocked() {
	s.stopLoopLocked()
	interval := time.Duration(s.status.IntervalMinutes) * time.Minute
	s.status.NextRunAt = time.Now().Add(interval).UTC().Format(time.RFC3339)
	stopCh := make(chan struct{})
	s.stopCh = stopCh
	go s.loop(stopCh, interval)
}

func (s *CrawlScheduler) stopLoopLocked() {
	if s.stopCh == nil {
		return
	}
	close(s.stopCh)
	s.stopCh = nil
}

func (s *CrawlScheduler) loop(stopCh <-chan struct{}, interval time.Duration) {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-timer.C:
			s.triggerScheduledRun()
			timer.Reset(interval)
		}
	}
}

func (s *CrawlScheduler) triggerScheduledRun() {
	result := s.runner.Trigger(false)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.status.Enabled {
		s.status.NextRunAt = ""
		return
	}
	s.status.LastTriggerAt = now.UTC().Format(time.RFC3339)
	if result.Accepted {
		if result.Queued {
			s.status.LastTriggerResult = "queued"
		} else {
			s.status.LastTriggerResult = "started"
		}
		s.status.LastError = ""
	} else {
		s.status.LastTriggerResult = "skipped"
		s.status.LastError = strings.TrimSpace(result.Message)
	}
	s.status.NextRunAt = now.Add(time.Duration(s.status.IntervalMinutes) * time.Minute).UTC().Format(time.RFC3339)
	if err := s.persistLocked(); err != nil {
		s.status.LastError = err.Error()
	}
}

func (s *CrawlScheduler) loadLocked() error {
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	parsed := crawlScheduleStore{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return err
	}

	s.status.Enabled = parsed.Enabled
	if parsed.IntervalMinutes > 0 {
		s.status.IntervalMinutes = normalizeCrawlScheduleIntervalMinutes(parsed.IntervalMinutes)
	}
	s.status.LastTriggerAt = strings.TrimSpace(parsed.LastTriggerAt)
	s.status.LastTriggerResult = strings.TrimSpace(parsed.LastTriggerResult)
	return nil
}

func (s *CrawlScheduler) persistLocked() error {
	store := crawlScheduleStore{
		Enabled:           s.status.Enabled,
		IntervalMinutes:   s.status.IntervalMinutes,
		LastTriggerAt:     strings.TrimSpace(s.status.LastTriggerAt),
		LastTriggerResult: strings.TrimSpace(s.status.LastTriggerResult),
	}
	body, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tempPath := s.filePath + ".tmp"
	if err := os.WriteFile(tempPath, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, s.filePath)
}
