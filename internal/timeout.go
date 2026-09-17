// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package internal

import (
	"context"
	"errors"
	"iter"

	"google.golang.org/adk/v2/model"
)

// GenerateWithTimeout honours the optional GenAI HTTPOptions.Timeout for an
// entire invocation, including retries and stream consumption. No timeout is
// added when unset or non-positive. generate must honour context cancellation.
// The deadline starts when iteration starts and is released on early exit.
// A non-partial response completes the model invocation. Its stream is closed
// before yielding it, so downstream tool execution does not consume this budget.
func GenerateWithTimeout(ctx context.Context, req *model.LLMRequest, generate func(context.Context) iter.Seq2[*model.LLMResponse, error]) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		callCtx := ctx
		cancel := func() {}
		if req != nil && req.Config != nil && req.Config.HTTPOptions != nil && req.Config.HTTPOptions.Timeout != nil && *req.Config.HTTPOptions.Timeout > 0 {
			callCtx, cancel = context.WithTimeout(ctx, *req.Config.HTTPOptions.Timeout)
			defer cancel()
		}
		failure := func(err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
				return errors.Join(context.DeadlineExceeded, err)
			}
			return err
		}
		if err := failure(nil); err != nil {
			yield(nil, err)
			return
		}
		var final *model.LLMResponse
		for resp, err := range generate(callCtx) {
			if err = failure(err); err != nil {
				yield(nil, err)
				return
			}
			if resp != nil && !resp.Partial {
				final = resp
				break
			}
			if !yield(resp, nil) {
				return
			}
		}
		if err := failure(nil); err != nil {
			yield(nil, err)
			return
		}
		cancel()
		if final != nil {
			yield(final, nil)
		}
	}
}
