package auth

import (
	"math"
	"strconv"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

type attempts struct {
	count int
	until time.Time
}

// Called under Service.mu, including password verification, to make the failure
// budget exact for concurrent requests. Source keys are resolved by the HTTP trusted-proxy policy.
func (s *Service) checkLimit(source string) error {
	now := time.Now()
	for key, a := range s.failures {
		if !now.Before(a.until) {
			delete(s.failures, key)
		}
	}
	a := s.failures[source]
	if a.count >= 10 {
		return limitError(a.until.Sub(now))
	}
	if len(s.failures) >= 10000 && a.count == 0 {
		return limitError(15 * time.Minute)
	}
	return nil
}
func limitError(wait time.Duration) error {
	return &fault.Error{Code: "too_many_attempts", Params: map[string]string{"retry_after": strconv.Itoa(max(1, int(math.Ceil(wait.Seconds()))))}}
}
func (s *Service) failed(source string) {
	a := s.failures[source]
	if a.count == 0 {
		a.until = time.Now().Add(15 * time.Minute)
	}
	a.count++
	s.failures[source] = a
	s.log.Warn("Administrator authentication failed", "mailbox_id", "", "count", a.count)
}
