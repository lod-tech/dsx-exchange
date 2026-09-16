// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Command loadgen drives synthetic load against the mock inference endpoint at a
// target request rate. It demonstrates how offered load raises the workload's
// power draw on the dashboard, and how the DSX Flex load target throttles it.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	url := flag.String("url", "http://127.0.0.1:8081/v1/chat/completions", "inference chat completions URL")
	rps := flag.Float64("rps", 20, "target requests per second")
	maxTokens := flag.Int("max-tokens", 64, "max_tokens per request (controls how long each request holds power)")
	duration := flag.Duration("duration", 0, "run duration (0 = until interrupted)")
	model := flag.String("model", "dsx-mock-llm", "model name")
	flag.Parse()

	if *rps <= 0 {
		log.Fatalf("loadgen: --rps must be positive")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	client := &http.Client{Timeout: 30 * time.Second}
	body, _ := json.Marshal(map[string]any{
		"model":      *model,
		"messages":   []map[string]string{{"role": "user", "content": "Summarize AI factory power management."}},
		"max_tokens": *maxTokens,
	})

	var accepted, shed, failed atomic.Uint64
	var wg sync.WaitGroup

	interval := time.Duration(float64(time.Second) / *rps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("loadgen: sending %.1f req/s to %s (max_tokens=%d)", *rps, *url, *maxTokens)
	reportTicker := time.NewTicker(2 * time.Second)
	defer reportTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			log.Printf("loadgen: done. accepted=%d shed=%d failed=%d", accepted.Load(), shed.Load(), failed.Load())
			return
		case <-reportTicker.C:
			log.Printf("loadgen: accepted=%d shed=%d failed=%d", accepted.Load(), shed.Load(), failed.Load())
		case <-ticker.C:
			wg.Add(1)
			go func() {
				defer wg.Done()
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, *url, bytes.NewReader(body))
				if err != nil {
					failed.Add(1)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					failed.Add(1)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				switch resp.StatusCode {
				case http.StatusOK:
					accepted.Add(1)
				case http.StatusTooManyRequests:
					shed.Add(1)
				default:
					failed.Add(1)
				}
			}()
		}
	}
}
