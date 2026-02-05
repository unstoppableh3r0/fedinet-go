package main

import (
	"fmt"
)

// TimelineError represents errors specific to timeline operations
type TimelineError struct {
	Code    string
	Message string
	Err     error
}

func (e *TimelineError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *TimelineError) Unwrap() error {
	return e.Err
}

// Error codes
const (
	ErrCodeInvalidRankingMode = "INVALID_RANKING_MODE"
	ErrCodeVersionNotFound    = "VERSION_NOT_FOUND"
	ErrCodeCacheExpired       = "CACHE_EXPIRED"
	ErrCodeCacheSizeExceeded  = "CACHE_SIZE_EXCEEDED"
	ErrCodeRateLimited        = "RATE_LIMITED"
	ErrCodeServerOverloaded   = "SERVER_OVERLOADED"
)

// NewTimelineError creates a new timeline error
func NewTimelineError(code, message string, err error) *TimelineError {
	return &TimelineError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}
