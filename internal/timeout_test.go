// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package internal

import (
	"context"
	"errors"
	"iter"
	"testing"
	"testing/synctest"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func timeoutRequest(d time.Duration) *model.LLMRequest {
	return &model.LLMRequest{Config: &genai.GenerateContentConfig{HTTPOptions: &genai.HTTPOptions{Timeout: &d}}}
}

func TestGenerateWithTimeout(t *testing.T) {
	for _, tc := range []struct {
		name        string
		parent, own time.Duration
		want        error
	}{
		{"own", 0, time.Minute, context.DeadlineExceeded},
		{"shorter parent", time.Second, time.Minute, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()
				if tc.parent > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tc.parent)
					defer cancel()
				}
				count := 0
				for _, err := range GenerateWithTimeout(ctx, timeoutRequest(tc.own), func(call context.Context) iter.Seq2[*model.LLMResponse, error] {
					return func(yield func(*model.LLMResponse, error) bool) { <-call.Done(); yield(nil, call.Err()) }
				}) {
					count++
					if !errors.Is(err, tc.want) {
						t.Fatalf("got %v, want %v", err, tc.want)
					}
					if tc.parent == 0 && ctx.Err() != nil {
						t.Fatal("request timeout cancelled parent")
					}
				}
				if count != 1 {
					t.Fatalf("got %d terminal errors", count)
				}
			})
		})
	}
}

func TestGenerateWithTimeoutUnsetAndEarlyExit(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "unset", true: "configured"}[configured], func(t *testing.T) {
			var seen context.Context
			var req *model.LLMRequest
			if configured {
				req = timeoutRequest(time.Hour)
			}
			seq := GenerateWithTimeout(t.Context(), req, func(ctx context.Context) iter.Seq2[*model.LLMResponse, error] {
				seen = ctx
				return func(yield func(*model.LLMResponse, error) bool) { yield(&model.LLMResponse{Partial: true}, nil) }
			})
			if seen != nil {
				t.Fatal("generation started before iteration")
			}
			for _, err := range seq {
				if err != nil {
					t.Fatal(err)
				}
				break
			}
			if configured {
				if !errors.Is(seen.Err(), context.Canceled) {
					t.Fatal("early exit did not release context")
				}
			} else if seen != t.Context() {
				t.Fatal("unset timeout replaced caller context")
			}
		})
	}
}

func TestGenerateWithTimeoutCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	for _, err := range GenerateWithTimeout(ctx, timeoutRequest(time.Hour), func(call context.Context) iter.Seq2[*model.LLMResponse, error] {
		return func(yield func(*model.LLMResponse, error) bool) { cancel(); <-call.Done(); yield(nil, call.Err()) }
	}) {
		if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wrong cancellation: %v", err)
		}
	}
}

func TestGenerateWithTimeoutExcludesFinalConsumer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		closed := false
		var callCtx context.Context
		count := 0
		seq := GenerateWithTimeout(t.Context(), timeoutRequest(time.Second), func(ctx context.Context) iter.Seq2[*model.LLMResponse, error] {
			callCtx = ctx
			return func(yield func(*model.LLMResponse, error) bool) {
				defer func() { closed = true }()
				yield(&model.LLMResponse{TurnComplete: true}, nil)
			}
		})
		for _, err := range seq {
			if err != nil {
				t.Fatal(err)
			}
			if !closed {
				t.Fatal("model stream remains open while downstream tools run")
			}
			if callCtx.Err() != context.Canceled {
				t.Fatal("model timer not released before yielding final response")
			}
			// synctest advances virtual time; this models synchronous ADK tool work.
			<-time.After(time.Hour)
			count++
		}
		if count != 1 {
			t.Fatalf("got %d responses", count)
		}
	})
}
