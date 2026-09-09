// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// retryTransport retries HTTP failures only. Successful streaming responses are
// returned untouched; errors while consuming them belong to the caller.
type retryTransport struct {
	base  http.RoundTripper
	sleep func(context.Context, time.Duration) error
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	current := req
	for attempt := 0; ; attempt++ {
		if err := req.Context().Err(); err != nil {
			if current.Body != nil {
				_ = current.Body.Close()
			}
			return nil, err
		}
		response, err := t.base.RoundTrip(current)
		if attempt == 2 || !retryable(req, response, err) {
			return response, err
		}
		next := req.Clone(req.Context())
		if req.Body != nil && req.Body != http.NoBody {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return response, err
			}
			next.Body = body
		}
		delay := retryDelay(response, attempt, time.Now())
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if sleepErr := t.sleep(req.Context(), delay); sleepErr != nil {
			if next.Body != nil {
				_ = next.Body.Close()
			}
			return nil, sleepErr
		}
		current = next
	}
}

func retryable(req *http.Request, response *http.Response, err error) bool {
	if req.Context().Err() != nil {
		return false
	}
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return false
	}
	if response == nil {
		return err != nil
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return false
	}
	switch response.Header.Get("x-should-retry") {
	case "true":
		return true
	case "false":
		return false
	}
	status := response.StatusCode
	return status == 408 || status == 409 || status == 429 || status >= 500
}

func retryDelay(response *http.Response, attempt int, now time.Time) time.Duration {
	if response != nil {
		value := response.Header.Get("Retry-After")
		if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 && seconds <= 60 {
			return time.Duration(seconds * float64(time.Second))
		}
		if date, err := http.ParseTime(value); err == nil {
			delay := date.Sub(now)
			if delay >= 0 && delay <= time.Minute {
				return delay
			}
		}
	}
	base := (500 * time.Millisecond) << attempt
	return base - rand.N(base/4)
}

func retrySleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
