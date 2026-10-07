// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkvercel

import (
	"errors"
	"fmt"
)

var (
	ErrRequestNil        = errors.New("vercel: request is nil")
	ErrNoContents        = errors.New("vercel: request has no contents")
	ErrMissingFinish     = errors.New("vercel: stream ended without a finish event")
	ErrUnknownStreamPart = errors.New("vercel: unknown Language Model V4 stream part")
)

type GatewayError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *GatewayError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("vercel gateway returned %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("vercel gateway returned %d", e.StatusCode)
}

// ToolInputError retains the exact gateway tool arguments when JSON decoding
// fails. RawInput can contain sensitive data; callers must only persist it in
// explicitly enabled private diagnostic traces, never ordinary logs.
// Error deliberately excludes RawInput.
type ToolInputError struct {
	ToolCallID string
	ToolName   string
	RawInput   string
	Err        error
}

func (e *ToolInputError) Error() string {
	return fmt.Sprintf("vercel: parse tool call %q input: %v", e.ToolName, e.Err)
}

func (e *ToolInputError) Unwrap() error { return e.Err }
