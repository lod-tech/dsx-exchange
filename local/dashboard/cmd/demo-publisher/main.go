// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Command demo-publisher connects to the DSX Event Bus as an OAuth2-authorized
// client and publishes sample events. It demonstrates the exact path an external
// application uses: fetch an OAuth2 token, connect over MQTT with username
// "oauthtoken", and publish to an authorized subject. The published events
// appear live on the DSX Live Dashboard.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

func main() {
	broker := flag.String("broker", "tcp://127.0.0.1:1883", "MQTT broker URL (port-forward csc-event-bus svc/nats 1883)")
	idpURL := flag.String("idp", "http://127.0.0.1:5556", "IdP base URL (port-forward idp svc/event-bus 5556); no trailing slash")
	clientID := flag.String("client-id", "demo-app", "OAuth2 client ID")
	clientSecret := flag.String("client-secret", "demo-app-secret", "OAuth2 client secret")
	scope := flag.String("scope", "mqtt", "OAuth2 scope")
	topic := flag.String("topic", "demo/events/temperature", "MQTT topic to publish to")
	count := flag.Int("count", 0, "number of messages to publish (0 = run until interrupted)")
	interval := flag.Duration("interval", time.Second, "delay between messages")
	flag.Parse()

	if strings.HasSuffix(*idpURL, "/") {
		log.Fatalf("demo-publisher: --idp must not end with /")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("demo-publisher: requesting OAuth2 token from %s/token as %q", *idpURL, *clientID)
	token, err := fetchToken(ctx, *idpURL, *clientID, *clientSecret, *scope)
	if err != nil {
		log.Fatalf("demo-publisher: %v", err)
	}

	opts := mqtt.NewClientOptions().
		AddBroker(*broker).
		SetClientID(fmt.Sprintf("%s-%d", *clientID, os.Getpid())).
		SetUsername("oauthtoken").
		SetPassword(token).
		SetConnectTimeout(10 * time.Second).
		SetAutoReconnect(false)

	client := mqtt.NewClient(opts)
	if tok := client.Connect(); tok.WaitTimeout(15*time.Second) && tok.Error() != nil {
		log.Fatalf("demo-publisher: connect failed: %v", tok.Error())
	} else if tok.Error() != nil {
		log.Fatalf("demo-publisher: connect failed: %v", tok.Error())
	}
	log.Printf("demo-publisher: connected to %s, publishing to %q", *broker, *topic)
	defer client.Disconnect(500)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	published := 0
	for {
		payload, _ := json.Marshal(map[string]any{
			"value":     20 + rand.Float64()*10, //nolint:gosec // demo data, not security-sensitive
			"timestamp": time.Now().UnixMilli(),
			"quality":   1,
			"source":    *clientID,
		})
		if tok := client.Publish(*topic, 0, false, payload); tok.WaitTimeout(5*time.Second) && tok.Error() != nil {
			log.Printf("demo-publisher: publish error: %v", tok.Error())
		} else {
			published++
			log.Printf("demo-publisher: published #%d to %s: %s", published, *topic, payload)
		}

		if *count > 0 && published >= *count {
			log.Printf("demo-publisher: published %d messages, exiting", published)
			return
		}
		select {
		case <-ctx.Done():
			log.Printf("demo-publisher: interrupted after %d messages", published)
			return
		case <-ticker.C:
		}
	}
}

func fetchToken(ctx context.Context, idpURL, clientID, clientSecret, scope string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     idpURL + "/token",
		Scopes:       []string{scope},
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	tok, err := cfg.Token(cctx)
	if err != nil {
		return "", fmt.Errorf("obtain OAuth2 token: %w", err)
	}
	return tok.AccessToken, nil
}
