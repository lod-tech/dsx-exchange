// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Command set-load-target acts as a DSX Flex ISV (independent software vendor):
// it obtains an OAuth2 token, connects over MQTT, and publishes a
// grid.loadtarget.set.v1 command that caps the mock AI factory's power draw. This
// is the "internal system" that controls power usage on the event bus.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/flex"
)

func main() {
	broker := flag.String("broker", "tcp://127.0.0.1:1883", "MQTT broker URL (port-forward csc-event-bus svc/nats 1883)")
	idpURL := flag.String("idp", "http://127.0.0.1:5556", "IdP base URL (port-forward idp svc/event-bus 5556); no trailing slash")
	clientID := flag.String("client-id", "grid-isv", "OAuth2 client ID")
	clientSecret := flag.String("client-secret", "grid-isv-secret", "OAuth2 client secret")
	scope := flag.String("scope", "mqtt", "OAuth2 scope")
	isvID := flag.String("isv-id", "acme-energy", "ISV identifier (topic + CloudEvents source)")
	targetMW := flag.Float64("target-mw", 20, "power cap in megawatts")
	feedTag := flag.String("feed-tag", "ai-factory-main", "feed tag the target applies to (empty = all feeds)")
	duration := flag.Duration("duration", 0, "how long the cap stays in effect (0 = until replaced)")
	clear := flag.Bool("clear", false, "remove any active constraint instead of setting one")
	bestEffort := flag.Bool("best-effort", true, "use the best_effort shedding strategy")
	flag.Parse()

	if strings.HasSuffix(*idpURL, "/") {
		log.Fatalf("set-load-target: --idp must not end with /")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Printf("set-load-target: requesting OAuth2 token from %s/token as %q", *idpURL, *clientID)
	token, err := fetchToken(ctx, *idpURL, *clientID, *clientSecret, *scope)
	if err != nil {
		log.Fatalf("set-load-target: %v", err)
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
		log.Fatalf("set-load-target: connect failed: %v", tok.Error())
	} else if tok.Error() != nil {
		log.Fatalf("set-load-target: connect failed: %v", tok.Error())
	}
	defer client.Disconnect(500)

	// Build the load target request.
	interval := flex.Interval{StartTime: time.Now().UTC().Format(time.RFC3339)}
	if *duration > 0 {
		interval.EndTime = time.Now().Add(*duration).UTC().Format(time.RFC3339)
	}
	var constraint *flex.LoadConstraint
	if !*clear {
		constraint = &flex.LoadConstraint{Value: *targetMW, Unit: flex.UnitMegawatt}
	}
	var feedTags []string
	if *feedTag != "" {
		feedTags = []string{*feedTag}
	}
	payload := flex.SetLoadTargetData{
		Targets: []flex.LoadTargetRequest{{
			Interval:       interval,
			FeedTags:       feedTags,
			LoadConstraint: constraint,
			Strategy:       &flex.Strategy{BestEffort: *bestEffort},
		}},
	}

	correlationID := uuid.NewString()
	event, err := flex.NewEvent(flex.TypeLoadTargetSet, flex.ISVSource(*isvID), "loadtarget", correlationID, payload)
	if err != nil {
		log.Fatalf("set-load-target: build event: %v", err)
	}
	b, err := event.Marshal()
	if err != nil {
		log.Fatalf("set-load-target: marshal event: %v", err)
	}

	topic := fmt.Sprintf("grid/v1/isv/%s/loadtarget/set", *isvID)
	if tok := client.Publish(topic, 1, false, b); tok.WaitTimeout(10*time.Second) && tok.Error() != nil {
		log.Fatalf("set-load-target: publish failed: %v", tok.Error())
	} else if tok.Error() != nil {
		log.Fatalf("set-load-target: publish failed: %v", tok.Error())
	}

	if *clear {
		log.Printf("set-load-target: published constraint removal to %s (correlation=%s)", topic, correlationID)
	} else {
		log.Printf("set-load-target: published %g MW cap to %s (feed=%q, correlation=%s)", *targetMW, topic, *feedTag, correlationID)
	}
}

func fetchToken(ctx context.Context, idpURL, clientID, clientSecret, scope string) (string, error) {
	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     idpURL + "/token",
		Scopes:       []string{scope},
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	tok, err := cfg.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("obtain OAuth2 token: %w", err)
	}
	return tok.AccessToken, nil
}
