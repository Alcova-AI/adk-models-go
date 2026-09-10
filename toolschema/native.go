// Copyright 2026 Alcova AI
// Licensed under the Apache License, Version 2.0.
package toolschema

import (
	"context"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
	"iter"
)

// Tools returns request tools without assuming a generation config is present.
func Tools(req *model.LLMRequest) []*genai.Tool {
	if req == nil || req.Config == nil {
		return nil
	}
	return req.Config.Tools
}

// WrapGemini validates the native Google SDK input. Typed schemas keep their
// native representation. Raw JSON schemas receive the same compatibility checks
// and explicit opt-in policy as other routes. The SDK still owns conversion.
func WrapGemini(llm model.LLM, config Config, route string) model.LLM {
	return &geminiModel{llm: llm, processor: New(config, Target{Provider: "google", Route: route})}
}

type geminiModel struct {
	llm       model.LLM
	processor *Processor
}

func (m *geminiModel) Name() string { return m.llm.Name() }
func (m *geminiModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	prepared, err := m.processor.Prepare(ctx, Tools(req))
	if err != nil {
		return func(yield func(*model.LLMResponse, error) bool) { yield(nil, err) }
	}
	if req == nil || req.Config == nil || len(prepared) == 0 {
		return m.llm.GenerateContent(ctx, req, stream)
	}
	copyReq := *req
	copyConfig := *req.Config
	copyReq.Config = &copyConfig
	copyConfig.Tools = make([]*genai.Tool, len(req.Config.Tools))
	for i, tool := range req.Config.Tools {
		copyTool := *tool
		copyConfig.Tools[i] = &copyTool
		copyTool.FunctionDeclarations = make([]*genai.FunctionDeclaration, len(tool.FunctionDeclarations))
		for j, fd := range tool.FunctionDeclarations {
			copyFD := *fd
			copyTool.FunctionDeclarations[j] = &copyFD
			if fd.Parameters == nil {
				copyFD.ParametersJsonSchema = prepared[fd.Name].Schema
			}
		}
	}
	return m.llm.GenerateContent(ctx, &copyReq, stream)
}
