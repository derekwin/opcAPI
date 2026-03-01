package provider

import (
	"context"
	"errors"
)

var (
	ErrProviderUnavailable = errors.New("provider unavailable")
	ErrUpstreamResponse    = errors.New("invalid upstream response")
)

type Message struct {
	Role    string
	Content string
}

type ChatRequest struct {
	Model    string
	Messages []Message
}

type ChatResult struct {
	Model            string
	Content          string
	PromptTokens     int
	CompletionTokens int
}

type ModelInfo struct {
	ID      string
	OwnedBy string
}

type Gateway interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResult, error)
	Models(ctx context.Context) []ModelInfo
}
