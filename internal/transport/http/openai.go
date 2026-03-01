package http

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/derekwin/opcAPI/internal/billing"
	"github.com/derekwin/opcAPI/internal/provider"
)

type chatCompletionsRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
	User     string        `json:"user,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type chatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   usage                  `json:"usage"`
}

type chatCompletionChoice struct {
	Index        int                   `json:"index"`
	Message      chatCompletionMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type chatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
}

type chunkChoice struct {
	Index        int        `json:"index"`
	Delta        chunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type chunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIErrorEnvelope struct {
	Error openAIError `json:"error"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

func (h *handler) chatCompletions(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req chatCompletionsRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}

	if strings.TrimSpace(req.Model) == "" {
		writeOpenAIError(w, r, http.StatusBadRequest, "missing_model", "invalid_request_error", "model is required", "model")
		return
	}
	if len(req.Messages) == 0 {
		writeOpenAIError(w, r, http.StatusBadRequest, "missing_messages", "invalid_request_error", "messages is required", "messages")
		return
	}

	apiKey := APIKeyFromContext(r.Context())
	tenantID := ""
	if h.policySync != nil {
		tenantID = strings.TrimSpace(h.policySync.ResolveTenantByKeyHash(apiKey))
	}
	if h.requireTenantBoundKey {
		if h.policySync == nil {
			writeOpenAIError(w, r, http.StatusServiceUnavailable, "policy_sync_unavailable", "server_error", "policy sync unavailable", "service")
			return
		}
		if tenantID == "" {
			writeOpenAIError(w, r, http.StatusForbidden, "tenant_binding_required", "permission_error", "api key is not bound to tenant", "tenant_id")
			return
		}
	}

	if h.limiter != nil {
		tokenDecision := h.limiter.AllowTokenUsage(apiKey)
		if !tokenDecision.Allowed {
			writeOpenAIError(w, r, http.StatusTooManyRequests, tokenDecision.Reason, "rate_limit_error", "daily token limit exceeded", "token_limit")
			return
		}
		if tokenDecision.Limit > 0 {
			w.Header().Set("X-TokenLimit-Daily", fmt.Sprintf("%d", tokenDecision.Limit))
			w.Header().Set("X-TokenLimit-Remaining-Daily", fmt.Sprintf("%d", tokenDecision.Remaining))
		}
	}

	providerReq := provider.ChatRequest{
		Model:    req.Model,
		Messages: toProviderMessages(req.Messages),
	}
	result, err := h.gateway.Chat(r.Context(), providerReq)
	if err != nil {
		h.logger.Error("provider chat failed", "err", err, "model", req.Model, "request_id", RequestIDFromContext(r.Context()))
		writeOpenAIError(w, r, http.StatusBadGateway, "upstream_error", "server_error", "upstream provider request failed", "provider")
		return
	}

	reply := strings.TrimSpace(result.Content)
	if reply == "" {
		reply = ""
	}

	resModel := result.Model
	if strings.TrimSpace(resModel) == "" {
		resModel = req.Model
	}

	id := "chatcmpl-" + shortID()
	created := time.Now().Unix()
	usageVal := usage{
		PromptTokens:     result.PromptTokens,
		CompletionTokens: result.CompletionTokens,
		TotalTokens:      result.PromptTokens + result.CompletionTokens,
	}
	if h.limiter != nil && usageVal.TotalTokens > 0 {
		h.limiter.AddTokenUsage(apiKey, usageVal.TotalTokens)
	}

	if err := h.applyUsageBilling(r, tenantID, usageVal); err != nil {
		writeInferenceBillingError(w, r, err)
		return
	}

	if req.Stream {
		h.streamChatCompletion(w, r, id, created, resModel, reply)
		return
	}

	resp := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resModel,
		Choices: []chatCompletionChoice{
			{
				Index: 0,
				Message: chatCompletionMessage{
					Role:    "assistant",
					Content: reply,
				},
				FinishReason: "stop",
			},
		},
		Usage: usageVal,
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) streamChatCompletion(w http.ResponseWriter, r *http.Request, id string, created int64, model string, reply string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeOpenAIError(w, r, http.StatusInternalServerError, "streaming_not_supported", "server_error", "streaming not supported", "stream")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	parts := splitForStreaming(reply)
	for i, part := range parts {
		choice := chunkChoice{Index: 0, Delta: chunkDelta{Content: part}, FinishReason: nil}
		if i == 0 {
			choice.Delta.Role = "assistant"
		}

		chunk := chatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []chunkChoice{choice},
		}

		if err := writeSSEData(w, chunk); err != nil {
			return
		}
		flusher.Flush()
	}

	finish := "stop"
	finalChunk := chatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []chunkChoice{{
			Index:        0,
			Delta:        chunkDelta{},
			FinishReason: &finish,
		}},
	}
	if err := writeSSEData(w, finalChunk); err != nil {
		return
	}
	flusher.Flush()

	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (h *handler) models(w http.ResponseWriter, r *http.Request) {
	models := h.gateway.Models(r.Context())
	if len(models) == 0 {
		models = []provider.ModelInfo{{ID: "stub/opcapi-mvp", OwnedBy: "opcapi"}}
	}

	items := make([]map[string]any, 0, len(models))
	now := time.Now().Unix()
	for _, m := range models {
		items = append(items, map[string]any{
			"id":       m.ID,
			"object":   "model",
			"created":  now,
			"owned_by": m.OwnedBy,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   items,
	})
}

func writeSSEData(w http.ResponseWriter, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err
}

func splitForStreaming(s string) []string {
	if len(s) <= 24 {
		return []string{s}
	}
	parts := make([]string, 0, (len(s)/24)+1)
	for i := 0; i < len(s); i += 24 {
		end := i + 24
		if end > len(s) {
			end = len(s)
		}
		parts = append(parts, s[i:end])
	}
	return parts
}

func shortID() string {
	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	if err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func toProviderMessages(messages []chatMessage) []provider.Message {
	out := make([]provider.Message, 0, len(messages))
	for _, m := range messages {
		out = append(out, provider.Message{
			Role:    strings.TrimSpace(m.Role),
			Content: normalizeMessageContent(m.Content),
		})
	}
	return out
}

func normalizeMessageContent(content any) string {
	s, ok := content.(string)
	if ok {
		return s
	}
	b, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(b)
}

func writeOpenAIError(w http.ResponseWriter, r *http.Request, status int, code, errType, msg, param string) {
	language, localized := localizeMessage(r, code, msg)
	w.Header().Set("Content-Language", language)
	writeJSON(w, status, openAIErrorEnvelope{
		Error: openAIError{
			Message: localized,
			Type:    errType,
			Param:   param,
			Code:    code,
		},
	})
}

func (h *handler) applyUsageBilling(r *http.Request, tenantID string, usageVal usage) error {
	if h == nil || h.billing == nil || strings.TrimSpace(tenantID) == "" || usageVal.TotalTokens <= 0 {
		return nil
	}

	_, _, err := h.billing.ChargeUsage(r.Context(), tenantID, usageVal.PromptTokens, usageVal.CompletionTokens)
	if err != nil && h.logger != nil {
		h.logger.Warn("billing usage charge failed",
			"tenant_id", tenantID,
			"prompt_tokens", usageVal.PromptTokens,
			"completion_tokens", usageVal.CompletionTokens,
			"err", err,
			"request_id", RequestIDFromContext(r.Context()),
		)
	}
	return err
}

func writeInferenceBillingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, billing.ErrInsufficientBalance):
		writeOpenAIError(w, r, http.StatusPaymentRequired, "insufficient_balance", "billing_error", "insufficient balance", "balance")
	case errors.Is(err, billing.ErrInvalidUsage), errors.Is(err, billing.ErrInvalidCurrency), errors.Is(err, billing.ErrInvalidAmount):
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_billing_request", "invalid_request_error", err.Error(), "billing")
	case errors.Is(err, billing.ErrStoreUnavailable):
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "billing_unavailable", "server_error", "billing service unavailable", "service")
	default:
		writeOpenAIError(w, r, http.StatusInternalServerError, "internal_error", "server_error", "internal server error", "")
	}
}
