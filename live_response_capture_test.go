// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.

package adkmodels_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Capture synthetic tool arguments before adapter decoding, never headers or
// reasoning content. The original response bytes are returned unchanged.
type liveToolResponseCapture struct {
	base  http.RoundTripper
	calls []map[string]any
}

func (c *liveToolResponseCapture) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := c.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		if parts, ok := obj["content"].([]any); ok {
			for _, part := range parts {
				if call, ok := part.(map[string]any); ok && (call["type"] == "tool-call" || call["type"] == "tool_use") {
					c.calls = append(c.calls, call)
				}
			}
		}
	}
	blocks := map[float64]map[string]any{}
	inputs := map[float64]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var call map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &call) != nil {
			continue
		}
		if call["type"] == "tool-call" {
			c.calls = append(c.calls, call)
		}
		index, _ := call["index"].(float64)
		switch call["type"] {
		case "content_block_start":
			block, _ := call["content_block"].(map[string]any)
			if block["type"] == "tool_use" {
				blocks[index] = block
			}
		case "content_block_delta":
			delta, _ := call["delta"].(map[string]any)
			if delta["type"] == "input_json_delta" {
				fragment, _ := delta["partial_json"].(string)
				inputs[index] += fragment
			}
		case "content_block_stop":
			if block := blocks[index]; block != nil {
				if input := inputs[index]; input != "" {
					var args any
					if json.Unmarshal([]byte(input), &args) == nil {
						block["input"] = args
					}
				}
				c.calls = append(c.calls, block)
				delete(blocks, index)
			}
		}
	}
	return response, nil
}
