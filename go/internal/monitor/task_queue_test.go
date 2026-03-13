package monitor

import (
	"testing"
	"time"
)

func TestBackgroundTaskCoordinatorQueuesMailBehindCrawl(t *testing.T) {
	coordinator := NewBackgroundTaskCoordinator()
	crawlStarted := make(chan bool, 2)
	mailStarted := make(chan struct{}, 2)
	coordinator.SetLaunchers(
		func(dryRun bool) { crawlStarted <- dryRun },
		func() { mailStarted <- struct{}{} },
	)

	first := coordinator.RequestCrawl(false)
	if !first.Accepted || !first.Started || first.Queued {
		t.Fatalf("first crawl trigger = %+v", first)
	}
	select {
	case dryRun := <-crawlStarted:
		if dryRun {
			t.Fatalf("unexpected dry-run start")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for crawl launch")
	}

	second := coordinator.RequestMail()
	if !second.Accepted || !second.Queued || second.Started {
		t.Fatalf("mail trigger = %+v", second)
	}
	select {
	case <-mailStarted:
		t.Fatal("mail should not start while crawl is active")
	default:
	}

	coordinator.Finish(backgroundTaskCrawl)
	select {
	case <-mailStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued mail to start")
	}
}

func TestBackgroundTaskCoordinatorPreservesPendingTaskOrder(t *testing.T) {
	coordinator := NewBackgroundTaskCoordinator()
	crawlStarted := make(chan bool, 4)
	mailStarted := make(chan struct{}, 4)
	coordinator.SetLaunchers(
		func(dryRun bool) { crawlStarted <- dryRun },
		func() { mailStarted <- struct{}{} },
	)

	if result := coordinator.RequestCrawl(false); !result.Started {
		t.Fatalf("initial crawl trigger = %+v", result)
	}
	<-crawlStarted

	if result := coordinator.RequestMail(); !result.Queued {
		t.Fatalf("mail trigger = %+v", result)
	}
	if result := coordinator.RequestCrawl(true); !result.Queued {
		t.Fatalf("queued crawl trigger = %+v", result)
	}

	coordinator.Finish(backgroundTaskCrawl)
	select {
	case <-mailStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued mail")
	}
	select {
	case dryRun := <-crawlStarted:
		t.Fatalf("crawl should not restart before mail finishes: %v", dryRun)
	default:
	}

	coordinator.Finish(backgroundTaskMail)
	select {
	case dryRun := <-crawlStarted:
		if !dryRun {
			t.Fatal("expected pending crawl to use the latest dry-run request")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued crawl restart")
	}
}
