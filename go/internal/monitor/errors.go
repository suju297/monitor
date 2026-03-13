package monitor

import "fmt"

type CrawlBlockedError struct {
	Message string
}

func (e *CrawlBlockedError) Error() string {
	return e.Message
}

type CrawlFetchError struct {
	Message string
}

func (e *CrawlFetchError) Error() string {
	return e.Message
}

func newBlockedError(format string, args ...any) error {
	return &CrawlBlockedError{Message: fmt.Sprintf(format, args...)}
}

func newFetchError(format string, args ...any) error {
	return &CrawlFetchError{Message: fmt.Sprintf(format, args...)}
}

func isBlockedErr(err error) bool {
	_, ok := err.(*CrawlBlockedError)
	return ok
}
