// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package httpapi serves the OpenAI-compatible inference API. Each request
// reserves a power slot from the admission engine, simulates token generation
// with realistic latency, and releases the slot when complete. Requests that
// cannot be admitted within the queue-wait budget are shed with HTTP 429.
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/openai"
	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/power"
)

const maxTokensCap = 512

// Server holds the inference HTTP handlers.
type Server struct {
	engine           *power.Engine
	model            string
	prefillLatency   time.Duration
	tokensPerSec     float64
	defaultMaxTokens int
}

// New builds a Server.
func New(engine *power.Engine, model string, prefill time.Duration, tokensPerSec float64, defaultMaxTokens int) *Server {
	return &Server{
		engine:           engine,
		model:            model,
		prefillLatency:   prefill,
		tokensPerSec:     tokensPerSec,
		defaultMaxTokens: defaultMaxTokens,
	}
}

// Handler returns the HTTP mux for the inference API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/completions", s.handleCompletions)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/stats", s.handleStats)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	snap := s.engine.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"in_flight":        snap.InFlight,
		"queued":           snap.Queued,
		"allowed":          snap.Allowed,
		"power_mw":         snap.PowerMW,
		"effective_cap_mw": snap.EffectiveCapMW,
		"target_active":    snap.TargetActive,
		"target_mw":        snap.TargetMW,
		"compliant":        snap.Compliant,
		"accepted_total":   snap.AcceptedTotal,
		"shed_total":       snap.ShedTotal,
		"completed_total":  snap.CompletedTotal,
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", "use GET")
		return
	}
	writeJSON(w, http.StatusOK, openai.ModelList{
		Object: "list",
		Data: []openai.Model{
			{ID: s.model, Object: "model", Created: time.Now().Unix(), OwnedBy: "dsx-exchange"},
		},
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", "use POST")
		return
	}
	var req openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request_error", "bad_json", err.Error())
		return
	}

	release, shed := s.engine.Acquire(r.Context())
	if shed {
		s.writeShed(w)
		return
	}
	defer release()

	nTokens := s.resolveMaxTokens(req.MaxTokens)
	words := generateTokens(nTokens)
	model := firstNonEmpty(req.Model, s.model)
	id := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()

	if req.Stream {
		s.streamChat(w, r, id, model, created, words)
		return
	}

	// Non-streaming: simulate the full generation time, then respond.
	if !sleepCtx(r.Context(), s.prefillLatency+s.genDuration(len(words))) {
		return
	}
	content := strings.Join(words, " ")
	writeJSON(w, http.StatusOK, openai.ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []openai.ChatChoice{{
			Index:        0,
			Message:      openai.ChatMessage{Role: "assistant", Content: content},
			FinishReason: "stop",
		}},
		Usage: openai.Usage{PromptTokens: promptTokens(req.Messages), CompletionTokens: len(words), TotalTokens: promptTokens(req.Messages) + len(words)},
	})
}

func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, id, model string, created int64, words []string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "server_error", "no_flush", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Initial role delta after the prefill latency.
	if !sleepCtx(r.Context(), s.prefillLatency) {
		return
	}
	role := openai.ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
		Choices: []openai.ChatChunkChoice{{Index: 0, Delta: openai.ChatDelta{Role: "assistant"}}}}
	if !writeSSE(w, flusher, role) {
		return
	}

	tokenDelay := s.tokenDelay()
	for i, word := range words {
		if !sleepCtx(r.Context(), tokenDelay) {
			return
		}
		content := word
		if i < len(words)-1 {
			content += " "
		}
		chunk := openai.ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
			Choices: []openai.ChatChunkChoice{{Index: 0, Delta: openai.ChatDelta{Content: content}}}}
		if !writeSSE(w, flusher, chunk) {
			return
		}
	}

	stop := "stop"
	final := openai.ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
		Choices: []openai.ChatChunkChoice{{Index: 0, Delta: openai.ChatDelta{}, FinishReason: &stop}}}
	if !writeSSE(w, flusher, final) {
		return
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", "use POST")
		return
	}
	var req openai.CompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request_error", "bad_json", err.Error())
		return
	}

	release, shed := s.engine.Acquire(r.Context())
	if shed {
		s.writeShed(w)
		return
	}
	defer release()

	nTokens := s.resolveMaxTokens(req.MaxTokens)
	words := generateTokens(nTokens)
	model := firstNonEmpty(req.Model, s.model)

	if !sleepCtx(r.Context(), s.prefillLatency+s.genDuration(len(words))) {
		return
	}
	writeJSON(w, http.StatusOK, openai.CompletionResponse{
		ID:      "cmpl-" + uuid.NewString(),
		Object:  "text_completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openai.CompletionChoice{{Text: strings.Join(words, " "), Index: 0, FinishReason: "stop"}},
		Usage:   openai.Usage{PromptTokens: len(strings.Fields(req.Prompt)), CompletionTokens: len(words), TotalTokens: len(strings.Fields(req.Prompt)) + len(words)},
	})
}

func (s *Server) resolveMaxTokens(req *int) int {
	n := s.defaultMaxTokens
	if req != nil && *req > 0 {
		n = *req
	}
	if n > maxTokensCap {
		n = maxTokensCap
	}
	if n < 1 {
		n = 1
	}
	return n
}

func (s *Server) tokenDelay() time.Duration {
	return time.Duration(float64(time.Second) / s.tokensPerSec)
}

func (s *Server) genDuration(n int) time.Duration {
	return time.Duration(float64(n) * float64(time.Second) / s.tokensPerSec)
}

func (s *Server) writeShed(w http.ResponseWriter) {
	snap := s.engine.Snapshot()
	w.Header().Set("Retry-After", "1")
	w.Header().Set("X-DSX-Power-MW", strconv.FormatFloat(snap.PowerMW, 'f', 2, 64))
	w.Header().Set("X-DSX-Target-MW", strconv.FormatFloat(snap.EffectiveCapMW, 'f', 2, 64))
	s.writeError(w, http.StatusTooManyRequests, "capacity_exceeded", "power_shed",
		fmt.Sprintf("request shed to hold power under the %.1f MW target (best_effort)", snap.EffectiveCapMW))
}

func (s *Server) writeError(w http.ResponseWriter, status int, typ, code, msg string) {
	writeJSON(w, status, openai.APIError{Error: openai.APIErrorBody{Message: msg, Type: typ, Code: code}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func promptTokens(messages []openai.ChatMessage) int {
	n := 0
	for _, m := range messages {
		n += len(strings.Fields(m.Content))
	}
	return n
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
