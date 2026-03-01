package provider

import (
	"context"
	"strings"
)

type StubGateway struct{}

func NewStubGateway() *StubGateway {
	return &StubGateway{}
}

func (s *StubGateway) Chat(_ context.Context, req ChatRequest) (ChatResult, error) {
	prompt := "(empty input)"
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			v := strings.TrimSpace(req.Messages[i].Content)
			if v != "" {
				prompt = v
			}
			break
		}
	}

	model := req.Model
	if model == "" {
		model = "stub/opcapi-mvp"
	}

	return ChatResult{
		Model:   model,
		Content: "opcAPI MVP stub response: " + truncate(prompt, 200),
	}, nil
}

func (s *StubGateway) Models(_ context.Context) []ModelInfo {
	return []ModelInfo{{ID: "stub/opcapi-mvp", OwnedBy: "opcapi"}}
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}
