package monitor

import "sync"

type backgroundTaskKind string

const (
	backgroundTaskNone  backgroundTaskKind = ""
	backgroundTaskCrawl backgroundTaskKind = "crawl"
	backgroundTaskMail  backgroundTaskKind = "mail"
)

type taskTriggerResult struct {
	Accepted bool
	Started  bool
	Queued   bool
	Message  string
}

type pendingCrawlTask struct {
	DryRun bool
}

type BackgroundTaskCoordinator struct {
	mu         sync.Mutex
	running    backgroundTaskKind
	pending    []backgroundTaskKind
	crawlTask  *pendingCrawlTask
	mailQueued bool
	startCrawl func(dryRun bool)
	startMail  func()
}

func NewBackgroundTaskCoordinator() *BackgroundTaskCoordinator {
	return &BackgroundTaskCoordinator{}
}

func (c *BackgroundTaskCoordinator) SetLaunchers(startCrawl func(dryRun bool), startMail func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startCrawl = startCrawl
	c.startMail = startMail
}

func (c *BackgroundTaskCoordinator) RequestCrawl(dryRun bool) taskTriggerResult {
	c.mu.Lock()
	switch c.running {
	case backgroundTaskNone:
		start := c.startCrawl
		c.running = backgroundTaskCrawl
		c.mu.Unlock()
		if start == nil {
			c.Finish(backgroundTaskCrawl)
			return taskTriggerResult{Message: "Task coordinator is not ready."}
		}
		go start(dryRun)
		return taskTriggerResult{
			Accepted: true,
			Started:  true,
			Message:  "Run started.",
		}
	case backgroundTaskCrawl:
		alreadyQueued := c.crawlTask != nil
		c.crawlTask = &pendingCrawlTask{DryRun: dryRun}
		c.enqueueLocked(backgroundTaskCrawl)
		c.mu.Unlock()
		if alreadyQueued {
			return taskTriggerResult{
				Accepted: true,
				Queued:   true,
				Message:  "Tracker run already queued. Updated the pending run.",
			}
		}
		return taskTriggerResult{
			Accepted: true,
			Queued:   true,
			Message:  "Tracker run already in progress. Queued one follow-up run.",
		}
	default:
		alreadyQueued := c.crawlTask != nil
		c.crawlTask = &pendingCrawlTask{DryRun: dryRun}
		c.enqueueLocked(backgroundTaskCrawl)
		c.mu.Unlock()
		if alreadyQueued {
			return taskTriggerResult{
				Accepted: true,
				Queued:   true,
				Message:  "Tracker run already queued behind mail sync. Updated the pending run.",
			}
		}
		return taskTriggerResult{
			Accepted: true,
			Queued:   true,
			Message:  "Mail sync is running. Tracker run queued to start next.",
		}
	}
}

func (c *BackgroundTaskCoordinator) RequestMail() taskTriggerResult {
	c.mu.Lock()
	switch c.running {
	case backgroundTaskNone:
		start := c.startMail
		c.running = backgroundTaskMail
		c.mu.Unlock()
		if start == nil {
			c.Finish(backgroundTaskMail)
			return taskTriggerResult{Message: "Task coordinator is not ready."}
		}
		go start()
		return taskTriggerResult{
			Accepted: true,
			Started:  true,
			Message:  "Mail sync started.",
		}
	case backgroundTaskMail:
		alreadyQueued := c.mailQueued
		c.mailQueued = true
		c.enqueueLocked(backgroundTaskMail)
		c.mu.Unlock()
		if alreadyQueued {
			return taskTriggerResult{
				Accepted: true,
				Queued:   true,
				Message:  "Mail sync already queued.",
			}
		}
		return taskTriggerResult{
			Accepted: true,
			Queued:   true,
			Message:  "Mail sync already in progress. Queued one follow-up sync.",
		}
	default:
		alreadyQueued := c.mailQueued
		c.mailQueued = true
		c.enqueueLocked(backgroundTaskMail)
		c.mu.Unlock()
		if alreadyQueued {
			return taskTriggerResult{
				Accepted: true,
				Queued:   true,
				Message:  "Mail sync already queued behind tracker run.",
			}
		}
		return taskTriggerResult{
			Accepted: true,
			Queued:   true,
			Message:  "Tracker run is running. Mail sync queued to start next.",
		}
	}
}

func (c *BackgroundTaskCoordinator) Finish(kind backgroundTaskKind) {
	var (
		nextKind backgroundTaskKind
		dryRun   bool
		start    func()
	)

	c.mu.Lock()
	if c.running != kind {
		c.mu.Unlock()
		return
	}
	c.running = backgroundTaskNone
	for len(c.pending) > 0 && start == nil {
		nextKind = c.pending[0]
		c.pending = c.pending[1:]
		switch nextKind {
		case backgroundTaskCrawl:
			if c.crawlTask == nil {
				continue
			}
			dryRun = c.crawlTask.DryRun
			c.crawlTask = nil
			launcher := c.startCrawl
			if launcher == nil {
				continue
			}
			c.running = backgroundTaskCrawl
			start = func() { launcher(dryRun) }
		case backgroundTaskMail:
			if !c.mailQueued {
				continue
			}
			c.mailQueued = false
			launcher := c.startMail
			if launcher == nil {
				continue
			}
			c.running = backgroundTaskMail
			start = launcher
		}
	}
	c.mu.Unlock()

	if start != nil {
		go start()
	}
}

func (c *BackgroundTaskCoordinator) enqueueLocked(kind backgroundTaskKind) {
	for _, existing := range c.pending {
		if existing == kind {
			return
		}
	}
	c.pending = append(c.pending, kind)
}
