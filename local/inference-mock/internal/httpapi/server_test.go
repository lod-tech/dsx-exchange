// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/openai"
	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/power"
)

func TestModelsEndpoint(t *testing.T) {
	engine := power.New(1, 10, 0, 10, 0, 10*time.Millisecond)
	srv := New(engine, "dsx-mock-llm", 0, 40, 8)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var list openai.ModelList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != "dsx-mock-llm" {
		t.Errorf("unexpected models: %+v", list.Data)
	}
}

func TestChatCompletionShedWhenCapZero(t *testing.T) {
	// default cap 0 MW => no admission => every request is shed.
	engine := power.New(1, 1, 0, 0, 0, 10*time.Millisecond)
	srv := New(engine, "dsx-mock-llm", 0, 40, 8)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"dsx-mock-llm","messages":[{"role":"user","content":"hi"}]}`))
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on shed")
	}
	var apiErr openai.APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if apiErr.Error.Code != "power_shed" {
		t.Errorf("error code = %q, want power_shed", apiErr.Error.Code)
	}
}

func TestChatCompletionServed(t *testing.T) {
	engine := power.New(1, 10, 0, 10, 0, 10*time.Millisecond)
	srv := New(engine, "dsx-mock-llm", 0, 1000, 4)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],"max_tokens":4}`))
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp openai.ChatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Object != "chat.completion" || len(resp.Choices) != 1 {
		t.Errorf("unexpected response: %+v", resp)
	}
	if resp.Usage.CompletionTokens != 4 {
		t.Errorf("completion tokens = %d, want 4", resp.Usage.CompletionTokens)
	}
}
